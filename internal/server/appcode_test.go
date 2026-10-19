package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var appCodeRe = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)

// Issue → redeem → the token is a handoff_session. Second use, expiry,
// cross-org and superseded codes all fail the same way.
func TestAppCodePairing(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	globex := f.signup("globex")

	// Signed-out callers can't mint codes (a bare client holds no session cookie).
	req, _ := http.NewRequest("POST", acme+"/api/app/code", strings.NewReader("{}"))
	req.Header.Set("X-Handoff", "1")
	resp, err := (&http.Client{Transport: f.client.Transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("signed-out issue: %d", resp.StatusCode)
	}

	code, out := f.do("POST", acme+"/api/app/code", map[string]any{}, nil)
	if code != 200 {
		t.Fatalf("issue: %d %v", code, out)
	}
	raw := out["code"].(string)
	if !appCodeRe.MatchString(raw) {
		t.Fatalf("code shape: %q", raw)
	}
	if exp, _ := out["expires_at"].(float64); exp < float64(time.Now().Unix()+550) || exp > float64(time.Now().Unix()+650) {
		t.Fatalf("expires_at: %v", out["expires_at"])
	}

	// The phone redeems it: fresh jar, no session. Lowercase, no dash — same code.
	f.client.Jar, _ = cookiejar.New(nil)
	if code, _ := f.do("POST", globex+"/api/app/redeem", map[string]string{"code": raw}, nil); code != 400 {
		t.Fatalf("cross-org redeem: %d", code)
	}
	code, out = f.do("POST", acme+"/api/app/redeem", map[string]string{"code": strings.ToLower(strings.ReplaceAll(raw, "-", ""))}, nil)
	if code != 200 {
		t.Fatalf("redeem: %d %v", code, out)
	}
	tok, _ := out["token"].(string)
	m, _ := out["member"].(map[string]any)
	if tok == "" || m["email"] != "admin@acme.test" || m["id"] == "" {
		t.Fatalf("redeem body: %v", out)
	}
	// The Set-Cookie it sent signs this client in.
	if code, out := f.do("GET", acme+"/api/me", nil, nil); code != 200 || out["member"] == nil {
		t.Fatalf("cookie session: %d %v", code, out)
	}
	// And the raw token works as handoff_session, the way the app sends it.
	f.client.Jar, _ = cookiejar.New(nil)
	if code, out := f.do("GET", acme+"/api/me", nil, map[string]string{"Cookie": "handoff_session=" + tok}); code != 200 || out["member"] == nil {
		t.Fatalf("token session: %d %v", code, out)
	}
	// Single use.
	if code, _ := f.do("POST", acme+"/api/app/redeem", map[string]string{"code": raw}, nil); code != 400 {
		t.Fatalf("second redeem: %d", code)
	}
}

// A new code retires the person's unused ones.
func TestAppCodeNewInvalidatesOld(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	var codes []string
	for range 2 {
		code, out := f.do("POST", acme+"/api/app/code", map[string]any{}, nil)
		if code != 200 {
			t.Fatalf("issue: %d %v", code, out)
		}
		codes = append(codes, out["code"].(string))
	}
	f.client.Jar, _ = cookiejar.New(nil)
	if code, _ := f.do("POST", acme+"/api/app/redeem", map[string]string{"code": codes[0]}, nil); code != 400 {
		t.Fatalf("superseded code redeemed: %d", code)
	}
	if code, _ := f.do("POST", acme+"/api/app/redeem", map[string]string{"code": codes[1]}, nil); code != 200 {
		t.Fatalf("latest code: %d", code)
	}
}

// Expired and malformed codes get the same 400 as unknown ones.
func TestAppCodeExpired(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	_, out := f.do("POST", acme+"/api/app/code", map[string]any{}, nil)
	raw := out["code"].(string)
	org, err := f.svc.OrgBySlug(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DB.InOrg(context.Background(), org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE app_codes SET expires_at = now() - interval '1 second'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.client.Jar, _ = cookiejar.New(nil)
	for _, in := range []string{raw, "AAAA-AAAA", "junk", ""} {
		if code, _ := f.do("POST", acme+"/api/app/redeem", map[string]string{"code": in}, nil); code != 400 {
			t.Fatalf("redeem %q: %d", in, code)
		}
	}
}

// Redeem guesses are throttled per client and per workspace; issuing is throttled per member.
func TestAppCodeThrottled(t *testing.T) {
	f := setup(t)
	acme := f.signup("acme")
	globex := f.signup("globex")

	// Per IP: the same address gets 10 guesses a minute.
	for i := range 11 {
		code, _ := f.do("POST", acme+"/api/app/redeem", map[string]string{"code": "AAAA-AAAA"}, nil)
		if i < 10 && code != 400 {
			t.Fatalf("guess %d: %d", i, code)
		}
		if i == 10 && code != 429 {
			t.Fatalf("guess %d not throttled: %d", i, code)
		}
	}
	// Per workspace: rotating addresses still hit the org cap.
	for i := range 11 {
		code, _ := f.do("POST", globex+"/api/app/redeem", map[string]string{"code": "AAAA-AAAA"},
			map[string]string{"X-Forwarded-For": fmt.Sprintf("10.0.0.%d", i)})
		if i < 10 && code != 400 {
			t.Fatalf("org guess %d: %d", i, code)
		}
		if i == 10 && code != 429 {
			t.Fatalf("org guess %d not throttled: %d", i, code)
		}
	}
	// Issuing: the signed-in person gets a fresh code each time, but only so fast.
	for i := range 11 {
		code, _ := f.do("POST", acme+"/api/app/code", map[string]any{}, nil)
		if i < 10 && code != 200 {
			t.Fatalf("issue %d: %d", i, code)
		}
		if i == 10 && code != 429 {
			t.Fatalf("issue %d not throttled: %d", i, code)
		}
	}
}
