package server

import (
	"encoding/json"
	"testing"

	"github.com/auth-com-br/tend/internal/session"
)

// shape is a tree's directions and leaf count, without ids or sizes.
func shape(n session.LayoutSnapshot) string {
	if len(n.Kids) == 0 {
		return "p"
	}
	out := n.Dir + "("
	for i, k := range n.Kids {
		if i > 0 {
			out += " "
		}
		out += shape(k)
	}
	return out + ")"
}

// TestANestedLayoutIsBuiltInItsOwnShape: an editor beside two panes one
// above the other is a column holding rows. If it regresses, applying it
// lays all three side by side, and the sizes cannot be copied on.
func TestANestedLayoutIsBuiltInItsOwnShape(t *testing.T) {
	s := newServer(t)
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	tree := session.LayoutSnapshot{Dir: "columns", Sizes: []float64{0.6, 0.4}, Kids: []session.LayoutSnapshot{
		{Pane: 1},
		{Dir: "rows", Sizes: []float64{0.5, 0.5}, Kids: []session.LayoutSnapshot{{Pane: 2}, {Pane: 3}}},
	}}
	specs := map[uint64]PaneSpec{}
	for _, id := range []uint64{1, 2, 3} {
		specs[id] = shell("exec cat")
	}
	tab, err := s.ApplyLayout(ws, "nested", tree, specs, shell("exec cat"))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.ExportLayout(tab)
	if err != nil {
		t.Fatal(err)
	}
	if shape(got) != shape(tree) {
		raw, _ := json.Marshal(got)
		t.Fatalf("built %s, want %s (%s)", shape(got), shape(tree), raw)
	}
	if len(got.Sizes) != 2 || got.Sizes[0] < 0.59 || got.Sizes[0] > 0.61 {
		t.Fatalf("sizes = %v, want 0.6 and 0.4", got.Sizes)
	}
}

// TestAPaneOfALayoutWithNoCommandIsAShellInItsDirectory: a preset's pane
// that says only where it is. If it regresses, it opens wherever the server
// was started.
func TestAPaneOfALayoutWithNoCommandIsAShellInItsDirectory(t *testing.T) {
	s := newServer(t)
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tree := session.LayoutSnapshot{Pane: 1}
	tab, err := s.ApplyLayout(ws, "here", tree, map[uint64]PaneSpec{1: {Dir: dir}}, shell("pwd; exec cat"))
	if err != nil {
		t.Fatal(err)
	}
	_, panes, err := s.ExportLayout(tab)
	if err != nil || len(panes) != 1 {
		t.Fatalf("panes = %+v, %v", panes, err)
	}
	waitFor(t, "the shell in its directory", func() bool { return screenHas(s, session.PaneID(panes[0].ID), dir) })
}
