# tend — instructions for AI agents

Read this file before touching anything. It is short on purpose. The longer
documents it points to are:

- `docs/PORTING.md` — what has been ported from herdr, what has not, and where
  each missing piece lives in herdr's source. **This is the work queue.**
- `docs/ARCHITECTURE.md` — how tend is built and why.

## What this project is

`tend` is a port of [herdr](https://github.com/herdrdev/herdr) — a terminal
runtime for AI coding agents, written in Rust — to Go, under a different name.
The owner's goal is a **1:1 port**: tend should do what herdr does, the way
herdr does it.

herdr's source is checked out next to this repository:

```
../herdr        the original, in Rust — the specification
../tend         this repository
```

If `../herdr` is missing, stop and ask for it. Do not port from memory.

## The rule that matters most

**When you implement or fix something, read how herdr does it first.**

This is not a style preference. The mouse selection feature went through eight
failed attempts — each one a plausible design — and was solved in an hour once
somebody read `../herdr/src/client/shell/mouse.rs`. herdr turned out not to do
the thing being built at all: it forwards the mouse to the program and copies
what the program copies. Every heuristic built before that was standing in for
something that already worked.

So, in order:

1. Find the feature in herdr (`docs/PORTING.md` has a map; otherwise `grep`).
2. Read the code, not just the names. Note what it does **not** do.
3. Port the behaviour. Go idiom where it differs from Rust, same behaviour.
4. If you deliberately diverge, write it down in `docs/PORTING.md` under
   "Different from herdr on purpose", with the reason.

## Verify against the real thing

A stand-in proves your code does what you thought. Only the real program proves
you thought the right thing.

- Testing agent behaviour? Run the real agent in a pane. `claude` is installed
  on the owner's machine. A shell script named `claude` is a fixture, not
  evidence: the real Claude Code enables any-event mouse tracking in SGR and
  keeps no scrollback, and no guess about it survived contact with the real one.
- When something fails on the owner's machine and not in your test, the
  difference is the finding. Probe the live session instead of theorising:
  `internal/client` can dial any running session and ask it what it sees.
- State what you verified and what you did not. "Tested with a fixture that
  imitates X" and "tested with X" are different claims.

## Build, test, install

Go lives at `~/.local/go` and is usually **not on PATH**. The Makefile resolves
it; for direct `go` commands, `export PATH="$HOME/.local/go/bin:$PATH"` first.

```bash
make dev        # fast loop: cached, stops at first failure, no vet
make check      # THE GATE: gofmt + vet + tests + tests under the race detector
make install    # build and install to GOBIN (or ~/.local/bin)
make restart    # rebuild, stop the session server, attach to a fresh one
make bench      # internal/vt hot paths must stay at 0 allocs/op
```

`make check` must pass before every commit. It takes about two minutes. **Read
its result before committing** — do not chain the commit onto the command that
prints it. A commit on a red gate has happened here once, exactly that way.

If a test fails only sometimes, find out why before calling it flaky. Three
"flaky" tests in this repository were real bugs: a liveness probe whose 250ms
timeout deleted a live server's socket under load, a wait that a blank redraw
could satisfy, and a server-side lookup that deadlocked on its own mutex.

## The trap everyone falls into: the server outlives the client

`tend` is a client and a detached server. **Rebuilding does not restart the
server.** `make build && ./bin/tend` attaches a new client to yesterday's
server, and everything the new code added on the server side is simply absent.

- After changing anything under `internal/server`, `internal/vt`,
  `internal/session` or `internal/pty`: the server must be restarted
  (`make restart`, or `tend kill -s <name> -server`). The layout, each pane's
  directory and its scrollback come back; **the programs running in the panes
  do not** — a restored pane is a new shell. An agent mid-task is lost.
  `tend handoff` (after `make install`) replaces the server and keeps the
  programs, and is what to reach for on the owner's machine — but only a server
  that already has the feature can do it, and a change to the handoff itself
  is not tested by a handoff from the old code.
- After changing only `cmd/tend` or `internal/ui`: restarting the client is
  enough (`ctrl+b d`, then `tend`).
- `tend kill -s <name>` **without** `-server` closes panes by number and leaves
  the server running. Do not tell the owner to run it as a restart.
- When you add a server capability that is not a new method — an event, a
  snapshot field — add it to `proto.KnownFeatures`, or an older server will go
  unnoticed and the feature will fail silently against it.

## Architecture boundaries

These are herdr's rules and they are enforced here by where code lives:

- **State is separate from runtime.** `internal/session` is pure data: no ptys,
  no goroutines, testable on its own. `internal/server` pairs it with live
  processes.
- **Render is pure.** `internal/ui` draws a `Frame` into a grid and mutates
  nothing. Geometry used for drawing is the same function used for hit-testing
  (`SidebarRowAt`, `TabAt`, `MenuItemAt`) — never two descriptions of one layout.
- **Shared fact or client state?** A fact about the session (which group a space
  is in, what an agent is doing) belongs to the server and goes over the wire.
  What one person is looking at (scroll position, folded groups, selection, the
  sidebar divider) stays in the client and is never sent.
- **Detection reads a snapshot.** It never touches the parser or the viewport.
- **The terminal core is pure Go, no cgo.** The owner chose this over binding
  libghostty. Do not "fix" it.
- **The desktop client lives in `desktop/`, its own Go module**, and is the
  only place cgo and Qt are allowed. It is another client of the same server;
  it imports tend's `internal/` packages and must not be imported by them.
  `make check` does not cover it: `make desktop-check` does. Read
  `docs/DESKTOP.md` before touching it.
- **Lock order** is server, then pane runtime, never the reverse. Do no I/O while
  holding the server lock: it is the lock every pane operation needs.

## Code conventions

- Comments say **why**, in full sentences, and are dense in this codebase. Match
  that. A comment restating the code is noise; a comment recording the bug that
  shaped the code is the most valuable kind here. Read a few files first.
- No `panic` in production paths. Errors are returned and named
  (`session.ErrNoSuchPane`, `proto.ErrUnknownMethod`).
- New dependencies need a reason. Current ones: `creack/pty`, `BurntSushi/toml`,
  `mattn/go-runewidth`, `golang.org/x/term`, `gopkg.in/yaml.v3` (the DeepSeek
  Harness's MCP servers are in a hand-written YAML file, edited keeping its
  comments). `whatsapp/`, a module of its own built into `tend-whatsapp`,
  adds `go.mau.fi/whatsmeow` (the owner's WhatsApp number linked as a
  companion device, as WhatsApp Web is — chosen by the owner knowing it is
  against WhatsApp's terms), `modernc.org/sqlite` (whatsmeow keeps its keys
  in SQL; this driver is pure Go, so no cgo), `rsc.io/qr` (the QR code to
  link with, drawn in the terminal) and `golang.org/x/text` (accent-blind
  search). They are kept out of the `tend` binary on purpose: they made it
  16 MB heavier. `make check` covers that module too (`make whatsapp-check`).
- Platform code is compile-gated by filename (`foreground_linux.go`,
  `foreground_other.go`), not by runtime checks.
- Every behaviour change comes with a test. Test names are sentences, and the
  doc comment says what breaks for the user if it regresses.
- End-to-end tests in `cmd/tend` drive the real binary over a pty. They assert on
  the rendered screen. Wait for the condition you need (`waitForScreen`,
  `sendUntil`, `openMenuOn`), never for a fixed time and a hope.

## Commits

- Lowercase conventional commits: `feat(ui): ...`, `fix: ...`, `test: ...`.
- The body explains the reasoning and names the bug when there was one. Look at
  `git log` — the history is written to be read.
- Commit and push only when the owner asks, or when continuing work they have
  already told you to commit as you go.
- Do not use the herdr name or marks in the product. `LICENSE` is Apache-2.0 and
  `NOTICE` attributes herdr; keep both.

## Working with the owner

The owner writes in Brazilian Portuguese and reads replies in it. Code, comments
and commit messages stay in English.

Report plainly. Say what you verified and what you did not. When you are wrong,
say so in a sentence and move on. When something keeps failing, stop adding
fixes and go get evidence — from herdr's source, or from the live session.
