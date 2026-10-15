package config

import (
	"strings"
	"testing"
)

func TestSecretRequiredOutsideDev(t *testing.T) {
	t.Setenv("HANDOFF_BASE_URL", "https://tasks.example.com")
	t.Setenv("DATABASE_URL", "postgres://localhost/handoff")
	for _, c := range []struct {
		dev, secret string
		ok          bool
	}{
		{"", "", false},
		{"", "too-short", false},
		{"", strings.Repeat("a", 32), true},
		{"1", "", true},
		{"1", "short-is-fine-in-dev", true},
	} {
		t.Setenv("HANDOFF_DEV", c.dev)
		t.Setenv("HANDOFF_SECRET", c.secret)
		cfg, err := Load()
		if (err == nil) != c.ok {
			t.Errorf("dev=%q secret=%q: err=%v", c.dev, c.secret, err)
			continue
		}
		if err == nil && len(cfg.Secret) == 0 {
			t.Errorf("dev=%q secret=%q: empty secret", c.dev, c.secret)
		}
	}
}
