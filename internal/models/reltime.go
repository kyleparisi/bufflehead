package models

import (
	"fmt"
	"time"
)

// RelativeTime renders t as a short, human relative string against now
// (e.g. "just now", "5m ago", "3h ago", "2d ago"). Anything older than a week
// falls back to an absolute "Jan 2" date. Times in the future read "just now".
func RelativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
