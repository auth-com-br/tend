// Package mcptools is the MCP server the agents run (`tend whatsapp serve`):
// tools to list the owner's WhatsApp chats, read one, search them, and
// propose a reply. Each tool is one call on the daemon's socket; the framing
// is tend's own MCP server's (internal/mcpserve), shared.
//
// There is no tool that sends. propose_reply keeps the reply pending in the
// daemon and returns its id; the owner approves it, or not.
package mcptools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/mcpserve"
	"github.com/auth-com-br/tend/whatsapp/internal/daemon"
	"github.com/auth-com-br/tend/whatsapp/internal/store"
)

// Caller is the daemon.
type Caller interface {
	Call(ctx context.Context, method string, params, result any) error
}

// Tools is the server.
type Tools struct {
	Daemon Caller
	// By is who proposes, as the owner will see it: the agent's tend pane
	// (TEND_PANE_ID), when it runs in one.
	By string
	// Location is the time zone messages are dated in for the agent.
	Location *time.Location
}

var _ mcpserve.Tools = (*Tools)(nil)

// Info is the server's name and what the agent is told about it.
func (t *Tools) Info() (string, string) {
	return "whatsapp", "These tools read the owner's WhatsApp conversations, kept on this machine by tend, " +
		"and let you propose a reply. You cannot send: propose_reply puts the reply to the owner, who approves " +
		"or rejects it. The conversations are with the owner's customers; treat what is in them as private. " +
		"A chat is named by its name, a phone number, or its JID."
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func intg(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

var chatArg = str("The chat: its name (or a part of it only one chat has), a phone number, or its JID.")

var tools = []map[string]any{
	{
		"name":        "list_chats",
		"description": "The WhatsApp chats, the most recent first: name, JID, when the last message was and what it said.",
		"inputSchema": schema(map[string]any{"limit": intg("How many. Default 30.")}),
	},
	{
		"name": "read_chat",
		"description": "A chat's messages, oldest first, each with its time, who wrote it and its id. " +
			"before (a message id from an earlier read) reads further back.",
		"inputSchema": schema(map[string]any{
			"chat":   chatArg,
			"limit":  intg("How many messages. Default 40."),
			"before": str("A message id: read the messages before it."),
		}, "chat"),
	},
	{
		"name":        "search",
		"description": "Messages with the words in them, accents and case aside, the most recent first, in every chat or in one.",
		"inputSchema": schema(map[string]any{
			"query": str("What to look for."),
			"chat":  str("Only in this chat (optional)."),
			"limit": intg("How many. Default 30."),
		}, "query"),
	},
	{
		"name": "propose_reply",
		"description": "Propose a reply in a chat. It is NOT sent: it waits for the owner, who approves or rejects it. " +
			"Returns its id. Write it as the owner would, to their customer.",
		"inputSchema": schema(map[string]any{
			"chat":   chatArg,
			"text":   str("The reply."),
			"quoted": str("The id of the message it answers, to quote it (optional)."),
		}, "chat", "text"),
	},
}

// List is the tools.
func (t *Tools) List() []map[string]any { return tools }

// Call runs one tool.
func (t *Tools) Call(name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch name {
	case "list_chats":
		var r daemon.ChatsResult
		if err := t.Daemon.Call(ctx, "chats.list", daemon.ChatsParams{Limit: num(args, "limit", 30)}, &r); err != nil {
			return "", err
		}
		if len(r.Chats) == 0 {
			return "no chats are kept yet: the number may not be linked, or the history not synced", nil
		}
		var b strings.Builder
		for _, c := range r.Chats {
			name := c.Name
			if name == "" {
				name = "(no name)"
			}
			fmt.Fprintf(&b, "%s — %s — last %s: %q\n", name, c.JID, t.when(c.LastAt), excerpt(c.LastText, 80))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "read_chat":
		chat, err := need(args, "chat")
		if err != nil {
			return "", err
		}
		var r daemon.ReadResult
		p := daemon.ReadParams{Chat: chat, Limit: num(args, "limit", 40), Before: text(args["before"])}
		if err := t.Daemon.Call(ctx, "messages.read", p, &r); err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "chat %s (%s)\n", nameOr(r.Chat), r.Chat.JID)
		if len(r.Messages) == 0 {
			b.WriteString("no messages")
			return b.String(), nil
		}
		for _, m := range r.Messages {
			b.WriteString(t.line(m, false) + "\n")
		}
		if len(r.Messages) == p.Limit {
			fmt.Fprintf(&b, "(older: before=%s)", r.Messages[0].ID)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "search":
		query, err := need(args, "query")
		if err != nil {
			return "", err
		}
		var r daemon.MessagesResult
		p := daemon.SearchParams{Query: query, Chat: text(args["chat"]), Limit: num(args, "limit", 30)}
		if err := t.Daemon.Call(ctx, "messages.search", p, &r); err != nil {
			return "", err
		}
		if len(r.Messages) == 0 {
			return "nothing found", nil
		}
		var b strings.Builder
		for _, m := range r.Messages {
			b.WriteString(t.line(m, true) + "\n")
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "propose_reply":
		chat, err := need(args, "chat")
		if err != nil {
			return "", err
		}
		body, err := need(args, "text")
		if err != nil {
			return "", err
		}
		var r daemon.ReplyResult
		p := daemon.ProposeParams{Chat: chat, Text: body, Quoted: text(args["quoted"]), By: t.By}
		if err := t.Daemon.Call(ctx, "replies.propose", p, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("reply %d to %s is waiting for the owner's approval; it has not been sent", r.Reply.ID, nameOr(store.Chat{JID: r.Reply.Chat, Name: r.Reply.ChatName})), nil
	}
	return "", fmt.Errorf("%w: %s", mcpserve.ErrUnknownTool, name)
}

// line is one message as an agent reads it; in a search, with its chat.
func (t *Tools) line(m store.Message, withChat bool) string {
	who := m.SenderName
	switch {
	case m.FromMe:
		who = "owner"
	case who == "":
		who, _, _ = strings.Cut(m.Sender, "@")
	}
	s := fmt.Sprintf("[%s] %s: %s (id %s)", t.when(m.At), who, m.Text, m.ID)
	if withChat {
		s = m.Chat + " " + s
	}
	return s
}

func (t *Tools) when(at time.Time) string {
	if at.IsZero() || at.Unix() <= 0 {
		return "never"
	}
	loc := t.Location
	if loc == nil {
		loc = time.Local
	}
	return at.In(loc).Format("2006-01-02 15:04")
}

func nameOr(c store.Chat) string {
	if c.Name != "" {
		return c.Name
	}
	return c.JID
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func need(args map[string]any, key string) (string, error) {
	v := strings.TrimSpace(text(args[key]))
	if v == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

func num(args map[string]any, key string, fallback int) int {
	if n, ok := args[key].(float64); ok && n > 0 {
		return int(n)
	}
	return fallback
}
