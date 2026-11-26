package core

import (
	"slices"
	"testing"
)

// TestTheGlobalMenuOpensTheKeysAndThePhoneView: the window's global menu
// offers the TUI's "keybinds" and "phone view..." — once hidden as screens
// the window had not — and they open the window's keys screen and the
// phone view's, which asks the session's server whether the view is on.
// Detaching, which for a window is closing it, stays out. If it regresses,
// the window lacks what the TUI's menu has, or offers it and does nothing.
func TestTheGlobalMenuOpensTheKeysAndThePhoneView(t *testing.T) {
	s := openTest(t)
	s.OpenMenu("global", 0, 0, 0, "", 0, 0)
	items := s.State().Menu.Items
	if !slices.Contains(items, "phone view...") || !slices.Contains(items, "keybinds") {
		t.Fatalf("the global menu: %v", items)
	}
	if slices.Contains(items, "detach") {
		t.Errorf("the window offers to detach: %v", items)
	}
	if err := s.PickMenu(slices.Index(items, "keybinds")); err != nil {
		t.Fatal(err)
	}
	if p := s.State().Panel; p != panelKeys {
		t.Errorf("keybinds opened %q", p)
	}

	s.OpenMenu("global", 0, 0, 0, "", 0, 0)
	if err := s.PickMenu(slices.Index(s.State().Menu.Items, "phone view...")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the server's answer", func() bool {
		p := s.State().Phone
		return p != nil && p.Loaded
	})
	if p := s.State().Phone; p.On || p.Busy != "" {
		t.Errorf("a session that never turned it on: %+v", p)
	}
}
