package models

import (
	"testing"
	"time"
)

func TestRelativeTime(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		delta time.Duration
		want  string
	}{
		{0, "just now"},
		{30 * time.Second, "just now"},
		{-5 * time.Minute, "just now"}, // future
		{5 * time.Minute, "5m ago"},
		{59 * time.Minute, "59m ago"},
		{2 * time.Hour, "2h ago"},
		{25 * time.Hour, "1d ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, c := range cases {
		got := RelativeTime(now.Add(-c.delta), now)
		if got != c.want {
			t.Errorf("RelativeTime(-%s) = %q, want %q", c.delta, got, c.want)
		}
	}
	// Older than a week → absolute date.
	if got := RelativeTime(now.Add(-10*24*time.Hour), now); got != "Jun 29" {
		t.Errorf("old date = %q, want %q", got, "Jun 29")
	}
}
