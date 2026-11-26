package core

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDaemon stands in for tend-whatsapp's daemon on its socket, speaking
// the protocol of whatsapp/internal/daemon/protocol.go. It is a stand-in,
// not the daemon: the daemon's own tests (whatsapp/internal/daemon) prove
// the daemon keeps the rule; these prove the window asks it the right
// things, and that it asks replies.approve only after the owner confirmed.
// Nothing here, or anywhere in the window, connects to WhatsApp.
type fakeDaemon struct {
	mu       sync.Mutex
	replies  map[int64]map[string]any
	approved []int64
	rejected []int64
}

func startFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	path, err := waSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	at := time.Date(2026, 11, 26, 9, 30, 0, 0, time.Local)
	d := &fakeDaemon{replies: map[int64]map[string]any{
		7: {"id": 7, "chat": "5511999@s.whatsapp.net", "chat_name": "Maria", "text": "Your order ships tomorrow.",
			"by": "pane 3", "state": "pending", "created_at": at},
	}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn, at)
		}
	}()
	return d
}

func (d *fakeDaemon) serve(conn net.Conn, at time.Time) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params struct {
			State string `json:"state"`
			Chat  string `json:"chat"`
			Query string `json:"query"`
			ID    int64  `json:"id"`
		} `json:"params"`
	}
	_ = json.Unmarshal(line, &req)
	d.mu.Lock()
	var result any
	var fail map[string]string
	chat := map[string]any{"jid": "5511999@s.whatsapp.net", "name": "Maria", "last_at": at, "last_text": "is my order coming?", "messages": 2}
	msgs := []map[string]any{
		{"chat": "5511999@s.whatsapp.net", "id": "m1", "sender": "5511999@s.whatsapp.net", "sender_name": "Maria", "at": at, "kind": "text", "text": "hello"},
		{"chat": "5511999@s.whatsapp.net", "id": "m2", "sender": "5511999@s.whatsapp.net", "sender_name": "Maria", "at": at, "kind": "text", "text": "is my order coming?"},
	}
	switch req.Method {
	case "status":
		pending := 0
		for _, r := range d.replies {
			if r["state"] == "pending" {
				pending++
			}
		}
		result = map[string]any{"linked": true, "connected": true, "jid": "5511888:3@s.whatsapp.net", "name": "Shop", "chats": 1, "messages": 2, "pending": pending}
	case "chats.list":
		result = map[string]any{"chats": []any{chat}}
	case "messages.read":
		result = map[string]any{"chat": chat, "messages": msgs}
	case "messages.search":
		var hits []any
		for _, m := range msgs {
			if strings.Contains(m["text"].(string), req.Params.Query) {
				hits = append(hits, m)
			}
		}
		result = map[string]any{"messages": hits}
	case "replies.list":
		list := []any{}
		for _, r := range d.replies {
			if req.Params.State == "" || r["state"] == req.Params.State {
				list = append(list, r)
			}
		}
		result = map[string]any{"replies": list}
	case "replies.approve", "replies.reject":
		r := d.replies[req.Params.ID]
		switch {
		case r == nil:
			fail = map[string]string{"code": "no_such_reply", "message": "no such reply"}
		case r["state"] != "pending" && r["state"] != "failed":
			fail = map[string]string{"code": "decided", "message": "the reply was already decided"}
		case req.Method == "replies.approve":
			r["state"] = "sent"
			d.approved = append(d.approved, req.Params.ID)
			result = map[string]any{"reply": r}
		default:
			r["state"] = "rejected"
			d.rejected = append(d.rejected, req.Params.ID)
			result = map[string]any{"reply": r}
		}
	default:
		fail = map[string]string{"code": "unknown_method", "message": "unknown method: " + req.Method}
	}
	d.mu.Unlock()
	resp := map[string]any{"id": req.ID, "result": result}
	if fail != nil {
		resp = map[string]any{"id": req.ID, "error": fail}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func (d *fakeDaemon) sent() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int64(nil), d.approved...)
}

func (d *fakeDaemon) turnedDown() []int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int64(nil), d.rejected...)
}

// waState is the WhatsApp tab now, nil while it is not open.
func waState(s *Session) *WhatsAppState {
	st := s.State()
	if st.Dock == nil || st.Dock.Loading {
		return nil
	}
	return st.Dock.WhatsApp
}

// TestTheWhatsAppTabShowsTheDaemonsChatsAndTheRepliesWaiting: the tab reads
// tend-whatsapp's daemon over its socket — that the number is linked, the
// chats, a chat's messages, a search, and the replies the agents proposed.
// If it regresses, the owner has to go to a terminal to see a reply an
// agent is waiting on.
func TestTheWhatsAppTabShowsTheDaemonsChatsAndTheRepliesWaiting(t *testing.T) {
	s := openTest(t)
	startFakeDaemon(t)
	s.ToggleDock("whatsapp")
	var wa *WhatsAppState
	waitFor(t, "the WhatsApp tab", func() bool { wa = waState(s); return wa != nil && wa.Running })
	if !wa.Linked || !wa.Connected || wa.Number != "+5511888" || len(wa.Chats) != 1 || wa.Chats[0].Name != "Maria" {
		t.Errorf("the tab is %+v", wa)
	}
	if len(wa.Replies) != 1 || wa.Replies[0].Text != "Your order ships tomorrow." || wa.Replies[0].ChatName != "Maria" {
		t.Errorf("the replies waiting are %+v", wa.Replies)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "chat", Chat: "5511999@s.whatsapp.net"}); err != nil {
		t.Fatal(err)
	}
	if wa = waState(s); wa.Open == nil || len(wa.Messages) != 2 || wa.Messages[1].From != "Maria" {
		t.Errorf("the chat open is %+v, %+v", wa.Open, wa.Messages)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "search", Query: "order"}); err != nil {
		t.Fatal(err)
	}
	if wa = waState(s); len(wa.Results) != 1 || wa.Results[0].ID != "m2" {
		t.Errorf("the search found %+v", wa.Results)
	}
}

// TestAReplyIsSentOnlyAfterTheOwnerConfirmsIt: "approve" shows the reply's
// exact text and chat, and only "send" on that confirmation calls the
// daemon's replies.approve, once, for the reply shown. If it regresses, a
// click sends a message to a customer in the owner's name that the owner
// did not read.
func TestAReplyIsSentOnlyAfterTheOwnerConfirmsIt(t *testing.T) {
	s := openTest(t)
	d := startFakeDaemon(t)
	s.ToggleDock("whatsapp")
	waitFor(t, "the WhatsApp tab", func() bool { wa := waState(s); return wa != nil && wa.Running })

	if err := s.WhatsApp(WhatsAppAction{Do: "approve", ID: 7}); err == nil {
		t.Error("a reply was approved without being shown")
	}
	if len(d.sent()) != 0 {
		t.Fatal("a reply was sent without the owner confirming it")
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "confirm", ID: 7}); err != nil {
		t.Fatal(err)
	}
	c := waState(s).Confirm
	if c == nil || c.ID != 7 || c.Text != "Your order ships tomorrow." || c.ChatName != "Maria" {
		t.Fatalf("the confirmation shows %+v", c)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "approve", ID: 8}); err == nil || len(d.sent()) != 0 {
		t.Error("a reply other than the one shown was approved")
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "confirm", ID: 7}); err != nil {
		t.Fatal(err)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "approve", ID: 7}); err != nil {
		t.Fatal(err)
	}
	if got := d.sent(); len(got) != 1 || got[0] != 7 {
		t.Errorf("sent %v; want reply 7 once", got)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "approve", ID: 7}); err == nil || len(d.sent()) != 1 {
		t.Error("a second press sent the reply again")
	}
	if wa := waState(s); wa.Confirm != nil || len(wa.Replies) != 0 {
		t.Errorf("after sending, the tab is %+v", wa)
	}
}

// TestARejectedReplyLeavesTheList: reject tells the daemon, which never
// sends it. If it regresses, a reply the owner turned down stays waiting.
func TestARejectedReplyLeavesTheList(t *testing.T) {
	s := openTest(t)
	d := startFakeDaemon(t)
	s.ToggleDock("whatsapp")
	waitFor(t, "the WhatsApp tab", func() bool { wa := waState(s); return wa != nil && wa.Running })
	if err := s.WhatsApp(WhatsAppAction{Do: "reject", ID: 7}); err != nil {
		t.Fatal(err)
	}
	if len(d.turnedDown()) != 1 || len(d.sent()) != 0 || len(waState(s).Replies) != 0 {
		t.Errorf("rejected %v, sent %v, still listed %v", d.turnedDown(), d.sent(), waState(s).Replies)
	}
}

// TestWithoutADaemonTheTabSaysSoAndLinksInATab: with no daemon answering
// the tab is not an error but a state, and linking opens `tend whatsapp
// link` in a tab of the session, where its QR code can be shown. If it
// regresses, the tab shows a socket error to somebody who has not set
// WhatsApp up yet, or linking has nowhere to show its code.
func TestWithoutADaemonTheTabSaysSoAndLinksInATab(t *testing.T) {
	s := openTest(t)
	s.ToggleDock("whatsapp")
	waitFor(t, "the WhatsApp tab", func() bool { return waState(s) != nil })
	if st := s.State(); st.Dock.Error != "" || st.Dock.WhatsApp.Running {
		t.Errorf("with no daemon the tab is %+v, error %q", st.Dock.WhatsApp, st.Dock.Error)
	}
	if err := s.WhatsApp(WhatsAppAction{Do: "link"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link tab", func() bool {
		for _, tab := range s.State().Tabs {
			if tab.Name == "WhatsApp link" && tab.Active {
				return true
			}
		}
		return false
	})
}
