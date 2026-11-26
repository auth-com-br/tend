package activity

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/usage"
)

// TestSinceTodayAndThisWeekStartAtMidnight: `-since today` is from this
// morning's midnight and `-since week` from Monday's, in local time — what a
// person asking for the day's or the week's total means, which "1d" and
// "7d" (the last 24 hours, the last seven days) are not. If it regresses,
// the day's total includes last night.
func TestSinceTodayAndThisWeekStartAtMidnight(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 30, 0, 0, time.Local) // a Wednesday
	if got, _ := ParseSince("today", now); !got.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, time.Local)) {
		t.Errorf("today = %v", got)
	}
	if got, _ := ParseSince("week", now); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)) {
		t.Errorf("week = %v", got)
	}
	sunday := time.Date(2026, 11, 15, 10, 0, 0, 0, time.Local)
	if got, _ := ParseSince("week", sunday); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)) {
		t.Errorf("week on a Sunday = %v", got)
	}
	if got, _ := ParseSince("7d", now); !got.Equal(now.AddDate(0, 0, -7)) {
		t.Errorf("7d = %v", got)
	}
	if _, err := ParseSince("yesterday", now); err == nil {
		t.Error("yesterday read as a start")
	}
}

// TestLoadKeepsTheRunsSinceAndPricesThem: Load is what `tend activity` and
// the desktop's spending screen both read, so a run from before the start is
// left out and the ones kept carry a cost. If it regresses, the window and
// the command disagree about what the day cost.
func TestLoadKeepsTheRunsSinceAndPricesThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	log := NewLog(path)
	spent := usage.Usage{"claude-opus-4-5": {Input: 1_000_000}}
	if err := log.Append(
		Record{Kind: KindStart, Time: t0.Add(-48 * time.Hour), Run: "old", Agent: "claude"},
		Record{Kind: KindStop, Time: t0.Add(-47 * time.Hour), Run: "old", Usage: spent},
		Record{Kind: KindStart, Time: t0, Run: "new", Agent: "claude", Companies: []string{"acme"}},
		Record{Kind: KindStop, Time: t0.Add(time.Hour), Run: "new", Usage: spent},
	); err != nil {
		t.Fatal(err)
	}
	runs, err := Load(path, Filter{Since: t0.Add(-time.Hour)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "new" || runs[0].Cost != 5 || !runs[0].CostComplete {
		t.Fatalf("runs = %+v", runs)
	}
}
