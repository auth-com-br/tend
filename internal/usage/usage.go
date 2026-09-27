// Package usage reads how many tokens an agent's conversation has spent, from
// the file the agent keeps it in, and what that is likely to have cost.
//
// Agents do not tell tend what they spend. Claude Code's /cost is for the
// person in front of it, and no hook carries a number. What they do keep is
// the conversation itself, on disk, with the usage the API returned for every
// reply — and the hooks already tell tend which conversation a pane is in.
// So the tokens are read from there, after the fact, and never from the
// screen.
//
// Both formats are the agents' own and undocumented, so everything here reads
// them loosely: a line that does not parse is skipped, and a file that is not
// there is no usage rather than an error worth stopping for.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Tokens is what one model spent, by the four prices a reply is billed at.
type Tokens struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	// CacheWrite is input written to the prompt cache, and CacheWrite1h
	// the part of it kept for an hour, which is billed higher.
	CacheWrite   int64 `json:"cache_write,omitempty"`
	CacheWrite1h int64 `json:"cache_write_1h,omitempty"`
	CacheRead    int64 `json:"cache_read,omitempty"`
}

// Total is every token, whatever it was billed at.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheWrite + t.CacheRead
}

// Add is the two together.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:        t.Input + o.Input,
		Output:       t.Output + o.Output,
		CacheWrite:   t.CacheWrite + o.CacheWrite,
		CacheWrite1h: t.CacheWrite1h + o.CacheWrite1h,
		CacheRead:    t.CacheRead + o.CacheRead,
	}
}

// Sub is what t has that base did not, never below zero: a conversation read
// at the start of a run and again at its end, the difference being what the
// run spent. A count that went down means the file was rewritten, and the
// honest answer then is nothing rather than a negative.
func (t Tokens) Sub(base Tokens) Tokens {
	d := func(a, b int64) int64 {
		if a < b {
			return 0
		}
		return a - b
	}
	return Tokens{
		Input:        d(t.Input, base.Input),
		Output:       d(t.Output, base.Output),
		CacheWrite:   d(t.CacheWrite, base.CacheWrite),
		CacheWrite1h: d(t.CacheWrite1h, base.CacheWrite1h),
		CacheRead:    d(t.CacheRead, base.CacheRead),
	}
}

// Usage is what a conversation spent, by model. A conversation can change
// model halfway, and the models are priced differently.
type Usage map[string]Tokens

// Add is both, model by model.
func (u Usage) Add(o Usage) Usage {
	out := Usage{}
	for m, t := range u {
		out[m] = t
	}
	for m, t := range o {
		out[m] = out[m].Add(t)
	}
	return out
}

// Sub is what u spent beyond base, model by model.
func (u Usage) Sub(base Usage) Usage {
	out := Usage{}
	for m, t := range u {
		if d := t.Sub(base[m]); d.Total() > 0 {
			out[m] = d
		}
	}
	return out
}

// Total is every token of every model.
func (u Usage) Total() Tokens {
	var t Tokens
	for _, v := range u {
		t = t.Add(v)
	}
	return t
}

// Models are the models in it, in order.
func (u Usage) Models() []string {
	out := make([]string, 0, len(u))
	for m := range u {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Claude is what a Claude Code conversation spent: the transcript at path,
// and the transcripts of the subagents it ran, which Claude Code keeps in a
// folder of the same name and which are billed like the rest.
func Claude(path string) (Usage, error) {
	u, err := claudeFile(path)
	if err != nil {
		return nil, err
	}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, sub := range subs {
		if su, err := claudeFile(sub); err == nil {
			u = u.Add(su)
		}
	}
	return u, nil
}

// claudeLine is the part of a transcript line that says what a reply cost.
type claudeLine struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			Creation      struct {
				OneHour int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

// claudeFile reads one transcript.
//
// Claude Code writes a reply as several lines, one per block of content, and
// every one of them carries the reply's usage. Adding each line would count
// a reply with a thought, some text and two tool calls four times; so a
// reply is counted once, by its message id, with the usage from its last
// line, which is the one written when the reply was finished.
func claudeFile(path string) (Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type reply struct {
		model  string
		tokens Tokens
	}
	replies := map[string]reply{}
	var order []string
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		raw, err := readLine(r)
		if len(raw) > 0 && bytes.Contains(raw, []byte(`"usage"`)) && bytes.Contains(raw, []byte(`"assistant"`)) {
			var l claudeLine
			if json.Unmarshal(raw, &l) == nil && l.Type == "assistant" && l.Message.Usage != nil && l.Message.Model != "" && l.Message.Model != "<synthetic>" {
				key := l.Message.ID
				if key == "" {
					key = l.RequestID
				}
				if key == "" {
					key = strings.Repeat("-", len(order)+1)
				}
				u := l.Message.Usage
				if _, seen := replies[key]; !seen {
					order = append(order, key)
				}
				replies[key] = reply{model: l.Message.Model, tokens: Tokens{
					Input:        u.Input,
					Output:       u.Output,
					CacheWrite:   u.CacheCreation,
					CacheWrite1h: u.Creation.OneHour,
					CacheRead:    u.CacheRead,
				}}
			}
		}
		if err != nil {
			break
		}
	}
	out := Usage{}
	for _, k := range order {
		rp := replies[k]
		out[rp.model] = out[rp.model].Add(rp.tokens)
	}
	return out, nil
}

// codexLine is the part of a Codex rollout line that says what was spent.
type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  *struct {
			Total struct {
				Input       int64 `json:"input_tokens"`
				CachedInput int64 `json:"cached_input_tokens"`
				CacheWrite  int64 `json:"cache_write_input_tokens"`
				Output      int64 `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// Codex is what a Codex conversation spent. Codex writes the running total
// after every turn, so the last one is the answer. Its input count includes
// the part that came from the cache, which is taken out here so the four
// numbers mean what Claude's do.
func Codex(path string) (Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	model := ""
	var last *Tokens
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		raw, err := readLine(r)
		switch {
		case bytes.Contains(raw, []byte(`"turn_context"`)):
			var l codexLine
			if json.Unmarshal(raw, &l) == nil && l.Type == "turn_context" && l.Payload.Model != "" {
				model = l.Payload.Model
			}
		case bytes.Contains(raw, []byte(`"token_count"`)):
			var l codexLine
			if json.Unmarshal(raw, &l) == nil && l.Payload.Type == "token_count" && l.Payload.Info != nil {
				t := l.Payload.Info.Total
				input := t.Input - t.CachedInput
				if input < 0 {
					input = 0
				}
				last = &Tokens{Input: input, Output: t.Output, CacheRead: t.CachedInput, CacheWrite: t.CacheWrite}
			}
		}
		if err != nil {
			break
		}
	}
	if last == nil {
		return Usage{}, nil
	}
	if model == "" {
		model = "codex"
	}
	return Usage{model: *last}, nil
}

// readLine is one whole line, however long. Transcripts hold tool output and
// pasted images on single lines of megabytes; a line too long for the buffer
// is not one that carries usage, so it is skipped rather than grown into.
func readLine(r *bufio.Reader) ([]byte, error) {
	raw, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		return nil, err
	}
	return raw, err
}

// FindClaude is the transcript of a Claude Code conversation, by its id,
// under Claude Code's config directory. The folder it is in is named after
// the directory the conversation was held in, which is not worth
// reconstructing: there is one file by that id.
func FindClaude(configDir, id string) string {
	if configDir == "" || !safeID(id) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(configDir, "projects", "*", id+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// FindCodex is the rollout of a Codex conversation, by its id, under Codex's
// home: sessions/<year>/<month>/<day>/rollout-<time>-<id>.jsonl.
func FindCodex(home, id string) string {
	if home == "" || !safeID(id) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// safeID is an id that can go into a pattern without reaching anywhere it
// should not: letters, digits and dashes.
func safeID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
