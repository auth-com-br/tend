package core

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// About and what's new: the TUI's about panel (cmd/tend/tui_version.go)
// and release notes (cmd/tend/tui_notes.go). About says which build the
// window is and which the server is — the server outlives its clients, and
// one left behind by an update is the first thing to know when something
// is missing — with where tend lives. What's new is the notes the server
// read, of the release running or of a newer one ready, told to the server
// as read when the screen closes, as herdr does.

// Version is this window's build, which main sets from the build's flags.
var Version = "development build"

// AboutState is the about screen.
type AboutState struct {
	Version string `json:"version"`
	Server  string `json:"server,omitempty"`
	Stale   bool   `json:"stale,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Notes   bool   `json:"notes,omitempty"`
	Site    string `json:"site"`
	Source  string `json:"source"`
	Sponsor string `json:"sponsor"`
}

// NotesState is the release notes: Body is markdown.
type NotesState struct {
	Version string `json:"version"`
	Body    string `json:"body"`
	Newer   bool   `json:"newer,omitempty"`
	Install string `json:"install,omitempty"`
	Loading bool   `json:"loading,omitempty"`
	Error   string `json:"error,omitempty"`
}

// serverStaleLocked is whether the server is another build than this
// window. The caller holds the lock.
func (s *Session) serverStaleLocked() bool {
	server := s.client.Server().Build
	return server != "" && ui.VersionLine(Version, server) != ui.VersionLine(Version, "")
}

// notesAvailableLocked is whether the server has release notes to show.
func (s *Session) notesAvailableLocked() bool {
	return s.snap.Update != nil && s.snap.Update.Notes != ""
}

func (s *Session) aboutLocked() *AboutState {
	return &AboutState{
		Version: ui.DisplayVersion(Version),
		Server:  ui.DisplayVersion(s.client.Server().Build),
		Stale:   s.serverStaleLocked(),
		Hint:    "run " + tools.HandoffCommand(s.name),
		Notes:   s.notesAvailableLocked(),
		Site:    ui.SiteURL,
		Source:  ui.SourceURL,
		Sponsor: ui.SponsorURL,
	}
}

// loadNotes reads the release notes from the server.
func (s *Session) loadNotes() {
	s.mu.Lock()
	c := s.client
	s.tools.notes = &NotesState{Loading: true}
	n := s.tools.notes
	s.mu.Unlock()
	s.stateChanged()
	notes, err := c.ReleaseNotes()
	s.mu.Lock()
	if s.tools.notes == n {
		if err != nil {
			n.Loading, n.Error = false, tools.ErrorText(err)
		} else {
			*n = NotesState{Version: notes.Version, Body: notes.Body, Newer: notes.Newer, Install: notes.Install}
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

// CloseNotes takes the release notes down and tells the server they were
// read, which it keeps for the notes of the release running only.
func (s *Session) CloseNotes() error {
	s.mu.Lock()
	n, c := s.tools.notes, s.client
	s.mu.Unlock()
	s.OpenPanel("")
	if n == nil || n.Version == "" || !c.Supports(proto.MethodReleaseNotesDismiss) {
		return nil
	}
	if err := c.DismissReleaseNotes(n.Version); err != nil {
		return err
	}
	return nil
}
