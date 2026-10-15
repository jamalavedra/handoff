package auth_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/auth"
	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

func setup(t *testing.T) (*auth.Store, string) {
	t.Helper()
	d := testutil.DB(t)
	base, _ := url.Parse("http://handoff.test")
	org, _, err := core.New(d, &config.Config{BaseURL: base}).CreateOrg(context.Background(),
		core.NewOrg{Slug: "acme", Name: "Acme", AdminEmail: "admin@acme.test", AdminName: "Admin"})
	if err != nil {
		t.Fatal(err)
	}
	return &auth.Store{DB: d}, org.ID
}

// A sign-in link works once, only before it expires, and only as itself.
func TestLoginTokenSingleUse(t *testing.T) {
	s, org := setup(t)
	ctx := context.Background()
	tok, err := s.CreateLoginToken(ctx, org, "New@Acme.test", "New", "/team", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeLoginToken(ctx, org, tok+"x"); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("wrong token: %v", err)
	}
	lt, err := s.ConsumeLoginToken(ctx, org, tok)
	if err != nil || lt.Email != "new@acme.test" || lt.Next != "/team" {
		t.Fatalf("consume: %+v %v", lt, err)
	}
	if _, err := s.ConsumeLoginToken(ctx, org, tok); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("second use: %v", err)
	}
	old, _ := s.CreateLoginToken(ctx, org, "new@acme.test", "New", "/", true)
	if err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE login_tokens SET expires_at = now() - interval '1 second' WHERE used_at IS NULL`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeLoginToken(ctx, org, old); !errors.Is(err, auth.ErrInvalid) {
		t.Fatalf("expired link: %v", err)
	}
}

// Emailed links are capped per address; used links free a slot and Google sign-ins skip the cap.
func TestLoginTokenCap(t *testing.T) {
	s, org := setup(t)
	ctx := context.Background()
	var first string
	for i := range 5 {
		tok, err := s.CreateLoginToken(ctx, org, "a@acme.test", "A", "/", true)
		if err != nil {
			t.Fatalf("link %d: %v", i+1, err)
		}
		if i == 0 {
			first = tok
		}
	}
	if _, err := s.CreateLoginToken(ctx, org, "A@acme.test", "A", "/", true); !errors.Is(err, auth.ErrBusy) {
		t.Fatalf("sixth link: %v", err)
	}
	if _, err := s.ConsumeLoginToken(ctx, org, first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoginToken(ctx, org, "a@acme.test", "A", "/", true); err != nil {
		t.Fatalf("used link still counted: %v", err)
	}
	if _, err := s.CreateLoginToken(ctx, org, "a@acme.test", "A", "/", false); err != nil {
		t.Fatalf("google sign-in capped: %v", err)
	}
}
