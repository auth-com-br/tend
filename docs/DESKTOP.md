# tend-desktop — tend in a window

`tend-desktop` is a Qt Quick window on a tend session. It is a second client
of the same server the terminal `tend` attaches to: both can be open on one
session at once, and closing the window leaves the server and every pane
running, as detaching the TUI does.

herdr has no desktop client; this is tend's own (see `docs/PORTING.md`,
"Different from herdr on purpose").

## Build and run

Needs Go, Qt 6 with Qt Quick (6.10 is what it was built against), and
pkg-config. On Ubuntu:

```
sudo apt install build-essential pkg-config qt6-base-dev qt6-declarative-dev \
  qml6-module-qtquick qml6-module-qtquick-controls qml6-module-qtquick-layouts \
  qml6-module-qtquick-window qml6-module-qtquick-templates qml6-module-qtqml-workerscript
```

```
make desktop            # desktop/bin/tend-desktop
make desktop-check      # the desktop's gate: gofmt, vet, tests
make desktop-install    # beside tend in GOBIN or ~/.local/bin
desktop/bin/tend-desktop -s work
```

The first build compiles the Qt bindings and takes a few minutes; later ones
are cached.

`tend-desktop` starts a session's server with the `tend` binary — the one
beside it, else the one on PATH, else `-tend <path>` — so the server is always
the terminal tend's, and a desktop and a tend installed together are the same
version.

To check the window without looking at it: `-shot out.png` saves what it shows
a moment after it opens and quits, and `TEND_DESKTOP_DEBUG=1` logs what it is
told (sizes, frames, keys) to standard error. Both work with
`QT_QPA_PLATFORM=offscreen`, but the offscreen platform does not reproduce
everything: the first bug of this window (below) did not happen there.

## How it is built

```
desktop/                 its own Go module: cgo and Qt live only here
  cmd/tend-desktop/      main: flags, the QML engine, wiring
    qml/main.qml         the window, embedded in the binary
  internal/core/         no Qt: connection, pane state, key encoding (tested)
  internal/qtterm/       the Terminal QML item (C++) and its cgo bridge
```

- **The terminal tend is untouched.** `desktop/` is a separate module, so the
  root module's `go build ./...`, `make check` and the `tend` binary do not
  see it and stay pure Go. It imports tend's `internal/` packages (`client`,
  `proto`, `vt`, `transport`) through a `replace` to `../`: Go allows it
  because the import path is under the same root.
- **Bindings: miqt** (`github.com/mappu/miqt`, MIT). It covers the QML engine
  but **not Qt Quick** — there is no `QQuickItem` to subclass from Go. So the
  terminal is drawn by a small C++ `QQuickPaintedItem` (`qtterm/`), compiled
  by cgo, which only paints cells Go gives it and reports keys and its size.
  Everything it paints is decided in Go (`core.FrameOf`). The rest of the
  window is QML.
- **moc** runs from `go generate` (`make desktop` does it) and writes
  `terminalitem.moc`, which is not committed: its output is tied to the Qt
  version that made it.
- **Same path as the TUI.** The window sizes the pane on the server, then
  subscribes, and keeps a `vt.Screen` per pane fed by the pane's raw output,
  as `cmd/tend/tui.go` does. The difference is the keyboard: the TUI forwards
  bytes its terminal already encoded, and the window has no terminal in front
  of it, so `core.EncodeQtKey` is that terminal, in xterm's encoding.
- **Threads.** Qt objects are touched only on the main thread
  (`mainthread.Start`); the connection's reader never calls the server
  (that deadlocks, as the TUI learned). Repaints are coalesced: one waits for
  the main thread at a time.

## Lessons

- **Wire every callback before loading the QML.** On Wayland the Terminal
  item is given its size while the QML loads. The resize handler was set
  after `LoadData`, the size was dropped, the pane was never sized or
  subscribed to, and the window stayed empty — keys reached the shell, but
  nothing came back to show it. Offscreen the size arrived a second time,
  so the bug only showed on the owner's screen.

## Design: a native client, as light as a terminal

The window is native all the way through: the sidebar, tabs, agent list,
files, diffs, issues and settings are Qt Quick, not the TUI drawn inside a
pane. Only a pane is a terminal. Everything the terminal tend does, the window
does too, from the same code; and it adds what a window can do that a
terminal cannot.

### Rules

1. **No browser engine in the window itself.** The interface is never HTML:
   no QtWebEngine, no QtWebView, no embedded Chromium behind the sidebar or
   the panels. That is what makes Electron apps (Orca among them) weigh
   hundreds of megabytes over several processes. Markdown is Qt's own
   (`Text.MarkdownText`), code is highlighted by tend's highlighter, images
   are `Image`. The one browser there is, is a *browser tab* the owner opens
   (below), and it costs nothing until one is open.
2. **One process.** The window is one process beside the shared server. No
   helper processes, no renderer per tab.
3. **Panels load when opened** (`Loader`), and go when closed. A window with
   only terminals pays for terminals.
4. **The weight is measured, not hoped for.** Budget, checked by running
   `-shot` and reading `/proc` (a test in phase 1):

   | | budget | measured, phase 0 (one pane, GNOME Wayland) |
   |---|---|---|
   | memory (PSS) | ≤ 150 MB with 10 panes and the sidebar | 101 MB |
   | binary, stripped | ≤ 40 MB | 31 MB |
   | processes | 1 (+ the server, shared with the TUI) | 1 |
   | start to first frame | ≤ 500 ms | not measured yet |

5. **Server facts over the wire, viewer state in the client** — the rule the
   TUI follows (AGENTS.md). What one window looks at (scroll, folded groups,
   the open panel) is never sent.
6. **One implementation of each behaviour.** A decision both clients make —
   what state an agent shows, which agent needs attention first, what a menu
   offers, what a notification says — lives once, in `internal/`, and both
   clients call it. Only drawing and input differ.

### Layers

```
 internal/server           the session: panes, agents, git, issues (unchanged contract)
        │ proto (unix socket)          │ automation socket (api)
 internal/client ── internal/apiclient (moved out of cmd/tend/automation.go)
        │
 shared client logic, no drawing:
   internal/ui menus, palettes, agent rows (exists)       ← both clients
   internal/sidebar  space rows, display state, attention order   (from tui_nav.go)
   internal/announce what to notify and when                      (from notify.go)
   internal/explorer tree, git, jj, grep, history, review, finish (exists; split from its grid)
   internal/tickets, glitchtip, keyring, worktree, preset          (exist)
        │                                   │
   cmd/tend (TUI: draws into cells)    desktop (Qt Quick: draws natively)
```

Moving logic out of `cmd/tend` is a refactor of the terminal client: its
behaviour must not change, and `make check` is what proves that. Each move is
its own commit, before the desktop feature that needs it.

### Go and QML

- **Lists are Go models.** Spaces, agents, tabs, the file tree, issues: each is
  a `QAbstractListModel` implemented in Go (miqt supports subclassing it),
  with roles named for QML. A list changes by rows, not by being rebuilt, so
  the scroll position and selection in the view survive an update.
- **Actions go through one bridge.** A small C++ `Tend.Bridge` object with one
  invokable, `call(method, args)`, into Go, and one signal, `changed(topic)`,
  out of it. QML says `Bridge.call("pane.split", {pane: id, direction:
  "columns"})`; Go maps it to the client. One door, so every action the
  window can take is listed in one Go switch, as the TUI's menu actions are
  (tui_menu.go `runMenu`).
- **Layout comes from the server.** `tab.layout` gives each pane's rectangle in
  cells for an area; the window asks with its area in cells of its font and
  multiplies — the same function the TUI draws with, so the two show the same
  splits. Dividers are dragged with `pane.adjust`, as in the TUI.

### The window

```
┌──────────────┬─────────────────────────────────────────────┬──────────────┐
│ ⌕ go to…  ⌘K │  api ×   web ×   review ×   +               │ FILES  CHANGES│
│              ├──────────────────────┬──────────────────────┤ ISSUES       │
│ SPACES       │                      │                      │ ▾ src/       │
│ ▾ acme       │  claude  (terminal)  │  shell (terminal)    │   app.go  M  │
│   api   main↑2│                      │                      │   db.go   A  │
│   web   feat │                      │                      │ ▸ docs/      │
│ ▸ personal   │                      ├──────────────────────┤              │
│              │                      │  codex (terminal)    │──────────────│
│ AGENTS       │                      │                      │ diff app.go  │
│ ● claude  api│                      │                      │ +12 −3  [r]  │
│   ctx 42%    │                      │                      │              │
│ ◐ codex  web │                      │                      │              │
│ ✓ claude rev │                      │                      │              │
├──────────────┴──────────────────────┴──────────────────────┴──────────────┤
│ desktop · 3 agents · 1 waiting                          $1.20 today       │
└────────────────────────────────────────────────────────────────────────────┘
```

Left, the sidebar as the TUI's (spaces in groups and companies, branch and
ahead/behind, agents with state and context use). Middle, the tab's panes,
real terminals. Right, a dock with the files panel's views (files, search,
changes, history) and the issues, errors and tickets lists, native. Bottom, a
status line. Everything is a mouse target as well as a key.

## Status and plan

| Phase | What | State |
|---|---|---|
| 0 | Window on a session: the focused pane, keyboard, resize | done |
| 1 | The bridge and Go models; sidebar (spaces, groups, agents and state); tabs; splits with dividers; focus; mouse in panes; scrollback; selection and copy; the weight test | next |
| 2 | Menus (from internal/ui's menu definitions), rename, dialogs, notifications (native), settings, themes from the settings, the kitty keyboard protocol | |
| 3 | The dock: files, search, changes with diff and review, history (internal/explorer's logic without its grid) | |
| 4 | Issues and PRs, errors, tickets, MCP, permissions, agent manager, sessions, context, worktrees, machines | |
| 5 | What only a window does: command palette, tray and native notifications, drag and drop into an agent, a tab in its own window, diff and review side by side, spending dashboard, native previews | |
| 6 | Planning: tasks with dates and agents | |
| 7 | WhatsApp for the agents (MCP server, replies approved by the owner) | |
| 8 | Browser tab (QtWebEngine, on demand) | |
| 9 | Packaging (.desktop, AppImage or .deb), CI | |

### Only in the window

Chosen by the owner (November 2026); none is started.

- **Command palette** (ctrl+K): every action and every space, tab and agent by
  name.
- **Tray icon and native notifications** with the agents' states: a blocked
  agent is seen with the window closed, and its question answered from the
  notification.
- **Drag and drop into an agent**: a file or image dropped on its pane is
  given to it (the path, or the image as Claude Code takes a pasted one).
- **A tab in its own window**: drag a tab out to put it on another monitor.
- **Diff and review side by side**, with comments on lines sent to the agent
  (the files panel's `r`, with a real diff view).
- **Spending dashboard**: context, tokens and cost per agent and per company
  over the day and the week, as charts (data from `internal/usage` and
  `internal/activity`).
- **Rendered previews**: markdown, images and PDFs in the dock, natively.

### Modules asked for beyond the TUI

- **Browser in a tab.** A web page as a tab beside the terminals, which the
  agents can drive (tend's browser already lets them, through the extension
  in an outside Chromium: `internal/browserext`). There is no light engine to
  embed in Qt on Linux — WebKitGTK is GTK's and cannot be put inside a Qt
  window on Wayland, and QtWebView falls back to QtWebEngine there — so the
  tab is QtWebEngine (Chromium), held to rule 1 by loading it only when a
  browser tab is opened: the QML for it is a separate component that imports
  QtWebEngine, the Go binary does not link it, and its renderer processes
  exist only while such a tab does. Ubuntu's package is about 200 MB on disk.
  Decided with the owner (November 2026): QtWebEngine, loaded on demand. The
  agents drive it as they drive the outside one.
- **WhatsApp for the agents.** The owner serves customers on WhatsApp, and the
  agents need to read those conversations (and draft replies). Shape: an MCP
  server, `tend whatsapp serve`, beside tend's own (`tend mcp serve`), with
  tools to list chats, read a chat, search, and *propose* a reply — sending
  is put to the owner, as the guard puts a destructive command (the
  conversations are with customers; a reply in the owner's name is theirs to
  approve). Messages stay on the machine. The way in, decided with the owner
  (November 2026): linking the existing number as a companion device, as
  WhatsApp Web does, through whatsmeow (Go, MPL-2.0), for its history and
  its live messages. The owner chose it knowing it is against WhatsApp's
  terms and that the number can be banned for it; tend says so when it is
  set up. The official Business Platform (Cloud API) was the alternative:
  no risk, but only a number registered for it, no history, and a public
  HTTPS webhook. whatsmeow keeps its session in SQL: with a pure-Go driver
  (modernc.org/sqlite), so tend stays without cgo, and in a module of its own
  if its size would weigh on the tend binary — to be measured.
- **Planning.** A place for the work ahead: tasks with dates, as a list and a
  calendar, tied to spaces and agents, with the tickets tend already reads
  (Linear, Jira, ClickUp) among them; a task can be given to an agent to
  start at a time. The tasks are the session's (server state, so the TUI and
  the phone see them too), and an agent started for one is started as
  `agent.start` starts one now. Decided with the owner (November 2026).
- **The integrations tend has** come over as native panels: git and jj,
  GitHub and GitLab issues and PRs, errors (GlitchTip, and Sentry through the
  same client — tested against a stand-in of Sentry's API, not sentry.io),
  tickets, MCP, permissions.

Programs that already run in a pane (`tend files`, `tend view`) still work in
the window as terminals until their native panels replace them.

Not yet, from phase 0: the kitty keyboard protocol (so Shift+Enter is a plain
Enter for Claude Code here), the input method (compose, dead keys go through
Qt's text), and the theme, which is catppuccin mocha until it is read from
the settings.
