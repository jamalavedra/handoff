// Package db owns the Postgres connection, migrations and per-org transactions.
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// AppRole is the unprivileged role the server switches to when it logs in as a
// superuser, so row-level security is never bypassed.
const AppRole = "handoff_app"

type DB struct {
	Pool *pgxpool.Pool
}

// Open migrates the schema and returns a pool whose connections are subject to RLS.
func Open(ctx context.Context, url string) (*DB, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)
	if err := migrate(ctx, conn); err != nil {
		return nil, err
	}
	var bypass bool
	if err := conn.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		return nil, fmt.Errorf("check role: %w", err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	if bypass {
		if err := ensureAppRole(ctx, conn); err != nil {
			return nil, err
		}
		cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
			_, err := c.Exec(ctx, "SET ROLE "+AppRole)
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	d := &DB{Pool: pool}
	if err := d.checkIsolation(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() { d.Pool.Close() }

func migrate(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrations table: %w", err)
	}
	// Serialize concurrent starts.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(72311)`); err != nil {
		return err
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(72311)`) //nolint:errcheck
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		v, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", e.Name())
		}
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, v).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, v); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func ensureAppRole(ctx context.Context, conn *pgx.Conn) error {
	stmts := []string{
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '` + AppRole + `') THEN
				CREATE ROLE ` + AppRole + ` NOLOGIN NOSUPERUSER NOBYPASSRLS;
			END IF;
		END $$`,
		`GRANT USAGE ON SCHEMA public TO ` + AppRole,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ` + AppRole,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + AppRole,
		`REVOKE UPDATE, DELETE, TRUNCATE ON events, schema_migrations FROM ` + AppRole,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			var pg *pgconn.PgError
			// Concurrent CREATE ROLE from another process.
			if errors.As(err, &pg) && pg.Code == "23505" {
				continue
			}
			return fmt.Errorf("app role: %w", err)
		}
	}
	return nil
}

// checkIsolation refuses to start if the pool could read org rows without an org set.
func (d *DB) checkIsolation(ctx context.Context) error {
	var bypass bool
	if err := d.Pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		return err
	}
	if bypass {
		return errors.New("database role bypasses row-level security; refusing to start")
	}
	return nil
}

// InOrg runs fn in a transaction scoped to one org by row-level security.
func (d *DB) InOrg(ctx context.Context, orgID string, fn func(pgx.Tx) error) error {
	if orgID == "" {
		return errors.New("InOrg: empty org id")
	}
	return pgx.BeginFunc(ctx, d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.org_id', $1, true)`, orgID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// IsUniqueViolation reports whether err is a unique-constraint violation.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
