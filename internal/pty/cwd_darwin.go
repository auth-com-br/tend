//go:build darwin

package pty

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// The kernel's proc_info call with PROC_PIDVNODEPATHINFO, which is what
// libproc's proc_pidinfo — and so lsof — use to read a process's current
// directory. It is a system call, so no cgo; the layout is <sys/proc_info.h>:
// struct proc_vnodepathinfo is two vnode_info_path (current directory, then
// root), each a 152-byte vnode_info followed by a MAXPATHLEN path.
const (
	procInfoCallPidInfo   = 2
	procPidVnodePathInfo  = 9
	vnodeInfoSize         = 152
	maxPathLen            = 1024
	procVnodePathInfoSize = 2 * (vnodeInfoSize + maxPathLen)
)

// Cwd returns the directory the pane's process is in now, or "" when it cannot
// be told. It is what a restored pane is started in, so that coming back means
// coming back to where the work was rather than to where the shell was opened.
func (p *Pty) Cwd() string { return processCwd(p.Pid()) }

// FollowCwd is where a new terminal opened from this pane should start:
// herdr's follow_cwd, the directory of the program in charge of the terminal
// now, and the shell's when that cannot be read (see cwd_linux.go).
func (p *Pty) FollowCwd() string {
	if p.f != nil {
		if pgrp, err := foregroundGroup(p.f); err == nil && pgrp > 0 {
			if dir := processCwd(pgrp); dir != "" {
				return dir
			}
		}
	}
	return p.Cwd()
}

// processCwd is a process's current directory, or "" for one that is gone or
// not this user's.
func processCwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	var buf [procVnodePathInfoSize]byte
	n, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, procInfoCallPidInfo, uintptr(pid),
		procPidVnodePathInfo, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 || n != procVnodePathInfoSize {
		return ""
	}
	dir := cString(buf[vnodeInfoSize : vnodeInfoSize+maxPathLen])
	if !filepath.IsAbs(dir) {
		return ""
	}
	return dir
}
