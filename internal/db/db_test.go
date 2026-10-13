package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/joalavedra/handoff/internal/db"
)

func TestOpenMigratesAndDropsPrivileges(t *testing.T) {
	url := os.Getenv("HANDOFF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HANDOFF_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Opening twice must be a no-op for migrations.
	d2, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
	var bypass bool
	if err := d.Pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil {
		t.Fatal(err)
	}
	if bypass {
		t.Fatal("pool role bypasses RLS")
	}
	var n int
	if err := d.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("tasks visible without app.org_id: %d", n)
	}
}
