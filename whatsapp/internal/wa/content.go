package wa

import (
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// content is what a message says, as an agent reads it: its kind and its
// text. Media is not downloaded — the agents read conversations, and a
// photo a customer sent is a placeholder with its caption, not a file on
// disk that nobody asked to keep. ok is false for what is not a message to
// read: reactions, receipts, the protocol's own messages.
func content(m *waE2E.Message) (kind, text string, ok bool) {
	if m == nil {
		return "", "", false
	}
	switch {
	case m.GetConversation() != "":
		return "text", m.GetConversation(), true
	case m.GetExtendedTextMessage() != nil:
		return "text", m.GetExtendedTextMessage().GetText(), true
	case m.GetImageMessage() != nil:
		return "image", placeholder("image", m.GetImageMessage().GetCaption()), true
	case m.GetVideoMessage() != nil:
		return "video", placeholder("video", m.GetVideoMessage().GetCaption()), true
	case m.GetAudioMessage() != nil:
		return "audio", fmt.Sprintf("[audio, %ds]", m.GetAudioMessage().GetSeconds()), true
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		return "document", placeholder("document "+d.GetFileName(), d.GetCaption()), true
	case m.GetStickerMessage() != nil:
		return "sticker", "[sticker]", true
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		where := strings.TrimSpace(l.GetName() + " " + l.GetAddress())
		return "location", strings.TrimSpace(fmt.Sprintf("[location %.5f,%.5f] %s", l.GetDegreesLatitude(), l.GetDegreesLongitude(), where)), true
	case m.GetContactMessage() != nil:
		return "contact", "[contact] " + m.GetContactMessage().GetDisplayName(), true
	case m.GetPollCreationMessage() != nil:
		p := m.GetPollCreationMessage()
		var opts []string
		for _, o := range p.GetOptions() {
			opts = append(opts, o.GetOptionName())
		}
		return "poll", fmt.Sprintf("[poll] %s: %s", p.GetName(), strings.Join(opts, " / ")), true
	}
	return "", "", false
}

func placeholder(what, caption string) string {
	if caption = strings.TrimSpace(caption); caption != "" {
		return "[" + strings.TrimSpace(what) + "] " + caption
	}
	return "[" + strings.TrimSpace(what) + "]"
}

// fromEvent is a received message as the store keeps it. An edit or a
// deletion is the message it changes, under that message's id, so the chat
// reads as the customer sees it now.
func fromEvent(evt *events.Message) (store.Message, bool) {
	info := evt.Info
	m := store.Message{
		Chat:       info.Chat.ToNonAD().String(),
		ID:         info.ID,
		Sender:     info.Sender.ToNonAD().String(),
		SenderName: info.PushName,
		FromMe:     info.IsFromMe,
		At:         info.Timestamp,
	}
	if m.At.IsZero() {
		m.At = time.Now()
	}
	msg := evt.Message
	if p := msg.GetProtocolMessage(); p != nil {
		switch p.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			m.ID, m.Kind, m.Text = p.GetKey().GetID(), "deleted", "[deleted]"
			return m, m.ID != ""
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			kind, text, ok := content(p.GetEditedMessage())
			m.ID, m.Kind, m.Text = p.GetKey().GetID(), kind, text
			return m, ok && m.ID != ""
		}
		return m, false
	}
	kind, text, ok := content(msg)
	m.Kind, m.Text = kind, text
	return m, ok && info.Chat.Server != types.BroadcastServer
}
