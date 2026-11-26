// Package daemon is tend-whatsapp's long-running process: it holds the one
// connection to WhatsApp (a linked number has one; two processes connecting
// with the same keys throw each other off), keeps what arrives in the store,
// and answers on a unix socket — the MCP server the agents run, the CLI the
// owner runs, and later the desktop's panel are all clients of it.
//
// # The rule
//
// An agent may propose a reply; only the owner sends one. replies.propose
// keeps the reply pending and sends nothing. replies.approve is what sends,
// once: the store moves the reply out of pending with a conditional update,
// so two approvals arriving together send it one time. replies.reject takes
// it out for good. The MCP server has no tool that approves — the owner does,
// from `tend whatsapp approve`, which asks at a terminal, or from a panel.
// The socket itself is the owner's (0600, in a 0700 directory), as tend's
// session socket is: a program running as the owner can reach it, an agent's
// shell among them, and that is the limit of what a file permission can say.
//
// # The protocol
//
// One JSON object per line each way, as tend's automation socket has it:
//
//	→ {"id":"1","method":"chats.list","params":{"limit":20}}
//	← {"id":"1","result":{"chats":[…]}}
//	← {"id":"1","error":{"code":"no_such_chat","message":"…"}}
//
// link is the one method that answers more than once: an "event" line per
// QR code (or the pairing code), then its result.
//
//	→ {"id":"2","method":"link","params":{"phone":""}}
//	← {"id":"2","event":{"type":"code","code":"2@…","timeout_seconds":60}}
//	← {"id":"2","result":{"jid":"5511…@s.whatsapp.net"}}
//
// Methods, and their params:
//
//	status                                    → linked, connected, jid, name, chats, messages, pending
//	chats.list       {limit}                  → chats
//	chats.resolve    {chat}                   → chat (by JID, phone number or name)
//	messages.read    {chat, limit, before}    → chat, messages (oldest first; before is a message id)
//	messages.search  {query, chat, limit}     → messages (most recent first)
//	replies.propose  {chat, text, quoted, by} → reply (pending; nothing is sent)
//	replies.list     {state}                  → replies ("" for all, "pending", "sent", …)
//	replies.approve  {id}                     → reply (sent, or failed with its error)
//	replies.reject   {id, reason}             → reply
//	link             {phone}                  → events, then jid
//	unlink           {forget}                 → {} (forget also erases the messages kept)
//	stop                                      → {} (the daemon exits)
package daemon

import (
	"encoding/json"
	"path/filepath"

	"github.com/auth-com-br/tend/internal/transport"
)

// Request is one call.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one line of an answer: an event, a result, or an error.
type Response struct {
	ID     string     `json:"id"`
	Result any        `json:"result,omitempty"`
	Event  *LinkEvent `json:"event,omitempty"`
	Error  *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is a failed call: a code a program tests and a message a person
// reads.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// LinkEvent is a step of linking the number.
type LinkEvent struct {
	// Type is "code" (a QR code to show), "pair_code" (the code to type on
	// the phone, when linking by phone number), "success" or "error".
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Timeout int    `json:"timeout_seconds,omitempty"`
	Message string `json:"message,omitempty"`
}

// SocketPath is the daemon's socket: a directory of its own under tend's
// runtime directory, so that listing the sessions there does not find it.
func SocketPath() (string, error) {
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

// StateDir is where the databases are: tend's state directory, under
// whatsapp/. They are kept on this machine and nowhere else.
func StateDir() (string, error) {
	dir, err := transport.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "whatsapp"), nil
}
