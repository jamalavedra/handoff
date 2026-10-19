package core

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// AgeAppTools makes a person's listed app tools look older, for tests of the refresh window.
func AgeAppTools(t *testing.T, s *Service, p *Principal, by time.Duration) {
	t.Helper()
	err := s.DB.InOrg(context.Background(), p.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE app_sessions SET updated_at = updated_at - make_interval(secs => $2) WHERE member_id::text = $1`, p.Member.ID, by.Seconds())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
