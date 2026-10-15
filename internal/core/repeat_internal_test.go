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
		{"monthly", day("2026-10-31"), "2026-11-30"},  // clamped to the month's last day
		{"monthly", day("2026-01-31"), "2026-10-31"},  // the 31st again where the month has one
		{"yearly", day("2028-02-29"), "2029-02-28"},
		{"yearly", day("2026-10-14"), "2027-10-14"},
		{"weekly", day("2026-09-01"), "2026-10-20"}, // missed weeks are skipped, the weekday kept
		{"daily", nil, "2026-10-15"},                // no due date: counted from today
	} {
		if got := nextDue(c.repeat, c.due, today, 0).Format("2006-01-02"); got != c.want {
			t.Errorf("%s from %v: got %s, want %s", c.repeat, c.due, got, c.want)
		}
	}
}

func TestSingleEmoji(t *testing.T) {
	for _, ok := range []string{"👍", "✅", "👀", "❤️", "👍🏽", "👩‍💻", "👨‍👩‍👧", "🇪🇸", "1️⃣", "🏳️‍🌈", "©️"} {
		if !singleEmoji(ok) {
			t.Errorf("%q should be one emoji", ok)
		}
	}
	for _, bad := range []string{"", "done", "👍👀", "a👍", "🇪", "1", "👍 ", "‍👍", "🇪🇸🇫🇷"} {
		if singleEmoji(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestCloserToday(t *testing.T) {
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC) // Oct 14 evening in California
	for local, want := range map[string]string{"2026-10-14": "2026-10-14", "2026-10-16": "2026-10-16", "2026-10-01": "2026-10-15", "": "2026-10-15", "x": "2026-10-15"} {
		if got := closerToday(local, now).Format("2006-01-02"); got != want {
			t.Errorf("%q: got %s, want %s", local, got, want)
		}
	}
}

func TestSeriesDay(t *testing.T) {
	day := func(s string) *time.Time {
		d, _ := time.Parse("2006-01-02", s)
		return &d
	}
	for _, c := range []struct {
		stored int
		due    string
		want   int
	}{
		{0, "2026-10-31", 31},
		{31, "2026-11-30", 31}, // clamped in November, back to the 31st in December
		{31, "2026-11-15", 15}, // moved by hand
		{29, "2029-02-28", 29}, // leap day kept for 2032
	} {
		if got := seriesDay(c.stored, day(c.due)); got != c.want {
			t.Errorf("%d %s: got %d, want %d", c.stored, c.due, got, c.want)
		}
	}
	if got := nextDue("monthly", day("2026-11-30"), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), 31).Format("2006-01-02"); got != "2026-12-31" {
		t.Errorf("anchored monthly: %s", got)
	}
}
