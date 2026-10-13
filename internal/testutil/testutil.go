// Package testutil gives each test a fresh database on HANDOFF_TEST_DATABASE_URL.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/db"
)

var n atomic.Int64

func DB(t *testing.T) *db.DB {
	t.Helper()
	admin := os.Getenv("HANDOFF_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("HANDOFF_TEST_DATABASE_URL not set (see README: docker compose up -d db)")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	name := fmt.Sprintf("handoff_t%d_%d_%d", os.Getpid(), time.Now().UnixNano()%1e6, n.Add(1))
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	d, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Close()
		c, err := pgx.Connect(ctx, admin)
		if err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			c.Close(ctx)
		}
	})
	return d
}
