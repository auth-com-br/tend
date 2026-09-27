# tend — architecture

tend is a terminal runtime for coding agents: it keeps agent terminals alive in
a background server, tracks whether each agent is working, blocked or idle, and
exposes that over a socket API so agents can drive each other.

It is an independent Go implementation of the architecture proven by
[herdr](https://github.com/herdrdev/herdr) (Apache-2.0). See `NOTICE`.

## Principles

These are load-bearing. A change that breaks one needs a reason written down.

1. **State is separate from runtime.** Session state is plain data, testable
   without PTYs or goroutines. A pane's state is not its process.
2. **Render is pure.** Layout computation mutates; drawing does not. The
   renderer takes a read-only snapshot and produces cells. No state changes
   during a draw.
3. **Detection is decoupled.** The detector reads a screen snapshot and nothing
   else. It never touches the parser, the PTY, or viewport state.
4. **Platform code is isolated.** OS behaviour lives behind build tags in
   `internal/pty` and `internal/platform`. Core packages contain no
   `runtime.GOOS` checks.
5. **No god packages.** When a package starts doing two jobs, split it before
   it grows a third.

## Multiplicative cost

Work reachable from parsing, rendering, layout and client fan-out is
multiplicative: per byte or event × panes × attached clients. Before adding work
to those paths, establish its frequency and cardinality.

Concretely, for `internal/vt` and the render loop:

- No allocation per byte, per cell or per frame. `Parser.Parse` is covered by
  benchmarks asserting `0 allocs/op`; keep them at zero.
- Hidden panes still parse output, but must not trigger presentation work.
- Hold locks over terminal state for as short a window as possible.

## Packages

| Package | Role |
|---|---|
| `internal/vt` | Terminal emulator core: escape-sequence parser, grid, screen state |
| `internal/pty` | PTY creation and process lifecycle, per platform |
| `internal/detect` | Manifest-driven agent state detection |
| `internal/session` | Pure state: workspaces, tabs, panes, layout |
| `internal/server` | Background daemon owning panes and terminals |
| `internal/proto` | Client/server wire messages |
| `internal/client` | TUI client |
| `internal/ui` | Pure rendering |
| `internal/explorer` | The files panel (`tend files`): tree, git, diff, search |
| `internal/activity` | The record of what agents did: the log file, runs folded from it, git facts, export |
| `internal/usage` | Tokens an agent's conversation spent, read from its own files, and their price |
| `internal/policy` | Rules on where agents may run, checked against facts it is given |
| `cmd/tend` | CLI entry point |

## Terminal core

`internal/vt` is written from scratch rather than binding an existing C
emulator. The trade is deliberate: no cgo, so cross-compilation stays trivial,
at the cost of owning correctness.

The mitigation is scope plus evidence. tend targets what a coding-agent
multiplexer actually needs, not xterm compatibility: CSI/OSC/DCS parsing, an
SGR-styled grid with scrollback, alt screen, mouse modes, and enough reflow to
resize panes without losing history. Everything outside that is explicitly out
of scope until something needs it.

`Parser` follows the [Paul Williams DEC ANSI state
machine](https://vt100.net/emu/dec_ansi_parser) with three extensions —
incremental UTF-8 decoding, colon-separated CSI sub-parameters, and APC capture
— and two deliberate deviations from xterm, both documented in the package
comment: 8-bit C1 controls are never honoured, and over-long string payloads are
dropped rather than truncated.

The property that matters most is chunk independence: PTY reads land on
arbitrary byte boundaries, so parsing a stream one byte at a time must produce
exactly the events that parsing it in one write produces.
`TestParseIsChunkIndependent` pins that.

### What the core implements today

`Parser` (escape-sequence machine), `Grid` (cells, rows, scrollback ring) and
`Screen` (the Handler that turns events into screen state): printing with
wrap, wide characters and combining marks; cursor movement and addressing;
erase, insert and delete for both cells and lines; scroll regions; SGR
including 256-colour and direct colour in both the semicolon and colon forms;
the alternate screen; mouse and paste modes; titles; and cursor reports.

Deliberate gaps, each a decision rather than an oversight:

- **Reflow is main-screen only.** An application on the alternate screen owns
  every cell of it and redraws on the resize that is about to reach it, so
  rewrapping there would fight a redraw that is already coming.
- **No character sets.** tend is UTF-8 only. `ESC ( B` and friends are
  consumed and ignored.
- **No 8-bit C1 controls**, for the UTF-8 reason in the package comment.
- **DCS and APC payloads are captured and discarded.** kitty graphics will
  read them later without the parser changing.

Two invariants hold the core together, both pinned by tests:

- **Chunk independence.** Feeding a session one byte at a time must land on
  exactly the screen a single write produces. PTY reads split anywhere.
- **No allocation in steady state.** Parsing and screen writes are
  `0 allocs/op`, and a full scrollback ring recycles rows rather than growing.

## Detection

Agent state detection is declarative. A manifest per agent describes ordered
rules over regions of the bottom of the screen buffer, with AND/OR gates and
negative guards.

Manifests are derived from herdr's, which is the single most valuable thing
tend inherits: they encode calibration against 22 real agent UIs. They are
kept byte-identical to their upstream — 22 manifests, 136 rules, 103 patterns
— so a future sync is a copy rather than a re-edit.

The patterns carry over because Rust's `regex` and Go's `regexp` are the same
engine family: RE2, no lookaround, no backreferences. The *semantics* need no
translation at all. Two pieces of *syntax* do, and 9 of the 103 patterns use
them:

- Rust accepts `\uFFFF` and `\u{FFFF}` for a codepoint; Go only `\x{FFFF}`.
- Rust supports Unicode properties such as `\p{Alphabetic}`; Go supports only
  categories and scripts.

`dialect.go` translates both at load time rather than editing the manifests,
which is what preserves the sync property. `\p{Alphabetic}` maps to `\p{L}`,
an approximation documented at the mapping rather than buried: Alphabetic also
covers `Nl` and some Indic combining marks, which cannot arise in the one thing
the manifests use it for — a letter following a spinner.

Anything the translator does not cover is passed through for Go's own compiler
to reject, so an unsupported construct fails loudly at load instead of quietly
disabling one agent's detection.
`TestBundledPatternsCompileAfterTranslation` walks every bundled pattern and is
the guard for that on the next sync.

Two rules carried over from that lineage, both learned the hard way:

- Detect from the **bottom buffer**, never the user-visible viewport. Users
  scroll; agent state does not move.
- Gate on controls that are invariant for a state. Never match incidental text
  that happens to appear on screen.

### Reflow

Narrowing a pane used to leave every line broken where it was: text wrapped at
120 columns kept its break at column 120 inside an 80 column pane. The
information needed to undo that was already recorded — `Row.wrapped` marks a
line that continues because it ran out of width, as opposed to one that ended
because the program wrote a newline — and reflow spends it.

The property is that **where the breaks fall is the only thing reflow may
change**. `TestReflowRoundTrip` takes content through six widths and checks the
unwrapped text after every one.

Two details are less obvious than they look:

- **The cursor is tracked as an offset into its logical line**, because that is
  the only description of its position that survives rewrapping. Its row and
  column both change; the character it sits on does not.
- **A wide character that will not fit leaves a spacer, not a space.** The
  distinction is invisible until the line is rewrapped a second time, when a
  space would reappear as a gap in the middle of the text. A spacer and the
  right half of a wide character both hold no rune, and are told apart by what
  precedes them: a continuation always follows the double-width cell it
  belongs to.

## The pty layer

`internal/pty` runs a command on a pseudo-terminal, because a pane's process
must believe it owns a terminal: that is what makes an agent draw a spinner,
report a title and answer cursor queries at all. Running it on a pipe changes
its behaviour and silently defeats detection.

One platform detail leaks far enough to be worth naming: when a child exits,
Linux fails the next read on the master side with `EIO` rather than reporting
end of file. The package translates that, so callers treat a pty like any
other reader instead of special-casing an errno only one platform produces.

Windows is a stub that returns `ErrUnsupported`. ConPTY is a different enough
mechanism to be its own piece of work rather than a port of the Unix path; the
seam is in place so that work lands in one file.

## Session state

`internal/session` holds the shape of a session — workspaces, tabs, panes and
their arrangement — as plain data. There is no PTY, no goroutine and no
terminal in the package, which is what lets a whole workspace's behaviour be
tested without starting anything. The server owns the runtime and pairs it with
these records by id.

Three decisions that are easy to get subtly wrong, and so are pinned by tests:

- **Splits name their arrangement, not their divider.** `Columns` places panes
  side by side, `Rows` stacks them. "Horizontal split" means opposite things in
  different multiplexers, and the ambiguity reliably puts panes in the wrong
  place.
- **Splitting the same way twice stays flat.** A second `Columns` split makes
  three panes in a row rather than nested halves, so the third pane is a third
  of the screen and not a quarter.
- **Geometry tiles exactly.** Each child's far edge comes from a cumulative
  fraction and the next child starts there, so rounding cannot open a one-cell
  seam. `assertTiles` checks coverage cell by cell rather than by summing
  widths, because a sum hides exactly the failure a user sees.

Focus movement works on the computed rectangles rather than on the tree, since
"the pane to the left" is a spatial question: the tree can nest two panes
arbitrarily far apart and they are still neighbours on screen.

`CheckInvariants` verifies everything the package promises about its own shape,
and the tests call it after every mutation. It exists because a layout tree
fails by drifting rather than by crashing — a stale index, focus on a closed
pane, split shares no longer summing to one — and each of those renders wrongly
instead of stopping, which makes them expensive to find later.

## The server

`internal/server` pairs the pure records in `internal/session` with the live
halves they describe: a pty, a terminal and a detector per pane. The split is
the point — a pane's identity and place in the layout outlive its process.

**Locking.** Two levels, always taken in this order: the server's lock, which
guards the session tree and the runtime map; then a pane runtime's own lock,
which guards that pane's terminal. Pane output never touches the server's lock,
so one busy agent cannot serialise every other pane through a single mutex.
Callbacks from inside a terminal — a title report, for instance — record into
the runtime and let the detection loop carry the result up, rather than
reaching for the session lock while a pane is parsing bytes.

**One detection loop, not one per pane.** Detection is per-pane work on a
shared schedule. A goroutine and a timer per pane would scale the cost of an
idle workspace with its size. A pane whose screen has not changed is skipped
entirely, so the interval bounds how stale a state can be rather than how much
work is done.

**A slow client loses events rather than stalling the runtime.** Subscriptions
are buffered and the server never blocks on one; a subscriber that stops
reading has events dropped and counted, so it can tell "nothing happened" from
"I fell behind" and resynchronise. A stuck client must not be able to freeze
the agents it is watching.

**Shutdown is bounded.** Closing a pane hangs it up, and the hangup is what
ends the goroutine reading it — closing the terminal alone does not. The pty
master is not registered with Go's poller, so a read already blocked on it is a
plain syscall that `Close` cannot interrupt, and the reader would park forever.
A pane that ignores the hangup is killed when the grace period expires. Panes
tend hangs up on purpose do not report the resulting hangup as a failure, or
every pane the user closes would show an error.

## The wire

`internal/proto` is the format, `internal/transport` is the socket,
`internal/client` is the other end. Together they make the server a daemon:
panes outlive every client, so clients may come and go while agents keep
working.

**Two kinds of traffic, two encodings.** Control messages are JSON, because
they are rare, structured, and worth being able to read in a hex dump. Pane
traffic is raw bytes behind an 8-byte pane id, because it is the hot path and
base64 inside JSON would cost a third more bytes and an encode per read.

**Screens, not byte streams.** A subscribed client is sent a pane's whole
screen on a tick, not the bytes that changed it. A client that falls behind
can miss any number of screens and still be correct once the next arrives,
because a screen describes a state rather than a change. Raw bytes would be
cheaper and unrecoverable if one were ever dropped — and a client can stall,
which is what decides it.

**Three compatibility rules**, each one the difference between a mismatch
failing clearly and failing subtly:

- Frame type numbers are permanent, and an unknown type is skipped rather than
  guessed at.
- Method names are permanent, and an unknown method is answered with an error,
  never a disconnect. A missing feature disables one action, not the session.
- New JSON fields are optional. A peer that has never heard of a field behaves
  as it did before the field existed.

The handshake exchanges versions and the server's method list, so a client can
disable an action the server cannot perform rather than letting a user discover
the gap.

**The socket is the access control.** There is no authentication beyond the
file system: the directory is `0700` and the socket `0600`, set before anyone
can reach it. Anyone who can connect can run a command as this user.

Two details that only show up in practice. A socket path longer than `sun_path`
— 108 bytes on Linux, 104 on macOS — fails with a bare "invalid argument", so
the length is checked first and reported with a way out. And a socket left by a
crashed server is removed on the next start, but only after checking that
nothing is listening on it: removing a live server's socket would orphan it,
leaving its panes running and unreachable.

## The client

`internal/ui` draws, `cmd/tend`'s attach drives. Drawing is pure — it takes a
description and fills a grid of cells, reading no socket, terminal or clock —
so a whole screenful is asserted in a test without any of those existing.

**Panes arrive as screens, and are composited here.** The server renders each
pane's terminal back to escape sequences (`vt.RenderScreen`), the client parses
them into a terminal of its own, and compositing those into one grid is
presentation, which is why it sits on this side of the socket.

The round trip is the property that makes it work: rendering a screen and
feeding the result to an empty screen of the same size reproduces it exactly.
`TestRenderScreenRoundTrip` checks that against the content that breaks each
choice in the encoder — trailing backgrounds, wide characters, combining marks,
a full last row that could scroll.

**Only changed rows are sent to the terminal.** A full repaint of a large
terminal is tens of kilobytes; most frames change a handful of rows, and
resending the rest is slow and visibly flickery. `vt.Painter` keeps the last
frame and emits the difference.

**Keys are a state machine over raw bytes**, not parsed events. Almost every
byte belongs to the focused pane and must reach it untouched, including escape
sequences the client has never heard of. Only the prefix and the one key after
it are the client's; an unbound key cancels the prefix and is forwarded, so a
mistyped command does not silently eat the next keystroke.

**Geometry is not reimplemented.** Which pane is "to the left" has a careful
answer — nearest edge, then largest shared border — and `session.Neighbor` is
reused rather than copied, because two implementations would eventually be two
that disagree.

The TUI is tested by running it: tend's own pty runs tend's own binary, and
tend's own terminal emulator reads what it drew. That makes it the one test
exercising the whole stack at once, and the one that fails when any layer stops
agreeing with another.

### Starting the server

`tend` with no arguments opens a session, starting a server for it when there
is none. Needing a daemon is tend's problem rather than the user's: a second
terminal running a server is a step that exists only because of how this is
built, and the server outlives the client either way, so starting it from the
client changes nothing about what happens afterwards.

The server is started detached — its own session, no controlling terminal — so
it does not receive the Ctrl+C meant for the client and does not die with the
terminal that launched it. Its output goes to a log file beside its socket,
because a background process with no output is one that fails silently.

Two clients racing to start one is fine: the second server cannot take the
socket, exits, and the client that started it connects to the first.

Only `attach` and `new` do this. `ls`, `kill` and `follow` report a missing
session instead — creating one on the way to listing it would report an empty
session rather than the absence of one.

### Mouse, scrolling and reconnecting

**A click belongs to whoever asked for it.** A pane running something that
requested mouse reporting gets the report forwarded; otherwise the click is
tend's and focuses the pane. The server reports each pane's mouse mode for
exactly this, since only it knows what the program inside asked for.

Mouse input is read in the SGR encoding because the older ones pack
coordinates into single bytes and cannot describe a click past column 223. A
report split across two reads is held rather than forwarded in halves, with a
bound on how long it is held: something that starts like a report and never
ends must not swallow real input.

**Scrolling is a view, not a mode.** The pane keeps running and the server
keeps its live screen; what changes is only which rows this client asks to be
drawn. That is why leaving the view needs no resynchronisation — nothing was
ever out of step.

**Reconnecting re-reads everything.** A server restarting is not the client's
failure, and exiting when it happens loses the user's place for a reason that
had nothing to do with them. After reconnecting, nothing about the old session
is assumed: the panes on the other side may be different ones carrying the same
numbers.

### Spaces, tabs and the agent list

A space holds tabs and a tab holds panes, and all three of "which space is
shown", "which of its tabs" and "which pane has the keyboard" belong to the
client rather than the server. Two people attached to one session should be
able to look at different things, so the server is asked only for what exists,
never for what is being looked at.

Tabs are scoped to their space. A bar listing every tab in the session puts
tabs the user cannot reach next to ones they can, which is worse than not
showing them.

The agent list is the one view that answers "which one stopped?" without
looking at every pane, which is why it groups by space and tab: that is where
an agent actually is, and reaching it means going there. The navigation cursor
and the focused pane are marked separately — moving the cursor must not move
the view, since a user reading down the list has not left the pane they are
working in.

New spaces and tabs are named by position rather than left blank. An unnamed
space shows as a dash and disappears from the status bar, which makes the thing
just created the hardest one to find. A rename seeds the prompt with the
current name and starts it selected, as any rename field would: typing replaces
it, backspace keeps it and edits from the end.

### Clickable things

Drawing and hit-testing work from the same description of the layout. The tab
bar computes its segments once, renders them, and answers a click from the same
list — so a click cannot land on something other than what it looks like it is
on, which is the failure two independent implementations eventually produce.

The tab bar is shown from the first tab rather than the second. It carries the
button that makes a new one, and a button that appears only once you already
have what it creates is a button nobody finds.

Every row of the agent list is clickable, headings included. A line that looks
clickable and is not is worse than one that is not drawn, so a space heading
goes to that space and a tab heading to that tab.

### The files panel

The file explorer is a program, `tend files`, run in a pane docked along the
left of the tab (`pane.dock`, the tab's full height, where a split could only
sit beside one pane). Being a pane rather than part of the client is what makes
it right over ssh: it runs where the server and the files are, and the client
only draws it. It names its binary through `TEND_BIN_PATH`, which the server
puts in every pane, so the panel is the server's build even when the client is
another. It asks git in git's porcelain formats, reads a directory only when it
is opened, reads the disk again every two seconds so an agent's new files show
up, and opens files through the automation socket: a tab running `$EDITOR`,
closing with it, and focused by a focus request because each client decides
what it looks at.

### Reading a chunk of input

Terminal input arrives in chunks that can hold several keys, and twice now a
handler looked at the first byte, acted on it, and reported the whole chunk
consumed — dropping everything after. `splitKeys` breaks a chunk into whole
keys so that shape of mistake is not available: every key is offered in turn,
and a handler that declines one hands back the rest.

A lone escape is never held as a possible mouse report. It is far more often
the Escape key, and holding it means the key does not arrive until the user
presses something else — which inside an editor is indistinguishable from tend
having eaten it.

### Settings

Every setting has a working default, so the file is optional and a partial one
is normal. An unknown key is an error rather than being ignored: a setting
silently dropped is one the user believes is in effect, which is the most
confusing way for configuration to fail. Settings are read before anything else
is checked, so a broken file is reported wherever tend is being run from.

## Scope

tend began as a lean core — server, ptys, panes, detection, a basic TUI — with
plugins, remote sessions, kitty graphics, worktrees and session handoff set
aside. That milestone is done, and the goal has since widened to a 1:1 port of
herdr, those included.

A server can be replaced while it runs: `internal/server/handoff.go` describes
the session and gives the pane terminals to a child started by
`cmd/tend/handoff.go`, which inherits them along with the listening socket.
`internal/pty` makes that possible by keeping the master non-blocking under
Go's poller, so a reader can be stopped without the process ending.

What has crossed over, what has not, and where each missing piece lives in
herdr's source is tracked in `PORTING.md`, which is the work queue.

A session outlives its server as a description, not as processes. The server
writes the arrangement down (`internal/server/persist.go`) and the next one to
start reads it back: same spaces, tabs and splits, each pane a new process in
the directory the old one was in, under the old one's scrollback. Handing live
processes from one server to the next is the part still missing.
