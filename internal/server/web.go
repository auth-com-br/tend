package server

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/web"
)

// The phone view (tend web) turned on and off from a client: the owner
// asked to switch it from the menu rather than keep a terminal or PM2 for
// it. It stays a process of its own — a client of the session, as it is when
// started by hand — and the server only starts it, says where it is, and
// stops it:
//
//   - web-<session>.on, beside the session's state, says it was turned on;
//     a server that starts and finds it starts the view again.
//   - web-<session>.json is what the view writes once it listens (its pid
//     and addresses), which is how it is found whoever started it — this
//     server, the one before a handoff, or the user by hand.
//   - web-<session>.log is what it said, for when it does not come up.

// webStartTimeout is how long a view has to say it listens.
const webStartTimeout = 6 * time.Second

func (s *Server) webPath(suffix string) string {
	return filepath.Join(s.cfg.WebStateDir, "web-"+s.cfg.SessionName+suffix)
}

// WebStatus is the phone view's state, as web.status answers it.
func (s *Server) WebStatus() proto.WebStatus {
	s.webMu.Lock()
	defer s.webMu.Unlock()
	return s.webStatusLocked("")
}

func (s *Server) webStatusLocked(failure string) proto.WebStatus {
	st := proto.WebStatus{Error: failure}
	if s.cfg.WebStateDir == "" {
		return st
	}
	_, err := os.Stat(s.webPath(".on"))
	st.On = err == nil
	if view, ok := s.webRunning(); ok {
		st.Running = true
		st.URLs = view.URLs
	}
	if creds, err := web.LoadCredentials(web.PasswordPath(s.cfg.WebStateDir, s.cfg.SessionName)); err == nil {
		st.User = creds.User
	}
	return st
}

// webRunning is the view that is running for this session, if one is.
func (s *Server) webRunning() (web.Status, bool) {
	view, err := web.ReadStatus(s.webPath(".json"))
	if err != nil || view.Pid <= 0 || !isWebProcess(view.Pid) {
		return web.Status{}, false
	}
	return view, true
}

// WebStart turns the phone view on: remembered, and started unless one
// already runs. A view that does not come up is said why, from its log.
func (s *Server) WebStart() proto.WebStatus {
	s.webMu.Lock()
	defer s.webMu.Unlock()
	if len(s.cfg.WebCommand) == 0 || s.cfg.WebStateDir == "" {
		return s.webStatusLocked("this server cannot start the phone view; run tend web yourself")
	}
	if err := os.MkdirAll(s.cfg.WebStateDir, 0o700); err != nil {
		return s.webStatusLocked(err.Error())
	}
	if err := os.WriteFile(s.webPath(".on"), []byte("on\n"), 0o600); err != nil {
		return s.webStatusLocked(err.Error())
	}
	return s.webStatusLocked(s.webLaunchLocked())
}

// webLaunchLocked starts the view unless one runs, and waits for it to
// listen; it returns why it did not, or "".
func (s *Server) webLaunchLocked() string {
	if _, ok := s.webRunning(); ok {
		return ""
	}
	logPath := s.webPath(".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err.Error()
	}
	defer logFile.Close()
	cmd := exec.Command(s.cfg.WebCommand[0], s.cfg.WebCommand[1:]...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	// A session of its own: the view outlives a handoff of this server, and
	// the next one finds it by what it wrote.
	detachWeb(cmd)
	if err := cmd.Start(); err != nil {
		return err.Error()
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	deadline := time.After(webStartTimeout)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			return "the phone view stopped: " + lastLines(logPath, 2)
		case <-deadline:
			return "the phone view did not start listening in time; see " + logPath
		case <-tick.C:
			if view, err := web.ReadStatus(s.webPath(".json")); err == nil && view.Pid == cmd.Process.Pid {
				return ""
			}
		}
	}
}

// WebStop turns the phone view off: forgotten, and the running one ended,
// whoever started it.
func (s *Server) WebStop() proto.WebStatus {
	s.webMu.Lock()
	defer s.webMu.Unlock()
	if s.cfg.WebStateDir == "" {
		return s.webStatusLocked("")
	}
	if err := os.Remove(s.webPath(".on")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return s.webStatusLocked(err.Error())
	}
	failure := ""
	if view, ok := s.webRunning(); ok {
		if err := stopWeb(view.Pid); err != nil {
			failure = fmt.Sprintf("the phone view (pid %d) did not stop: %v", view.Pid, err)
		}
	}
	if failure == "" {
		_ = os.Remove(s.webPath(".json"))
	}
	return s.webStatusLocked(failure)
}

// webRestore starts the view again when it was left on.
func (s *Server) webRestore() {
	if len(s.cfg.WebCommand) == 0 || s.cfg.WebStateDir == "" {
		return
	}
	if _, err := os.Stat(s.webPath(".on")); err != nil {
		return
	}
	s.webMu.Lock()
	defer s.webMu.Unlock()
	if failure := s.webLaunchLocked(); failure != "" {
		s.logf("phone view: %s", failure)
	}
}

// lastLines is the end of a log, for saying why a process stopped.
func lastLines(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "no log at " + path
	}
	var lines []string
	for _, l := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			lines = append(lines, string(bytes.TrimSpace(l)))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return "it said nothing"
	}
	return strings.Join(lines, " — ")
}
