package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// TestFollowingAClaudeConversationReadsOnlyWhatWasAdded: the follower
// counts what the whole-file reader counts — a reply once, whatever its
// lines; subagents included — reading each time only what was appended, a
// line caught half-written finished on the next read; the context is the
// last reply's prompt, in the window of the model Claude Code says it is
// ("[1m]": a million tokens). If it regresses, the sidebar's tokens disagree
// with the record, or the context gauge says 80% of a window five times the
// size it thinks.
func TestFollowingAClaudeConversationReadsOnlyWhatWasAdded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.jsonl")
	write(t, path, `{"type":"attachment","attachment":{"type":"model","identity":{"modelId":"claude-opus-5-5[1m]"}}}`, claudeThinking, claudeTool, claudeUser)
	f := NewFollower("claude", path)
	l, err := f.Read()
	if err != nil {
		t.Fatal(err)
	}
	if l.Context != 6002 || l.Window != 1_000_000 || l.Model != "claude-opus-5-5" {
		t.Errorf("after one reply: context %d of %d, model %q", l.Context, l.Window, l.Model)
	}

	// The second reply arrives in two writes, the first ending mid-line.
	half := len(claudeSecond) / 2
	appendTo(t, path, claudeSecond[:half])
	if l, _ = f.Read(); l.Context != 6002 {
		t.Errorf("a half-written line was read: context %d", l.Context)
	}
	appendTo(t, path, claudeSecond[half:]+"\n"+claudeSynth+"\n")
	write(t, filepath.Join(dir, "abc", "subagents", "agent-1.jsonl"),
		`{"type":"assistant","message":{"id":"s1","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":7}}}`)
	l, _ = f.Read()
	whole, _ := Claude(path)
	if l.Usage.Total() != whole.Total() || len(l.Usage) != 2 {
		t.Errorf("followed %+v, whole file %+v", l.Usage, whole)
	}
	if l.Context != 6203 || l.ContextPercent() != 1 {
		t.Errorf("after the second reply: context %d (%d%%)", l.Context, l.ContextPercent())
	}

	// Rewritten shorter: read again from the start.
	write(t, path, claudeSecond)
	if l, _ = f.Read(); l.Usage["claude-opus-5-5"].Output != 40 {
		t.Errorf("after a rewrite: %+v", l.Usage)
	}
}

// TestAClaudeModelsWindowIsClaudeCodes: the window is the one Claude Code
// gives the model — a million for those that have it natively, whatever
// the id says, 200k for the older ones unless "[1m]" asks for more. Found
// with the real Claude Code, whose /context said 48.7k/1m for an Opus 5.5
// whose id carries no "[1m]", where tend said 24% of 200k.
func TestAClaudeModelsWindowIsClaudeCodes(t *testing.T) {
	for id, want := range map[string]int64{
		"claude-opus-5-5": 1_000_000, "claude-opus-5-5[1m]": 1_000_000, "claude-haiku-5-5": 1_000_000,
		"claude-opus-4-7": 1_000_000, "claude-sonnet-4-6": 200_000, "claude-sonnet-4-6[1m]": 1_000_000,
		"claude-haiku-4-5-20251001": 200_000, "claude-3-7-sonnet": 200_000,
	} {
		if got := claudeWindow(id); got != want {
			t.Errorf("%s: %d, want %d", id, got, want)
		}
	}
	path := filepath.Join(t.TempDir(), "x.jsonl")
	write(t, path, strings.ReplaceAll(claudeSecond, "claude-opus-5-5", "claude-sonnet-4-6"))
	l, _ := NewFollower("claude", path).Read()
	if l.Window != 200_000 || l.ContextPercent() != 3 {
		t.Errorf("with no model line: window %d, %d%%", l.Window, l.ContextPercent())
	}
}

// TestFollowingCodexReadsItsWindowAndItsLimit: Codex writes, with each
// running total, the last turn's tokens, its model's window and the
// provider's rate limit — how much is used and when it resets; the follower
// takes the last of each, and the gauge is Codex's own: the numbers here are
// a real turn's, which Codex's /status called "98% left (20.6K used /
// 475K)". If it regresses, the sidebar disagrees with the agent, or an
// agent about to be stopped by its limit gives no warning.
func TestFollowingCodexReadsItsWindowAndItsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-x.jsonl")
	write(t, path,
		`{"type":"turn_context","payload":{"model":"gpt-6"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":200,"output_tokens":20},"last_token_usage":{"input_tokens":20606,"output_tokens":5,"total_tokens":20611},"model_context_window":475000},"rate_limits":{"primary":{"used_percent":91.5,"window_minutes":300,"resets_at":1794787010},"rate_limit_reached_type":null}}}`)
	f := NewFollower("codex", path)
	l, _ := f.Read()
	if l.Usage["gpt-6"] != (Tokens{Input: 100, Output: 20, CacheRead: 200}) || l.Context != 20611 || l.ContextPercent() != 2 {
		t.Errorf("usage %+v, context %d (%d%%)", l.Usage, l.Context, l.ContextPercent())
	}
	if l.Limit == nil || l.Limit.UsedPercent != 91.5 || l.Limit.Window != 5*time.Hour || l.Limit.Resets.Unix() != 1794787010 || l.Limit.Reached {
		t.Fatalf("limit %+v", l.Limit)
	}
	appendTo(t, path, `{"type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":100,"window_minutes":300,"resets_at":1794787010},"rate_limit_reached_type":"rate_limit_reached"}}}`+"\n")
	if l, _ = f.Read(); !l.Limit.Reached || l.Context != 20611 {
		t.Errorf("reached: %+v, context %d", l.Limit, l.Context)
	}
}
