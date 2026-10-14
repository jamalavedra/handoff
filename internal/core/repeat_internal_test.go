package core

import (
	"testing"
	"time"
)

func TestNextDue(t *testing.T) {
	day := func(s string) *time.Time {
		d, _ := time.Parse("2006-01-02", s)
		return &d
	}
	today := *day("2026-10-14") // a Wednesday
	for _, c := range []struct {
		repeat string
		due    *time.Time
		want   string
	}{
		{"daily", day("2026-10-14"), "2026-10-15"},
		{"weekly", day("2026-10-16"), "2026-10-23"},
		{"biweekly", day("2026-10-16"), "2026-10-30"},
		{"weekdays", day("2026-10-16"), "2026-10-19"}, // Friday -> Monday
		{"monthly", day("2026-10-31"), "2026-12-01"},  // Go normalises Nov 31
		{"yearly", day("2026-10-14"), "2027-10-14"},
		{"weekly", day("2026-09-01"), "2026-10-20"}, // missed weeks are skipped, the weekday kept
		{"daily", nil, "2026-10-15"},                // no due date: counted from today
	} {
		if got := nextDue(c.repeat, c.due, today).Format("2006-01-02"); got != c.want {
			t.Errorf("%s from %v: got %s, want %s", c.repeat, c.due, got, c.want)
		}
	}
}
