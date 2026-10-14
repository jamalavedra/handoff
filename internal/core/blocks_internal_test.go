package core

import (
	"net/url"
	"testing"
)

func TestImageFromHTML(t *testing.T) {
	page, _ := url.Parse("https://shop.example/costumes/dolly")
	cases := map[string]string{
		`<meta property="og:image" content="/img/dolly.jpg"><meta name="twitter:image" content="https://x.example/t.jpg">`: "https://shop.example/img/dolly.jpg",
		`<meta content='https://cdn.example/a.png?w=1&amp;h=2' property='og:image:secure_url'>`:                            "https://cdn.example/a.png?w=1&h=2",
		`<meta name="twitter:image" content="https://x.example/t.jpg">`:                                                    "https://x.example/t.jpg",
		`<meta property="og:image" content="http://insecure.example/a.jpg">`:                                               "",
		`<title>no picture</title>`: "",
	}
	for html, want := range cases {
		if got := imageFromHTML(html, page); got != want {
			t.Errorf("%s: got %q, want %q", html, got, want)
		}
	}
}
