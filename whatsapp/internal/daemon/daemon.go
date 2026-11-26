package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// Conn is the WhatsApp connection, behind an interface so that everything
// above it — the rule, the socket, the tools — is tested without a network
// and without a phone.
type Conn interface {
	// Status is whether a number is linked and the connection is up.
	Status() ConnStatus
	// Send sends text to chat, as a reply to quoted when it is not nil, and
	// returns WhatsApp's id for the message.
	Send(ctx context.Context, chat, text string, quoted *store.Message) (string, error)
	// Link pairs a number, reporting each code to show, until it is linked
	// or ctx ends. With a phone number it is the pairing-code flow (a code
	// typed on the phone) instead of the QR code.
	Link(ctx context.Context, phone string, report func(LinkEvent) error) (jid string, err error)
	// Unlink removes this device from the number and forgets its keys.
	Unlink(ctx context.Context) error
}

// ConnStatus is what Status says.
type ConnStatus struct {
	Linked    bool   `json:"linked"`
	Connected bool   `json:"connected"`
	JID       string `json:"jid,omitempty"`
	Name      string `json:"name,omitempty"`
}

// Daemon answers on the socket.
type Daemon struct {
	Store *store.Store
	Conn  Conn
	// Notify, when set, tells the owner a reply is waiting for them: the
	// tend session's notification, the way a blocked agent is announced.
	Notify func(title, body string)
	// Forget erases what is kept besides the store (nothing, in tests).
	Forget func() error

	stopOnce sync.Once
	stopped  chan struct{}
}

// ErrNotConnected is an approval while WhatsApp is not connected: the reply
// stays pending rather than becoming a failed send.
var ErrNotConnected = errors.New("WhatsApp is not connected; the reply stays pending")

// New is a daemon on a store and a connection.
func New(s *store.Store, c Conn) *Daemon {
	return &Daemon{Store: s, Conn: c, stopped: make(chan struct{})}
}

// Stopped is closed when a client asked the daemon to stop.
func (d *Daemon) Stopped() <-chan struct{} { return d.stopped }

// Serve answers every connection on ln until it is closed.
func (d *Daemon) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-d.stopped:
				return nil
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go d.serveConn(conn)
	}
}

func (d *Daemon) serveConn(conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := bufio.NewReaderSize(conn, 1<<20)
	var mu sync.Mutex
	enc := json.NewEncoder(conn)
	write := func(resp Response) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(resp)
	}
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var req Request
			if jerr := json.Unmarshal(line, &req); jerr != nil {
				_ = write(Response{Error: &ErrorBody{"parse_error", jerr.Error()}})
			} else {
				result, herr := d.Handle(ctx, req, func(ev LinkEvent) error {
					return write(Response{ID: req.ID, Event: &ev})
				})
				resp := Response{ID: req.ID, Result: result}
				if herr != nil {
					resp = Response{ID: req.ID, Error: errorBody(herr)}
				}
				if write(resp) != nil {
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func errorBody(err error) *ErrorBody {
	code := "error"
	switch {
	case errors.Is(err, store.ErrNoSuchChat):
		code = "no_such_chat"
	case errors.Is(err, store.ErrAmbiguousChat):
		code = "ambiguous_chat"
	case errors.Is(err, store.ErrNoSuchReply):
		code = "no_such_reply"
	case errors.Is(err, store.ErrNoSuchMessage):
		code = "no_such_message"
	case errors.Is(err, store.ErrDecided):
		code = "decided"
	case errors.Is(err, ErrNotConnected):
		code = "not_connected"
	case errors.Is(err, errUnknownMethod):
		code = "unknown_method"
	}
	return &ErrorBody{Code: code, Message: err.Error()}
}

var errUnknownMethod = errors.New("unknown method")

// Params of the methods.
type (
	ChatsParams struct {
		Limit int `json:"limit,omitempty"`
	}
	ChatParams struct {
		Chat string `json:"chat"`
	}
	ReadParams struct {
		Chat   string `json:"chat"`
		Limit  int    `json:"limit,omitempty"`
		Before string `json:"before,omitempty"`
	}
	SearchParams struct {
		Query string `json:"query"`
		Chat  string `json:"chat,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	ProposeParams struct {
		Chat   string `json:"chat"`
		Text   string `json:"text"`
		Quoted string `json:"quoted,omitempty"`
		By     string `json:"by,omitempty"`
	}
	RepliesParams struct {
		State string `json:"state,omitempty"`
	}
	DecideParams struct {
		ID     int64  `json:"id"`
		Reason string `json:"reason,omitempty"`
	}
	LinkParams struct {
		Phone string `json:"phone,omitempty"`
	}
	UnlinkParams struct {
		Forget bool `json:"forget,omitempty"`
	}
)

// Results of the methods that are more than one thing.
type (
	StatusResult struct {
		ConnStatus
		Chats    int `json:"chats"`
		Messages int `json:"messages"`
		Pending  int `json:"pending"`
	}
	ChatsResult struct {
		Chats []store.Chat `json:"chats"`
	}
	ChatResult struct {
		Chat store.Chat `json:"chat"`
	}
	ReadResult struct {
		Chat     store.Chat      `json:"chat"`
		Messages []store.Message `json:"messages"`
	}
	MessagesResult struct {
		Messages []store.Message `json:"messages"`
	}
	ReplyResult struct {
		Reply store.Reply `json:"reply"`
	}
	RepliesResult struct {
		Replies []store.Reply `json:"replies"`
	}
	LinkResult struct {
		JID string `json:"jid"`
	}
)

// Handle answers one request; emit carries link's events.
func (d *Daemon) Handle(ctx context.Context, req Request, emit func(LinkEvent) error) (any, error) {
	decode := func(v any) error {
		if len(req.Params) == 0 {
			return nil
		}
		if err := json.Unmarshal(req.Params, v); err != nil {
			return fmt.Errorf("%s: invalid params: %w", req.Method, err)
		}
		return nil
	}
	switch req.Method {
	case "status":
		chats, messages, pending, err := d.Store.Counts(ctx)
		if err != nil {
			return nil, err
		}
		return StatusResult{ConnStatus: d.Conn.Status(), Chats: chats, Messages: messages, Pending: pending}, nil
	case "chats.list":
		var p ChatsParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		chats, err := d.Store.Chats(ctx, p.Limit)
		return ChatsResult{Chats: nonNil(chats)}, err
	case "chats.resolve":
		var p ChatParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		c, err := d.Store.ResolveChat(ctx, p.Chat)
		return ChatResult{Chat: c}, err
	case "messages.read":
		var p ReadParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		c, err := d.Store.ResolveChat(ctx, p.Chat)
		if err != nil {
			return nil, err
		}
		msgs, err := d.Store.Messages(ctx, c.JID, p.Limit, p.Before)
		return ReadResult{Chat: c, Messages: nonNil(msgs)}, err
	case "messages.search":
		var p SearchParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		chat := ""
		if p.Chat != "" {
			c, err := d.Store.ResolveChat(ctx, p.Chat)
			if err != nil {
				return nil, err
			}
			chat = c.JID
		}
		msgs, err := d.Store.Search(ctx, p.Query, chat, p.Limit)
		return MessagesResult{Messages: nonNil(msgs)}, err
	case "replies.propose":
		var p ProposeParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		return d.propose(ctx, p)
	case "replies.list":
		var p RepliesParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		replies, err := d.Store.Replies(ctx, p.State)
		return RepliesResult{Replies: nonNil(replies)}, err
	case "replies.approve":
		var p DecideParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		r, err := d.approve(ctx, p.ID)
		return ReplyResult{Reply: r}, err
	case "replies.reject":
		var p DecideParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		r, err := d.Store.Reject(ctx, p.ID, p.Reason)
		return ReplyResult{Reply: r}, err
	case "link":
		var p LinkParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		jid, err := d.Conn.Link(ctx, p.Phone, emit)
		return LinkResult{JID: jid}, err
	case "unlink":
		var p UnlinkParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if err := d.Conn.Unlink(ctx); err != nil {
			return nil, err
		}
		if p.Forget {
			if err := d.Store.Forget(ctx); err != nil {
				return nil, err
			}
			if d.Forget != nil {
				if err := d.Forget(); err != nil {
					return nil, err
				}
			}
		}
		return struct{}{}, nil
	case "stop":
		d.stopOnce.Do(func() { close(d.stopped) })
		return struct{}{}, nil
	}
	return nil, fmt.Errorf("%w: %s", errUnknownMethod, req.Method)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (d *Daemon) propose(ctx context.Context, p ProposeParams) (any, error) {
	c, err := d.Store.ResolveChat(ctx, p.Chat)
	if err != nil {
		return nil, err
	}
	r, err := d.Store.Propose(ctx, c.JID, p.Text, p.Quoted, p.By)
	if err != nil {
		return nil, err
	}
	if d.Notify != nil {
		name := c.Name
		if name == "" {
			name = c.JID
		}
		d.Notify("WhatsApp: a reply to "+name+" waits for you",
			fmt.Sprintf("%s — tend whatsapp approve %d, or reject %d", excerpt(r.Text, 80), r.ID, r.ID))
	}
	return ReplyResult{Reply: r}, nil
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// approve sends a reply the owner approved, once.
func (d *Daemon) approve(ctx context.Context, id int64) (store.Reply, error) {
	// Checked before the reply leaves pending: an approval while offline
	// would otherwise become a failed send, and the owner would be asked to
	// look in the chat for a message that cannot have gone.
	if !d.Conn.Status().Connected {
		r, err := d.Store.Reply(ctx, id)
		if err != nil {
			return r, err
		}
		return r, ErrNotConnected
	}
	r, err := d.Store.StartSending(ctx, id)
	if err != nil {
		return r, err
	}
	var quoted *store.Message
	if r.Quoted != "" {
		if m, err := d.Store.Message(ctx, r.Chat, r.Quoted); err == nil {
			quoted = &m
		}
	}
	// The send is given its own time, not the caller's: a client that hangs
	// up mid-send must not leave a message half-decided.
	sendCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	sentID, sendErr := d.Conn.Send(sendCtx, r.Chat, r.Text, quoted)
	cancel()
	r, err = d.Store.FinishSending(context.Background(), id, sentID, sendErr)
	if err != nil {
		return r, err
	}
	if sendErr != nil {
		return r, fmt.Errorf("sending reply %d: %w", id, sendErr)
	}
	// The sent message is in the chat now; WhatsApp echoes it back too, and
	// the store keeps one copy by its id.
	status := d.Conn.Status()
	_ = d.Store.PutMessages(context.Background(), store.Message{
		Chat: r.Chat, ID: sentID, Sender: status.JID, FromMe: true, At: time.Now(), Kind: "text", Text: r.Text,
	})
	return r, nil
}
