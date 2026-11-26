package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "wa", "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

var ctx = context.Background()

func at(min int) time.Time { return time.Date(2026, 11, 26, 10, min, 0, 0, time.UTC) }

// TestTheConversationsAreReadableByTheOwnerOnly: the database and its
// directory are the owner's alone. If it regresses, another user on the
// machine reads the owner's customers' conversations.
func TestTheConversationsAreReadableByTheOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wa")
	s, err := Open(filepath.Join(dir, "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutMessages(ctx, Message{Chat: "1@s.whatsapp.net", ID: "a", At: at(1), Text: "oi"}); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %v, want 0700", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode = %v, want no access for group or others", e.Name(), fi.Mode().Perm())
		}
	}
}

// TestAChatReadsOldestFirstAndPagesBackByMessageID: read_chat shows the last
// messages in the order they were written, and before= reads further back
// without repeating any. If it regresses, an agent reads a conversation
// backwards or in loops.
func TestAChatReadsOldestFirstAndPagesBackByMessageID(t *testing.T) {
	s := open(t)
	chat := "5511999990000@s.whatsapp.net"
	for i, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		if err := s.PutMessages(ctx, Message{Chat: chat, ID: id, At: at(i), Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Messages(ctx, chat, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "m4" || got[1].ID != "m5" {
		t.Fatalf("last two = %+v", got)
	}
	older, err := s.Messages(ctx, chat, 2, got[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 2 || older[0].ID != "m2" || older[1].ID != "m3" {
		t.Fatalf("before m4 = %+v", older)
	}
}

// TestAMessageKeptTwiceIsOneMessage: history sync resends what a live
// message brought, and an edit arrives under the original id. If it
// regresses, the agent reads every message twice, or the old text of an
// edited one.
func TestAMessageKeptTwiceIsOneMessage(t *testing.T) {
	s := open(t)
	chat := "1@s.whatsapp.net"
	m := Message{Chat: chat, ID: "x", Sender: chat, SenderName: "Maria", At: at(1), Text: "quero 2"}
	if err := s.PutMessages(ctx, m, m); err != nil {
		t.Fatal(err)
	}
	m.Text, m.SenderName = "quero 3", ""
	if err := s.PutMessages(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Messages(ctx, chat, 10, "")
	if len(got) != 1 || got[0].Text != "quero 3" || got[0].SenderName != "Maria" {
		t.Fatalf("got %+v", got)
	}
}

// TestSearchIgnoresAccentsAndCase: customers write Portuguese without its
// accents, and the agent may search with or without them. If it regresses,
// "nao" does not find "Não".
func TestSearchIgnoresAccentsAndCase(t *testing.T) {
	s := open(t)
	_ = s.PutMessages(ctx,
		Message{Chat: "a@s.whatsapp.net", ID: "1", At: at(1), Text: "Não recebi o PEDIDO"},
		Message{Chat: "b@s.whatsapp.net", ID: "2", At: at(2), Text: "nao chegou a encomenda"},
		Message{Chat: "b@s.whatsapp.net", ID: "3", At: at(3), Text: "obrigado"},
	)
	got, err := s.Search(ctx, "NÃO", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "1" {
		t.Fatalf("search = %+v", got)
	}
	got, _ = s.Search(ctx, "pedido", "b@s.whatsapp.net", 10)
	if len(got) != 0 {
		t.Fatalf("search in another chat found %+v", got)
	}
}

// TestAChatIsFoundByNamePhoneOrJIDAndNeverByGuess: an agent names a chat
// the way a person would. Two chats matching is an error naming both. If it
// regresses, an agent reads — or proposes a reply to — the wrong customer.
func TestAChatIsFoundByNamePhoneOrJIDAndNeverByGuess(t *testing.T) {
	s := open(t)
	_ = s.PutMessages(ctx,
		Message{Chat: "5511988887777@s.whatsapp.net", ID: "1", At: at(1), Text: "a"},
		Message{Chat: "5521977776666@s.whatsapp.net", ID: "2", At: at(2), Text: "b"},
		Message{Chat: "120363000@g.us", ID: "3", At: at(3), Text: "c"},
	)
	_ = s.NameChat(ctx, "5511988887777@s.whatsapp.net", "Maria Souza", false)
	_ = s.NameChat(ctx, "5521977776666@s.whatsapp.net", "Mariana Lima", false)
	_ = s.NameChat(ctx, "120363000@g.us", "Loja — pedidos", false)

	for ref, want := range map[string]string{
		"5511988887777@s.whatsapp.net": "5511988887777@s.whatsapp.net",
		"+55 11 98888-7777":            "5511988887777@s.whatsapp.net",
		"maria souza":                  "5511988887777@s.whatsapp.net",
		"lima":                         "5521977776666@s.whatsapp.net",
		"pedidos":                      "120363000@g.us",
	} {
		c, err := s.ResolveChat(ctx, ref)
		if err != nil || c.JID != want {
			t.Errorf("%q = %q, %v; want %q", ref, c.JID, err, want)
		}
	}
	if _, err := s.ResolveChat(ctx, "maria"); !errors.Is(err, ErrAmbiguousChat) {
		t.Errorf("maria: err = %v, want ambiguous", err)
	}
	if _, err := s.ResolveChat(ctx, "joão"); !errors.Is(err, ErrNoSuchChat) {
		t.Errorf("joão: err = %v, want no such chat", err)
	}
}

// TestAPushNameDoesNotReplaceTheContactsName: the owner knows a customer by
// the name in their phone; the customer's own push name only names a chat
// that has none, and the contact list names only chats there are. If it
// regresses, chats are renamed by what customers call themselves, or the
// list fills with every contact in the phone.
func TestAPushNameDoesNotReplaceTheContactsName(t *testing.T) {
	s := open(t)
	jid := "1@s.whatsapp.net"
	_ = s.PutMessages(ctx, Message{Chat: jid, ID: "1", At: at(1), Text: "a"})
	_ = s.NameChat(ctx, jid, "Dona Maria (padaria)", false)
	_ = s.NameChat(ctx, jid, "Mah ✨", true)
	_ = s.RenameChat(ctx, "2@s.whatsapp.net", "Somebody", false)
	chats, _ := s.Chats(ctx, 10)
	if len(chats) != 1 || chats[0].Name != "Dona Maria (padaria)" {
		t.Fatalf("chats = %+v", chats)
	}
}

// TestProposingKeepsAReplyPendingAndSendsNothing: a proposal is a row in
// pending, nothing more. If it regresses, the store would hand a reply on to
// be sent without the owner.
func TestProposingKeepsAReplyPendingAndSendsNothing(t *testing.T) {
	s := open(t)
	jid := "1@s.whatsapp.net"
	_ = s.PutMessages(ctx, Message{Chat: jid, ID: "q", At: at(1), Text: "tem?"})
	r, err := s.Propose(ctx, jid, "Tem sim!", "q", "p_3")
	if err != nil {
		t.Fatal(err)
	}
	if r.State != Pending || r.Quoted != "q" || r.By != "p_3" {
		t.Fatalf("reply = %+v", r)
	}
	if _, err := s.Propose(ctx, jid, "x", "nope", ""); !errors.Is(err, ErrNoSuchMessage) {
		t.Errorf("quoting a message not in the chat: err = %v", err)
	}
	if _, err := s.Propose(ctx, jid, "  ", "", ""); err == nil {
		t.Error("an empty reply was kept")
	}
}

// TestTwoApprovalsOfOneReplyStartOneSend: approvals arriving together — two
// clicks, the CLI and a panel — move the reply to sending once. If it
// regresses, the customer gets the message twice.
func TestTwoApprovalsOfOneReplyStartOneSend(t *testing.T) {
	s := open(t)
	r, _ := s.Propose(ctx, "1@s.whatsapp.net", "ok", "", "")
	var wg sync.WaitGroup
	var mu sync.Mutex
	started := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.StartSending(ctx, r.ID); err == nil {
				mu.Lock()
				started++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if started != 1 {
		t.Fatalf("%d approvals started a send, want 1", started)
	}
}

// TestARejectedReplyCannotBeApproved: rejected is final. If it regresses, a
// reply the owner turned down could still be sent.
func TestARejectedReplyCannotBeApproved(t *testing.T) {
	s := open(t)
	r, _ := s.Propose(ctx, "1@s.whatsapp.net", "ok", "", "")
	if _, err := s.Reject(ctx, r.ID, "tom errado"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartSending(ctx, r.ID); !errors.Is(err, ErrDecided) {
		t.Fatalf("approving a rejected reply: err = %v", err)
	}
}

// TestAReplyLeftSendingByAStoppedDaemonIsNotSentAgainSilently: whether it
// went is unknown, so it becomes failed with a word to look first. If it
// regresses, it stays stuck, or goes back to pending and an approval sends
// it a second time without warning.
func TestAReplyLeftSendingByAStoppedDaemonIsNotSentAgainSilently(t *testing.T) {
	s := open(t)
	r, _ := s.Propose(ctx, "1@s.whatsapp.net", "ok", "", "")
	if _, err := s.StartSending(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetSending(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Reply(ctx, r.ID)
	if got.State != Failed || got.Error == "" {
		t.Fatalf("reply = %+v", got)
	}
}

// TestForgetErasesEverything: unlink -forget leaves nothing of the
// conversations. If it regresses, a customer's request to be erased is not
// honoured.
func TestForgetErasesEverything(t *testing.T) {
	s := open(t)
	_ = s.PutMessages(ctx, Message{Chat: "1@s.whatsapp.net", ID: "1", At: at(1), Text: "a"})
	_, _ = s.Propose(ctx, "1@s.whatsapp.net", "ok", "", "")
	if err := s.Forget(ctx); err != nil {
		t.Fatal(err)
	}
	chats, messages, pending, err := s.Counts(ctx)
	if err != nil || chats+messages+pending != 0 {
		t.Fatalf("after forget: %d chats, %d messages, %d pending, %v", chats, messages, pending, err)
	}
}
