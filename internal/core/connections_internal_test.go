package core

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestNoURL(t *testing.T) {
	inner := &url.Error{Op: "Get", URL: "https://a.test/mcp?key=SECRET", Err: errors.New("connection refused")}
	err := &url.Error{Op: "Post", URL: "https://b.test/token?t=TOKEN", Err: fmt.Errorf("refresh: %w", inner)}
	if s := noURL(err); strings.Contains(s, "SECRET") || strings.Contains(s, "TOKEN") || !strings.Contains(s, "connection refused") {
		t.Fatal(s)
	}
}
