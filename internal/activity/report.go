package activity

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/integration"
	"github.com/auth-com-br/tend/internal/usage"
)

// What a report on the record is made of, kept here rather than in the
// command that prints it: `tend activity` and the desktop's spending screen
// read the same runs the same way, so the window's "today" is the
// command's `-since today` to the cent.

// Load is the runs a report is about: the record at path folded into runs,
// those the filter keeps, with what the running ones have spent so far
// measured from their conversations and every run priced.
func Load(path string, f Filter, find Transcripts, prices map[string]usage.Price) ([]*Run, error) {
	recs, err := Read(path)
	if err != nil {
		return nil, err
	}
	all, _ := Fold(recs)
	var runs []*Run
	for _, r := range all {
		if f.Keep(r) {
			runs = append(runs, r)
		}
	}
	Measure(runs, find)
	Price(runs, prices)
	return runs, nil
}

// DefaultTranscripts finds a conversation's file where the agents keep them
// on this machine.
func DefaultTranscripts(agentLabel string, c Conversation) string {
	switch agentLabel {
	case "claude":
		if dir, err := integration.ClaudeDir(); err == nil {
			return usage.FindClaude(dir, c.ID)
		}
	case "codex":
		if dir, err := integration.CodexDir(); err == nil {
			return usage.FindCodex(dir, c.ID)
		}
	}
	return ""
}

// ParseSince reads a report's start: today, week, a number of days or
// hours back, or a date.
func ParseSince(s string, now time.Time) (time.Time, error) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today":
		return midnight, nil
	case "week":
		// Since Monday: the week a person means by "this week".
		return midnight.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7)), nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if days, err := strconv.Atoi(n); err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("-since is today, week, 7d, 12h or a date as 2006-01-02, not %q", s)
}
