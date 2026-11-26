// Package store keeps what tend-whatsapp knows: the chats, their messages,
// and the replies the agents have proposed, with what the owner decided about
// each.
//
// It is pure data, as internal/session is for tend's server: no connection to
// WhatsApp, no goroutines, testable on its own. The WhatsApp side writes what
// it receives into it; the daemon reads it for the agents and moves replies
// from proposed to sent.
//
// It is SQLite through modernc.org/sqlite, the C library translated to Go, so
// the binary is built with CGO_ENABLED=0 like tend's. The same driver holds
// whatsmeow's device keys in a database of their own (see internal/wa); the
// two are apart so that unlinking can drop the keys and keep the messages, or
// forget both.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	// The pure-Go SQLite: registers the "sqlite" driver.
	_ "modernc.org/sqlite"
)

// Errors a caller tells apart.
var (
	ErrNoSuchChat    = errors.New("no such chat")
	ErrAmbiguousChat = errors.New("more than one chat matches")
	ErrNoSuchReply   = errors.New("no such reply")
	ErrNoSuchMessage = errors.New("no such message")
	// ErrDecided is a reply that is no longer the owner's to decide: it was
	// sent, is being sent, or was rejected.
	ErrDecided = errors.New("the reply was already decided")
)

// Message is one WhatsApp message as it is kept.
type Message struct {
	Chat string `json:"chat"`
	ID   string `json:"id"`
	// Sender is the JID of who wrote it; SenderName the name WhatsApp gave
	// for them when it arrived (the push name), which may be empty.
	Sender     string    `json:"sender"`
	SenderName string    `json:"sender_name,omitempty"`
	FromMe     bool      `json:"from_me"`
	At         time.Time `json:"at"`
	// Kind is "text", or what else it was ("image", "audio", "document",
	// …); Text is the text or the caption, with a placeholder for what has
	// none, so an agent reading the chat knows something was there.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Chat is one conversation.
type Chat struct {
	JID      string    `json:"jid"`
	Name     string    `json:"name,omitempty"`
	LastAt   time.Time `json:"last_at"`
	LastText string    `json:"last_text,omitempty"`
	Messages int       `json:"messages"`
}

// Reply states.
const (
	// Pending: proposed by an agent, waiting for the owner.
	Pending = "pending"
	// Sending: approved, being sent now. It is a state of its own so that two
	// approvals arriving together send once: only one of them moves the
	// reply out of pending.
	Sending = "sending"
	Sent    = "sent"
	// Failed: approved, and the send failed; the owner may approve it again.
	Failed   = "failed"
	Rejected = "rejected"
)

// Reply is a reply an agent proposed.
type Reply struct {
	ID   int64  `json:"id"`
	Chat string `json:"chat"`
	// ChatName is the chat's name when the reply was listed, for the owner
	// deciding on it.
	ChatName string `json:"chat_name,omitempty"`
	Text     string `json:"text"`
	// Quoted is the id of a message in the chat the reply answers, empty
	// when it answers none.
	Quoted string `json:"quoted,omitempty"`
	// By is who proposed it: the agent's pane, as the MCP server knows it.
	By        string    `json:"by,omitempty"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	DecidedAt time.Time `json:"decided_at,omitzero"`
	// SentID is WhatsApp's id for the message once sent; Error why the last
	// send failed, or why it was rejected.
	SentID string `json:"sent_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Store is the database.
type Store struct {
	db *sql.DB
}

// filePerm and dirPerm keep the conversations the owner's alone: they are
// with customers, and nobody else on the machine is to read them.
const (
	filePerm os.FileMode = 0o600
	dirPerm  os.FileMode = 0o700
)

// Open opens, and creates if need be, the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, err
	}
	// SQLite creates a database 0644 under the usual umask, and its journal
	// files take the database's mode; made 0600 first, all of them are.
	if err := CreatePrivate(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		return nil, err
	}
	// One connection: SQLite has one writer anyway, and a second connection
	// is a second place for "database is locked" to come from.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: %s: %w", path, err)
	}
	return s, nil
}

// CreatePrivate makes an empty file at path, readable by its owner only,
// unless there is one; one that is there is narrowed to the same.
func CreatePrivate(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, filePerm)
	if err != nil {
		return err
	}
	_ = f.Close()
	return os.Chmod(path, filePerm)
}

// DSN is how modernc's driver is told to open a file: foreign keys on (which
// whatsmeow's schema relies on), a wait instead of an error when the file is
// busy, and the write-ahead log, so the agents reading do not hold up a
// message arriving.
func DSN(path string) string {
	return "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	jid     TEXT PRIMARY KEY,
	name    TEXT NOT NULL DEFAULT '',
	last_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	chat        TEXT NOT NULL,
	id          TEXT NOT NULL,
	sender      TEXT NOT NULL DEFAULT '',
	sender_name TEXT NOT NULL DEFAULT '',
	from_me     INTEGER NOT NULL DEFAULT 0,
	at          INTEGER NOT NULL,
	kind        TEXT NOT NULL DEFAULT 'text',
	text        TEXT NOT NULL DEFAULT '',
	fold        TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (chat, id)
);
CREATE INDEX IF NOT EXISTS messages_by_time ON messages (chat, at);
CREATE TABLE IF NOT EXISTS replies (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	chat       TEXT NOT NULL,
	text       TEXT NOT NULL,
	quoted     TEXT NOT NULL DEFAULT '',
	by         TEXT NOT NULL DEFAULT '',
	state      TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	decided_at INTEGER NOT NULL DEFAULT 0,
	sent_id    TEXT NOT NULL DEFAULT '',
	error      TEXT NOT NULL DEFAULT ''
);
`

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

// Fold is text as search compares it: lower case and without accents, so an
// agent searching "nao" finds "Não" — SQLite's own lower() knows only ASCII,
// and Portuguese is written with accents that customers often leave out.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// PutMessages keeps messages, replacing one already kept with the same chat
// and id: an edit arrives as the same message again, and a history sync
// sends what a live message already brought.
func (s *Store) PutMessages(ctx context.Context, msgs ...Message) error {
	if len(msgs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, m := range msgs {
		if m.Chat == "" || m.ID == "" {
			continue
		}
		kind := m.Kind
		if kind == "" {
			kind = "text"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages (chat, id, sender, sender_name, from_me, at, kind, text, fold)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (chat, id) DO UPDATE SET
				kind = excluded.kind, text = excluded.text, fold = excluded.fold,
				sender_name = CASE WHEN excluded.sender_name != '' THEN excluded.sender_name ELSE messages.sender_name END`,
			m.Chat, m.ID, m.Sender, m.SenderName, m.FromMe, m.At.UnixMilli(), kind, m.Text, Fold(m.Text)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO chats (jid, last_at) VALUES (?, ?)
			ON CONFLICT (jid) DO UPDATE SET last_at = max(chats.last_at, excluded.last_at)`,
			m.Chat, m.At.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// NameChat gives a chat its name. A weak name — a sender's push name — is
// only taken when the chat has none: a contact's or a group's name is the
// one the owner knows the chat by, and a customer's self-chosen push name
// must not replace it.
func (s *Store) NameChat(ctx context.Context, jid, name string, weak bool) error {
	name = strings.TrimSpace(name)
	if jid == "" || name == "" {
		return nil
	}
	q := `INSERT INTO chats (jid, name) VALUES (?, ?)
		ON CONFLICT (jid) DO UPDATE SET name = excluded.name`
	if weak {
		q += ` WHERE chats.name = ''`
	}
	_, err := s.db.ExecContext(ctx, q, jid, name)
	return err
}

// RenameChat is NameChat for a chat already kept, and nothing for one that
// is not: the phone's contact list names the chats there are, and must not
// add one per contact the owner has never written to.
func (s *Store) RenameChat(ctx context.Context, jid, name string, weak bool) error {
	name = strings.TrimSpace(name)
	if jid == "" || name == "" {
		return nil
	}
	q := `UPDATE chats SET name = ? WHERE jid = ?`
	if weak {
		q += ` AND name = ''`
	}
	_, err := s.db.ExecContext(ctx, q, name, jid)
	return err
}

// Chats lists the chats, the most recent first.
func (s *Store) Chats(ctx context.Context, limit int) ([]Chat, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.jid, c.name, c.last_at,
			coalesce((SELECT text FROM messages m WHERE m.chat = c.jid ORDER BY at DESC LIMIT 1), ''),
			(SELECT count(*) FROM messages m WHERE m.chat = c.jid)
		FROM chats c ORDER BY c.last_at DESC, c.jid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var at int64
		if err := rows.Scan(&c.JID, &c.Name, &at, &c.LastText, &c.Messages); err != nil {
			return nil, err
		}
		c.LastAt = time.UnixMilli(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveChat finds the chat an agent means: its JID, a phone number in it
// (digits, with or without + and spaces), or its name — exactly, or a part
// of it that only one chat's name has. Two chats matching is an error that
// names them, so the agent asks again instead of reading the wrong customer.
func (s *Store) ResolveChat(ctx context.Context, ref string) (Chat, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Chat{}, fmt.Errorf("%w: no chat given", ErrNoSuchChat)
	}
	chats, err := s.Chats(ctx, 1<<30)
	if err != nil {
		return Chat{}, err
	}
	for _, c := range chats {
		if c.JID == ref {
			return c, nil
		}
	}
	if digits, ok := phoneNumber(ref); ok {
		var found []Chat
		for _, c := range chats {
			user, _, _ := strings.Cut(c.JID, "@")
			if strings.HasSuffix(user, digits) {
				found = append(found, c)
			}
		}
		if c, err := one(ref, found); !errors.Is(err, ErrNoSuchChat) {
			return c, err
		}
	}
	folded := Fold(ref)
	var exact, part []Chat
	for _, c := range chats {
		name := Fold(c.Name)
		switch {
		case name == "":
		case name == folded:
			exact = append(exact, c)
		case strings.Contains(name, folded):
			part = append(part, c)
		}
	}
	if len(exact) > 0 {
		return one(ref, exact)
	}
	return one(ref, part)
}

func one(ref string, found []Chat) (Chat, error) {
	switch len(found) {
	case 0:
		return Chat{}, fmt.Errorf("%w: %q", ErrNoSuchChat, ref)
	case 1:
		return found[0], nil
	}
	var names []string
	for _, c := range found {
		names = append(names, fmt.Sprintf("%s (%s)", c.Name, c.JID))
	}
	return Chat{}, fmt.Errorf("%w %q: %s", ErrAmbiguousChat, ref, strings.Join(names, ", "))
}

// phoneNumber is ref's digits when ref is written as a phone number: digits,
// with the +, spaces, dashes and brackets people write one with. Six digits
// at least, so a name with a number in it is not read as one.
func phoneNumber(ref string) (string, bool) {
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("+ -()", r):
		default:
			return "", false
		}
	}
	return b.String(), b.Len() >= 6
}

// Messages reads a chat's last messages, oldest first, as a chat reads.
// before, when given, is a message id: the page ends just before it, which
// is how an agent reads further back.
func (s *Store) Messages(ctx context.Context, chat string, limit int, before string) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	until := int64(1 << 62)
	if before != "" {
		m, err := s.Message(ctx, chat, before)
		if err != nil {
			return nil, err
		}
		until = m.At.UnixMilli()
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat, id, sender, sender_name, from_me, at, kind, text FROM messages
		WHERE chat = ? AND (at < ? OR (at = ? AND id < ?))
		ORDER BY at DESC, id DESC LIMIT ?`, chat, until, until, before, limit)
	if err != nil {
		return nil, err
	}
	out, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// Message is one message, by chat and id.
func (s *Store) Message(ctx context.Context, chat, id string) (Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat, id, sender, sender_name, from_me, at, kind, text FROM messages
		WHERE chat = ? AND id = ?`, chat, id)
	if err != nil {
		return Message{}, err
	}
	out, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(out) == 0 {
		return Message{}, fmt.Errorf("%w: %s in %s", ErrNoSuchMessage, id, chat)
	}
	return out[0], nil
}

// Search finds messages whose text has query in it, accents and case aside,
// the most recent first; in one chat when chat is given.
func (s *Store) Search(ctx context.Context, query, chat string, limit int) ([]Message, error) {
	query = Fold(strings.TrimSpace(query))
	if query == "" {
		return nil, errors.New("nothing to search for")
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat, id, sender, sender_name, from_me, at, kind, text FROM messages
		WHERE instr(fold, ?) > 0 AND (? = '' OR chat = ?)
		ORDER BY at DESC LIMIT ?`, query, chat, chat, limit)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var at int64
		if err := rows.Scan(&m.Chat, &m.ID, &m.Sender, &m.SenderName, &m.FromMe, &at, &m.Kind, &m.Text); err != nil {
			return nil, err
		}
		m.At = time.UnixMilli(at)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Counts is how much is kept, for status.
func (s *Store) Counts(ctx context.Context) (chats, messages, pending int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM chats), (SELECT count(*) FROM messages),
		(SELECT count(*) FROM replies WHERE state = 'pending')`).Scan(&chats, &messages, &pending)
	return
}

// Forget erases every chat, message and reply kept: what the owner asks for
// when unlinking with -forget, and what a customer's request to be erased
// (LGPD's right to deletion) comes down to when it is all of them.
func (s *Store) Forget(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages; DELETE FROM chats; DELETE FROM replies;`); err != nil {
		return err
	}
	// The rows are gone from the tables; VACUUM takes them out of the file's
	// free pages too, where they would otherwise stay readable.
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}

// Propose keeps a reply an agent proposed, pending. It sends nothing: that
// is the whole of the rule, and it is here, where the reply is made, rather
// than in a caller that could forget it.
func (s *Store) Propose(ctx context.Context, chat, text, quoted, by string) (Reply, error) {
	if strings.TrimSpace(text) == "" {
		return Reply{}, errors.New("the reply is empty")
	}
	if quoted != "" {
		if _, err := s.Message(ctx, chat, quoted); err != nil {
			return Reply{}, err
		}
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO replies (chat, text, quoted, by, state, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		chat, text, quoted, by, Pending, now.UnixMilli())
	if err != nil {
		return Reply{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Reply{}, err
	}
	return s.Reply(ctx, id)
}

const replyColumns = `r.id, r.chat, coalesce(c.name, ''), r.text, r.quoted, r.by, r.state, r.created_at, r.decided_at, r.sent_id, r.error`

// Reply is one reply.
func (s *Store) Reply(ctx context.Context, id int64) (Reply, error) {
	out, err := s.queryReplies(ctx, `WHERE r.id = ?`, id)
	if err != nil {
		return Reply{}, err
	}
	if len(out) == 0 {
		return Reply{}, fmt.Errorf("%w: %d", ErrNoSuchReply, id)
	}
	return out[0], nil
}

// Replies lists replies in a state (every state when it is empty), the
// oldest first: the owner decides them in the order they were asked.
func (s *Store) Replies(ctx context.Context, state string) ([]Reply, error) {
	return s.queryReplies(ctx, `WHERE ? = '' OR r.state = ? ORDER BY r.id`, state, state)
}

func (s *Store) queryReplies(ctx context.Context, where string, args ...any) ([]Reply, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+replyColumns+` FROM replies r LEFT JOIN chats c ON c.jid = r.chat `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reply
	for rows.Next() {
		var r Reply
		var created, decided int64
		if err := rows.Scan(&r.ID, &r.Chat, &r.ChatName, &r.Text, &r.Quoted, &r.By, &r.State, &created, &decided, &r.SentID, &r.Error); err != nil {
			return nil, err
		}
		r.CreatedAt = time.UnixMilli(created)
		if decided != 0 {
			r.DecidedAt = time.UnixMilli(decided)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StartSending takes a reply the owner approved out of pending (or failed,
// for an approval given again after a failed send) and into sending. Only
// one caller can: the update is conditional on the state, so two approvals
// of one reply send it once, whichever process they come from.
func (s *Store) StartSending(ctx context.Context, id int64) (Reply, error) {
	return s.transition(ctx, id, Sending, "", "", Pending, Failed)
}

// FinishSending records how the send went.
func (s *Store) FinishSending(ctx context.Context, id int64, sentID string, sendErr error) (Reply, error) {
	if sendErr != nil {
		return s.transition(ctx, id, Failed, "", sendErr.Error(), Sending)
	}
	return s.transition(ctx, id, Sent, sentID, "", Sending)
}

// Reject takes a pending (or failed) reply out of the owner's queue for
// good; it is never sent.
func (s *Store) Reject(ctx context.Context, id int64, reason string) (Reply, error) {
	return s.transition(ctx, id, Rejected, "", reason, Pending, Failed)
}

// ResetSending puts back replies a daemon that stopped mid-send left in
// sending: whether WhatsApp got them is not known, so they become failed —
// the owner sees the error and decides — rather than pending, which an
// approval would send a second time without saying so.
func (s *Store) ResetSending(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE replies SET state = ?, error = ? WHERE state = ?`,
		Failed, "tend-whatsapp stopped while sending it; it may or may not have gone — look in the chat before approving it again", Sending)
	return err
}

func (s *Store) transition(ctx context.Context, id int64, to, sentID, reason string, from ...string) (Reply, error) {
	q := `UPDATE replies SET state = ?, decided_at = ?, sent_id = CASE WHEN ? != '' THEN ? ELSE sent_id END, error = ?
		WHERE id = ? AND state IN (` + strings.TrimSuffix(strings.Repeat("?,", len(from)), ",") + `)`
	args := []any{to, time.Now().UnixMilli(), sentID, sentID, reason, id}
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return Reply{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Reply{}, err
	}
	r, err := s.Reply(ctx, id)
	if err != nil {
		return Reply{}, err
	}
	if n == 0 {
		return r, fmt.Errorf("%w: reply %d is %s", ErrDecided, id, r.State)
	}
	return r, nil
}
