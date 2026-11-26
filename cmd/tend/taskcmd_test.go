package main

import (
	"testing"
	"time"
)

// TestATaskTimeIsReadAsAPersonTypesIt: `tend task add -at` and `-due` take
// a date and time, a date, a time of day — the next one, so 09:00 typed in
// the evening is tomorrow morning — a duration from now, or RFC 3339, and
// refuse anything else rather than guess. If it regresses, an agent planned
// for nine starts at once, or a typo plans it for no time at all.
func TestATaskTimeIsReadAsAPersonTypesIt(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 11, 26, 20, 0, 0, 0, loc)
	for in, want := range map[string]time.Time{
		"2026-11-27 09:00":          time.Date(2026, 11, 27, 9, 0, 0, 0, loc),
		"2026-11-27T09:30":          time.Date(2026, 11, 27, 9, 30, 0, 0, loc),
		"2026-11-28":                time.Date(2026, 11, 28, 0, 0, 0, 0, loc),
		"09:00":                     time.Date(2026, 11, 27, 9, 0, 0, 0, loc),
		"21:15":                     time.Date(2026, 11, 26, 21, 15, 0, 0, loc),
		"+90m":                      now.Add(90 * time.Minute),
		"2026-11-27T09:00:00-03:00": time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC),
	} {
		got, err := parseWhen(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseWhen(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"tomorrow", "+soon", "27/11"} {
		if _, err := parseWhen(bad, now); err == nil {
			t.Errorf("parseWhen(%q) was taken", bad)
		}
	}
}
