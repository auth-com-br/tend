package workon

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

// TestTheSocketsNamesAreInternalAPIs: the method names and the space id
// workon sends are the automation socket's. If it regresses, every start
// of work fails with an unknown method or no such space.
func TestTheSocketsNamesAreInternalAPIs(t *testing.T) {
	if methodWorktreeList != api.MethodWorktreeList || methodWorktreeCreate != api.MethodWorktreeCreate || methodWorktreeOpen != api.MethodWorktreeOpen {
		t.Error("the worktree methods differ from internal/api's")
	}
	if got, want := spaceID(12), api.WorkspaceID(session.WorkspaceID(12)); got != want {
		t.Errorf("space id %q, want %q", got, want)
	}
}

// TestAWorktreeIsItsRepositorysProject: a directory in a worktree belongs
// to the repository the worktree was made from, and one in no repository
// is its own project. If it regresses, an error fixed in its worktree
// opens on no server, or the wrong one.
func TestAWorktreeIsItsRepositorysProject(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("needs git: %v %s", err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	tree := filepath.Join(t.TempDir(), "fix-1")
	git("worktree", "add", "-q", "-b", "fix-1", tree)
	sub := filepath.Join(tree, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(repo)
	if got, _ := filepath.EvalSymlinks(ProjectRoot(sub)); got != real {
		t.Errorf("worktree dir's project is %q, want %q", got, real)
	}
	plain := t.TempDir()
	if got := ProjectRoot(plain); got != plain {
		t.Errorf("a folder in no repository is %q, want itself", got)
	}
}

// TestTheAgentIsTheSettingsElseThePanesElseClaude: the agent work starts
// in is the one the settings name, else the pane's when tend can hand it a
// prompt, else claude. If it regresses, work on an issue starts in a shell,
// or ignores the agent the owner chose.
func TestTheAgentIsTheSettingsElseThePanesElseClaude(t *testing.T) {
	for _, c := range []struct{ configured, pane, want string }{
		{"codex", "claude", "codex"},
		{"cursor-agent", "", "cursor"},
		{"", "gemini", "gemini"},
		{"", "bash", "claude"},
		{"", "", "claude"},
	} {
		if got := AgentFor(c.configured, c.pane); got != c.want {
			t.Errorf("AgentFor(%q, %q) = %q, want %q", c.configured, c.pane, got, c.want)
		}
	}
}

// fakeSocket answers the worktree methods as the automation socket does,
// keeping what it was asked.
type fakeSocket struct {
	trees []map[string]any
	asked []string
	tabs  []proto.PaneSpec
}

func (f *fakeSocket) call(method string, params map[string]any) (map[string]any, error) {
	f.asked = append(f.asked, method)
	switch method {
	case api.MethodWorktreeList:
		list := []any{}
		for _, t := range f.trees {
			list = append(list, t)
		}
		return map[string]any{"worktrees": list}, nil
	case api.MethodWorktreeOpen:
		return map[string]any{"workspace": map[string]any{"workspace_id": "w_7"}}, nil
	case api.MethodWorktreeCreate:
		path := "/wt/" + params["branch"].(string)
		f.trees = append(f.trees, map[string]any{"branch": params["branch"], "path": path})
		return map[string]any{"workspace": map[string]any{"workspace_id": "w_9"}, "worktree": map[string]any{"path": path}}, nil
	}
	return nil, nil
}

func (f *fakeSocket) NewTab(ws uint64, name string, spec proto.PaneSpec) (uint64, uint64, error) {
	f.tabs = append(f.tabs, spec)
	return 3, 4, nil
}

// TestWorkOnAnIssueIsBegunOnce: the first start makes a worktree on the
// issue's branch and starts the agent there with the prompt; the second
// finds it and only goes to it. If it regresses, an issue gets two
// worktrees and two agents, or the agent is started without the issue.
func TestWorkOnAnIssueIsBegunOnce(t *testing.T) {
	f := &fakeSocket{}
	got, err := Issue(f.call, f, "/src/shop", 42, "Checkout button is grey", "gemini", "Complete it")
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace != 9 || got.Tab != 3 || got.Message != "#42: gemini on issue-42-checkout-button-is-grey" {
		t.Errorf("first start: %+v", got)
	}
	if len(f.tabs) != 1 || !reflect.DeepEqual(f.tabs[0].Command[4:], []string{"gemini", "-i", "Complete it"}) ||
		f.tabs[0].Dir != "/wt/issue-42-checkout-button-is-grey" || f.tabs[0].Agent != "gemini" {
		t.Errorf("the agent's tab: %+v", f.tabs)
	}
	again, err := Issue(f.call, f, "/src/shop", 42, "Checkout button is grey", "gemini", "Complete it")
	if err != nil {
		t.Fatal(err)
	}
	if again.Workspace != 7 || again.Tab != 0 || !strings.Contains(again.Message, "already has a worktree") || len(f.tabs) != 1 {
		t.Errorf("second start: %+v, tabs %d", again, len(f.tabs))
	}
}

// TestAFixIsMadeInTheSpacesRepository: a fix names the space, not a
// folder, and an existing branch of that name is gone to. If it regresses,
// an error's fix is made in the wrong repository, or twice.
func TestAFixIsMadeInTheSpacesRepository(t *testing.T) {
	f := &fakeSocket{trees: []map[string]any{{"branch": "fix-shop-1", "path": "/wt/fix-shop-1"}}}
	got, err := Fix(f.call, f, 5, "fix-shop-1", "SHOP-1", "claude", "fix it")
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace != 7 || got.Message != "SHOP-1 already has a worktree: fix-shop-1" || len(f.tabs) != 0 {
		t.Errorf("an existing fix: %+v", got)
	}
	got, err = Fix(f.call, f, 5, "ticket-eng-2", "ENG-2", "claude", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspace != 9 || got.Tab != 3 || got.Message != "ENG-2: claude on ticket-eng-2" {
		t.Errorf("a new fix: %+v", got)
	}
}
