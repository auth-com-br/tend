// Package wa is the one piece that talks to WhatsApp: whatsmeow
// (go.mau.fi/whatsmeow), linked to the owner's number as a companion
// device, the way WhatsApp Web is. It implements daemon.Conn, and writes
// what arrives — live messages and the history the phone syncs after
// linking — into the store.
//
// whatsmeow speaks WhatsApp Web's protocol without WhatsApp's consent; it is
// against WhatsApp's terms, and a number used through it can be banned. The
// owner chose it knowing so (docs/DESKTOP.md), and `tend whatsapp link` says
// it before pairing.
//
// Nothing here is exercised by the tests against WhatsApp itself: they have
// no phone to pair. What is tested is what it hands the store
// (content_test.go) and that its databases open with the pure-Go driver.
package wa

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/auth-com-br/tend/whatsapp/internal/daemon"
	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// DeviceFile is whatsmeow's database, beside the messages: the device's
// keys, the Signal sessions, the contacts the phone shares.
const DeviceFile = "device.db"

// Conn is the connection.
type Conn struct {
	container *sqlstore.Container
	store     *store.Store
	log       waLog.Logger
	// OnLoggedOut is told when the phone removed this device.
	OnLoggedOut func()

	mu     sync.Mutex
	client *whatsmeow.Client
	// linking is set while a link is in progress: there is one connection,
	// and a second link would replace the first one's websocket.
	linking bool
}

var _ daemon.Conn = (*Conn)(nil)

// Open opens whatsmeow's database in dir and makes the client, unconnected.
func Open(ctx context.Context, dir string, st *store.Store, log waLog.Logger) (*Conn, error) {
	if log == nil {
		log = waLog.Noop
	}
	// What the phone lists under "Linked devices".
	wastore.SetOSInfo("tend", [3]uint32{1, 0, 0})
	path := filepath.Join(dir, DeviceFile)
	if err := store.CreatePrivate(path); err != nil {
		return nil, err
	}
	// "sqlite" is modernc's driver name; go.mau.fi/util/dbutil reads any
	// name starting with "sqlite" as the SQLite dialect, so whatsmeow's
	// sqlstore runs on it as it does on mattn's cgo driver ("sqlite3").
	container, err := sqlstore.New(ctx, "sqlite", store.DSN(path), log.Sub("store"))
	if err != nil {
		return nil, fmt.Errorf("whatsapp: opening %s: %w", path, err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return nil, err
	}
	c := &Conn{container: container, store: st, log: log}
	c.client = c.newClient(device)
	return c, nil
}

func (c *Conn) newClient(device *wastore.Device) *whatsmeow.Client {
	cli := whatsmeow.NewClient(device, c.log.Sub("client"))
	cli.AddEventHandler(func(evt any) { c.handle(cli, evt) })
	return cli
}

func (c *Conn) current() *whatsmeow.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// Start connects when a number is linked. Unlinked, it waits for Link.
func (c *Conn) Start() error {
	cli := c.current()
	if cli.Store.ID == nil {
		return nil
	}
	return cli.Connect()
}

// Close disconnects and closes the database.
func (c *Conn) Close() error {
	c.current().Disconnect()
	return c.container.Close()
}

// Status says whether a number is linked and the connection up.
func (c *Conn) Status() daemon.ConnStatus {
	cli := c.current()
	s := daemon.ConnStatus{Linked: cli.Store.ID != nil, Connected: cli.IsConnected() && cli.IsLoggedIn()}
	if cli.Store.ID != nil {
		s.JID = cli.Store.ID.ToNonAD().String()
		s.Name = cli.Store.PushName
	}
	return s
}

// Send sends a text message, quoting the message it answers.
func (c *Conn) Send(ctx context.Context, chat, text string, quoted *store.Message) (string, error) {
	to, err := types.ParseJID(chat)
	if err != nil {
		return "", fmt.Errorf("whatsapp: %q is not a chat: %w", chat, err)
	}
	msg := &waE2E.Message{Conversation: &text}
	if quoted != nil {
		ctxInfo := &waE2E.ContextInfo{
			StanzaID:      new(quoted.ID),
			Participant:   new(quoted.Sender),
			QuotedMessage: &waE2E.Message{Conversation: new(quoted.Text)},
		}
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: &text, ContextInfo: ctxInfo}}
	}
	resp, err := c.current().SendMessage(ctx, to, msg)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// Link pairs the number: QR codes for the phone to scan, or, given a phone
// number, the code to type on it (WhatsApp → Linked devices → Link with
// phone number instead).
func (c *Conn) Link(ctx context.Context, phone string, report func(daemon.LinkEvent) error) (jid string, err error) {
	c.mu.Lock()
	cli := c.client
	if cli.Store.ID != nil {
		c.mu.Unlock()
		return "", errors.New("a number is already linked; unlink it first")
	}
	if c.linking {
		c.mu.Unlock()
		return "", errors.New("a link is already in progress")
	}
	c.linking = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.linking = false
		c.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A link that did not end linked leaves no half-open websocket behind,
	// and a device whatsmeow deleted on a failed pairing (it does, on a
	// PairError) is replaced: it would refuse to connect again.
	defer func() {
		if err == nil {
			return
		}
		cli.Disconnect()
		if cli.Store.Deleted {
			c.fresh()
		}
	}()
	qr, err := cli.GetQRChannel(ctx)
	if err != nil {
		return "", err
	}
	if err := cli.Connect(); err != nil {
		return "", err
	}
	if phone = digits(phone); phone != "" {
		// whatsmeow: the name must read "Browser (OS)", from a list the
		// server accepts.
		code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
		if err != nil {
			return "", err
		}
		if err := report(daemon.LinkEvent{Type: "pair_code", Code: code, Timeout: 160}); err != nil {
			return "", err
		}
	}
	for item := range qr {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if phone != "" {
				continue
			}
			if err := report(daemon.LinkEvent{Type: "code", Code: item.Code, Timeout: int(item.Timeout / time.Second)}); err != nil {
				// Whoever asked is gone: nobody is looking at the code.
				return "", err
			}
		case whatsmeow.QRChannelSuccess.Event:
			if cli.Store.ID == nil {
				return "", errors.New("whatsapp: paired, but no device id was kept")
			}
			return cli.Store.ID.ToNonAD().String(), nil
		case whatsmeow.QRChannelEventError:
			return "", fmt.Errorf("whatsapp: pairing failed: %w", item.Error)
		case whatsmeow.QRChannelTimeout.Event:
			return "", errors.New("whatsapp: the codes ran out before the phone linked; run link again")
		default:
			return "", fmt.Errorf("whatsapp: pairing ended: %s", item.Event)
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", errors.New("whatsapp: pairing ended without a result")
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Unlink removes this device from the number. When WhatsApp cannot be told
// (offline), the keys are deleted here anyway: the phone then shows the
// device until it notices it is gone, and the owner can remove it there.
func (c *Conn) Unlink(ctx context.Context) error {
	cli := c.current()
	if cli.Store.ID == nil {
		return nil
	}
	if err := cli.Logout(ctx); err != nil {
		cli.Disconnect()
		if derr := cli.Store.Delete(ctx); derr != nil {
			return fmt.Errorf("whatsapp: logging out: %v; deleting the keys: %w", err, derr)
		}
	}
	c.fresh()
	return nil
}

// fresh replaces a client whose device was deleted, which whatsmeow will not
// connect again, with one on a new device, ready to link.
func (c *Conn) fresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = c.newClient(c.container.NewDevice())
}

func (c *Conn) handle(cli *whatsmeow.Client, evt any) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	switch e := evt.(type) {
	case *events.Message:
		m, ok := fromEvent(e)
		if !ok {
			return
		}
		if err := c.store.PutMessages(ctx, m); err != nil {
			c.log.Errorf("keeping message %s: %v", m.ID, err)
		}
		if !e.Info.IsGroup && !e.Info.IsFromMe {
			_ = c.store.NameChat(ctx, m.Chat, e.Info.PushName, true)
		}
	case *events.HistorySync:
		c.history(ctx, cli, e)
	case *events.Connected:
		c.nameFromContacts(ctx, cli)
	case *events.LoggedOut:
		// The phone removed the device, or WhatsApp did. whatsmeow has
		// deleted the keys already.
		c.fresh()
		if c.OnLoggedOut != nil {
			c.OnLoggedOut()
		}
	}
}

// history keeps what the phone sends after linking: the recent chats and
// their messages, in batches.
func (c *Conn) history(ctx context.Context, cli *whatsmeow.Client, e *events.HistorySync) {
	for _, conv := range e.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil || chat.Server == types.BroadcastServer {
			continue
		}
		if name := conv.GetName(); name != "" {
			_ = c.store.NameChat(ctx, chat.ToNonAD().String(), name, false)
		}
		var batch []store.Message
		for _, hm := range conv.GetMessages() {
			evt, err := cli.ParseWebMessage(chat, hm.GetMessage())
			if err != nil {
				continue
			}
			if m, ok := fromEvent(evt); ok {
				batch = append(batch, m)
			}
		}
		if err := c.store.PutMessages(ctx, batch...); err != nil {
			c.log.Errorf("keeping history of %s: %v", chat, err)
		}
	}
	c.nameFromContacts(ctx, cli)
}

// nameFromContacts names the chats with the names the owner's phone has for
// them, which whatsmeow keeps from the phone's contact sync.
func (c *Conn) nameFromContacts(ctx context.Context, cli *whatsmeow.Client) {
	contacts, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}
	for jid, info := range contacts {
		name := info.FullName
		if name == "" {
			name = info.BusinessName
		}
		weak := false
		if name == "" {
			name, weak = info.PushName, true
		}
		_ = c.store.RenameChat(ctx, jid.ToNonAD().String(), name, weak)
	}
}
