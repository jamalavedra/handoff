package core

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/config"
)

func TestImageFromHTML(t *testing.T) {
	page, _ := url.Parse("https://shop.example/costumes/dolly")
	cases := map[string]string{
		`<meta property="og:image" content="/img/dolly.jpg"><meta name="twitter:image" content="https://x.example/t.jpg">`:                             "https://shop.example/img/dolly.jpg",
		`<meta content='https://cdn.example/a.png?w=1&amp;h=2' property='og:image:secure_url'>`:                                                        "https://cdn.example/a.png?w=1&h=2",
		`<meta name="twitter:image" content="https://x.example/t.jpg">`:                                                                                "https://x.example/t.jpg",
		`<meta property="og:image" content="http://insecure.example/a.jpg">`:                                                                           "",
		`<meta property="og:image" content="http://insecure.example/a.jpg"><meta property="og:image:secure_url" content="https://safe.example/a.jpg">`: "https://safe.example/a.jpg",
		`<title>no picture</title>`: "",
	}
	for html, want := range cases {
		if got := imageFromHTML(html, page); got != want {
			t.Errorf("%s: got %q, want %q", html, got, want)
		}
	}
}

func TestImageProxySignature(t *testing.T) {
	s := &Service{Cfg: &config.Config{Secret: []byte("k")}}
	p, _ := url.Parse(s.imageProxy("https://cdn.example/a.jpg"))
	if p.Path != "/api/image" || p.Query().Get("u") != "https://cdn.example/a.jpg" || p.Query().Get("s") != s.imageSig("https://cdn.example/a.jpg") {
		t.Fatalf("proxy url: %s", p)
	}
	if _, _, err := s.ProxyImage(t.Context(), "https://cdn.example/other.jpg", p.Query().Get("s")); !IsCode(err, ErrNotFound) {
		t.Fatalf("unsigned address served: %v", err)
	}
}

func TestFoundPages(t *testing.T) {
	a := &assistantRun{turn: &assistantTurn{}}
	a.foundPages(`Results for "https://evil.test/?secret=1": see (https://en.wikipedia.org/wiki/Paris_(film)), https://shop.test/a.`,
		`{"query":"https://evil.test/?secret=1"}`)
	for u, want := range map[string]bool{
		"https://en.wikipedia.org/wiki/Paris_(film)": true,
		"https://shop.test/a":                        true,
		"https://evil.test/?secret=1":                false,
	} {
		if a.turn.found[u] != want {
			t.Errorf("found[%q] = %v, want %v (all: %v)", u, a.turn.found[u], want, a.turn.found)
		}
	}
}

func TestImageProxyFollowsRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			http.Redirect(w, r, "/b", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	}))
	defer srv.Close()
	s := &Service{Cfg: &config.Config{Secret: []byte("k")},
		OutboundHTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	src := srv.URL + "/a"
	if ct, b, err := s.ProxyImage(t.Context(), src, s.imageSig(src)); err != nil || ct != "image/png" || string(b) != "png" {
		t.Fatalf("redirected image: %q %q %v", ct, b, err)
	}
}

func TestParseReminder(t *testing.T) {
	now := time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)
	for at, want := range map[string]string{
		"2026-10-24":                "2026-10-24",
		"2026-10-24T09:30":          "2026-10-24T09:30",
		"2026-10-24T09:30:00-07:00": "2026-10-24T09:30",
		"2026-10-18":                "2026-10-18", // yesterday somewhere
		"2026-10-01":                "",
		"2040-01-01":                "",
		"next friday":               "",
		"2026-13-01":                "",
	} {
		got, err := parseReminder(reminderIn{Title: "Order", At: at}, now)
		if (err == nil) != (want != "") || got.At != want {
			t.Errorf("%s: got %q (%v), want %q", at, got.At, err, want)
		}
		if err == nil && got.AllDay != (len(want) == 10) {
			t.Errorf("%s: all_day %v", at, got.AllDay)
		}
	}
}

func TestProfileOf(t *testing.T) {
	for raw, want := range map[string][2]string{
		"https://www.linkedin.com/in/jane-doe/": {"linkedin", "jane-doe"},
		"https://uk.linkedin.com/in/jane":       {"linkedin", "jane"},
		"https://github.com/joalavedra":         {"github", "joalavedra"},
		"https://twitter.com/jack":              {"x", "@jack"},
		"https://x.com/jack/status/1":           {"x", "@jack"},
		"https://www.instagram.com/someone/":    {"instagram", "@someone"},
		"https://www.youtube.com/@creator":      {"youtube", "@creator"},
		"https://mastodon.social/@gargron":      {"mastodon", "@gargron@mastodon.social"},
		"https://janedoe.dev/about":             {"web", "janedoe.dev"},
	} {
		u, _ := url.Parse(raw)
		if n, h := profileOf(u); n != want[0] || h != want[1] {
			t.Errorf("%s: got %s %q, want %s %q", raw, n, h, want[0], want[1])
		}
	}
}

func TestRouteMode(t *testing.T) {
	for in, want := range map[string]string{"": "walk", "Walking": "walk", "cycling": "bike", "car": "drive", "boat": ""} {
		if got := routeMode(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
