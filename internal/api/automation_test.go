package api

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
)

// result digs the result object out of a reply, failing on an error reply.
func result(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	if body, bad := reply["error"].(map[string]any); bad {
		t.Fatalf("call failed: %v", body)
	}
	out, _ := reply["result"].(map[string]any)
	return out
}

func call(t *testing.T, h *harness, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": method, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return h.call(t, string(raw))
}

// TestAScriptCanDriveAPaneAndReadItBack is what the automation half is for:
// send something, wait for the pane to answer, read the answer. If it
// regresses, nothing written against tend from the outside works.
func TestAScriptCanDriveAPaneAndReadItBack(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// The pane is running "sleep 30", so give it a shell of its own to talk to.
	res := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": pane, "direction": "down", "command": []string{"/bin/sh"},
	}))
	shell, _ := res["pane"].(map[string]any)
	id, _ := shell["pane_id"].(string)
	if id == "" {
		t.Fatalf("split returned %v", res)
	}

	// The output differs from the line typed (%s), so the wait is for the
	// shell running it, not for the terminal echoing it. Waiting on the echo
	// went on before the shell had started, and under load the ctrl+c below
	// reached a shell not yet ignoring SIGINT and killed it.
	call(t, h, MethodPaneSendText, map[string]any{
		"pane_id": id, "text": "printf 'hello-from-%s\\n' a-script\n",
	})
	res = result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{
		"pane_id": id, "contains": "hello-from-a-script", "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Fatal("the pane never printed what was sent to it")
	}

	res = result(t, call(t, h, MethodPaneRead, map[string]any{"pane_id": id, "source": "recent"}))
	text, _ := res["text"].(string)
	if !strings.Contains(text, "hello-from-a-script") {
		t.Errorf("read = %q, want what the shell printed", text)
	}
	// A read says where the pane is, so a script can walk the session from it.
	if res["workspace_id"] == nil || res["tab_id"] == nil {
		t.Errorf("read did not say where the pane is: %v", res)
	}

	// ctrl+c reaches the program as a signal, not as the letter c.
	call(t, h, MethodPaneSendText, map[string]any{"pane_id": id, "text": "sleep 9"})
	call(t, h, MethodPaneSendKeys, map[string]any{"pane_id": id, "keys": []string{"ctrl+c"}})
	call(t, h, MethodPaneSendText, map[string]any{"pane_id": id, "text": "printf 'after-%s\\n' ctrl-c\n"})
	res = result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{
		"pane_id": id, "contains": "after-ctrl-c", "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		after := result(t, call(t, h, MethodPaneRead, map[string]any{"pane_id": id, "source": "recent"}))
		t.Errorf("the shell never came back after ctrl+c; the pane shows:\n%v", after["text"])
	}
}

// TestTheSessionCanBeWalkedAndChanged: a script has to be able to find its way
// around and make room for itself without a client attached.
func TestTheSessionCanBeWalkedAndChanged(t *testing.T) {
	h := start(t)

	snap := result(t, call(t, h, MethodSessionSnapshot, nil))
	spaces, _ := snap["workspaces"].([]any)
	if len(spaces) != 1 {
		t.Fatalf("snapshot has %d spaces, want 1", len(spaces))
	}

	made := result(t, call(t, h, MethodWorkspaceCreate, map[string]any{"name": "scripted"}))
	ws, _ := made["workspace"].(map[string]any)
	wsID, _ := ws["workspace_id"].(string)
	if wsID == "" {
		t.Fatalf("create returned %v", made)
	}

	made = result(t, call(t, h, MethodTabCreate, map[string]any{
		"workspace_id": wsID, "name": "work", "command": []string{"/bin/sh", "-c", "sleep 20"},
	}))
	tab, _ := made["tab"].(map[string]any)
	tabID, _ := tab["tab_id"].(string)
	if tabID == "" {
		t.Fatalf("tab.create returned %v", made)
	}

	tabs := result(t, call(t, h, MethodTabList, map[string]any{"workspace_id": wsID}))
	if list, _ := tabs["tabs"].([]any); len(list) != 1 {
		t.Errorf("the new space has %d tabs, want 1", len(list))
	}

	call(t, h, MethodWorkspaceClose, map[string]any{"workspace_id": wsID})
	snap = result(t, call(t, h, MethodSessionSnapshot, nil))
	if spaces, _ := snap["workspaces"].([]any); len(spaces) != 1 {
		t.Errorf("after closing, %d spaces remain, want 1", len(spaces))
	}
}

// TestWaitingForAnAgentReportsWhetherItTimedOut: a wait that cannot say
// whether it ended because the agent finished or because time ran out makes
// every script that uses it wrong.
func TestWaitingForAnAgentReportsWhetherItTimedOut(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// Nothing is reporting, so the pane sits in "unknown" and the wait ends on
	// its own clock.
	started := time.Now()
	res := result(t, call(t, h, MethodAgentWait, map[string]any{
		"target": pane, "until": []string{"idle"}, "timeout_ms": 300,
	}))
	if res["timed_out"] != true {
		t.Errorf("wait = %v, want a timeout", res)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the wait took %s, far past its timeout", elapsed)
	}

	// A hook reporting idle is what the wait is watching for. It comes over a
	// second connection: one connection answers one call at a time, so a
	// report sent down the connection that is waiting would arrive after it.
	reporter := h.another(t)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = reporter.send(`{"id":"r","method":"pane.report_agent","params":{"pane_id":"` + pane +
			`","source":"s","agent":"deploy-bot","state":"idle","seq":1}}`)
	}()
	res = result(t, call(t, h, MethodAgentWait, map[string]any{
		"target": pane, "until": []string{"idle"}, "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Fatalf("the wait timed out although idle was reported: %v", res)
	}
	info, _ := res["agent"].(map[string]any)
	if info["agent_state"] != "idle" {
		t.Errorf("wait returned %v", info)
	}

	// And the agent is then findable by name rather than by pane.
	res = result(t, call(t, h, MethodAgentGet, map[string]any{"target": "deploy-bot"}))
	if got, _ := res["agent"].(map[string]any); got["pane_id"] != pane {
		t.Errorf("agent.get by name returned %v", got)
	}
}

// TestBadTargetsAreNamedNotGuessed: a prompt sent to the wrong agent cannot be
// taken back, so an ambiguous name is an error.
func TestBadTargetsAreNamedNotGuessed(t *testing.T) {
	h := start(t)
	if code := errorCode(call(t, h, MethodAgentPrompt, map[string]any{
		"target": "nobody", "text": "hi",
	})); code != "agent_not_found" {
		t.Errorf("code = %q, want agent_not_found", code)
	}
	if code := errorCode(call(t, h, MethodPaneSendKeys, map[string]any{
		"pane_id": PaneID(h.pane), "keys": []string{"chorus"},
	})); code != "invalid_key" {
		t.Errorf("code = %q, want invalid_key", code)
	}
}

// TestAWorktreeRootIsNeverRelative: with no directory configured, a new
// worktree was created as "project/branch" relative to the repository — inside
// the checkout it was meant to sit beside. Whatever the setting says, git must
// be handed an absolute path.
func TestAWorktreeRootIsNeverRelative(t *testing.T) {
	for _, setting := range []string{"", "relative/dir", "~/elsewhere", "/abs/dir"} {
		a := &API{worktreeDir: setting}
		if root := a.worktreeRoot(); !filepath.IsAbs(root) {
			t.Errorf("setting %q gave the root %q", setting, root)
		}
	}
}

// TestSubscribingFollowsTheSessionWithoutGaps is what events.wait cannot do:
// a caller that acts on every event needs them in order, and a call that
// answers one and returns loses whatever happened before the next call.
func TestSubscribingFollowsTheSessionWithoutGaps(t *testing.T) {
	h := start(t)
	watcher := h.another(t)

	res := result(t, call(t, watcher, MethodEventsSubscribe, map[string]any{
		"kinds": []string{"pane.opened", "pane.closed"},
	}))
	if res["type"] != "subscribed" {
		t.Fatalf("subscribe = %v", res)
	}

	// Three panes in a row, on another connection. A caller that answered one
	// event at a time would see the first and miss the rest.
	var ids []string
	for i := 0; i < 3; i++ {
		made := result(t, call(t, h, MethodPaneSplit, map[string]any{
			"pane_id": PaneID(h.pane), "command": []string{"/bin/sh", "-c", "sleep 30"},
		}))
		pane, _ := made["pane"].(map[string]any)
		ids = append(ids, text(pane["pane_id"]))
	}

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		ev := watcher.next(t)
		// Asked for by tend's older name, named by herdr's.
		if ev["event"] != "pane.created" {
			t.Fatalf("event %d = %v, want pane.created", i, ev)
		}
		seen[text(ev["pane_id"])] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("the stream missed %s; it saw %v", id, seen)
		}
	}

	// Only what was asked for: a state change is not one of the two kinds.
	call(t, h, MethodPaneClose, map[string]any{"pane_id": ids[0]})
	if ev := watcher.next(t); ev["event"] != "pane.closed" {
		t.Errorf("after closing a pane the stream said %v", ev)
	}
}

// text is the string at a key, for reading a result.
func text(v any) string {
	s, _ := v.(string)
	return s
}

// TestALayoutCanBeSavedAndBuiltAgain: an arrangement somebody set up — panes
// in a shape, each in its own directory — is worth having back tomorrow. If
// it regresses, setting up a session is hand work every time.
func TestALayoutCanBeSavedAndBuiltAgain(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)

	// Two more panes, so the shape is worth exporting.
	for i := 0; i < 2; i++ {
		result(t, call(t, h, MethodPaneSplit, map[string]any{
			"pane_id": pane, "direction": "down", "command": []string{"/bin/sh", "-c", "sleep 30"},
		}))
	}

	tabs := result(t, call(t, h, MethodTabList, nil))
	list, _ := tabs["tabs"].([]any)
	first, _ := list[0].(map[string]any)
	tabID := text(first["tab_id"])

	exported := result(t, call(t, h, MethodLayoutExport, map[string]any{"tab_id": tabID}))
	if exported["tree"] == nil {
		t.Fatalf("export = %v", exported)
	}
	panes, _ := exported["panes"].([]any)
	if len(panes) != 3 {
		t.Fatalf("the exported layout has %d panes, want 3", len(panes))
	}

	applied := result(t, call(t, h, MethodLayoutApply, map[string]any{
		"name": "rebuilt", "tree": exported["tree"], "panes": exported["panes"],
	}))
	if applied["type"] != "layout_applied" {
		t.Fatalf("apply = %v", applied)
	}

	// The new tab has the same number of panes, in a space of its own.
	rebuilt := result(t, call(t, h, MethodTabList, map[string]any{
		"workspace_id": text(applied["workspace_id"]),
	}))
	got, _ := rebuilt["tabs"].([]any)
	if len(got) != 1 {
		t.Fatalf("the new space has %d tabs", len(got))
	}
	tab, _ := got[0].(map[string]any)
	inPanes, _ := tab["panes"].([]any)
	if len(inPanes) != 3 {
		t.Errorf("the rebuilt tab has %d panes, want 3", len(inPanes))
	}
}

// TestMovingAPaneToAnotherTabDoesNotRestartIt: moving a running agent must
// leave it running. Closing and reopening would lose whatever it was doing,
// which is the reason to move rather than reopen.
func TestMovingAPaneToAnotherTabDoesNotRestartIt(t *testing.T) {
	h := start(t)

	// A pane whose process can be recognised afterwards.
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": PaneID(h.pane), "command": []string{"/bin/sh", "-c", "sleep 30"},
	}))
	pane, _ := made["pane"].(map[string]any)
	moving := text(pane["pane_id"])
	pid := pane["pid"]

	spaces := result(t, call(t, h, MethodWorkspaceCreate, map[string]any{"name": "elsewhere"}))
	ws, _ := spaces["workspace"].(map[string]any)
	made = result(t, call(t, h, MethodTabCreate, map[string]any{
		"workspace_id": text(ws["workspace_id"]), "name": "there",
		"command": []string{"/bin/sh", "-c", "sleep 30"},
	}))
	root, _ := made["root_pane"].(map[string]any)

	if res := result(t, call(t, h, MethodPaneMove, map[string]any{
		"pane_id": moving, "target_pane_id": text(root["pane_id"]), "direction": "down",
	})); res["type"] != "ok" {
		t.Fatalf("move = %v", res)
	}

	got := result(t, call(t, h, MethodPaneGet, map[string]any{"pane_id": moving}))
	info, _ := got["pane"].(map[string]any)
	if info["pid"] != pid {
		t.Errorf("the pane's process changed: %v, was %v", info["pid"], pid)
	}
	tabs := result(t, call(t, h, MethodTabList, map[string]any{"workspace_id": text(ws["workspace_id"])}))
	list, _ := tabs["tabs"].([]any)
	tab, _ := list[0].(map[string]any)
	panes, _ := tab["panes"].([]any)
	if len(panes) != 2 {
		t.Errorf("the tab it moved to has %d panes, want 2", len(panes))
	}
}

// TestExplainingWhyAnAgentIsShownAsItIs: "why does it say that?" is the
// question detection raises most often, and guessing at a screen that has
// since changed is the alternative.
func TestExplainingWhyAnAgentIsShownAsItIs(t *testing.T) {
	h := start(t)

	// A pane that looks like claude waiting for an answer, so a rule fires.
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": PaneID(h.pane), "agent": "claude",
		"command": []string{"/bin/sh", "-c", "printf 'Do you want to proceed?\\n  1. Yes\\n'; sleep 30"},
	}))
	pane, _ := made["pane"].(map[string]any)
	id := text(pane["pane_id"])

	deadline := time.Now().Add(5 * time.Second)
	var explain map[string]any
	for time.Now().Before(deadline) {
		res := result(t, call(t, h, MethodAgentExplain, map[string]any{"target": id, "screen": true}))
		explain, _ = res["explain"].(map[string]any)
		if text(explain["matched_rule"]) != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if text(explain["matched_rule"]) == "" {
		t.Fatalf("nothing explains the state: %v", explain)
	}
	if text(explain["source"]) != "screen" {
		t.Errorf("source = %q, want the screen", explain["source"])
	}
	rules, _ := explain["evaluated_rules"].([]any)
	if len(rules) < 2 {
		t.Errorf("only %d rules were reported; the point is seeing them all", len(rules))
	}
	if !strings.Contains(text(explain["screen"]), "Do you want to proceed?") {
		t.Errorf("the screen detection read was not reported: %q", explain["screen"])
	}

	// A hook answering for the pane says so, rather than naming a rule that
	// had nothing to do with it.
	h.call(t, `{"id":"r","method":"pane.report_agent","params":{"pane_id":"`+id+
		`","source":"my-hook","agent":"claude","state":"working","seq":1}}`)
	res := result(t, call(t, h, MethodAgentExplain, map[string]any{"target": id}))
	explain, _ = res["explain"].(map[string]any)
	if text(explain["source"]) != "hook:my-hook" {
		t.Errorf("source = %q, want the hook", explain["source"])
	}
}

// TestAPluginCanFindItsWayAroundATab: a plugin that docks a panel on the edge
// of a tab needs to know which pane is on the edge and what is beside what;
// one that reacts to what is running needs to know what that is.
func TestAPluginCanFindItsWayAroundATab(t *testing.T) {
	h := start(t)
	left := PaneID(h.pane)
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": left, "direction": "right", "command": []string{"/bin/sh", "-c", "sleep 30"},
	}))
	pane, _ := made["pane"].(map[string]any)
	right := text(pane["pane_id"])

	n := result(t, call(t, h, MethodPaneNeighbor, map[string]any{"pane_id": left, "direction": "right"}))
	if text(n["neighbor_pane_id"]) != right {
		t.Errorf("the pane right of %s is %v, want %s", left, n["neighbor_pane_id"], right)
	}
	n = result(t, call(t, h, MethodPaneNeighbor, map[string]any{"pane_id": left, "direction": "left"}))
	if _, has := n["neighbor_pane_id"]; has {
		t.Errorf("a pane on the tab's left edge has a neighbour to its left: %v", n)
	}

	e := result(t, call(t, h, MethodPaneEdges, map[string]any{"pane_id": left}))
	if e["left"] != true || e["right"] != false || e["up"] != true || e["down"] != true {
		t.Errorf("edges of the left pane = %v", e)
	}

	info := result(t, call(t, h, MethodPaneProcesses, map[string]any{"pane_id": right}))
	proc, _ := info["process"].(map[string]any)
	if proc["shell_pid"] == nil || proc["shell_pid"] == float64(0) {
		t.Errorf("process info = %v", proc)
	}
}

// TestAnAgentCanBeGivenAName is herdr's agent.rename: a name addresses the
// agent when its own label does not say which pane is meant, a bad or taken
// name is refused with herdr's codes, and no name takes it away. If it
// regresses, a script driving two claudes cannot tell them apart.
func TestAnAgentCanBeGivenAName(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)
	result(t, call(t, h, "pane.report_agent", map[string]any{
		"pane_id": pane, "source": "s", "agent": "claude", "state": "idle", "seq": 1,
	}))

	res := result(t, call(t, h, MethodAgentRename, map[string]any{"target": pane, "name": "reviewer"}))
	if got, _ := res["agent"].(map[string]any); got["name"] != "reviewer" {
		t.Errorf("rename returned %v", got)
	}
	res = result(t, call(t, h, MethodAgentGet, map[string]any{"target": "reviewer"}))
	if got, _ := res["agent"].(map[string]any); got["pane_id"] != pane {
		t.Errorf("agent.get by the given name returned %v", got)
	}

	if code := errorCode(call(t, h, MethodAgentRename, map[string]any{"target": pane, "name": "Bad Name"})); code != "invalid_agent_name" {
		t.Errorf("a bad name gave %q", code)
	}
	other := result(t, call(t, h, MethodPaneSplit, map[string]any{"pane_id": pane, "direction": "down", "command": []string{"sleep", "30"}}))
	otherPane, _ := other["pane"].(map[string]any)
	otherID, _ := otherPane["pane_id"].(string)
	if code := errorCode(call(t, h, MethodAgentRename, map[string]any{"target": otherID, "name": "x"})); code != "not_an_agent" {
		t.Errorf("naming a pane with no agent gave %q", code)
	}
	result(t, call(t, h, "pane.report_agent", map[string]any{
		"pane_id": otherID, "source": "s", "agent": "claude", "state": "idle", "seq": 1,
	}))
	if code := errorCode(call(t, h, MethodAgentRename, map[string]any{"target": otherID, "name": "reviewer"})); code != "duplicate_agent_name" {
		t.Errorf("a taken name gave %q", code)
	}

	res = result(t, call(t, h, MethodAgentRename, map[string]any{"target": "reviewer"}))
	if got, _ := res["agent"].(map[string]any); got["name"] != nil {
		t.Errorf("no name should take it away, got %v", got)
	}
}

// TestHerdrsLifecycleEventsAreSent: renaming and closing are announced by
// herdr's names, which is what a plugin written for herdr hooks on. If it
// regresses, such a plugin installs, loads, and never runs.
func TestHerdrsLifecycleEventsAreSent(t *testing.T) {
	h := start(t)
	watcher := h.another(t)
	res := result(t, call(t, watcher, MethodEventsSubscribe, map[string]any{
		"kinds": []string{"tab.renamed", "tab.closed", "workspace.renamed"},
	}))
	if res["type"] != "subscribed" {
		t.Fatalf("subscribe = %v", res)
	}
	made := result(t, call(t, h, "tab.create", map[string]any{"workspace_id": "w_1", "command": []string{"sleep", "30"}}))
	tab, _ := made["tab"].(map[string]any)
	tabID := text(tab["tab_id"])
	result(t, call(t, h, "tab.rename", map[string]any{"tab_id": tabID, "name": "build"}))
	result(t, call(t, h, "tab.close", map[string]any{"tab_id": tabID}))

	for _, want := range []string{"tab.renamed", "tab.closed"} {
		if ev := watcher.next(t); ev["event"] != want || ev["tab_id"] != tabID {
			t.Fatalf("event = %v, want %s for %s", ev, want, tabID)
		}
	}
}

// TestTheMethodsAHerdrPluginCalls: pane.layout, pane.send_input,
// pane.rename, pane.focus and tab.focus are what herdr's plugins use to dock
// a panel beside the work (the owner's herdr-sidebar calls all five). If one
// regresses, such a plugin fails on its first call.
func TestTheMethodsAHerdrPluginCalls(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)
	made := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": pane, "direction": "right", "command": []string{"/bin/sh"},
	}))
	shell, _ := made["pane"].(map[string]any)
	other := text(shell["pane_id"])

	layout := result(t, call(t, h, MethodPaneLayout, map[string]any{"pane_id": pane}))
	l, _ := layout["layout"].(map[string]any)
	panes, _ := l["panes"].([]any)
	splits, _ := l["splits"].([]any)
	if len(panes) != 2 || len(splits) != 1 {
		t.Fatalf("layout = %v, want two panes and one split", l)
	}
	if sp, _ := splits[0].(map[string]any); sp["direction"] != "right" {
		t.Errorf("split = %v, want right", sp)
	}

	result(t, call(t, h, MethodPaneSendInput, map[string]any{
		"pane_id": other, "text": "echo $((6*7))-ran", "keys": []string{"Enter"},
	}))
	res := result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{
		// Only the command's output says 42: the echoed line says $((6*7)).
		"pane_id": other, "contains": "42-ran", "timeout_ms": 5000,
	}))
	if res["timed_out"] == true {
		t.Errorf("text and keys did not both arrive: %v", res)
	}

	renamed := result(t, call(t, h, MethodPaneRename, map[string]any{"pane_id": other, "label": "sidebar"}))
	if info, _ := renamed["pane"].(map[string]any); info["title"] != "sidebar" {
		t.Errorf("rename returned %v", renamed)
	}

	focus := result(t, call(t, h, MethodPaneFocus, map[string]any{"pane_id": other}))
	if focus["type"] != "focus_requested" || focus["pane_id"] != other {
		t.Errorf("pane.focus = %v", focus)
	}
	tabs := result(t, call(t, h, MethodTabFocus, map[string]any{"tab_id": "t_1"}))
	if tabs["type"] != "focus_requested" {
		t.Errorf("tab.focus = %v", tabs)
	}
}

// TestAScriptCanSaySomethingAboutASpace is herdr's
// workspace.report_metadata: a value about a space reaches the snapshot the
// sidebar draws its $name tokens from, and one said for a while goes. If it
// regresses, a space row configured with $jj_status is always empty.
func TestAScriptCanSaySomethingAboutASpace(t *testing.T) {
	h := start(t)
	result(t, call(t, h, MethodWorkspaceReportMetadata, map[string]any{
		"workspace_id": "w_1", "source": "jj", "tokens": map[string]any{"jj_status": "clean"},
	}))
	result(t, call(t, h, MethodWorkspaceReportMetadata, map[string]any{
		"workspace_id": "w_1", "source": "ci", "tokens": map[string]any{"ci": "green"}, "ttl_ms": 50,
	}))
	tokens := func() string {
		snap := h.srv.Snapshot()
		out := ""
		for _, tok := range snap.Workspaces[0].Tokens {
			out += tok.Key + "=" + tok.Value + ";"
		}
		return out
	}
	if got := tokens(); got != "ci=green;jj_status=clean;" {
		t.Errorf("tokens = %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for tokens() != "jj_status=clean;" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := tokens(); got != "jj_status=clean;" {
		t.Errorf("after the ttl, tokens = %q", got)
	}
	if code := errorCode(call(t, h, MethodWorkspaceReportMetadata, map[string]any{
		"workspace_id": "w_99", "source": "x", "tokens": map[string]any{"a": "b"},
	})); code != "workspace_not_found" {
		t.Errorf("an unknown space gave %q", code)
	}
}

// TestAScriptCanPutAViewOnTheAgentList is herdr's agent.view.set and
// agent.view.clear: the view goes into the session clients draw from, and a
// clear naming another source leaves it in place. If it regresses, one
// plugin's clear wipes another's view, or a set changes nothing.
func TestAScriptCanPutAViewOnTheAgentList(t *testing.T) {
	h := start(t)
	res := result(t, call(t, h, MethodAgentViewSet, map[string]any{
		"source": "triage", "label": "waiting",
		"filter": map[string]any{"op": "eq", "field": "status", "value": "blocked"},
	}))
	if res["active"] != true || res["source"] != "triage" {
		t.Fatalf("set = %v", res)
	}
	if v := h.srv.Snapshot().AgentView; v == nil || v.Label != "waiting" || v.Filter == nil {
		t.Fatalf("snapshot view = %+v", v)
	}
	if res := result(t, call(t, h, MethodAgentViewClear, map[string]any{"source": "someone-else"})); res["active"] != true {
		t.Error("a clear by another source should leave the view")
	}
	if res := result(t, call(t, h, MethodAgentViewClear, map[string]any{"source": "triage"})); res["active"] != false {
		t.Error("a clear by its own source should take it away")
	}
	if code := errorCode(call(t, h, MethodAgentViewSet, map[string]any{
		"source": "x", "filter": map[string]any{"op": "like"},
	})); code != "invalid_agent_view" {
		t.Errorf("a bad view gave %q", code)
	}
}

// TestPaneListSaysWhereEachPaneIsAndWhereItsProgramIs: pane.list carries
// herdr's workspace_id, tab_id, focused, cwd and foreground_cwd, the live
// directory being where the program has cd'd to. If it regresses, the files
// panel cannot follow the pane beside it.
func TestPaneListSaysWhereEachPaneIsAndWhereItsProgramIs(t *testing.T) {
	h := start(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := result(t, call(t, h, MethodPaneSplit, map[string]any{
		"pane_id": PaneID(h.pane), "direction": "down", "command": []string{"/bin/sh"},
	}))
	shell, _ := res["pane"].(map[string]any)
	id, _ := shell["pane_id"].(string)
	// Waited for by what only the output says: the command as typed echoes
	// "moved-%s" before the cd has run, and matching that raced it.
	call(t, h, MethodPaneSendText, map[string]any{"pane_id": id, "text": "cd " + dir + " && printf 'moved-%s\\n' 2\n"})
	result(t, call(t, h, MethodPaneWaitForOutput, map[string]any{"pane_id": id, "contains": "moved-2", "timeout_ms": 5000}))
	h.srv.FocusPane(parsePane(t, id), 0)

	list := result(t, call(t, h, MethodPaneList, nil))
	panes, _ := list["panes"].([]any)
	var got map[string]any
	for _, p := range panes {
		if m, _ := p.(map[string]any); m["pane_id"] == id {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("pane %s not listed: %v", id, panes)
	}
	if got["foreground_cwd"] != dir {
		t.Errorf("foreground_cwd = %v, want %s", got["foreground_cwd"], dir)
	}
	if got["focused"] != true || got["tab_id"] == nil || got["workspace_id"] == nil {
		t.Errorf("place and focus: %v", got)
	}
}

func parsePane(t *testing.T, id string) session.PaneID {
	t.Helper()
	n, ok := parseID("p_", id)
	if !ok {
		t.Fatalf("pane id %q", id)
	}
	return session.PaneID(n)
}

// TestTheMethodsHerdrHasForWhereAPaneIs: pane.current answers a script with
// its own pane (or the one focused), agent.focus shows an agent's pane, and
// pane.scroll asks the clients to show a pane scrolled back. If one
// regresses, an agent driving tend cannot find itself, or show the person
// the agent it started and what it said.
func TestTheMethodsHerdrHasForWhereAPaneIs(t *testing.T) {
	h := start(t)
	pane := PaneID(h.pane)
	sub := h.srv.Subscribe(64)
	defer sub.Close()

	cur := result(t, call(t, h, MethodPaneCurrent, map[string]any{"caller_pane_id": pane}))
	if info, _ := cur["pane"].(map[string]any); cur["type"] != "pane_current" || info["pane_id"] != pane {
		t.Fatalf("pane.current = %v", cur)
	}

	focused := result(t, call(t, h, MethodAgentFocus, map[string]any{"target": pane}))
	if focused["type"] != "agent_info" {
		t.Fatalf("agent.focus = %v", focused)
	}
	scrolled := result(t, call(t, h, MethodPaneScroll, map[string]any{"pane_id": pane, "offset_from_bottom": 30}))
	if scrolled["type"] != "pane_info" {
		t.Fatalf("pane.scroll = %v", scrolled)
	}

	var sawFocus, sawScroll bool
	deadline := time.After(5 * time.Second)
	for !sawFocus || !sawScroll {
		select {
		case ev := <-sub.C:
			if ev.Pane != h.pane {
				continue
			}
			switch ev.Kind {
			case server.EventFocusRequest:
				sawFocus = true
			case server.EventScrollRequest:
				if ev.Offset != 30 {
					t.Fatalf("scroll asked for offset %d, want 30", ev.Offset)
				}
				sawScroll = true
			}
		case <-deadline:
			t.Fatalf("the clients were not asked: focus %v, scroll %v", sawFocus, sawScroll)
		}
	}
}
