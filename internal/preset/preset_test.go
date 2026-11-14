package preset

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, text string) (File, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "work.toml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _, err := Load("work", dir)
	return f, err
}

// TestThePresetInTheDocsBuildsTheShapeItDraws is the example at the top of
// preset.go, word for word: an agent beside two panes one above the other,
// the agent's pane 60% wide. If it regresses, the documented preset opens
// something else, or not at all.
func TestThePresetInTheDocsBuildsTheShapeItDraws(t *testing.T) {
	doc := `
[[space]]
name = "api"
dir = "/code/api"

  [[space.tab]]
  name = "work"
  split = "columns"

    [[space.tab.pane]]
    agent = "claude"
    size = 0.6

    [[space.tab.pane]]
    split = "rows"
      [[space.tab.pane.pane]]
      run = "npm run dev"
      [[space.tab.pane.pane]]
      dir = "server"
`
	f, err := load(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(f.Spaces[0].Tabs[0], f.Spaces[0].Dir)
	if err != nil {
		t.Fatal(err)
	}
	tr := b.Tree
	if tr.Dir != "columns" || len(tr.Kids) != 2 || tr.Kids[1].Dir != "rows" || len(tr.Kids[1].Kids) != 2 {
		t.Fatalf("tree = %+v", tr)
	}
	if len(tr.Sizes) != 2 || tr.Sizes[0] < 0.599 || tr.Sizes[0] > 0.601 {
		t.Fatalf("sizes = %v", tr.Sizes)
	}
	want := []Leaf{
		{Dir: "/code/api", Agent: "claude"},
		{Dir: "/code/api", Run: "npm run dev"},
		{Dir: "/code/api/server"},
	}
	if len(b.Leaves) != 3 {
		t.Fatalf("leaves = %+v", b.Leaves)
	}
	for i, l := range want {
		if b.Leaves[i] != l {
			t.Errorf("pane %d = %+v, want %+v", i+1, b.Leaves[i], l)
		}
	}
	if got := Leaves(tr); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("leaf order = %v", got)
	}
}

// TestATabOfOnePaneSaysWhatItRunsItself: the short form.
func TestATabOfOnePaneSaysWhatItRunsItself(t *testing.T) {
	f, err := load(t, "[[space]]\nname = \"x\"\n[[space.tab]]\nrun = \"htop\"\n")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(f.Spaces[0].Tabs[0], "/w")
	if err != nil || len(b.Leaves) != 1 || b.Leaves[0].Run != "htop" {
		t.Fatalf("built %+v, %v", b, err)
	}
}

// TestAMistakeInAPresetIsSaidNotGuessedAt: a misspelt key, a pane that
// runs two things, a split that is neither way, sizes that leave nothing.
// Each would otherwise open something other than what was written.
func TestAMistakeInAPresetIsSaidNotGuessedAt(t *testing.T) {
	cases := map[string]string{
		"comand":          "[[space]]\n[[space.tab]]\ncomand = \"x\"\n",
		"not both":        "[[space]]\n[[space.tab]]\nrun = \"a\"\nagent = \"claude\"\n",
		"columns or rows": "[[space]]\n[[space.tab]]\nsplit = \"diagonal\"\n[[space.tab.pane]]\n[[space.tab.pane]]\n",
		"leave nothing":   "[[space]]\n[[space.tab]]\n[[space.tab.pane]]\nsize = 0.7\n[[space.tab.pane]]\nsize = 0.5\n[[space.tab.pane]]\n",
		"no [[space]]":    "# nothing yet\n",
	}
	for want, text := range cases {
		f, err := load(t, text)
		if err == nil {
			_, err = Build(f.Spaces[0].Tabs[0], "/w")
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want one saying %q", text, err, want)
		}
	}
}
