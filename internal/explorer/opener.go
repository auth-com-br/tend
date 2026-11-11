package explorer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// SessionOpener opens files in an editor in a tab of their own, through the
// session's automation socket: the socket and the pane are what tend puts
// in every pane's environment (TEND_SOCKET_PATH, TEND_PANE_ID).
//
// A file already open in a tab this explorer opened is gone back to rather
// than opened twice, as an editor's explorer focuses the tab it has.
type SessionOpener struct {
	Socket string
	Pane   string
	// Editor is the command to edit with, which the file's path is given to
	// as its last argument by the shell ("$EDITOR" may carry flags).
	Editor string

	opened map[string]string
	seq    atomic.Uint64
	// preview is the pane showing previews, reused for the next one, and
	// previewTab the tab it is the whole of.
	preview, previewTab string
}

// callTimeout bounds one call: a session that does not answer should not
// hang the panel.
const callTimeout = 5 * time.Second

// call makes one request on its own connection and returns its result.
func (o *SessionOpener) call(method string, params any) (map[string]any, error) {
	conn, err := net.DialTimeout("unix", o.Socket, callTimeout)
	if err != nil {
		return nil, fmt.Errorf("the session is not answering: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(callTimeout))
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req, _ := json.Marshal(map[string]any{
		"id": "files-" + strconv.FormatUint(o.seq.Add(1), 10), "method": method, "params": json.RawMessage(raw),
	})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var reply struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return nil, err
	}
	if reply.Error != nil {
		return nil, errors.New(reply.Error.Message)
	}
	return reply.Result, nil
}

// Open opens path in the editor, or goes to the tab it is already open in.
// A line above zero starts the editor there, for the editors that say how
// (see editorAt); a file already open is gone back to as it is.
func (o *SessionOpener) Open(path string, line int) error {
	if o.Socket == "" || o.Pane == "" {
		return errors.New("nowhere to open files: not running in a tend pane")
	}
	if pane, ok := o.opened[path]; ok {
		if _, err := o.call("pane.focus", map[string]any{"pane_id": pane}); err == nil {
			return nil
		}
		// Closed since: open it again.
		delete(o.opened, path)
	}
	layout, err := o.call("pane.layout", map[string]any{"pane_id": o.Pane})
	if err != nil {
		return err
	}
	inner, _ := layout["layout"].(map[string]any)
	ws, _ := inner["workspace_id"].(string)
	if ws == "" {
		return errors.New("cannot tell which space this panel is in")
	}
	editor := o.Editor
	if editor == "" {
		editor = "vi"
	}
	created, err := o.call("tab.create", map[string]any{
		"workspace_id": ws,
		"name":         filepath.Base(path),
		// The editor through a shell, so an $EDITOR with flags in it works;
		// the path as an argument, so one with spaces or quotes in it
		// reaches the editor as it is.
		"command":       []string{"/bin/sh", "-c", editorAt(editor, line) + ` "$1"`, "tend-edit", path},
		"dir":           filepath.Dir(path),
		"close_on_exit": true,
	})
	if err != nil {
		return err
	}
	root, _ := created["root_pane"].(map[string]any)
	pane, _ := root["pane_id"].(string)
	if pane == "" {
		return nil
	}
	if o.opened == nil {
		o.opened = map[string]string{}
	}
	o.opened[path] = pane
	// A new tab is not shown by itself: each client decides what it looks
	// at. Asking for focus is how the one being used goes to it.
	_, err = o.call("pane.focus", map[string]any{"pane_id": pane})
	return err
}

// editorAt is the editor's command with the cursor put on a line. vi and its
// family, nano, emacs, micro and kakoune take +N before the file; an editor
// not known to is opened at the top rather than handed an argument it
// might read as a file name.
func editorAt(editor string, line int) string {
	if line <= 0 {
		return editor
	}
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return editor
	}
	switch filepath.Base(fields[0]) {
	case "vi", "vim", "nvim", "view", "nano", "emacs", "emacsclient", "micro", "kak", "mg", "joe", "ne":
		return editor + " +" + strconv.Itoa(line)
	}
	return editor
}

// Siblings lists the other panes of this pane's tab and where their
// programs are, from pane.list.
func (o *SessionOpener) Siblings() ([]Sibling, error) {
	if o.Socket == "" || o.Pane == "" {
		return nil, errors.New("not running in a tend pane")
	}
	res, err := o.call("pane.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	panes, _ := res["panes"].([]any)
	tab := ""
	for _, raw := range panes {
		if p, _ := raw.(map[string]any); p["pane_id"] == o.Pane {
			tab, _ = p["tab_id"].(string)
		}
	}
	if tab == "" {
		return nil, errors.New("this pane is in no tab the session lists")
	}
	var out []Sibling
	for _, raw := range panes {
		p, _ := raw.(map[string]any)
		id, _ := p["pane_id"].(string)
		if id == o.Pane || p["tab_id"] != tab {
			continue
		}
		cwd, _ := p["foreground_cwd"].(string)
		focused, _ := p["focused"].(bool)
		out = append(out, Sibling{Pane: id, Cwd: cwd, Focused: focused})
	}
	return out, nil
}

// Preview shows a file read-only in a tab of its own, with line (from one)
// in view, and goes to that tab: the tab already showing a preview is
// pointed at this file and renamed after it, and one is opened when there
// is none. It is one tab, reused, so looking through files does not stack a
// tab per file; enter, or o in the preview, opens the editor in a tab that
// stays.
//
// This is herdr-sidebar's default placement. tend first split a pane
// beside the main one instead, so the panel stayed in view, but that took
// columns from the terminal being worked in; the owner asked for the tab.
func (o *SessionOpener) Preview(path string, line int, dir string) error {
	if o.Socket == "" || o.Pane == "" {
		return errors.New("nowhere to preview: not running in a tend pane")
	}
	name := filepath.Base(path)
	if o.preview != "" {
		if _, err := o.call("pane.send_text", map[string]any{
			"pane_id": o.preview, "text": RetargetSequence(path, line),
		}); err == nil {
			if o.previewTab != "" {
				_, _ = o.call("tab.rename", map[string]any{"tab_id": o.previewTab, "name": name})
			}
			_, err = o.call("pane.focus", map[string]any{"pane_id": o.preview})
			return err
		}
		o.preview, o.previewTab = "", "" // closed since
	}
	layout, err := o.call("pane.layout", map[string]any{"pane_id": o.Pane})
	if err != nil {
		return err
	}
	inner, _ := layout["layout"].(map[string]any)
	ws, _ := inner["workspace_id"].(string)
	if ws == "" {
		return errors.New("cannot tell which space this panel is in")
	}
	created, err := o.call("tab.create", map[string]any{
		"workspace_id": ws,
		"name":         name,
		"command": []string{"/bin/sh", "-c", `exec "${TEND_BIN_PATH:-tend}" view -line "$2" "$1"`,
			"tend-view", path, strconv.Itoa(line)},
		// In the project, so the panel following its siblings does not
		// follow the preview somewhere else.
		"dir":           dir,
		"close_on_exit": true,
	})
	if err != nil {
		return err
	}
	tab, _ := created["tab"].(map[string]any)
	o.previewTab, _ = tab["tab_id"].(string)
	root, _ := created["root_pane"].(map[string]any)
	o.preview, _ = root["pane_id"].(string)
	if o.preview == "" {
		return nil
	}
	// A new tab is not shown by itself: each client decides what it looks
	// at. Asking for focus is how the one being used goes to it.
	_, err = o.call("pane.focus", map[string]any{"pane_id": o.preview})
	return err
}

// AddContext puts a file in the session's context buffer (context.add).
func (o *SessionOpener) AddContext(kind, path string) error {
	if o.Socket == "" {
		return errors.New("not running in a tend pane")
	}
	_, err := o.call("context.add", map[string]any{"kind": kind, "path": path, "source": "files"})
	return err
}

// Review sends a review to an agent in this panel's tab: the one the panel
// follows when it is an agent, else the first agent the tab has. It is typed
// into the agent's prompt and submitted, as `tend agent prompt` does.
func (o *SessionOpener) Review(prefer, text string) (string, error) {
	if o.Socket == "" || o.Pane == "" {
		return "", errors.New("not running in a tend pane")
	}
	res, err := o.call("pane.list", map[string]any{})
	if err != nil {
		return "", err
	}
	panes, _ := res["panes"].([]any)
	tab := ""
	for _, raw := range panes {
		if p, _ := raw.(map[string]any); p["pane_id"] == o.Pane {
			tab, _ = p["tab_id"].(string)
		}
	}
	var target, agent string
	for _, raw := range panes {
		p, _ := raw.(map[string]any)
		id, _ := p["pane_id"].(string)
		name, _ := p["agent"].(string)
		if id == o.Pane || p["tab_id"] != tab || name == "" {
			continue
		}
		if target == "" || id == prefer {
			target, agent = id, name
		}
	}
	if target == "" {
		return "", errors.New("no agent in this tab to send the review to")
	}
	if _, err := o.call("pane.send_text", map[string]any{"pane_id": target, "text": text, "submit": true}); err != nil {
		return "", err
	}
	return agent, nil
}
