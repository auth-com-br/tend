package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/auth-com-br/tend/internal/transport"
)

// The window's browser tabs are a browser the session can drive (#74),
// attached as tend's extension attaches an outside Chromium
// (docs/BROWSER.md, "The protocol"): browser.attach on the automation
// socket, and from then on the commands any agent or script sends —
// tend browser open, the MCP server's tools — come to the window: open a
// page in a new browser tab, navigate the one in front, turn element
// picking on or off. What is picked goes back as browser.context, into the
// session's context buffer, from where it is handed to an agent — the
// extension's own path, so an agent cannot tell the two browsers apart.

// browserName is what the window attaches as, which browser.status lists.
const browserName = "tend-desktop"

// AttachBrowser keeps the window attached as a browser while the session is
// open, attaching again when the connection ends (a server handed over, a
// restart). command is called with each command, off the main thread.
func (s *Session) AttachBrowser(command func(action, url string, on bool)) {
	go func() {
		for {
			// An error is not said: a server older than the browser
			// protocol, or one being handed over, is tried again quietly,
			// and the window's own browser tabs work either way.
			_ = s.attachOnce(command)
			select {
			case <-s.done:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

func (s *Session) attachOnce(command func(action, url string, on bool)) error {
	path, err := transport.APISocketPath(s.name)
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	go func() {
		<-s.done
		_ = conn.Close()
	}()
	defer conn.Close()
	req, _ := json.Marshal(map[string]any{
		"id": "desktop-browser", "method": "browser.attach",
		"params": map[string]any{"name": browserName},
	})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<16)
	first, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	var reply struct {
		Result struct {
			Type string `json:"type"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(first), &reply); err != nil {
		return err
	}
	if reply.Error != nil {
		return fmt.Errorf("browser.attach: %s", reply.Error.Message)
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		var c struct {
			Type   string `json:"type"`
			Action string `json:"action"`
			URL    string `json:"url"`
			On     bool   `json:"on"`
		}
		if json.Unmarshal([]byte(line), &c) != nil || c.Type != "browser_command" {
			continue
		}
		command(c.Action, c.URL, c.On)
	}
}

// Picked is what element picking captured on a page: the extension's item.
type Picked struct {
	URL        string            `json:"url"`
	Title      string            `json:"title"`
	Selector   string            `json:"selector"`
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Attributes map[string]string `json:"attributes"`
	Note       string            `json:"note"`
}

// SendPicked hands what was picked to the session's context buffer, as the
// extension does (browser.context, an element from the browser).
func (s *Session) SendPicked(p Picked) error {
	params := map[string]any{
		"kind": "element", "source": "browser", "url": p.URL, "title": p.Title,
		"selector": p.Selector, "tag": p.Tag, "text": p.Text, "attributes": p.Attributes,
	}
	if p.Note != "" {
		params["note"] = p.Note
	}
	_, err := s.apiCall("browser.context", params)
	return err
}
