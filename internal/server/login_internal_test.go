package server

import "testing"

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/":                   "/",
		"/tasks/acme-1?x=1#c": "/tasks/acme-1?x=1#c",
		"":                    "/",
		"tasks":               "/",
		"https://evil.com":    "/",
		"javascript:alert(1)": "/",
		"//evil.com":          "/",
		`/\evil.com`:          "/",
		"/\t/evil.com":        "/",
		"/\n/evil.com":        "/",
		"/\r/evil.com":        "/",
		"/\x00/evil.com":      "/",
		"/\x7f/evil.com":      "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
