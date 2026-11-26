// Command tend-whatsapp is tend's WhatsApp for the agents: the owner's
// number linked as a companion device, its conversations kept on this
// machine, an MCP server with which the agents read them and propose
// replies, and the owner's approval before any reply is sent.
//
// It is a binary of its own, in a Go module of its own, because of what it
// weighs: whatsmeow and the pure-Go SQLite made tend 16 MB heavier when
// measured (22.2 MB to 38.3 MB, linux/amd64, CGO_ENABLED=0, November 2026),
// on the binary every user downloads and every remote attach copies, for a
// module only some will use. `tend whatsapp …` runs it when it
// is installed beside tend or on PATH.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/mcpserve"
	"github.com/auth-com-br/tend/whatsapp/internal/daemon"
	"github.com/auth-com-br/tend/whatsapp/internal/mcptools"
	"github.com/auth-com-br/tend/whatsapp/internal/qrterm"
	"github.com/auth-com-br/tend/whatsapp/internal/store"
	"github.com/auth-com-br/tend/whatsapp/internal/wa"
)

var version = "development build"

const usage = `tend whatsapp — the owner's WhatsApp, for the agents

usage: tend whatsapp <command> [options]

  link [-phone N]   link your number as a companion device (QR code, or a
                    code to type on the phone with -phone)
  status            whether it is linked and connected, and what is kept
  unlink [-forget]  remove this device from the number; -forget also erases
                    the conversations kept here
  serve             the MCP server for the agents (stdio):
                      claude mcp add whatsapp -- tend whatsapp serve
  pending [-all]    the replies the agents proposed, waiting for you
  approve <id>      send a proposed reply (asks you at the terminal first)
  reject <id> [why] never send it
  daemon            the process holding the connection (started on demand)
  stop              stop the daemon
  version           print the version

The conversations stay on this machine, in tend's state directory. The agents
can read them and propose replies; only you send.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "daemon":
		err = runDaemon(args)
	case "link":
		err = runLink(args)
	case "status":
		err = runStatus(args)
	case "unlink":
		err = runUnlink(args)
	case "serve", "mcp":
		err = runServe(args)
	case "pending":
		err = runPending(args)
	case "approve":
		err = runApprove(args)
	case "reject":
		err = runReject(args)
	case "stop":
		err = runStop(args)
	case "version":
		fmt.Printf("tend-whatsapp %s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "tend whatsapp: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "tend whatsapp: %v\n", err)
		os.Exit(1)
	}
}

// --- the daemon --------------------------------------------------------------

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	session := fs.String("s", "", "the tend session to notify when a reply waits (default: the pane's, else \"default\")")
	verbose := fs.Bool("v", false, "log whatsmeow's own messages")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := daemon.StateDir()
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "messages.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	// A daemon that stopped mid-send left a reply nobody knows the fate of.
	if err := st.ResetSending(ctx); err != nil {
		return err
	}
	conn, err := wa.Open(ctx, dir, st, logger(*verbose))
	if err != nil {
		return err
	}
	defer conn.Close()

	path, err := daemon.SocketPath()
	if err != nil {
		return err
	}
	ln, err := daemon.Listen(path)
	if err != nil {
		return err
	}
	defer os.Remove(path)

	d := daemon.New(st, conn)
	d.Notify = notifier(*session)
	// whatsmeow's own copy of the contacts goes with the device's keys,
	// which unlinking deletes; what is left is the daemon's log, which
	// names chats in its errors.
	d.Forget = func() error {
		err := os.Truncate(logPath(), 0)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	conn.OnLoggedOut = func() {
		d.Notify("WhatsApp: this device was unlinked", "the phone removed it; tend whatsapp link links it again")
	}
	if err := conn.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "tend-whatsapp: connecting: %v\n", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
		case <-d.Stopped():
		}
		_ = ln.Close()
	}()
	return d.Serve(ln)
}

// notifier tells the tend session a reply is waiting, through its
// automation socket's notification.show (what `tend notify` does). Without a
// session running, nobody is told and nothing fails: the reply is still in
// `tend whatsapp pending`.
func notifier(session string) func(title, body string) {
	socket := os.Getenv(api.EnvSocketPath)
	return func(title, body string) {
		path := socket
		if session != "" || path == "" {
			p, err := apiSocketPath(session)
			if err != nil {
				return
			}
			path = p
		}
		go notify(path, title, body)
	}
}

// --- commands that talk to the daemon -----------------------------------------

func client() (*daemon.Client, error) {
	path, err := daemon.SocketPath()
	if err != nil {
		return nil, err
	}
	return &daemon.Client{Path: path}, nil
}

// running is a client of a daemon that answers, started if none does: the
// MCP server and the CLI should not need the owner to have started anything.
func running(ctx context.Context) (*daemon.Client, error) {
	c, err := client()
	if err != nil {
		return nil, err
	}
	var s daemon.StatusResult
	if err := c.Call(ctx, "status", nil, &s); err == nil {
		return c, nil
	} else if !errors.Is(err, daemon.ErrNoDaemon) {
		return nil, err
	}
	if err := startDaemon(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := c.Call(ctx, "status", nil, &s)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the daemon did not start: %w (its log: %s)", err, logPath())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func logPath() string {
	dir, err := daemon.StateDir()
	if err != nil {
		return "daemon.log"
	}
	return filepath.Join(dir, "daemon.log")
}

func startDaemon() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath()), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(self, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// The warning linking prints. It is plain on purpose: the owner chose this
// way in knowing it, and it is said again each time a number is linked.
const linkWarning = `Linking your WhatsApp number to tend

  • tend links as a companion device, as WhatsApp Web does, through
    whatsmeow — a program WhatsApp did not write and does not allow.
    This is against WhatsApp's terms of service. WhatsApp can BAN THE
    NUMBER for it, without warning, and a banned number may not come back.
    Do not link a number you cannot afford to lose.
  • The conversations are kept on this machine only (%s),
    and are read by the AI agents you give the tools to. The agents send
    what they read to their providers (Anthropic, OpenAI, …) as part of
    their work.
  • LGPD: these are conversations with your customers — personal data of
    theirs. Handing them to AI agents is processing that data; you are the
    controller, and are answerable for having a basis for it, telling your
    customers, and erasing it when asked (tend whatsapp unlink -forget
    erases everything kept here).
  • No reply is sent without your approval: the agents only propose.
`

func runLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	phone := fs.String("phone", "", "link with a code typed on the phone instead of a QR code: the number, with country code (5511…)")
	accept := fs.Bool("accept-risk", false, "do not ask; the risk above is understood")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, _ := daemon.StateDir()
	fmt.Printf(linkWarning, dir)
	if !*accept {
		if !confirm("\nLink anyway? Type yes: ", "yes") {
			return errors.New("not linked")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	var r daemon.LinkResult
	err = c.Stream(ctx, "link", daemon.LinkParams{Phone: *phone}, func(ev daemon.LinkEvent) {
		switch ev.Type {
		case "code":
			art, err := qrterm.Render(ev.Code)
			if err != nil {
				fmt.Fprintf(os.Stderr, "drawing the QR code: %v\n", err)
				return
			}
			// Each new code replaces the last on screen.
			fmt.Print("\x1b[2J\x1b[H")
			fmt.Print(art)
			fmt.Printf("\nOn the phone: WhatsApp → Linked devices → Link a device, and scan this.\n(a new code in %ds; ctrl+c gives up)\n", ev.Timeout)
		case "pair_code":
			fmt.Printf("\nOn the phone: WhatsApp → Linked devices → Link a device → Link with phone number instead,\nand type:  %s\n", ev.Code)
		}
	}, &r)
	if err != nil {
		return err
	}
	fmt.Printf("\nlinked: %s. The phone now sends the recent history; it takes a few minutes.\n", r.JID)
	return nil
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	var s daemon.StatusResult
	if err := c.Call(ctx, "status", nil, &s); err != nil {
		return err
	}
	switch {
	case !s.Linked:
		fmt.Println("not linked: tend whatsapp link")
	case s.Connected:
		fmt.Printf("linked as %s (%s), connected\n", s.JID, s.Name)
	default:
		fmt.Printf("linked as %s, not connected (reconnecting)\n", s.JID)
	}
	fmt.Printf("kept: %d chats, %d messages; %d replies waiting for you\n", s.Chats, s.Messages, s.Pending)
	return nil
}

func runUnlink(args []string) error {
	fs := flag.NewFlagSet("unlink", flag.ExitOnError)
	forget := fs.Bool("forget", false, "also erase every conversation and reply kept on this machine")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	if err := c.Call(ctx, "unlink", daemon.UnlinkParams{Forget: *forget}, nil); err != nil {
		return err
	}
	if *forget {
		fmt.Println("unlinked; the conversations kept here are erased")
	} else {
		fmt.Println("unlinked; the conversations kept here stay (unlink -forget erases them)")
	}
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := running(context.Background())
	if err != nil {
		return err
	}
	tools := &mcptools.Tools{Daemon: c, By: os.Getenv(api.EnvPaneID)}
	return mcpserve.ServeTools(tools, version, os.Stdin, os.Stdout)
}

func runPending(args []string) error {
	fs := flag.NewFlagSet("pending", flag.ExitOnError)
	all := fs.Bool("all", false, "every reply, decided ones too")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	state := store.Pending
	if *all {
		state = ""
	}
	var r daemon.RepliesResult
	if err := c.Call(ctx, "replies.list", daemon.RepliesParams{State: state}, &r); err != nil {
		return err
	}
	if len(r.Replies) == 0 {
		fmt.Println("no replies waiting")
		return nil
	}
	for _, rep := range r.Replies {
		printReply(os.Stdout, rep)
	}
	return nil
}

func printReply(w io.Writer, r store.Reply) {
	to := r.ChatName
	if to == "" {
		to = r.Chat
	}
	fmt.Fprintf(w, "#%d  to %s  [%s]", r.ID, to, r.State)
	if r.By != "" {
		fmt.Fprintf(w, "  proposed by %s", r.By)
	}
	fmt.Fprintf(w, "  %s\n", r.CreatedAt.Format("2006-01-02 15:04"))
	for _, line := range strings.Split(r.Text, "\n") {
		fmt.Fprintf(w, "    %s\n", line)
	}
	if r.Error != "" {
		fmt.Fprintf(w, "    (%s)\n", r.Error)
	}
}

func replyID(args []string, usage string) (int64, error) {
	if len(args) == 0 {
		return 0, errors.New(usage)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a reply id: %s", args[0], usage)
	}
	return id, nil
}

func runApprove(args []string) error {
	id, err := replyID(args, "usage: tend whatsapp approve <id>")
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	var all daemon.RepliesResult
	if err := c.Call(ctx, "replies.list", daemon.RepliesParams{}, &all); err != nil {
		return err
	}
	var found *store.Reply
	for i := range all.Replies {
		if all.Replies[i].ID == id {
			found = &all.Replies[i]
		}
	}
	if found == nil {
		return fmt.Errorf("no reply %d", id)
	}
	printReply(os.Stdout, *found)
	// The approval is asked at a terminal. An agent's shell tool runs
	// without one, so an agent that reaches for this command is stopped
	// here; it is a bar, not a lock (see the daemon package).
	if !confirm("\nSend this, in your name? Type send: ", "send") {
		return errors.New("not sent")
	}
	var r daemon.ReplyResult
	if err := c.Call(ctx, "replies.approve", daemon.DecideParams{ID: id}, &r); err != nil {
		return err
	}
	fmt.Printf("sent (%s)\n", r.Reply.SentID)
	return nil
}

func runReject(args []string) error {
	id, err := replyID(args, "usage: tend whatsapp reject <id> [why]")
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, err := running(ctx)
	if err != nil {
		return err
	}
	var r daemon.ReplyResult
	if err := c.Call(ctx, "replies.reject", daemon.DecideParams{ID: id, Reason: strings.Join(args[1:], " ")}, &r); err != nil {
		return err
	}
	fmt.Printf("reply %d rejected; it will not be sent\n", id)
	return nil
}

func runStop(args []string) error {
	c, err := client()
	if err != nil {
		return err
	}
	err = c.Call(context.Background(), "stop", nil, nil)
	if errors.Is(err, daemon.ErrNoDaemon) {
		fmt.Println("the daemon was not running")
		return nil
	}
	return err
}

// confirm asks at the terminal, and is false when there is none: an answer
// typed by a program is not the owner's.
func confirm(prompt, word string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "\nthis needs you at a terminal: stdin is not one")
		return false
	}
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(line), word)
}
