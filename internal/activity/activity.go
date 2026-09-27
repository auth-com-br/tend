// Package activity is the record of what agents did: when each one started
// and stopped, in which project and on which branch, what it was working on,
// which files changed while it ran, and what its conversation spent.
//
// It is tend's own; herdr keeps no such record. A team that lets agents work
// on its code wants to be able to answer "what did they do, and what did it
// cost" after the fact, and the one program that sees every agent start and
// stop is the session server. So the server writes it down, and anybody can
// read it back — `tend activity` — without a server running.
//
// The record is a file of JSON lines, appended to and never rewritten. An
// audit trail that is edited in place is one nobody can trust, and appending
// is also what lets several session servers share one file: each line is a
// single write, which the kernel does not interleave with another's.
//
// A run is folded from its lines when the file is read, rather than written
// whole at its end, so that a server that dies mid-run leaves the start of
// the story behind instead of nothing.
package activity

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/usage"
)

// FileName is the record's name in tend's state directory.
const FileName = "activity.jsonl"

// Kinds of line.
const (
	// KindStart is an agent recognised in a pane.
	KindStart = "start"
	// KindConversation is the conversation a running agent is in, named by
	// its hook, with what it had already spent when it was first seen: a
	// resumed conversation arrives with its past, and that past is not this
	// run's.
	KindConversation = "conversation"
	// KindStop is an agent gone from its pane.
	KindStop = "stop"
	// KindViolation is an agent running against a policy: one tend saw
	// start but did not start itself, and so could not refuse.
	KindViolation = "violation"
	// KindRefused is an agent tend was asked to start and would not.
	KindRefused = "refused"
)

// Why a run ended.
const (
	ReasonExited   = "agent exited"
	ReasonClosed   = "pane closed"
	ReasonReplaced = "another agent took the pane"
	ReasonStopped  = "server stopped"
	// ReasonLost is a run whose server went away without saying — a crash,
	// a kill. When it ended is not known, only that it had by the time a
	// server looked again.
	ReasonLost = "server went away"
)

// Place is where an agent works: the project, the checkout and the branch.
type Place struct {
	// Dir is the directory the agent was in when it started.
	Dir string `json:"dir,omitempty"`
	// Project is the repository's main checkout, which is what a project is
	// here: every worktree of one repository is the same project.
	Project string `json:"project,omitempty"`
	// Checkout is the checkout the agent works in, a linked worktree or the
	// main one.
	Checkout string `json:"checkout,omitempty"`
	Branch   string `json:"branch,omitempty"`
	// Worktree is whether Checkout is a linked worktree rather than the
	// repository's own.
	Worktree bool `json:"worktree,omitempty"`
	// Head is the commit checked out when the agent started, which is what
	// its changes are measured from.
	Head string `json:"head,omitempty"`
}

// Record is one line of the file. Which fields are set depends on Kind.
type Record struct {
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`
	// Run ties a run's lines together.
	Run string `json:"run,omitempty"`
	// Session is the tend session, and Pane the pane in it.
	Session string `json:"session,omitempty"`
	Pane    uint64 `json:"pane,omitempty"`
	Agent   string `json:"agent,omitempty"`

	// Start (and a refusal): where, and under what name in the session.
	Place
	Space string `json:"space,omitempty"`
	Tab   string `json:"tab,omitempty"`

	// Conversation.
	Conversation string      `json:"conversation,omitempty"`
	Transcript   string      `json:"transcript,omitempty"`
	Baseline     usage.Usage `json:"baseline,omitempty"`

	// Stop.
	Reason  string      `json:"reason,omitempty"`
	Files   []string    `json:"files,omitempty"`
	Commits int         `json:"commits,omitempty"`
	Usage   usage.Usage `json:"usage,omitempty"`

	// Violation and refusal.
	Policy  string `json:"policy,omitempty"`
	Message string `json:"message,omitempty"`
}

// Path is where the record is kept, in tend's state directory.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Log appends to the record.
type Log struct {
	path string
	mu   sync.Mutex
}

// NewLog writes to the file at path, which is made when first written.
func NewLog(path string) *Log { return &Log{path: path} }

// Path is the file written.
func (l *Log) Path() string { return l.path }

// Append writes records, each as one line in one write.
//
// The file is opened for each call rather than held: a server runs for weeks,
// and a file held that long is one somebody rotates or deletes underneath it,
// after which every line would go to a file nobody can find.
func (l *Log) Append(recs ...Record) error {
	if l == nil || l.path == "" || len(recs) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	// Private, like the rest of tend's state: it names projects, branches
	// and files.
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, r := range recs {
		if r.Time.IsZero() {
			r.Time = time.Now()
		}
		r.Time = r.Time.UTC().Truncate(time.Millisecond)
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// Read reads every record in the file. A file that is not there is an empty
// record. A line that does not parse — the half of one a crash cut off — is
// skipped: the rest of the record is still true.
func Read(path string) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f)
}

// Decode reads records from r.
func Decode(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if json.Unmarshal(line, &rec) != nil || rec.Kind == "" {
			continue
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// Conversation is one conversation a run was in, and what it had spent when
// the run first saw it.
type Conversation struct {
	ID         string      `json:"id,omitempty"`
	Transcript string      `json:"transcript,omitempty"`
	Baseline   usage.Usage `json:"-"`
}

// Violation is a policy an agent ran against, or would have.
type Violation struct {
	Time    time.Time `json:"time"`
	Policy  string    `json:"policy"`
	Message string    `json:"message"`
	// Refused is a launch tend would not make; otherwise the agent ran.
	Refused bool   `json:"refused,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Place
	Run string `json:"run,omitempty"`
}

// PR is the pull request a run's branch became, when somebody looked.
type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// Run is one agent's stretch in one pane, from the lines about it.
type Run struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Pane    uint64 `json:"pane"`
	Agent   string `json:"agent"`
	Place
	Space string `json:"space,omitempty"`
	Tab   string `json:"tab,omitempty"`
	// For is what the run was working on, as far as its branch says: an
	// issue, an error, a ticket.
	For string `json:"for,omitempty"`

	Started time.Time `json:"started"`
	// Ended is zero while the run is going, and for one whose end is not
	// known (ReasonLost).
	Ended   time.Time `json:"ended,omitempty"`
	Running bool      `json:"running"`
	Reason  string    `json:"reason,omitempty"`

	Conversations []Conversation `json:"conversations,omitempty"`
	Files         []string       `json:"files,omitempty"`
	Commits       int            `json:"commits,omitempty"`
	// Usage is what the run's conversations spent while it ran. For a run
	// still going it is filled by Measure; the file holds it only once the
	// run is over.
	Usage usage.Usage `json:"usage,omitempty"`
	// Cost is Usage priced, and CostComplete whether every model in it had
	// a price. Both are filled by Price.
	Cost         float64 `json:"cost_usd"`
	CostComplete bool    `json:"cost_complete"`

	Violations []Violation `json:"violations,omitempty"`
	PR         *PR         `json:"pr,omitempty"`
}

// Duration is how long the run went on, up to now for one still going, and
// zero when its end is not known.
func (r Run) Duration(now time.Time) time.Duration {
	switch {
	case r.Running:
		return now.Sub(r.Started)
	case r.Ended.IsZero():
		return 0
	}
	return r.Ended.Sub(r.Started)
}

// Fold turns records into runs, oldest first, and the refusals that belong to
// no run.
func Fold(recs []Record) (runs []*Run, refused []Violation) {
	byID := map[string]*Run{}
	for _, rec := range recs {
		switch rec.Kind {
		case KindStart:
			if _, dup := byID[rec.Run]; dup || rec.Run == "" {
				// The same run started twice is a server taking over from
				// another that had already written its start: the run goes
				// on, and its first start is the one that counts.
				continue
			}
			r := &Run{
				ID: rec.Run, Session: rec.Session, Pane: rec.Pane, Agent: rec.Agent,
				Place: rec.Place, Space: rec.Space, Tab: rec.Tab,
				For:     WorkFor(rec.Branch),
				Started: rec.Time, Running: true,
			}
			byID[rec.Run] = r
			runs = append(runs, r)
		case KindConversation:
			if r := byID[rec.Run]; r != nil {
				r.Conversations = append(r.Conversations, Conversation{
					ID: rec.Conversation, Transcript: rec.Transcript, Baseline: rec.Baseline,
				})
			}
		case KindStop:
			if r := byID[rec.Run]; r != nil && r.Running {
				r.Running = false
				r.Reason = rec.Reason
				if rec.Reason != ReasonLost {
					r.Ended = rec.Time
				}
				r.Files, r.Commits, r.Usage = rec.Files, rec.Commits, rec.Usage
			}
		case KindViolation:
			if r := byID[rec.Run]; r != nil {
				r.Violations = append(r.Violations, violationOf(rec))
			}
		case KindRefused:
			refused = append(refused, violationOf(rec))
		}
	}
	return runs, refused
}

func violationOf(rec Record) Violation {
	return Violation{
		Time: rec.Time, Policy: rec.Policy, Message: rec.Message,
		Refused: rec.Kind == KindRefused, Agent: rec.Agent, Place: rec.Place, Run: rec.Run,
	}
}

// Transcripts finds a conversation's file from what its hook said about it.
type Transcripts func(agent string, c Conversation) string

// Measure fills in what running runs have spent so far, from their
// conversations as they are now less what they had when the run began. A run
// that is over already has its number, taken when it ended.
func Measure(runs []*Run, find Transcripts) {
	for _, r := range runs {
		if r.Running {
			r.Usage = Spent(r.Agent, r.Conversations, find)
		}
	}
}

// Spent is what conversations have spent beyond their baselines.
func Spent(agent string, convs []Conversation, find Transcripts) usage.Usage {
	total := usage.Usage{}
	for _, c := range convs {
		path := c.Transcript
		if path == "" && find != nil {
			path = find(agent, c)
		}
		u, err := Read1(agent, path)
		if err != nil {
			continue
		}
		total = total.Add(u.Sub(c.Baseline))
	}
	return total
}

// Read1 is what one conversation file has spent, read the way its agent
// writes it.
func Read1(agent, path string) (usage.Usage, error) {
	if path == "" {
		return nil, errors.New("activity: no transcript")
	}
	if agent == "codex" {
		return usage.Codex(path)
	}
	return usage.Claude(path)
}

// Price fills in each run's cost.
func Price(runs []*Run, overrides map[string]usage.Price) {
	for _, r := range runs {
		r.Cost, r.CostComplete = r.Usage.Cost(overrides)
	}
}

// Filter is which runs a report is about.
type Filter struct {
	// Project keeps the runs in the repository this directory is in, or
	// under it when it is in none.
	Project string
	Agent   string
	Since   time.Time
}

// Keep reports whether a run passes the filter.
func (f Filter) Keep(r *Run) bool {
	if f.Agent != "" && !strings.EqualFold(f.Agent, r.Agent) {
		return false
	}
	if !f.Since.IsZero() && r.Started.Before(f.Since) && !(r.Running || r.Ended.After(f.Since)) {
		return false
	}
	if f.Project != "" {
		p := filepath.Clean(f.Project)
		if r.Project != "" {
			return filepath.Clean(r.Project) == p
		}
		return within(r.Dir, p)
	}
	return true
}

func within(dir, root string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// WorkFor is what a branch says it is for, in the names tend's own panels
// give the branches they make: issue-<n>-… for an issue, fix-<id> for an
// error, ticket-<key> for a ticket. Any other branch says nothing.
func WorkFor(branch string) string {
	switch {
	case strings.HasPrefix(branch, "issue-"):
		n := strings.TrimPrefix(branch, "issue-")
		if i := strings.IndexByte(n, '-'); i >= 0 {
			n = n[:i]
		}
		if n != "" && strings.Trim(n, "0123456789") == "" {
			return "issue #" + n
		}
	case strings.HasPrefix(branch, "fix-") && len(branch) > len("fix-"):
		return "error " + strings.ToUpper(strings.TrimPrefix(branch, "fix-"))
	case strings.HasPrefix(branch, "ticket-") && len(branch) > len("ticket-"):
		return "ticket " + strings.ToUpper(strings.TrimPrefix(branch, "ticket-"))
	}
	return ""
}

// Total is one line of a summary: every run of one project, or one agent.
type Total struct {
	Key          string      `json:"key"`
	Runs         int         `json:"runs"`
	Running      int         `json:"running"`
	Duration     Seconds     `json:"duration_seconds"`
	Files        int         `json:"files"`
	Usage        usage.Usage `json:"usage,omitempty"`
	Tokens       int64       `json:"tokens"`
	Cost         float64     `json:"cost_usd"`
	CostComplete bool        `json:"cost_complete"`
	Violations   int         `json:"violations"`
}

// Seconds is a duration written as a number of seconds.
type Seconds float64

// Summarize totals runs by what key says each belongs to, the most expensive
// first.
func Summarize(runs []*Run, key func(*Run) string, now time.Time) []Total {
	by := map[string]*Total{}
	var order []string
	for _, r := range runs {
		k := key(r)
		t := by[k]
		if t == nil {
			t = &Total{Key: k, Usage: usage.Usage{}, CostComplete: true}
			by[k] = t
			order = append(order, k)
		}
		t.Runs++
		if r.Running {
			t.Running++
		}
		t.Duration += Seconds(r.Duration(now).Seconds())
		t.Files += len(r.Files)
		t.Usage = t.Usage.Add(r.Usage)
		t.Cost += r.Cost
		t.CostComplete = t.CostComplete && r.CostComplete
		t.Violations += len(r.Violations)
	}
	out := make([]Total, 0, len(order))
	for _, k := range order {
		t := by[k]
		t.Tokens = t.Usage.Total().Total()
		out = append(out, *t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Cost > out[j].Cost })
	return out
}

// ByProject is the key that groups runs by project.
func ByProject(r *Run) string {
	switch {
	case r.Project != "":
		return r.Project
	case r.Dir != "":
		return r.Dir
	}
	return "(unknown)"
}

// ByAgent is the key that groups runs by agent.
func ByAgent(r *Run) string { return r.Agent }

// RunID names a run: the session, the pane and when it began, which no two
// runs share.
func RunID(session string, pane uint64, started time.Time) string {
	return fmt.Sprintf("%s-%d-%d", session, pane, started.UnixMilli())
}
