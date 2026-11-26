package api

import (
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/server"
)

// The browser methods' stream (server/browser.go): browser.attach turns a
// connection into the commands an attached browser follows, as
// events.subscribe turns one into events (subscribe.go).

// browserErr is a browser method's failure in the socket's terms.
func browserErr(err error) error {
	if errors.Is(err, server.ErrNoBrowser) {
		return fail("no_browser", "%s", err.Error())
	}
	return fail("invalid_params", "%s", err.Error())
}

// followBrowser turns the connection into the commands an attached browser
// follows, one line each, until it hangs up or the server stops.
//
// The browser was attached when the request was answered (cmds, detach),
// not here: attached after the reply, a page sent the moment the browser
// had its answer found no browser — the gap events.subscribe had
// (subscribe.go).
func (a *API) followBrowser(conn net.Conn, cmds <-chan server.BrowserCommand, detach func()) error {
	defer detach()
	// The browser says nothing on this connection once attached; a read
	// that ends is it gone, which lets it go at once rather than at the
	// next command it would have failed to take.
	gone := make(chan struct{})
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := conn.Read(buf); err != nil {
				close(gone)
				return
			}
		}
	}()
	enc := json.NewEncoder(conn)
	for {
		select {
		case <-gone:
			return nil
		case cmd, ok := <-cmds:
			if !ok {
				return nil
			}
			out := map[string]any{"type": "browser_command", "action": cmd.Action}
			if cmd.URL != "" {
				out["url"] = cmd.URL
			}
			if cmd.Action == server.BrowserSelect {
				out["on"] = cmd.On
			}
			_ = conn.SetWriteDeadline(time.Now().Add(streamWrite))
			if err := enc.Encode(out); err != nil {
				return nil
			}
		}
	}
}

// captureItem is what a browser sent, as a context item.
func captureItem(p BrowserCaptureParams) proto.ContextItem {
	return proto.ContextItem{Kind: p.Kind, Title: p.Title, URL: p.URL, Selector: p.Selector,
		Tag: p.Tag, Text: p.Text, Attributes: p.Attributes, Note: p.Note}
}
