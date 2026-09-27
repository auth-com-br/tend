package usage

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// These lines are shaped as Claude Code writes them: one line per block of a
// reply, each with the reply's usage, and the last with the final count.
const (
	claudeThinking = `{"type":"assistant","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":5000,"output_tokens":10,"cache_creation":{"ephemeral_1h_input_tokens":1000,"ephemeral_5m_input_tokens":0}}}}`
	claudeTool     = `{"type":"assistant","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":1000,"cache_read_input_tokens":5000,"output_tokens":120,"cache_creation":{"ephemeral_1h_input_tokens":1000,"ephemeral_5m_input_tokens":0}}}}`
	claudeUser     = `{"type":"user","message":{"role":"user","content":"hello, with \"usage\" in it"}}`
	claudeSecond   = `{"type":"assistant","requestId":"req_2","message":{"id":"msg_2","model":"claude-opus-5-5","usage":{"input_tokens":3,"cache_creation_input_tokens":200,"cache_read_input_tokens":6000,"output_tokens":40}}}`
	claudeSynth    = `{"type":"assistant","message":{"id":"msg_3","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`
)

// TestAClaudeReplyIsCountedOnceWhateverItsLines: Claude Code writes a reply
// with a thought and a tool call as two lines carrying the same usage. Adding
// both would double what a conversation cost, and every cost in the report
// would be wrong by however many blocks the replies had.
func TestAClaudeReplyIsCountedOnceWhateverItsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conv.jsonl")
	write(t, path, claudeThinking, claudeTool, claudeUser, claudeSecond, claudeSynth, `not json`)
	u, err := Claude(path)
	if err != nil {
		t.Fatal(err)
	}
	got := u["claude-opus-5-5"]
	want := Tokens{Input: 5, Output: 160, CacheWrite: 1200, CacheWrite1h: 1000, CacheRead: 11000}
	if got != want {
		t.Errorf("tokens = %+v, want %+v", got, want)
	}
	if len(u) != 1 {
		t.Errorf("models = %v, want only claude-opus-5-5 (a synthetic reply is not billed)", u.Models())
	}
}

// TestAClaudeConversationIncludesItsSubagents: subagents are billed like the
// conversation that ran them, and Claude Code keeps them in a folder beside
// it. Leaving them out would report a run that fanned out as cheap.
func TestAClaudeConversationIncludesItsSubagents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.jsonl")
	write(t, path, claudeSecond)
	write(t, filepath.Join(dir, "abc", "subagents", "agent-1.jsonl"),
		`{"type":"assistant","message":{"id":"s1","model":"claude-haiku-4-5","usage":{"input_tokens":100,"output_tokens":7}}}`)
	u, err := Claude(path)
	if err != nil {
		t.Fatal(err)
	}
	if u["claude-haiku-4-5"].Output != 7 || u["claude-opus-5-5"].Output != 40 {
		t.Errorf("usage = %+v", u)
	}
}

// TestCodexIsItsLastRunningTotal: Codex writes the conversation's total after
// every turn, so summing them would count the first turn once per turn after.
// Its input includes the cached part, which is separated so the numbers mean
// what Claude's do and are priced alike.
func TestCodexIsItsLastRunningTotal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-2026-09-25T13-15-41-id.jsonl")
	write(t, path,
		`{"type":"turn_context","payload":{"model":"gpt-6"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":5}}}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":200,"output_tokens":20}}}}`,
	)
	u, err := Codex(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := u["gpt-6"]; got != (Tokens{Input: 100, Output: 20, CacheRead: 200}) {
		t.Errorf("tokens = %+v", got)
	}
}

// TestFindingATranscriptByItsID: the hooks name Claude and Codex
// conversations by id, and the files are named after it somewhere below the
// agents' own directories. An id is used in a file pattern, so one that is not
// an id finds nothing rather than whatever it matches.
func TestFindingATranscriptByItsID(t *testing.T) {
	claude := t.TempDir()
	want := filepath.Join(claude, "projects", "-home-me-app", "1234-abcd.jsonl")
	write(t, want, claudeSecond)
	if got := FindClaude(claude, "1234-abcd"); got != want {
		t.Errorf("FindClaude = %q, want %q", got, want)
	}
	if got := FindClaude(claude, "*"); got != "" {
		t.Errorf("FindClaude(*) = %q, want nothing", got)
	}

	codex := t.TempDir()
	want = filepath.Join(codex, "sessions", "2026", "09", "25", "rollout-2026-09-25T13-15-41-01a0-e052.jsonl")
	write(t, want, `{}`)
	if got := FindCodex(codex, "01a0-e052"); got != want {
		t.Errorf("FindCodex = %q, want %q", got, want)
	}
}

// TestUsageSubtractsWhatAConversationHadAlreadySpent: a run that resumes a
// conversation inherits its past, which was some earlier run's. What is left
// after taking the baseline away is what this run spent.
func TestUsageSubtractsWhatAConversationHadAlreadySpent(t *testing.T) {
	now := Usage{"m": {Input: 10, Output: 50, CacheRead: 100}, "n": {Output: 3}}
	base := Usage{"m": {Input: 4, Output: 50, CacheRead: 120}}
	got := now.Sub(base)
	if got["m"] != (Tokens{Input: 6}) || got["n"] != (Tokens{Output: 3}) {
		t.Errorf("Sub = %+v", got)
	}
}

// TestAModelIsPricedByTheLongestIDItStartsWith: claude-opus-5-5 starts with
// claude-opus-5 too, and is priced differently. Matching the first prefix
// found would price one as the other, depending on the order of a map.
func TestAModelIsPricedByTheLongestIDItStartsWith(t *testing.T) {
	for model, want := range map[string]float64{
		"claude-opus-5-5":          4,
		"claude-opus-5":            5,
		"claude-opus-4-5-20251101": 5,
		"claude-sonnet-5[1m]":      2,
	} {
		p, ok := PriceOf(model, nil)
		if !ok || p.Input != want {
			t.Errorf("PriceOf(%q) = %+v, %v, want input %v", model, p, ok, want)
		}
	}
	if _, ok := PriceOf("claude-opus-50", nil); ok {
		t.Error("claude-opus-50 was priced as claude-opus-5")
	}
	if p, _ := PriceOf("claude-opus-5-5", map[string]Price{"claude-opus-5-5": {Input: 1}}); p.Input != 1 {
		t.Error("an override did not replace the built-in price")
	}
}

// TestACostCountsEachKindOfTokenAtItsOwnPrice: a cache read costs a
// twentieth of an input token on some models and the same as one written for
// an hour costs twice. Pricing every token as input overstates an agent's cost
// several times over, since most of what it sends is read from the cache.
func TestACostCountsEachKindOfTokenAtItsOwnPrice(t *testing.T) {
	p := Price{Input: 4, Output: 20, CacheRead: 0.2}
	got := p.Cost(Tokens{Input: 1e6, Output: 1e6, CacheWrite: 3e6, CacheWrite1h: 1e6, CacheRead: 10e6})
	want := 4 + 20 + 2*4*1.25 + 1*4*2 + 10*0.2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Cost = %v, want %v", got, want)
	}

	cost, complete := Usage{"mystery-model": {Output: 1e6}, "claude-haiku-4-5": {Output: 1e6}}.Cost(nil)
	if complete || cost != 5 {
		t.Errorf("Cost = %v, %v; want 5 and incomplete, for a model with no price", cost, complete)
	}
}
