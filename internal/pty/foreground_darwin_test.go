//go:build darwin

package pty

import (
	"os"
	"slices"
	"syscall"
	"testing"
)

// TestAScriptIsNamedAfterItselfNotItsInterpreter: macOS names a process after
// the interpreter, Linux after the script. An agent shipped as a script
// (codex is "node /opt/homebrew/bin/codex") must read as the agent, or a Mac
// never sees it start; a shell at its prompt must stay a shell.
func TestAScriptIsNamedAfterItselfNotItsInterpreter(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"node", "/opt/homebrew/bin/codex"}, "codex"},
		{[]string{"node", "--no-warnings", "/usr/local/bin/gemini", "chat"}, "gemini"},
		{[]string{"/bin/sh", "/tmp/x/claude"}, "claude"},
		{[]string{"-zsh"}, ""},
		{[]string{"zsh", "-i"}, ""},
		{[]string{"sh", "-c", "exec claude"}, ""},
	} {
		if got := scriptName(c.argv); got != c.want {
			t.Errorf("scriptName(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
}

// TestTheKernelIsReadForAGroupsMemoryAndArguments reads this test's own
// process, which is real: its group's resident memory is more than nothing,
// and its arguments are its own. If it regresses, sleeping agents (which
// weigh a group's memory) and recognising an agent by its script both go
// blind on a Mac.
func TestTheKernelIsReadForAGroupsMemoryAndArguments(t *testing.T) {
	if mem := GroupMemory(syscall.Getpgrp()); mem <= 0 {
		t.Errorf("GroupMemory of our own group = %d", mem)
	}
	if got := processArgs(os.Getpid()); !slices.Equal(got, os.Args) {
		t.Errorf("processArgs = %q, want %q", got, os.Args)
	}
	if err := SignalGroup(0, syscall.SIGCONT); err == nil {
		t.Error("a signal to group 0 should be refused, not sent to our own group")
	}
}
