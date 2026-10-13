//go:build !linux && !darwin

package pty

import "syscall"

// Foreground has no implementation here yet.
//
// The terminal knows its foreground process group everywhere that has
// job control, but turning a process id into a name is per-platform: Linux
// reads /proc, the BSDs and macOS need sysctl. Returning nothing means
// detection falls back to the command the pane was started with, which is
// right for a pane opened as an agent and wrong only for one opened as a
// shell — a smaller gap than guessing.
func (p *Pty) Foreground() string { return "" }

// ForegroundArgv has no implementation here either, for the same reason.
func (p *Pty) ForegroundArgv() []string { return nil }

// Process is one process in the terminal's foreground group.
type Process struct {
	Pid     int      `json:"pid"`
	Name    string   `json:"name"`
	Command []string `json:"argv,omitempty"`
}

// ProcessInfo is what is known about who runs in the terminal. Off Linux there
// is no /proc to read, so only the shell is known.
type ProcessInfo struct {
	ShellPid        int       `json:"shell_pid"`
	ForegroundGroup int       `json:"foreground_process_group_id,omitempty"`
	TTY             string    `json:"tty,omitempty"`
	Foreground      []Process `json:"foreground_processes,omitempty"`
}

// Processes reports the shell only.
func (p *Pty) Processes() ProcessInfo { return ProcessInfo{ShellPid: p.Pid()} }

// ChildForeground cannot be told here: without the foreground group, a
// pane's agent is taken to be its own process.
func (p *Pty) ChildForeground() int { return 0 }

// SignalGroup has no implementation here.
func SignalGroup(int, syscall.Signal) error { return syscall.ENOSYS }

// GroupMemory cannot be read without /proc.
func GroupMemory(int) int64 { return 0 }
