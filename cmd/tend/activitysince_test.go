package main

import (
	"testing"
	"time"
)

// TestSinceTodayAndThisWeekStartAtMidnight: `-since today` is from this
// morning's midnight and `-since week` from Monday's, in local time — what a
// person asking for the day's or the week's total means, which "1d" and
// "7d" (the last 24 hours, the last seven days) are not. If it regresses,
// the day's total includes last night.
func TestSinceTodayAndThisWeekStartAtMidnight(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 30, 0, 0, time.Local) // a Wednesday
	if got, _ := parseSince("today", now); !got.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, time.Local)) {
		t.Errorf("today = %v", got)
	}
	if got, _ := parseSince("week", now); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)) {
		t.Errorf("week = %v", got)
	}
	sunday := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	if got, _ := parseSince("week", sunday); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)) {
		t.Errorf("week on a Sunday = %v", got)
	}
	if got, _ := parseSince("7d", now); !got.Equal(now.AddDate(0, 0, -7)) {
		t.Errorf("7d = %v", got)
	}
}
