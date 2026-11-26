package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/mcpserve"
	"github.com/auth-com-br/tend/whatsapp/internal/daemon"
	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// inProcess calls the daemon's Handle directly, through JSON as the socket
// would, so the tools are tested against the real daemon and store.
type inProcess struct{ d *daemon.Daemon }

func (c inProcess) Call(ctx context.Context, method string, params, result any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	out, err := c.d.Handle(ctx, daemon.Request{ID: "t", Method: method, Params: raw}, nil)
	if err != nil {
		return err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

type neverSends struct{ sent int }

func (n *neverSends) Status() daemon.ConnStatus {
	return daemon.ConnStatus{Linked: true, Connected: true}
}
func (n *neverSends) Send(context.Context, string, string, *store.Message) (string, error) {
	n.sent++
	return "X", nil
}
func (n *neverSends) Link(context.Context, string, func(daemon.LinkEvent) error) (string, error) {
	return "", nil
}
func (n *neverSends) Unlink(context.Context) error { return nil }

const maria = "5511988887777@s.whatsapp.net"

func setup(t *testing.T) (*Tools, *neverSends, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	base := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	_ = st.PutMessages(ctx,
		store.Message{Chat: maria, ID: "m1", Sender: maria, SenderName: "Maria", At: base, Text: "Oi, o pedido 123 não chegou"},
		store.Message{Chat: maria, ID: "m2", FromMe: true, At: base.Add(time.Minute), Text: "Vou verificar"},
		store.Message{Chat: "5521977776666@s.whatsapp.net", ID: "j1", At: base.Add(2 * time.Minute), Text: "Bom dia"},
	)
	_ = st.NameChat(ctx, maria, "Maria Souza", false)
	conn := &neverSends{}
	return &Tools{Daemon: inProcess{daemon.New(st, conn)}, By: "p_4", Location: time.UTC}, conn, st
}

func call(t *testing.T, tools *Tools, name string, args map[string]any) string {
	t.Helper()
	out, err := tools.Call(name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

// TestTheToolsAreTheFourAndNoneSends: the agent is given list_chats,
// read_chat, search and propose_reply, over tend's MCP framing. If it
// regresses — a send tool appears — an agent can write to a customer without
// the owner.
func TestTheToolsAreTheFourAndNoneSends(t *testing.T) {
	tools, _, _ := setup(t)
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"
	var out bytes.Buffer
	if err := mcpserve.ServeTools(tools, "test", strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"name":"whatsapp"`) {
		t.Fatalf("answers = %q", lines)
	}
	var list struct {
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range list.Result.Tools {
		names = append(names, tool.Name)
	}
	if got := strings.Join(names, ","); got != "list_chats,read_chat,search,propose_reply" {
		t.Fatalf("tools = %s", got)
	}
}

// TestReadingAChatShowsWhoSaidWhatWithIDs: each line has the time, the
// writer (the owner as "owner") and the id a reply can quote. If it
// regresses, an agent cannot tell the customer from the owner, or quote.
func TestReadingAChatShowsWhoSaidWhatWithIDs(t *testing.T) {
	tools, _, _ := setup(t)
	out := call(t, tools, "read_chat", map[string]any{"chat": "Maria"})
	for _, want := range []string{"chat Maria Souza (" + maria + ")", "[2026-11-26 09:00] Maria: Oi, o pedido 123 não chegou (id m1)", "owner: Vou verificar (id m2)"} {
		if !strings.Contains(out, want) {
			t.Errorf("read_chat lacks %q:\n%s", want, out)
		}
	}
	list := call(t, tools, "list_chats", nil)
	if !strings.HasPrefix(list, "(no name) — 5521977776666@s.whatsapp.net") || !strings.Contains(list, "Maria Souza") {
		t.Errorf("list_chats:\n%s", list)
	}
	found := call(t, tools, "search", map[string]any{"query": "nao chegou"})
	if !strings.Contains(found, maria) || !strings.Contains(found, "id m1") {
		t.Errorf("search:\n%s", found)
	}
}

// TestProposeReplyOnlyQueues: propose_reply answers with the reply's id and
// says it is not sent, and nothing is sent. If it regresses, the agent
// believes it answered the customer, or it did.
func TestProposeReplyOnlyQueues(t *testing.T) {
	tools, conn, st := setup(t)
	out := call(t, tools, "propose_reply", map[string]any{"chat": "+55 11 98888-7777", "text": "Já verifiquei, chega amanhã.", "quoted": "m1"})
	if !strings.Contains(out, "reply 1 to Maria Souza") || !strings.Contains(out, "not been sent") {
		t.Errorf("propose_reply said %q", out)
	}
	if conn.sent != 0 {
		t.Fatalf("propose_reply sent %d messages", conn.sent)
	}
	replies, _ := st.Replies(context.Background(), store.Pending)
	if len(replies) != 1 || replies[0].By != "p_4" || replies[0].Quoted != "m1" {
		t.Fatalf("pending = %+v", replies)
	}
}

// TestAnAmbiguousChatIsTheAgentsToResolve: two chats matching is a tool
// error naming both. If it regresses, a reply is proposed to the wrong
// customer.
func TestAnAmbiguousChatIsTheAgentsToResolve(t *testing.T) {
	tools, _, st := setup(t)
	_ = st.NameChat(context.Background(), "5521977776666@s.whatsapp.net", "Mariana", false)
	_, err := tools.Call("propose_reply", map[string]any{"chat": "mari", "text": "oi"})
	if err == nil || !strings.Contains(err.Error(), "Mariana") || !strings.Contains(err.Error(), "Maria Souza") {
		t.Fatalf("err = %v", err)
	}
}
