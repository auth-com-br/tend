package wa

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

func message(chat string, id string, m *waE2E.Message) *events.Message {
	jid, _ := types.ParseJID(chat)
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: jid, Sender: jid},
			ID:            id, PushName: "Maria", Timestamp: time.Unix(1_790_000_000, 0),
		},
		Message: m,
	}
}

// TestWhatAMessageSaysIsKeptAsText: text, captions and placeholders for
// media, so the agent knows a photo or an audio was there. If it regresses,
// a chat reads with gaps where the customer sent a picture of the problem.
func TestWhatAMessageSaysIsKeptAsText(t *testing.T) {
	cases := []struct {
		msg        *waE2E.Message
		kind, text string
	}{
		{&waE2E.Message{Conversation: new("oi")}, "text", "oi"},
		{&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: new("respondendo")}}, "text", "respondendo"},
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: new("a caixa veio assim")}}, "image", "[image] a caixa veio assim"},
		{&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Seconds: new(uint32(12))}}, "audio", "[audio, 12s]"},
		{&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: new("nota.pdf")}}, "document", "[document nota.pdf]"},
	}
	for _, c := range cases {
		m, ok := fromEvent(message("5511988887777@s.whatsapp.net", "A", c.msg))
		if !ok || m.Kind != c.kind || m.Text != c.text {
			t.Errorf("%v: got %q %q %v, want %q %q", c.msg, m.Kind, m.Text, ok, c.kind, c.text)
		}
	}
	if _, ok := fromEvent(message("5511988887777@s.whatsapp.net", "R", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: new("👍")}})); ok {
		t.Error("a reaction was kept as a message")
	}
	if _, ok := fromEvent(message("status@broadcast", "S", &waE2E.Message{Conversation: new("status")})); ok {
		t.Error("a status update was kept as a chat message")
	}
}

// TestAnEditOrDeletionChangesTheMessageItNames: the protocol message names
// the original by id, and the store keeps it under that id. If it
// regresses, an agent reads a text the customer corrected or took back.
func TestAnEditOrDeletionChangesTheMessageItNames(t *testing.T) {
	edit := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: new("ORIG")},
		EditedMessage: &waE2E.Message{Conversation: new("quero 3, não 2")},
	}}
	m, ok := fromEvent(message("5511988887777@s.whatsapp.net", "EDIT", edit))
	if !ok || m.ID != "ORIG" || m.Text != "quero 3, não 2" {
		t.Errorf("edit = %+v, %v", m, ok)
	}
	revoke := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  &waCommon.MessageKey{ID: new("ORIG")},
	}}
	m, ok = fromEvent(message("5511988887777@s.whatsapp.net", "DEL", revoke))
	if !ok || m.ID != "ORIG" || m.Kind != "deleted" {
		t.Errorf("revoke = %+v, %v", m, ok)
	}
}

// TestWhatsmeowsStoreRunsOnThePureGoSQLite: whatsmeow's sqlstore creates its
// schema and reopens it through modernc's "sqlite" driver, with no cgo, and
// its file is the owner's alone. If it regresses, the daemon cannot keep a
// linked device — or tend-whatsapp needs cgo again.
func TestWhatsmeowsStoreRunsOnThePureGoSQLite(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for range 2 {
		c, err := Open(context.Background(), dir, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		if s := c.Status(); s.Linked || s.Connected {
			t.Errorf("a fresh device says %+v", s)
		}
		if err := c.container.Close(); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, DeviceFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("device.db mode = %v", info.Mode().Perm())
	}
}
