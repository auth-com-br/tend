package activity

import (
	"bytes"
	"encoding/csv"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/usage"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

// TestTheRecordReadsBackWhatWasWritten, and a line cut off by a crash does
// not take the rest of the record with it: an audit trail that is unreadable
// after one bad write is not one.
func TestTheRecordReadsBackWhatWasWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", FileName)
	log := NewLog(path)
	if err := log.Append(Record{Kind: KindStart, Time: t0, Run: "r1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"kind":"stop","run":"r1","ti`)
	_, _ = f.WriteString("\n")
	f.Close()
	if err := log.Append(Record{Kind: KindStop, Time: t0.Add(time.Minute), Run: "r1", Reason: ReasonExited}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the record is %v, %v; want readable by its owner alone", info.Mode().Perm(), err)
	}
	recs, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Kind != KindStart || recs[1].Reason != ReasonExited {
		t.Errorf("records = %+v", recs)
	}
	if got, _ := Read(filepath.Join(t.TempDir(), "none")); got != nil {
		t.Errorf("a record that is not there read as %v", got)
	}
}

// TestARunIsFoldedFromItsLines: a start, the conversations named while it
// ran, the policy it broke and its stop make one run. A second start with the
// same id is a replacement server taking over, not a second run, and a run
// lost with its server has no end rather than the time somebody noticed.
func TestARunIsFoldedFromItsLines(t *testing.T) {
	recs := []Record{
		{Kind: KindStart, Time: t0, Run: "r1", Session: "s", Pane: 3, Agent: "claude",
			Place: Place{Project: "/p", Checkout: "/wt/p/issue-10-x", Branch: "issue-10-x", Worktree: true}},
		{Kind: KindConversation, Time: t0, Run: "r1", Conversation: "c1", Transcript: "/t/c1.jsonl",
			Baseline: usage.Usage{"m": {Output: 5}}},
		{Kind: KindViolation, Time: t0, Run: "r1", Policy: "max_agents", Message: "too many"},
		{Kind: KindStart, Time: t0.Add(time.Hour), Run: "r1", Agent: "claude"},
		{Kind: KindStop, Time: t0.Add(2 * time.Hour), Run: "r1", Reason: ReasonClosed,
			Files: []string{"a.go"}, Commits: 2, Usage: usage.Usage{"m": {Output: 9}}},
		{Kind: KindStart, Time: t0, Run: "r2", Agent: "codex"},
		{Kind: KindStop, Time: t0.Add(5 * time.Hour), Run: "r2", Reason: ReasonLost},
		{Kind: KindRefused, Time: t0, Agent: "claude", Policy: "protected_branch", Place: Place{Branch: "main"}},
	}
	runs, refused := Fold(recs)
	if len(runs) != 2 || len(refused) != 1 {
		t.Fatalf("runs = %d, refused = %d", len(runs), len(refused))
	}
	r := runs[0]
	if r.Running || r.For != "issue #10" || r.Duration(time.Time{}) != 2*time.Hour ||
		len(r.Conversations) != 1 || len(r.Violations) != 1 || r.Commits != 2 || r.Usage["m"].Output != 9 {
		t.Errorf("run = %+v", r)
	}
	if lost := runs[1]; lost.Running || !lost.Ended.IsZero() || lost.Duration(t0.Add(time.Hour)) != 0 {
		t.Errorf("lost run = %+v; want over, with no end", lost)
	}
	if !refused[0].Refused || refused[0].Branch != "main" {
		t.Errorf("refused = %+v", refused[0])
	}
}

// TestWhatABranchSaysItIsFor: the panels name the branches they make after
// what they are for, and the record reads that back. A branch named anything
// else is for nothing the record can tell.
func TestWhatABranchSaysItIsFor(t *testing.T) {
	for branch, want := range map[string]string{
		"issue-10-auditoria-custo": "issue #10",
		"issue-7":                  "issue #7",
		"issue-x-y":                "",
		"fix-proj-1a":              "error PROJ-1A",
		"ticket-eng-42":            "ticket ENG-42",
		"main":                     "",
	} {
		if got := WorkFor(branch); got != want {
			t.Errorf("WorkFor(%q) = %q, want %q", branch, got, want)
		}
	}
}

// TestAFilterKeepsAProjectAndWhatRanSince: a report on one project includes
// its worktrees, since they are the project, and a report since Monday
// includes a run that started on Sunday and was still going.
func TestAFilterKeepsAProjectAndWhatRanSince(t *testing.T) {
	a := &Run{Place: Place{Project: "/src/app", Checkout: "/wt/app/x"}, Started: t0, Ended: t0.Add(time.Hour)}
	b := &Run{Place: Place{Dir: "/src/app/sub"}, Started: t0}
	old := &Run{Place: Place{Project: "/src/app"}, Started: t0.Add(-48 * time.Hour), Ended: t0.Add(-47 * time.Hour)}
	going := &Run{Place: Place{Project: "/src/app"}, Started: t0.Add(-48 * time.Hour), Running: true}
	other := &Run{Place: Place{Project: "/src/other"}, Started: t0}

	f := Filter{Project: "/src/app"}
	if !f.Keep(a) || !f.Keep(b) || f.Keep(other) {
		t.Error("the project filter kept the wrong runs")
	}
	f = Filter{Since: t0.Add(-time.Hour)}
	if !f.Keep(a) || f.Keep(old) || !f.Keep(going) {
		t.Error("the since filter kept the wrong runs")
	}
}

// TestASummaryTotalsByProject, the most expensive first, and says when a
// cost is a lower bound because some model in it had no price.
func TestASummaryTotalsByProject(t *testing.T) {
	runs := []*Run{
		{Place: Place{Project: "/a"}, Started: t0, Ended: t0.Add(time.Minute), Cost: 1, CostComplete: true, Files: []string{"x"}},
		{Place: Place{Project: "/b"}, Started: t0, Ended: t0.Add(time.Minute), Cost: 3, CostComplete: false},
		{Place: Place{Project: "/a"}, Started: t0, Running: true, Cost: 1, CostComplete: true},
	}
	got := Summarize(runs, ByProject, t0.Add(2*time.Minute))
	if len(got) != 2 || got[0].Key != "/b" || got[0].CostComplete {
		t.Fatalf("totals = %+v", got)
	}
	if a := got[1]; a.Runs != 2 || a.Running != 1 || a.Cost != 2 || a.Files != 1 || a.Duration != 180 {
		t.Errorf("/a = %+v", a)
	}
}

// TestTheCSVHasARowPerRunWithItsFiles: the CSV is what goes into a
// spreadsheet, so its columns are fixed and a run's files stay in one cell.
func TestTheCSVHasARowPerRunWithItsFiles(t *testing.T) {
	runs := []*Run{{ID: "r1", Agent: "claude", Started: t0, Ended: t0.Add(90 * time.Second),
		Files: []string{"a.go", "b, c.go"}, Usage: usage.Usage{"m": {Input: 7}}, Cost: 0.5, CostComplete: true,
		PR: &PR{Number: 4, URL: "https://x/pull/4"}}}
	var buf bytes.Buffer
	if err := WriteRunsCSV(&buf, runs, t0); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %v, %v", rows, err)
	}
	row := map[string]string{}
	for i, col := range rows[0] {
		row[col] = rows[1][i]
	}
	want := map[string]string{"run": "r1", "duration_seconds": "90", "files_changed": "2",
		"files": "a.go;b, c.go", "input_tokens": "7", "cost_usd": "0.5000", "pr": "https://x/pull/4"}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s = %q, want %q", k, row[k], v)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestAPlaceAndItsChangesComeFromGit: a worktree is told from the main
// checkout, a branch keeps its whole name, and what changed since a run's
// first commit includes what it committed, what it left uncommitted and what
// it created — while what was there before it started does not count.
func TestAPlaceAndItsChangesComeFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	gitRun(t, root, "init", "-q", "-b", "main", repo)
	os.WriteFile(filepath.Join(repo, "old.txt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(repo, "keep.txt"), []byte("1"), 0o644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")

	main := Inspect(filepath.Join(repo))
	if main.Project != repo || main.Worktree || main.Branch != "main" || main.Head == "" {
		t.Errorf("main checkout = %+v", main)
	}

	wt := filepath.Join(root, "wt")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "feature/x", wt)
	start := Inspect(wt)
	if start.Project != repo || !start.Worktree || start.Branch != "feature/x" {
		t.Errorf("worktree = %+v", start)
	}

	os.WriteFile(filepath.Join(wt, "old.txt"), []byte("2"), 0o644)
	gitRun(t, wt, "commit", "-q", "-am", "two")
	os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n"), 0o644)
	files, commits := Changes(start)
	if want := []string{"keep.txt", "new.txt", "old.txt"}; !reflect.DeepEqual(files, want) || commits != 1 {
		t.Errorf("changes = %v, %d commits; want %v, 1", files, commits, want)
	}

	if p := Inspect(t.TempDir()); p.Project != "" || p.Dir == "" {
		t.Errorf("outside a repository = %+v", p)
	}
}

// TestTheTableSaysWhatIsGoingOn: running runs say so, an unknown end is a
// question mark rather than a made-up length, and a cost with an unpriced
// model is a lower bound.
func TestTheTableSaysWhatIsGoingOn(t *testing.T) {
	runs := []*Run{
		{Agent: "claude", Started: t0, Running: true, Cost: 1.5, CostComplete: true, Place: Place{Project: "/src/app", Branch: "issue-3-x"}, For: "issue #3"},
		{Agent: "codex", Started: t0, Reason: ReasonLost, CostComplete: false},
	}
	var buf bytes.Buffer
	if err := WriteRunsTable(&buf, runs, t0.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"running", "5m", "issue #3", "app", "$1.50", "?", ReasonLost, "≥$1.50"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}
