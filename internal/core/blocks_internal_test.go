package core

import (
	"net/url"
	"testing"

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
