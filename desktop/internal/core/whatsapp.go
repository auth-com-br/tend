package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
)

// The dock's WhatsApp tab (issue #73): tend-whatsapp's daemon, seen from the
// window. The daemon holds the one connection to WhatsApp and answers on a
// unix socket (whatsapp/internal/daemon/protocol.go); this tab is one more
// client of it, beside the agents' MCP server and the owner's CLI, and
// never connects to WhatsApp itself.
//
// The protocol is mirrored here rather than imported: the daemon's package
// is internal to the whatsapp module, which Go does not let another module
// import, and importing it would also bring whatsmeow and its SQL driver
// into the window for the sake of a few JSON shapes.
//
// The rule the daemon keeps holds here too: an agent proposes, only the
// owner sends. A reply is approved in two steps — "approve" shows the exact
// text and the chat in the window, and only "send" there calls the
// daemon's replies.approve, with the id of the reply that was shown.

// waRequest and waResponse are the protocol's lines.
type waRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type waResponse struct {
	Result json.RawMessage `json:"result"`
	Event  json.RawMessage `json:"event"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// errNoWhatsApp is a socket nobody answers on: tend-whatsapp is not
// installed, or its daemon is not running.
var errNoWhatsApp = errors.New("tend-whatsapp's daemon is not running")

// waSocketPath is the daemon's socket, as daemon.SocketPath puts it: a
// directory of its own under tend's runtime directory.
func waSocketPath() (string, error) {
	dir, err := transport.RuntimeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "whatsapp", "daemon.sock")
	if err := transport.CheckSocketPath(path); err != nil {
		return "", err
	}
	return path, nil
}

var waNextID atomic.Int64

// whatsAppPoll is how often the open tab reads the daemon again.
var whatsAppPoll = 4 * time.Second

// waCall makes one call on a connection of its own, as the daemon's own
// client does, and decodes its result into result (when not nil). The
// window never calls link, the one method that answers with events first:
// linking needs a terminal to show the QR code in.
func waCall(ctx context.Context, method string, params, result any) error {
	path, err := waSocketPath()
	if err != nil {
		return err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return errNoWhatsApp
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	line, err := json.Marshal(waRequest{ID: strconv.FormatInt(waNextID.Add(1), 10), Method: method, Params: params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	for {
		reply, err := r.ReadBytes('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("tend-whatsapp's daemon hung up: %w", err)
		}
		var resp waResponse
		if err := json.Unmarshal(reply, &resp); err != nil {
			return err
		}
		switch {
		case resp.Error != nil:
			return errors.New(resp.Error.Message)
		case len(resp.Event) > 0 && string(resp.Event) != "null":
			continue
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
}

// The daemon's shapes (whatsapp/internal/store), as far as the tab reads
// them.
type (
	waStatus struct {
		Linked    bool   `json:"linked"`
		Connected bool   `json:"connected"`
		JID       string `json:"jid"`
		Name      string `json:"name"`
		Chats     int    `json:"chats"`
		Messages  int    `json:"messages"`
		Pending   int    `json:"pending"`
	}
	waChat struct {
		JID      string    `json:"jid"`
		Name     string    `json:"name"`
		LastAt   time.Time `json:"last_at"`
		LastText string    `json:"last_text"`
		Messages int       `json:"messages"`
	}
	waMessage struct {
		Chat       string    `json:"chat"`
		ID         string    `json:"id"`
		Sender     string    `json:"sender"`
		SenderName string    `json:"sender_name"`
		FromMe     bool      `json:"from_me"`
		At         time.Time `json:"at"`
		Kind       string    `json:"kind"`
		Text       string    `json:"text"`
	}
	waReply struct {
		ID        int64     `json:"id"`
		Chat      string    `json:"chat"`
		ChatName  string    `json:"chat_name"`
		Text      string    `json:"text"`
		Quoted    string    `json:"quoted"`
		By        string    `json:"by"`
		State     string    `json:"state"`
		CreatedAt time.Time `json:"created_at"`
		Error     string    `json:"error"`
	}
)

// waTab is what the session keeps of the tab.
type waTab struct {
	running bool
	status  waStatus
	chats   []waChat
	// open is the chat being read, and messages what was read of it.
	open     *waChat
	messages []waMessage
	query    string
	results  []waMessage
	replies  []waReply
	// confirm is the reply the owner pressed "approve" on, shown whole
	// until they send it or cancel.
	confirm *waReply
	busy    bool
	// polling is a poller running while the tab is open: the daemon has no
	// events to subscribe to, and a reply an agent proposes should appear
	// without the owner pressing anything.
	polling atomic.Bool
}

// WhatsAppState is the tab as QML draws it.
type WhatsAppState struct {
	Running   bool   `json:"running"`
	Linked    bool   `json:"linked"`
	Connected bool   `json:"connected"`
	Number    string `json:"number,omitempty"`
	Name      string `json:"name,omitempty"`
	// Kept says what the daemon holds, and that it is on this machine.
	Kept     string         `json:"kept,omitempty"`
	Chats    []WAChatRow    `json:"chats"`
	Open     *WAChatRow     `json:"open,omitempty"`
	Messages []WAMessageRow `json:"messages,omitempty"`
	Query    string         `json:"query,omitempty"`
	Results  []WAMessageRow `json:"results,omitempty"`
	Replies  []WAReplyRow   `json:"replies"`
	Confirm  *WAReplyRow    `json:"confirm,omitempty"`
	Busy     bool           `json:"busy,omitempty"`
}

// WAChatRow is a chat in the list.
type WAChatRow struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Last     string `json:"last,omitempty"`
	LastText string `json:"lastText,omitempty"`
	Count    int    `json:"count"`
}

// WAMessageRow is a message as the tab shows it.
type WAMessageRow struct {
	Chat   string `json:"chat"`
	ID     string `json:"id"`
	From   string `json:"from"`
	FromMe bool   `json:"fromMe"`
	At     string `json:"at"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// WAReplyRow is a reply an agent proposed.
type WAReplyRow struct {
	ID       int64  `json:"id"`
	Chat     string `json:"chat"`
	ChatName string `json:"chatName"`
	Text     string `json:"text"`
	By       string `json:"by,omitempty"`
	State    string `json:"state"`
	At       string `json:"at"`
	Error    string `json:"error,omitempty"`
}

func waStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	now := time.Now()
	if y, m, d := now.Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("02 Jan 15:04")
}

func waChatRow(c waChat) WAChatRow {
	name := c.Name
	if name == "" {
		name = c.JID
	}
	return WAChatRow{JID: c.JID, Name: name, Last: waStamp(c.LastAt), LastText: c.LastText, Count: c.Messages}
}

func waMessageRow(m waMessage) WAMessageRow {
	from := m.SenderName
	if m.FromMe {
		from = "you"
	} else if from == "" {
		from = m.Sender
	}
	return WAMessageRow{Chat: m.Chat, ID: m.ID, From: from, FromMe: m.FromMe, At: waStamp(m.At), Kind: m.Kind, Text: m.Text}
}

func waReplyRow(r waReply) WAReplyRow {
	name := r.ChatName
	if name == "" {
		name = r.Chat
	}
	return WAReplyRow{ID: r.ID, Chat: r.Chat, ChatName: name, Text: r.Text, By: r.By, State: r.State, At: waStamp(r.CreatedAt), Error: r.Error}
}

// whatsAppStateLocked is the tab now. The caller holds the lock.
func (s *Session) whatsAppStateLocked() *WhatsAppState {
	w := &s.wa
	st := &WhatsAppState{
		Running: w.running, Linked: w.status.Linked, Connected: w.status.Connected,
		Number: waNumber(w.status.JID), Name: w.status.Name, Query: w.query, Busy: w.busy,
		Chats: []WAChatRow{}, Replies: []WAReplyRow{},
	}
	if w.running && w.status.Linked {
		st.Kept = fmt.Sprintf("%d chats, %d messages kept on this machine", w.status.Chats, w.status.Messages)
	}
	for _, c := range w.chats {
		st.Chats = append(st.Chats, waChatRow(c))
	}
	if w.open != nil {
		row := waChatRow(*w.open)
		st.Open = &row
		for _, m := range w.messages {
			st.Messages = append(st.Messages, waMessageRow(m))
		}
	}
	if w.query != "" {
		st.Results = []WAMessageRow{}
		for _, m := range w.results {
			st.Results = append(st.Results, waMessageRow(m))
		}
	}
	for _, r := range w.replies {
		st.Replies = append(st.Replies, waReplyRow(r))
	}
	if w.confirm != nil {
		row := waReplyRow(*w.confirm)
		st.Confirm = &row
	}
	return st
}

// waNumber is a JID as a phone number: 5511…@s.whatsapp.net, or with a
// device after a colon, is +5511….
func waNumber(jid string) string {
	for i := 0; i < len(jid); i++ {
		if jid[i] == '@' || jid[i] == ':' {
			if i == 0 {
				return ""
			}
			return "+" + jid[:i]
		}
	}
	return jid
}

// waAwaiting are the replies the owner is asked about: the pending ones,
// and the ones whose send failed, which may be approved again.
func waAwaiting(ctx context.Context) ([]waReply, error) {
	var out []waReply
	for _, state := range []string{"pending", "failed"} {
		var res struct {
			Replies []waReply `json:"replies"`
		}
		if err := waCall(ctx, "replies.list", map[string]any{"state": state}, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Replies...)
	}
	return out, nil
}

// loadWhatsApp reads the daemon's status, chats and the replies waiting,
// and the chat open when there is one. A daemon that is not running is not
// an error: the tab says so and offers to start it.
func (s *Session) loadWhatsApp() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var st waStatus
	err := waCall(ctx, "status", nil, &st)
	if errors.Is(err, errNoWhatsApp) {
		s.mu.Lock()
		s.wa.running, s.wa.status, s.wa.chats, s.wa.replies = false, waStatus{}, nil, nil
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	var chats struct {
		Chats []waChat `json:"chats"`
	}
	if err := waCall(ctx, "chats.list", map[string]any{"limit": 100}, &chats); err != nil {
		return err
	}
	replies, err := waAwaiting(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.wa.running, s.wa.status, s.wa.chats, s.wa.replies = true, st, chats.Chats, replies
	open := s.wa.open
	s.mu.Unlock()
	if open != nil {
		return s.readChat(ctx, open.JID)
	}
	return nil
}

func (s *Session) readChat(ctx context.Context, chat string) error {
	var res struct {
		Chat     waChat      `json:"chat"`
		Messages []waMessage `json:"messages"`
	}
	if err := waCall(ctx, "messages.read", map[string]any{"chat": chat, "limit": 200}, &res); err != nil {
		return err
	}
	s.mu.Lock()
	s.wa.open, s.wa.messages = &res.Chat, res.Messages
	s.mu.Unlock()
	return nil
}

// pollWhatsApp reads the tab again every few seconds while it is open, and
// stops when it is not: one poller at a time.
func (s *Session) pollWhatsApp(every time.Duration) {
	if !s.wa.polling.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.wa.polling.Store(false)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
			}
			s.mu.Lock()
			open, confirming := s.dock.tab == "whatsapp", s.wa.confirm != nil
			s.mu.Unlock()
			if !open {
				return
			}
			// While the owner reads a reply to approve, the list under it is
			// left as it is.
			if confirming {
				continue
			}
			if err := s.loadWhatsApp(); err == nil {
				s.stateChanged()
			}
		}
	}()
}

// WhatsAppAction is one thing the tab asks for.
type WhatsAppAction struct {
	Do    string `json:"do"`
	Chat  string `json:"chat"`
	Query string `json:"query"`
	ID    int64  `json:"id"`
	Text  string `json:"text"`
}

// WhatsApp does what the tab asks.
func (s *Session) WhatsApp(a WhatsAppAction) error {
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	var err error
	say := ""
	switch a.Do {
	case "refresh":
		err = s.loadWhatsApp()
	case "start":
		// tend whatsapp status starts the daemon when none answers, as
		// every command of it does.
		s.setWABusy(true)
		out, xerr := exec.CommandContext(ctx, s.tendPath, "whatsapp", "status").CombinedOutput()
		s.setWABusy(false)
		if xerr != nil {
			err = fmt.Errorf("tend whatsapp status: %v: %s", xerr, firstLine(string(out)))
		} else {
			err = s.loadWhatsApp()
		}
	case "link":
		err = s.openLinkTab()
	case "chat":
		err = s.readChat(ctx, a.Chat)
	case "back":
		s.mu.Lock()
		s.wa.open, s.wa.messages = nil, nil
		s.mu.Unlock()
	case "search":
		if a.Query == "" {
			s.mu.Lock()
			s.wa.query, s.wa.results = "", nil
			s.mu.Unlock()
			break
		}
		var res struct {
			Messages []waMessage `json:"messages"`
		}
		err = waCall(ctx, "messages.search", map[string]any{"query": a.Query, "limit": 50}, &res)
		if err == nil {
			s.mu.Lock()
			s.wa.query, s.wa.results = a.Query, res.Messages
			s.mu.Unlock()
		}
	case "confirm":
		// The reply is read from the daemon again, not taken from the list
		// the window has: what the owner is shown is what will be sent.
		var replies []waReply
		replies, err = waAwaiting(ctx)
		if err == nil {
			err = fmt.Errorf("reply %d is no longer waiting for you", a.ID)
			for _, r := range replies {
				if r.ID == a.ID {
					r := r
					s.mu.Lock()
					s.wa.confirm = &r
					s.mu.Unlock()
					err = nil
				}
			}
		}
	case "cancel":
		s.mu.Lock()
		s.wa.confirm = nil
		s.mu.Unlock()
	case "approve":
		say, err = s.approveWhatsApp(ctx, a.ID)
	case "reject":
		err = waCall(ctx, "replies.reject", map[string]any{"id": a.ID, "reason": a.Text}, nil)
		if err == nil {
			say = fmt.Sprintf("reply %d rejected: it will not be sent", a.ID)
			err = s.loadWhatsApp()
		}
	default:
		return fmt.Errorf("the WhatsApp tab asked for %q", a.Do)
	}
	if say != "" && s.Status != nil {
		s.Status(say)
	}
	s.stateChanged()
	return err
}

// approveWhatsApp sends the reply the owner confirmed. Only the reply shown
// in the confirmation is sent, and only once: a second press, or an id
// that is not the one shown, sends nothing.
func (s *Session) approveWhatsApp(ctx context.Context, id int64) (string, error) {
	s.mu.Lock()
	shown := s.wa.confirm
	if shown == nil || shown.ID != id {
		s.mu.Unlock()
		return "", fmt.Errorf("reply %d was not the one shown to you: nothing was sent", id)
	}
	s.wa.confirm = nil
	s.wa.busy = true
	s.mu.Unlock()
	s.stateChanged()
	var res struct {
		Reply waReply `json:"reply"`
	}
	err := waCall(ctx, "replies.approve", map[string]any{"id": id}, &res)
	s.setWABusy(false)
	if lerr := s.loadWhatsApp(); err == nil {
		err = lerr
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sent to %s", waReplyRow(*shown).ChatName), nil
}

func (s *Session) setWABusy(busy bool) {
	s.mu.Lock()
	s.wa.busy = busy
	s.mu.Unlock()
	s.stateChanged()
}

// openLinkTab opens `tend whatsapp link` in a new tab of the space shown:
// linking shows a QR code to scan, which needs a terminal, and is the
// owner's to do — it is never done behind their back.
func (s *Session) openLinkTab() error {
	s.mu.Lock()
	c, ws, tend := s.client, s.workspace, s.tendPath
	s.mu.Unlock()
	if ws == 0 {
		if err := s.ensurePane(); err != nil {
			return err
		}
		s.mu.Lock()
		ws = s.workspace
		s.mu.Unlock()
	}
	if tend == "" {
		tend = "tend"
	}
	spec := proto.PaneSpec{Command: []string{"/bin/sh", "-c", resumeScript, "tend-whatsapp", tend, "whatsapp", "link"}}
	tab, _, err := c.NewTab(ws, "WhatsApp link", spec)
	if err != nil {
		return err
	}
	return s.ShowTab(tab)
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
