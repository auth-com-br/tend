package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// What an agent is spending as it works, for showing beside it (issue #46):
// the conversation's tokens so far, how full its context window is, and,
// where the agent writes it down, how much of the provider's usage limit is
// gone. Read from the same files as the rest of this package, but followed —
// each read takes only what was appended since the last — since a
// conversation's file grows to megabytes and is looked at every few seconds.

// Limit is a provider's usage limit as the agent last recorded it: Codex
// writes its rate limits into every token count. Claude Code writes none.
type Limit struct {
	// UsedPercent is how much of the window is gone, 0 to 100.
	UsedPercent float64 `json:"used_percent"`
	// Window is how long the limit's window is; Resets when it starts over.
	Window time.Duration `json:"window,omitempty"`
	Resets time.Time     `json:"resets,omitempty"`
	// Reached is set when the agent recorded that the limit stopped it.
	Reached bool `json:"reached,omitempty"`
}

// Live is a conversation as it stands.
type Live struct {
	Usage Usage
	// Model is the model of the last reply.
	Model string
	// Context is how many tokens the last reply was given — the
	// conversation as the model now sees it — and Window how many it can
	// take; Window is zero when the agent did not say and it is not known.
	Context, Window int64
	// Reserve is the part of the window the agent never counts as free.
	Reserve int64
	Limit   *Limit
}

// codexReserve is the part of the window Codex keeps out of its gauge: what
// its own instructions take, BASELINE_TOKENS in its source. Checked against
// its /status: 20,611 tokens of 475K is "98% left", not 96%.
const codexReserve = 12_000

// ContextPercent is how full the context window is, or -1 when the window
// is not known.
func (l Live) ContextPercent() int {
	if l.Window <= l.Reserve {
		return -1
	}
	eff := l.Window - l.Reserve
	used := min(max(l.Context-l.Reserve, 0), eff)
	// Rounded as the agents round: what is left, as Codex says it, which
	// for Claude Code's share used comes to the same.
	return int(100 - ((eff-used)*100+eff/2)/eff)
}

// Follower reads a conversation's file as it grows.
type Follower struct {
	agent string
	path  string
	files map[string]*followed
	live  Live
	// identity is the model Claude Code last said it is, "[1m]" and all.
	identity string
	// codexModel is the model Codex last said it turned to; its token
	// counts do not name one.
	codexModel string
}

// followed is one file read up to offset.
type followed struct {
	offset  int64
	size    int64
	replies map[string]claudeReply
	order   []string
	partial []byte
}

type claudeReply struct {
	model  string
	tokens Tokens
}

// NewFollower follows the conversation at path, kept by agent ("claude" or
// "codex").
func NewFollower(agent, path string) *Follower {
	return &Follower{agent: agent, path: path, files: map[string]*followed{}}
}

// Path is the file followed.
func (f *Follower) Path() string { return f.path }

// Read takes what was appended since the last read and says the
// conversation as it now stands. A file that shrank was rewritten, and is
// read again from the start.
func (f *Follower) Read() (Live, error) {
	if f.agent == "codex" {
		if err := f.readCodex(); err != nil {
			return f.live, err
		}
		return f.live, nil
	}
	if err := f.readClaude(f.path, true); err != nil {
		return f.live, err
	}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(f.path, ".jsonl"), "subagents", "*.jsonl"))
	for _, sub := range subs {
		_ = f.readClaude(sub, false)
	}
	total := Usage{}
	for _, st := range f.files {
		for _, k := range st.order {
			r := st.replies[k]
			total[r.model] = total[r.model].Add(r.tokens)
		}
	}
	f.live.Usage = total
	return f.live, nil
}

// appended is what the file gained since st's offset, in whole lines.
func appended(path string, st *followed) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() < st.size {
		*st = followed{replies: map[string]claudeReply{}}
	}
	st.size = info.Size()
	if info.Size() == st.offset {
		return nil, nil
	}
	if _, err := file.Seek(st.offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	st.offset += int64(len(data))
	data = append(st.partial, data...)
	cut := bytes.LastIndexByte(data, '\n')
	if cut < 0 {
		st.partial = data
		return nil, nil
	}
	st.partial = append([]byte(nil), data[cut+1:]...)
	return data[:cut+1], nil
}

// claudeModelLine is the line Claude Code writes when it says which model
// it is — "claude-opus-5-5[1m]" for the one with the million-token window,
// which the replies' own model name ("claude-opus-5-5") does not tell.
type claudeModelLine struct {
	Type       string `json:"type"`
	Attachment struct {
		Type     string `json:"type"`
		Identity struct {
			ModelID string `json:"modelId"`
		} `json:"identity"`
	} `json:"attachment"`
}

// claudeWindows are the models whose context window is a million tokens
// without asking for it, by the longest id they start with — Claude Code's
// own table (its model list's `context: {window: 1e6, native_1m: true}`,
// read in version 2.1.322). The rest take 200k, and a million when Claude
// Code says "[1m]". Guessing from the name alone said 24% of a conversation
// Claude Code's /context put at 5%.
var claudeWindows = []string{
	"claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-sonnet-5",
	"claude-haiku-5", "claude-fable-5", "claude-mythos-5",
}

// claudeWindow is the context window of a Claude Code model id.
func claudeWindow(modelID string) int64 {
	if strings.Contains(modelID, "[1m]") {
		return 1_000_000
	}
	for _, prefix := range claudeWindows {
		if strings.HasPrefix(modelID, prefix) {
			return 1_000_000
		}
	}
	return 200_000
}

func (f *Follower) readClaude(path string, main bool) error {
	st := f.files[path]
	if st == nil {
		st = &followed{replies: map[string]claudeReply{}}
		f.files[path] = st
	}
	data, err := appended(path, st)
	if err != nil {
		return err
	}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		raw := data[:i]
		data = data[i+1:]
		if main && bytes.Contains(raw, []byte(`"identity"`)) {
			var l claudeModelLine
			if json.Unmarshal(raw, &l) == nil && l.Attachment.Type == "model" && l.Attachment.Identity.ModelID != "" {
				f.identity = l.Attachment.Identity.ModelID
			}
			continue
		}
		if !bytes.Contains(raw, []byte(`"usage"`)) || !bytes.Contains(raw, []byte(`"assistant"`)) {
			continue
		}
		var l claudeLine
		if json.Unmarshal(raw, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil || l.Message.Model == "" || l.Message.Model == "<synthetic>" {
			continue
		}
		key := l.Message.ID
		if key == "" {
			key = l.RequestID
		}
		if key == "" {
			key = strings.Repeat("-", len(st.order)+1)
		}
		u := l.Message.Usage
		if _, seen := st.replies[key]; !seen {
			st.order = append(st.order, key)
		}
		st.replies[key] = claudeReply{model: l.Message.Model, tokens: Tokens{
			Input: u.Input, Output: u.Output, CacheWrite: u.CacheCreation,
			CacheWrite1h: u.Creation.OneHour, CacheRead: u.CacheRead,
		}}
		if main {
			// What the model was given for this reply is the conversation
			// as it sees it: the new input and everything from the cache.
			f.live.Model = l.Message.Model
			f.live.Context = u.Input + u.CacheCreation + u.CacheRead
			// The model line says "[1m]", the replies do not; it counts
			// while the replies are still from that model — a /model
			// halfway changes the window too.
			id := l.Message.Model
			if strings.HasPrefix(f.identity, id) {
				id = f.identity
			}
			f.live.Window = claudeWindow(id)
		}
	}
	return nil
}

// codexCount is a Codex token count with what Usage leaves out of it.
type codexCount struct {
	Payload struct {
		Type string `json:"type"`
		Info *struct {
			Last struct {
				Total int64 `json:"total_tokens"`
			} `json:"last_token_usage"`
			Window int64 `json:"model_context_window"`
		} `json:"info"`
		RateLimits *struct {
			Primary *struct {
				UsedPercent   float64 `json:"used_percent"`
				WindowMinutes int64   `json:"window_minutes"`
				ResetsAt      int64   `json:"resets_at"`
			} `json:"primary"`
			Reached *string `json:"rate_limit_reached_type"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

func (f *Follower) readCodex() error {
	st := f.files[f.path]
	if st == nil {
		st = &followed{replies: map[string]claudeReply{}}
		f.files[f.path] = st
	}
	data, err := appended(f.path, st)
	if err != nil {
		return err
	}
	r := bufio.NewReader(bytes.NewReader(data))
	for {
		raw, err := readLine(r)
		switch {
		case bytes.Contains(raw, []byte(`"turn_context"`)):
			var l codexLine
			if json.Unmarshal(raw, &l) == nil && l.Type == "turn_context" && l.Payload.Model != "" {
				f.codexModel = l.Payload.Model
			}
		case bytes.Contains(raw, []byte(`"token_count"`)):
			var l codexLine
			var c codexCount
			if json.Unmarshal(raw, &l) != nil || l.Payload.Type != "token_count" || json.Unmarshal(raw, &c) != nil {
				break
			}
			if l.Payload.Info != nil {
				t := l.Payload.Info.Total
				model := f.codexModel
				if model == "" {
					model = "codex"
				}
				f.live.Usage = Usage{model: {Input: max(t.Input-t.CachedInput, 0), Output: t.Output, CacheRead: t.CachedInput, CacheWrite: t.CacheWrite}}
				f.live.Model = model
				// The last turn, what it was given and what it said: Codex's
				// tokens_in_context_window.
				f.live.Context = c.Payload.Info.Last.Total
				f.live.Window = c.Payload.Info.Window
				f.live.Reserve = codexReserve
			}
			if rl := c.Payload.RateLimits; rl != nil && rl.Primary != nil {
				lim := &Limit{UsedPercent: rl.Primary.UsedPercent, Window: time.Duration(rl.Primary.WindowMinutes) * time.Minute}
				if rl.Primary.ResetsAt > 0 {
					lim.Resets = time.Unix(rl.Primary.ResetsAt, 0)
				}
				lim.Reached = rl.Reached != nil && *rl.Reached != ""
				f.live.Limit = lim
			}
		}
		if err != nil {
			break
		}
	}
	return nil
}
