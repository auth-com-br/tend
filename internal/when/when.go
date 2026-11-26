// Package when reads a time the way a person types one, for a task's start
// or due time: the CLI's -at and -due, and tend-desktop's task form, take
// the same forms because they read them here.
package when

import (
	"fmt"
	"strings"
	"time"
)

// Parse reads a time the way a person types one: a date and a time, a
// date (its midnight), a time of day (the next one, so 09:00 typed at ten is
// tomorrow's), a duration from now (+30m), or RFC 3339. Local time
// throughout, which is the clock the person is reading.
func Parse(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "+"); ok {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return time.Time{}, fmt.Errorf("%q is not a duration (+30m, +2h)", s)
		}
		return now.Add(d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("15:04", s, now.Location()); err == nil {
		day := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if !day.After(now) {
			day = day.AddDate(0, 0, 1)
		}
		return day, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time: try 2026-11-27 09:00, 09:00 or +30m", s)
}
