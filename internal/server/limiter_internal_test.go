package server

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/login", nil)
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 1.2.3.4")
	if ip := clientIP(r); ip != "1.2.3.4" {
		t.Fatalf("trusted a forged address: %s", ip)
	}
	l := newLimiter(1, time.Minute)
	if !l.allow("a") || l.allow("a") {
		t.Fatal("limit not applied")
	}
	for i := range 10001 {
		l.allow(fmt.Sprint(i))
	}
	if l.allow("a") {
		t.Fatal("a flood of new keys reset the limit")
	}
}
