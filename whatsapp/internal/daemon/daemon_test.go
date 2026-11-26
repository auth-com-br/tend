package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// fakeConn is WhatsApp without the network: it records what it is asked to
// send.
type fakeConn struct {
	mu        sync.Mutex
	connected bool
	sent      []sent
	fail      error
	linkCodes []string
}

type sent struct {
	chat, text string
	quoted     *store.Message
}

func (f *fakeConn) Status() ConnStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ConnStatus{Linked: true, Connected: f.connected, JID: "5511000000000@s.whatsapp.net"}
}

func (f *fakeConn) Send(_ context.Context, chat, text string, quoted *store.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.sent = append(f.sent, sent{chat, text, quoted})
	return "SENT" + string(rune('0'+len(f.sent))), nil
}

func (f *fakeConn) Link(ctx context.Context, phone string, report func(LinkEvent) error) (string, error) {
	for _, c := range f.linkCodes {
		if err := report(LinkEvent{Type: "code", Code: c, Timeout: 20}); err != nil {
			return "", err
		}
	}
	return "5511000000000@s.whatsapp.net", nil
}

func (f *fakeConn) Unlink(context.Context) error { return nil }

func (f *fakeConn) sends() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sent...)
}

const customer = "5511988887777@s.whatsapp.net"

// serving is a daemon on a real socket in a private runtime directory, with
// one chat kept.
func serving(t *testing.T) (*Client, *fakeConn, *Daemon) {
	t.Helper()
	dir, err := os.MkdirTemp("", "wa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TEND_RUNTIME_DIR", dir)
	st, err := store.Open(filepath.Join(dir, "state", "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	_ = st.PutMessages(ctx, store.Message{Chat: customer, ID: "q1", Sender: customer, SenderName: "Maria",
		At: time.Now().Add(-time.Minute), Text: "Vocês entregam sábado?"})
	_ = st.NameChat(ctx, customer, "Maria Souza", false)

	conn := &fakeConn{connected: true}
	d := New(st, conn)
	path, err := SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = d.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	return &Client{Path: path}, conn, d
}

func propose(t *testing.T, c *Client) store.Reply {
	t.Helper()
	var r ReplyResult
	if err := c.Call(context.Background(), "replies.propose", ProposeParams{Chat: "maria", Text: "Entregamos sim!", Quoted: "q1", By: "p_2"}, &r); err != nil {
		t.Fatal(err)
	}
	return r.Reply
}

// TestTheDaemonSocketIsTheOwnersOnly: the socket is 0600 in a 0700
// directory, beside — not among — the sessions' sockets. If it regresses,
// another user on the machine reads the conversations and approves replies,
// or `tend ls` lists the daemon as a session.
func TestTheDaemonSocketIsTheOwnersOnly(t *testing.T) {
	c, _, _ := serving(t)
	info, err := os.Stat(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(c.Path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("socket directory mode = %v, want 0700", dirInfo.Mode().Perm())
	}
	if filepath.Dir(c.Path) == os.Getenv("TEND_RUNTIME_DIR") {
		t.Error("the socket is beside the sessions' sockets")
	}
	// A second daemon is refused: one number has one connection.
	if _, err := Listen(c.Path); err == nil {
		t.Error("a second daemon took the socket")
	}
}

// TestProposingNeverSends: an agent's proposal waits for the owner. If it
// regresses, an agent writes to a customer in the owner's name.
func TestProposingNeverSends(t *testing.T) {
	c, conn, _ := serving(t)
	r := propose(t, c)
	if r.State != store.Pending || r.ID == 0 || r.Chat != customer {
		t.Fatalf("reply = %+v", r)
	}
	if got := conn.sends(); len(got) != 0 {
		t.Fatalf("a proposal sent %+v", got)
	}
}

// TestApprovingSendsExactlyOnce: an approval sends the reply, quoting the
// message it answers; approving it again sends nothing more. If it
// regresses, the customer gets nothing, or the same message twice.
func TestApprovingSendsExactlyOnce(t *testing.T) {
	c, conn, _ := serving(t)
	r := propose(t, c)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.Call(ctx, "replies.approve", DecideParams{ID: r.ID}, nil)
		}()
	}
	wg.Wait()
	close(errs)
	ok := 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case !IsCode(err, "decided"):
			t.Errorf("approval failed with %v", err)
		}
	}
	got := conn.sends()
	if ok != 1 || len(got) != 1 {
		t.Fatalf("%d approvals succeeded, %d sends; want 1 and 1", ok, len(got))
	}
	if got[0].chat != customer || got[0].text != "Entregamos sim!" || got[0].quoted == nil || got[0].quoted.ID != "q1" {
		t.Errorf("sent %+v", got[0])
	}
	var after ReplyResult
	if err := c.Call(ctx, "replies.approve", DecideParams{ID: r.ID}, &after); !IsCode(err, "decided") {
		t.Errorf("approving a sent reply: %v", err)
	}
	// The sent reply is in the chat, for the agent reading it next.
	var read ReadResult
	_ = c.Call(ctx, "messages.read", ReadParams{Chat: customer}, &read)
	if n := len(read.Messages); n != 2 || !read.Messages[n-1].FromMe {
		t.Errorf("chat after sending = %+v", read.Messages)
	}
}

// TestRejectingNeverSends: a rejected reply is never sent, approved after or
// not. If it regresses, a reply the owner turned down reaches the customer.
func TestRejectingNeverSends(t *testing.T) {
	c, conn, _ := serving(t)
	r := propose(t, c)
	ctx := context.Background()
	if err := c.Call(ctx, "replies.reject", DecideParams{ID: r.ID, Reason: "não"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(ctx, "replies.approve", DecideParams{ID: r.ID}, nil); !IsCode(err, "decided") {
		t.Errorf("approving a rejected reply: %v", err)
	}
	if got := conn.sends(); len(got) != 0 {
		t.Fatalf("a rejected reply was sent: %+v", got)
	}
}

// TestAnApprovalWhileOfflineLeavesTheReplyPending: nothing can go, so
// nothing is marked as tried. If it regresses, the owner is told a send
// failed and asked to check the chat for a message that cannot be there.
func TestAnApprovalWhileOfflineLeavesTheReplyPending(t *testing.T) {
	c, conn, _ := serving(t)
	r := propose(t, c)
	conn.mu.Lock()
	conn.connected = false
	conn.mu.Unlock()
	if err := c.Call(context.Background(), "replies.approve", DecideParams{ID: r.ID}, nil); !IsCode(err, "not_connected") {
		t.Fatalf("err = %v", err)
	}
	var list RepliesResult
	_ = c.Call(context.Background(), "replies.list", RepliesParams{State: store.Pending}, &list)
	if len(list.Replies) != 1 {
		t.Fatalf("pending = %+v", list.Replies)
	}
}

// TestAFailedSendCanBeApprovedAgain: a send WhatsApp refused is the owner's
// to try again. If it regresses, the reply is stuck.
func TestAFailedSendCanBeApprovedAgain(t *testing.T) {
	c, conn, _ := serving(t)
	r := propose(t, c)
	conn.mu.Lock()
	conn.fail = errors.New("websocket closed")
	conn.mu.Unlock()
	if err := c.Call(context.Background(), "replies.approve", DecideParams{ID: r.ID}, nil); err == nil {
		t.Fatal("a failed send was reported as sent")
	}
	conn.mu.Lock()
	conn.fail = nil
	conn.mu.Unlock()
	var ok ReplyResult
	if err := c.Call(context.Background(), "replies.approve", DecideParams{ID: r.ID}, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Reply.State != store.Sent || len(conn.sends()) != 1 {
		t.Fatalf("reply = %+v, sends = %d", ok.Reply, len(conn.sends()))
	}
}

// TestAProposalIsAnnounced: the owner is told a reply waits, with the
// command that decides it. If it regresses, replies wait unseen.
func TestAProposalIsAnnounced(t *testing.T) {
	c, _, d := serving(t)
	told := make(chan string, 1)
	d.Notify = func(title, body string) { told <- title + " | " + body }
	r := propose(t, c)
	select {
	case got := <-told:
		want := "approve " + strconv.FormatInt(r.ID, 10)
		if !strings.Contains(got, "Maria Souza") || !strings.Contains(got, want) {
			t.Errorf("notice = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nobody was told")
	}
}

// TestLinkStreamsItsCodes: the QR codes reach the client one by one before
// the result. If it regresses, `tend whatsapp link` shows nothing to scan.
func TestLinkStreamsItsCodes(t *testing.T) {
	c, conn, _ := serving(t)
	conn.linkCodes = []string{"2@first", "2@second"}
	var codes []string
	var r LinkResult
	err := c.Stream(context.Background(), "link", LinkParams{}, func(ev LinkEvent) { codes = append(codes, ev.Code) }, &r)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || codes[1] != "2@second" || r.JID == "" {
		t.Fatalf("codes = %q, result = %+v", codes, r)
	}
}
