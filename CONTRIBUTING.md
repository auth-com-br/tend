# Contributing to tend

Thanks for wanting to help. This page is for people; [AGENTS.md](AGENTS.md)
says the same rules more tersely for AI agents, and is worth reading too.

## What tend is

tend is a terminal runtime for AI coding agents: a detached server that keeps
agent terminals alive, knows whether each agent is working, blocked or idle,
and a client that shows them in spaces, tabs and panes. It began as a Go port
of [herdr](https://github.com/herdrdev/herdr) (Rust), and has features of its
own on top — the files panel, agent sessions, GitHub issues and pull
requests, the context buffer and the browser extension.

Two documents are the map:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — how tend is built, and the
  principles a change must not break.
- [docs/PORTING.md](docs/PORTING.md) — what has been ported from herdr, what
  has not, what is tend's own, and where each missing piece lives in herdr's
  source. **This is the work queue.**

## Setting up

You need Go 1.27 or newer, git, and a Unix (Linux or macOS; Windows through
WSL). Some features use other programs when they are there — `gh` for
GitHub, Chromium or Edge for tend's browser, `paplay` for sounds — and the
tests that need one skip or use a stand-in when it is not.

```bash
git clone https://github.com/auth-com-br/tend
cd tend
make dev        # the fast loop: cached, stops at the first failure
make install    # build, and put tend in GOBIN or ~/.local/bin
tend            # start a session, or attach to the running one
```

If you work on something ported from herdr, check herdr out beside tend:

```
../herdr        the original, in Rust — the specification
../tend         this repository
```

## The rule that matters most

**Before you implement or fix something that herdr has, read how herdr does
it.** Find it (docs/PORTING.md has a map; otherwise grep), read the code and
not just the names, note what it does *not* do, and port the behaviour — Go
idiom where Go differs from Rust, the same behaviour. If you diverge on
purpose, write it down in docs/PORTING.md under "Different from herdr on
purpose", with the reason.

This is not taste. A feature here went through eight plausible designs that
failed before somebody read herdr's source and found it did not do the thing
being built at all.

For a feature that is tend's own, look for how a tool the users already know
does it first, and say which in the code and in PORTING.md — the issues panel
follows Orca's, for instance.

## The trap: the server outlives the client

tend is a client and a detached server, and **rebuilding does not restart the
server.** A new client attached to yesterday's server simply lacks whatever
the new code added on the server side.

- Changed `internal/server`, `internal/vt`, `internal/session` or
  `internal/pty`? The server must be replaced: `tend handoff` (after
  `make install`) does it and keeps the programs running in the panes;
  `make restart` or `tend kill -s <name> -server` restarts it, and the
  programs in the panes are lost.
- Changed only `cmd/tend` or `internal/ui`? Restarting the client is enough:
  `ctrl+b d`, then `tend`.
- Adding something the server offers? A new method goes in
  `proto.KnownMethods`; anything else — an event, a field — in
  `proto.KnownFeatures`. That is how a client notices an older server and
  says so, instead of failing silently.

## The gate

```bash
make check      # gofmt + vet + tests + tests under the race detector
```

`make check` must pass before every commit. It takes a couple of minutes.
Read its result before committing; do not chain the commit onto it.

Other targets: `make watch` reruns `make dev` on every change, `make bench`
runs the terminal core's benchmarks (`internal/vt` must stay at 0 allocs/op),
`make restart` rebuilds and starts a fresh server. `make deb` and
`make appimage` package the programs (docs/DESKTOP.md, "Packages"). CI
(`.github/workflows/ci.yml`) runs `make check`, and `make desktop-check` and
`make deb` in an Ubuntu container with Qt 6.

If a test fails only sometimes, find out why before calling it flaky. Several
"flaky" tests here were real bugs — the latest, a test that failed only under
the race detector, was a list reload wiping a message before it could be
read.

## Writing code

- **Comments say why**, in full sentences. A comment restating the code is
  noise; one recording the bug that shaped the code is the most valuable kind
  here. Read a few files first and match them.
- **No `panic` in production paths.** Errors are returned, and named where a
  caller tells them apart (`session.ErrNoSuchPane`, `github.ErrNotLoggedIn`).
- **Respect the boundaries** (docs/ARCHITECTURE.md): `internal/session` is pure
  data; drawing in `internal/ui` mutates nothing, and the same geometry is used
  to draw and to hit-test; detection reads a snapshot; the terminal core is
  pure Go, no cgo; lock the server before a pane, never the reverse, and do no
  I/O while holding the server's lock.
- **Shared fact or client state?** A fact about the session goes over the
  wire from the server. What one person is looking at — scroll, selection,
  what is typed into a search — stays in the client.
- **Platform code** is split by file name (`foo_linux.go`, `foo_other.go`),
  not by runtime checks.
- **New dependencies need a reason.** Today there are four: `creack/pty`,
  `BurntSushi/toml`, `mattn/go-runewidth`, `golang.org/x/term`.

## Tests

Every change in behaviour comes with a test.

- Test names are sentences, and the doc comment says what breaks for the user
  if it regresses:

  ```go
  // TestAConversationOpenInAPaneIsNotDeleted: ... If it regresses, deleting
  // old sessions deletes the file of an agent still writing to it.
  ```

- End-to-end tests in `cmd/tend` drive the real binary over a pty and assert on
  the rendered screen. Wait for the condition you need (`waitForScreen`,
  `sendUntil`), never for a fixed time.
- Tests never touch your real setup: they use temporary runtime directories and
  a quiet settings file, and stand-ins for outside programs (`gh`, `claude`)
  put first on PATH.

**Verify against the real thing, and say which you did.** A stand-in proves
your code does what you thought; only the real program proves you thought the
right thing. "Tested with a script that imitates gh" and "tested with gh" are
different claims — write down which in your pull request.

## Commits and pull requests

- Lowercase conventional commits: `feat(ui): ...`, `fix: ...`, `test: ...`,
  `docs: ...`.
- The body explains the reasoning, and names the bug when there was one. The
  history is written to be read — `git log` shows the style.
- Sign off every commit (`git commit -s`), which adds a
  `Signed-off-by: Your Name <you@example.com>` line. It is the
  [Developer Certificate of Origin](https://developercertificate.org/): your
  statement that you wrote the change, or have the right to submit it, under
  the project's licence. A check on every pull request looks for it; a
  commit without it can be fixed with `git commit --amend -s` or
  `git rebase --signoff`.
- One pull request per change. Say what it does, why, how you verified it
  (and against what), and what you did not verify.
- Update the docs a change affects: README.md for what users see,
  docs/PORTING.md for anything ported or deliberately different.

## Reporting a bug

A security problem is not reported here: see [SECURITY.md](SECURITY.md).

Open an issue with what you did, what you expected, what happened, and
`tend version`. If a server is involved, say whether you restarted it or ran
`tend handoff` after upgrading — a new client on an old server is the most
common cause of "it does nothing".

## Licence and names

tend is made by [Auth Tecnologia Ltda](https://auth.com.br) and is
Apache-2.0 ([LICENSE](LICENSE)); by contributing, and signing off, you agree
your work is under it.

Everyone taking part is expected to follow the
[code of conduct](CODE_OF_CONDUCT.md). [NOTICE](NOTICE) attributes herdr; keep it. Do not use the herdr
name or marks in the product itself — in code comments and docs, referring to
herdr as the original is right and expected.
