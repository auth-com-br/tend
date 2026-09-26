# Security

tend runs programs, holds terminals and talks to a browser, so a flaw in it
can matter. Thank you for reporting one privately.

## Reporting

Write to **seguranca@auth.com.br** — do not open a public issue. Say what
you found, how to reproduce it, which version (`tend version`) and which
platform. If you prefer, use GitHub's private vulnerability reporting on
this repository instead ("Report a vulnerability" under Security).

What to expect:

- an answer within **3 working days** that the report was received;
- an assessment, and a plan or a question, within **10 working days**;
- a fix released as soon as it is ready, with credit to you in the release
  notes unless you ask otherwise.

Please give us the time to release a fix before telling others.

## Supported versions

Fixes go into the latest release only. `tend update` (or
`tend update -handoff`, which keeps what is running) moves to it.

## What tend keeps, and where

| What | Where | Who can read it |
|---|---|---|
| The session's layout, each pane's directory and command | `~/.local/state/tend/` (or `$XDG_STATE_HOME/tend`) | its owner only (files 0600, folders 0700) |
| What each pane printed (scrollback), to come back after a restart | the same folder | its owner only |
| The server's socket, which drives everything | the runtime folder (`$XDG_RUNTIME_DIR/tend`) | its owner only (socket 0600 in a 0700 folder) |
| GlitchTip tokens | the **system keyring** (GNOME Keyring / KWallet through `secret-tool`, the macOS Keychain) when there is one; otherwise the settings file | the keyring's owner; the settings file is written 0600 |
| GitHub access | none: tend runs `gh`, which keeps its own login | — |

tend sends nothing about you anywhere: no telemetry. It asks GitHub (through
`gh`), your GlitchTip servers, and GitHub's release page for updates, and
nothing else.

## How it is built to be safe

- **One user.** Everything that drives the session — the client socket and
  the automation socket — is a Unix socket only its owner can open. There is
  no network listener.
- **The browser extension asks little.** Its native messaging bridge may
  only send what was picked on a page (`browser.context`,
  `browser.send_to_agent`) and ask the browser's status; it cannot drive
  panes, run commands or reach the server's other methods.
- **Text from outside is inert.** What is sent to an agent — an issue, an
  error, a page — has every control character removed before it is typed
  into a pane (`capture.Inert`), so nothing in it can act as keys, end a
  paste, or submit; it is typed, and the user presses enter.
- **Updates are checked.** `tend update` and the installer download a
  release from GitHub and install it only when its SHA-256 matches the
  release's manifest or `SHA256SUMS`. Signing the releases themselves, so a
  compromised release page could not pass, is planned.
- **Handoff keeps what runs, and nothing else.** A server hands its
  terminals to its successor by descriptors inherited at start, never over a
  socket another process could connect to.
- **Tokens are not printed.** They go only in the `Authorization` header of
  the request they are for; error messages carry the server's address and
  answer, never the token, and `tend config` prints no values.

## What is in scope

- the server and client (`tend`), its automation socket and its handoff;
- the installer (`install.sh`) and the update manifest;
- tend's browser extension and its native messaging bridge;
- the integrations tend installs into agents (`tend integration install`).

Agents themselves (Claude Code, Codex and the others) and the programs run
in panes are their own projects; report their problems to them.

tend is made by Auth Tecnologia Ltda, Belo Horizonte, Brazil.
