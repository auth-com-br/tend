package mcp

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The rules every write to an agent's file follows. The files belong to the
// agents, which write them too — Claude Code several times a minute — so a
// write here must not undo one there, must not leave half a file, and must
// leave the file as private as it found it (~/.claude.json is 0600, and
// holds tokens).

// writeOpts is what one file needs beyond its path.
type writeOpts struct {
	// lockDir is a directory the agent itself creates while it writes the
	// file, taken here the same way: Claude Code's ~/.claude.json.lock
	// (found by tracing `claude mcp add`: mkdir, write a temporary file,
	// rename it over, rmdir).
	lockDir string
	// backups is where the day's first version of the file is copied
	// before it is changed, and backupName what the copy is called.
	backups, backupName string
	// mode is the mode a file created here gets.
	mode fs.FileMode
}

// errChanging is a file that kept changing between being read and written.
var errChanging = errors.New("kept changing while tend was editing it; try again")

// staleLock is how old a lock directory can be before it is taken to be
// left behind by a program that died: proper-lockfile's own default, which
// is what Claude Code uses.
const staleLock = 10 * time.Second

// editFile applies edit to the file at path, following the rules above.
// edit gets the current bytes — nil for a file that is not there — and
// returns the new ones; returning the same bytes writes nothing.
//
// The edit is done again on the fresh bytes when the file changed between
// the read and the write, so another program's change is kept rather than
// overwritten; after three tries it gives up.
func editFile(path string, opts writeOpts, edit func([]byte) ([]byte, error)) error {
	real := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		// A dotfiles repository's symlink stays a symlink: the file it
		// points to is the one written.
		real = resolved
	}
	for attempt := 0; attempt < 3; attempt++ {
		before, mode, err := readFile(real, opts.mode)
		if err != nil {
			return err
		}
		after, err := edit(before)
		if err != nil {
			return err
		}
		if bytes.Equal(before, after) {
			return nil
		}
		unlock, err := lock(opts.lockDir)
		if err != nil {
			return err
		}
		now, _, err := readFile(real, opts.mode)
		if err != nil {
			unlock()
			return err
		}
		if sha256.Sum256(now) != sha256.Sum256(before) {
			unlock()
			continue
		}
		if before != nil && opts.backups != "" {
			if err := backup(opts.backups, opts.backupName, before); err != nil {
				unlock()
				return fmt.Errorf("backing up %s: %w", path, err)
			}
		}
		err = writeAtomic(real, after, mode)
		unlock()
		return err
	}
	return fmt.Errorf("%s %w", path, errChanging)
}

// readFile is a file's bytes and mode; a file that is not there is nil
// bytes and the default mode.
func readFile(path string, def fs.FileMode) ([]byte, fs.FileMode, error) {
	if def == 0 {
		def = 0o600
	}
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, def, nil
	}
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	if data == nil {
		data = []byte{}
	}
	return data, info.Mode().Perm(), nil
}

// writeAtomic replaces a file with data in one rename, so a reader sees
// the old file or the new one and never half of either.
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tend-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // after a successful rename there is nothing there
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// lock takes a lock directory the way proper-lockfile does — mkdir, which
// either makes it or finds it there — waiting up to five seconds for the
// holder, and taking over one older than staleLock. No lockDir is no lock.
func lock(dir string) (func(), error) {
	if dir == "" {
		return func() {}, nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return func() { _ = os.Remove(dir) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if info, err := os.Stat(dir); err == nil && time.Since(info.ModTime()) > staleLock {
			_ = os.Remove(dir)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%s is held by another program; try again", dir)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// keptBackups is how many daily backups of one file are kept.
const keptBackups = 14

// backup copies the day's first version of a file into dir as
// name.YYYYMMDD, and keeps the newest keptBackups of that name.
func backup(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, name+"."+time.Now().Format("20060102"))
	if _, err := os.Stat(path); err == nil {
		return nil // the day's first version is already kept
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, name+".*"))
	sort.Strings(matches)
	for len(matches) > keptBackups {
		if strings.HasPrefix(filepath.Base(matches[0]), name+".") {
			_ = os.Remove(matches[0])
		}
		matches = matches[1:]
	}
	return nil
}
