package server

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/update"
)

// The release check is herdr's auto_update (`update.rs`, `app/mod.rs`): the
// server looks for a newer release when it starts and every half hour after,
// saves the release's notes, and tells every client, which says so. It stops
// looking once it has found one. It installs nothing: `tend update` does,
// when somebody runs it.
//
// It runs only from a release build, as herdr's runs only from one, since a
// build from a working tree has no place in the order releases come in; and
// only when UpdateSource says to, which is the settings file's
// [update] version_check, read again each time so turning it off or on
// needs no restart.

// updateInterval is herdr's AUTO_UPDATE_CHECK_INTERVAL.
const updateInterval = 30 * time.Minute

// UpdateInstall is how an update is installed, as the notice and the notes
// say. herdr's says to detach, update and attach again; tend's handoff
// replaces the server under the programs in it, so nothing needs leaving.
const UpdateInstall = "press u in the release notes (menu → update ready), or run `tend update -handoff`; what is running keeps running"

// updateInstall is UpdateInstall for this server's own binary: one a
// package manager installed is updated by it (herdr's
// update_install_instruction), and tend handoff then moves the running
// sessions onto the new file.
func updateInstall() string {
	if self, err := os.Executable(); err == nil {
		if m, ok := update.ManagerOf(self); ok {
			return "run `" + m.Command + "`, then `tend handoff`; what is running keeps running"
		}
	}
	return UpdateInstall
}

// updateState is what the check knows, under s.mu.
type updateState struct {
	// ready is the version of a newer release, once found.
	ready string
	// notes is the version whose notes are kept on disk.
	notes string
}

// loadUpdateState reads the notes a previous check saved, so an update
// found before a restart is still "ready" after it (herdr's App start).
func (s *Server) loadUpdateState() {
	if s.cfg.NotesPath == "" {
		return
	}
	n, newer, ok := update.LoadNotes(s.cfg.NotesPath, s.cfg.Build)
	if !ok {
		return
	}
	s.mu.Lock()
	s.update.notes = n.Version
	if newer || (s.cfg.FakeUpdate != "" && n.Version == s.cfg.FakeUpdate) {
		s.update.ready = n.Version
	}
	s.mu.Unlock()
}

// updateSnapshotLocked is the snapshot's part of it.
func (s *Server) updateSnapshotLocked() *proto.UpdateInfo {
	if s.update.ready == "" && s.update.notes == "" {
		return nil
	}
	info := &proto.UpdateInfo{Ready: s.update.ready, Notes: s.update.notes}
	if info.Ready != "" {
		info.Install = updateInstall()
	}
	return info
}

// updateLoop checks at once and then every updateInterval, until a release
// is found or the server stops.
func (s *Server) updateLoop() {
	defer s.wg.Done()
	interval := s.cfg.UpdateInterval
	if interval <= 0 {
		interval = updateInterval
	}
	for {
		if s.checkForUpdate() {
			return
		}
		select {
		case <-s.done:
			return
		case <-time.After(interval):
		}
	}
}

// checkForUpdate runs one check, and reports whether a release was found.
// Nothing is held while it reads the network.
func (s *Server) checkForUpdate() bool {
	s.mu.Lock()
	known := s.update.ready != ""
	s.mu.Unlock()
	if known {
		return true
	}

	var release update.Release
	if fake := s.cfg.FakeUpdate; fake != "" {
		// herdr's HERDR_FAKE_UPDATE_VERSION: a release that is not there,
		// to see the notice and the notes without publishing one.
		release = update.Release{Version: fake, Notes: fakeNotes(fake)}
	} else {
		if _, ok := update.ParseVersion(s.cfg.Build); !ok {
			return true // not a release build: nothing to compare with, ever
		}
		url, enabled := s.cfg.UpdateSource()
		if !enabled {
			return false // looked at again next time, in case it is turned on
		}
		r, found, err := update.CheckLatest(url, s.cfg.Build)
		if err != nil {
			if !errors.Is(err, update.ErrNoManifest) {
				log.Printf("update check: %v", err)
			}
			return false
		}
		if !found {
			return false
		}
		release = r
	}

	if s.cfg.NotesPath != "" {
		if err := update.SaveNotes(s.cfg.NotesPath, release.Version, release.Notes); err != nil {
			log.Printf("update check: saving the release notes: %v", err)
		}
	}
	s.mu.Lock()
	s.update.ready = release.Version
	s.update.notes = release.Version
	s.mu.Unlock()
	s.publish(Event{Kind: EventUpdateReady, Title: release.Version, Body: updateInstall()})
	return true
}

// fakeNotes stand in for a fake release's.
func fakeNotes(version string) string {
	return fmt.Sprintf("### New\n- A pretend %s, set by TEND_FAKE_UPDATE_VERSION, to see how a release is announced.\n\n### Fixed\n- Nothing: this release does not exist.", version)
}

// ReleaseNotes are the notes kept, as release_notes.get returns them.
func (s *Server) ReleaseNotes() (proto.ReleaseNotes, bool) {
	if s.cfg.NotesPath == "" {
		return proto.ReleaseNotes{}, false
	}
	n, newer, ok := update.LoadNotes(s.cfg.NotesPath, s.cfg.Build)
	if !ok {
		return proto.ReleaseNotes{}, false
	}
	// A fake release is newer than whatever is running, whatever it is.
	if s.cfg.FakeUpdate != "" && n.Version == s.cfg.FakeUpdate {
		newer = true
	}
	out := proto.ReleaseNotes{Version: n.Version, Body: n.Body, Newer: newer}
	if newer {
		out.Install = updateInstall()
	}
	return out, true
}

// ErrStaleNotes is herdr's stale_release_notes: the notes read were
// replaced before they were dismissed.
var ErrStaleNotes = errors.New("the release notes are no longer current")

// DismissReleaseNotes marks the notes of version read.
func (s *Server) DismissReleaseNotes(version string) error {
	if s.cfg.NotesPath == "" {
		return nil
	}
	n, _, ok := update.LoadNotes(s.cfg.NotesPath, s.cfg.Build)
	if !ok || n.Version != version {
		return ErrStaleNotes
	}
	return update.DismissNotes(s.cfg.NotesPath, s.cfg.Build)
}
