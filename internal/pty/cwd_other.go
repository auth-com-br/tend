//go:build !linux && !darwin

package pty

// Cwd is not known on this platform, and a restored pane starts where the
// original was opened.
func (p *Pty) Cwd() string { return "" }

// FollowCwd is Cwd here: with no /proc there is neither.
func (p *Pty) FollowCwd() string { return p.Cwd() }
