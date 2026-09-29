package main

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/ui"
)

// The folder picker (internal/ui/folderpick.go) for a group's folder: the
// folders are listed by the server, since they are the ones on the machine
// the panes run on, and a click goes into one, goes up, or takes it.

type folderPickState struct {
	popup ui.FolderPick
	group string
	// seq numbers the listings asked for, so a slow answer for a folder
	// already left does not replace the one being looked at.
	seq int
}

func (t *tui) folderPickUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.folderPick != nil
}

func (t *tui) folderPickFrameLocked() *ui.FolderPick {
	if t.folderPick == nil {
		return nil
	}
	p := t.folderPick.popup
	return &p
}

func (t *tui) closeFolderPick() {
	t.mu.Lock()
	t.folderPick = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// openFolderPick puts the picker up for a group's folder, at the folder
// set, or else the space in view's, or else the home folder.
func (t *tui) openFolderPick(group string) {
	t.mu.Lock()
	start := t.groupDirLocked(group)
	if start == "" {
		if w, ok := t.workspaceLocked(); ok {
			start = w.Dir
		}
	}
	t.folderPick = &folderPickState{
		popup: ui.FolderPick{Title: "folder for " + group, Path: start},
		group: group,
	}
	t.dirty = true
	t.mu.Unlock()
	t.loadFolder(start)
}

// loadFolder lists a folder into the picker. A folder that cannot be read
// leaves the picker where it was, saying why.
func (t *tui) loadFolder(path string) {
	t.mu.Lock()
	p := t.folderPick
	if p == nil {
		t.mu.Unlock()
		return
	}
	p.seq++
	seq := p.seq
	p.popup.Loading, p.popup.Error = true, ""
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	go func() {
		list, err := t.client.ListDirs(path)
		t.mu.Lock()
		defer t.wakeUp()
		defer t.mu.Unlock()
		p := t.folderPick
		if p == nil || p.seq != seq {
			return
		}
		t.dirty = true
		p.popup.Loading = false
		if err != nil {
			p.popup.Error = refusalText(err)
			return
		}
		p.popup.Path, p.popup.Parent, p.popup.Dirs = list.Path, list.Parent, list.Dirs
		p.popup.Query, p.popup.Selected = "", 0
	}()
}

// chooseFolderRow does what a row says: takes the folder shown, goes up, or
// goes into a folder.
func (t *tui) chooseFolderRow(row int) {
	t.mu.Lock()
	p := t.folderPick
	if p == nil || p.popup.Loading {
		t.mu.Unlock()
		return
	}
	rows := p.popup.Rows()
	if row < 0 || row >= len(rows) {
		t.mu.Unlock()
		return
	}
	r, path, parent, group := rows[row], p.popup.Path, p.popup.Parent, p.group
	t.mu.Unlock()

	switch r.Kind {
	case ui.FolderUp:
		t.loadFolder(parent)
	case ui.FolderDir:
		t.loadFolder(filepath.Join(path, r.Name))
	case ui.FolderUse:
		if err := t.client.SetGroupDir(group, path); err != nil {
			t.mu.Lock()
			if t.folderPick != nil {
				t.folderPick.popup.Error = refusalText(err)
				t.dirty = true
			}
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		t.closeFolderPick()
		t.setMessage(group+": new spaces start in "+tildeHome(path), false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}
}

// refusalText is what the server said, without the method it said it to.
func refusalText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && !strings.Contains(msg[:i], " ") {
		return msg[i+2:]
	}
	return msg
}

// moveFolderPickLocked moves the cursor among the rows.
func (t *tui) moveFolderPickLocked(delta int) {
	p := &t.folderPick.popup
	p.Selected = min(max(p.Selected+delta, 0), len(p.Rows())-1)
}

// folderPickKeys: esc cancels, enter does the row under the cursor, the
// arrows move it, ← goes up and → goes in, and typing filters. A query that
// is a path — starting with / or ~ — goes there on enter, for somebody who
// knows where they are going.
func (t *tui) folderPickKeys(data []byte) {
	for _, key := range splitKeys(data) {
		t.mu.Lock()
		p := t.folderPick
		if p == nil {
			t.mu.Unlock()
			return
		}
		t.dirty = true
		query := p.popup.Query
		switch {
		case key == "\x1b" || key == "\x03":
			t.folderPick = nil
			t.mu.Unlock()
		case key == "\r" || key == "\n":
			if strings.HasPrefix(query, "/") || strings.HasPrefix(query, "~") {
				t.mu.Unlock()
				t.loadFolder(query)
				continue
			}
			row := p.popup.Selected
			t.mu.Unlock()
			t.chooseFolderRow(row)
		case key == "\x1b[A":
			t.moveFolderPickLocked(-1)
			t.mu.Unlock()
		case key == "\x1b[B":
			t.moveFolderPickLocked(1)
			t.mu.Unlock()
		case key == "\x1b[D" || (query == "" && (key == "\x7f" || key == "\x08")):
			parent := p.popup.Parent
			t.mu.Unlock()
			if parent != "" {
				t.loadFolder(parent)
			}
		case key == "\x1b[C":
			row := p.popup.Selected
			isDir := row < len(p.popup.Rows()) && p.popup.Rows()[row].Kind == ui.FolderDir
			t.mu.Unlock()
			if isDir {
				t.chooseFolderRow(row)
			}
		case key == "\x7f" || key == "\x08":
			_, size := utf8.DecodeLastRuneInString(query)
			p.popup.Query = query[:len(query)-size]
			p.popup.Selected = p.popup.FirstDir()
			t.mu.Unlock()
		case len(key) >= 1 && (key[0] >= 0x20 && key[0] != 0x7f || key[0] >= 0x80) && utf8.ValidString(key):
			p.popup.Query += key
			p.popup.Selected = p.popup.FirstDir()
			t.mu.Unlock()
		default:
			t.mu.Unlock()
		}
	}
	t.wakeUp()
}

// folderPickMouse answers the mouse while the picker is up: a click on a
// row does it, the wheel moves the cursor, and a click outside or on the
// close mark puts the picker away.
func (t *tui) folderPickMouse(ev ui.MouseEvent) (bool, error) {
	t.mu.Lock()
	p := t.folderPick
	if p == nil {
		t.mu.Unlock()
		return false, nil
	}
	row, inside := ui.FolderPickAt(p.popup, t.cols, t.rows, ev.X, ev.Y)
	t.dirty = true
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.moveFolderPickLocked(-1)
	case ui.MouseWheelDown:
		t.moveFolderPickLocked(1)
	case ui.MousePress:
		switch {
		case !inside || ui.OnCloseMark(ui.FolderPickRect(p.popup, t.cols, t.rows), ev.X, ev.Y):
			t.folderPick = nil
		case row >= 0:
			p.popup.Selected = row
			t.mu.Unlock()
			t.chooseFolderRow(row)
			return true, nil
		}
	}
	t.mu.Unlock()
	t.wakeUp()
	return true, nil
}
