# Porting herdr to tend — status and work queue

tend is a 1:1 port of herdr (Rust, at `../herdr`) to Go. This file is the record
of what has crossed over, what has not, and where each missing piece lives in
herdr's source. Read `AGENTS.md` first for how to work.

**Keep this file true.** When you port something, move it from "Not ported" to
"Ported" in the same commit. When you diverge on purpose, record it under
"Different from herdr on purpose". A queue that lies is worse than none.

Sizes: herdr is about 258k lines of Rust; tend is about 18k lines of Go plus 11k
of tests. herdr's API has about 110 methods; tend's protocol has 18.

---

## How to port a feature

1. **Locate it in herdr.** Use the map below, then `grep -rn` in `../herdr/src`.
   herdr's user docs are in `../herdr/docs/next/website/src/content/docs/` and
   say what a feature is *for*, which the code often does not.
2. **Read the code, and note what it does not do.** Absences are the easy thing
   to miss and the expensive thing to get wrong.
3. **Classify the state** the feature needs: a shared session fact goes in
   `internal/session` and over the wire; what one viewer is looking at stays in
   `cmd/tend`. See `AGENTS.md`, "Architecture boundaries".
4. **Extend the protocol if needed.** A new method: add the constant, the params,
   the dispatch case in `internal/server/serve.go`, the entry in `server.Methods`,
   the entry in `proto.KnownMethods`, and a client wrapper. A new event or
   snapshot field: add it to `proto.KnownFeatures`.
5. **Write the test with the feature**, end-to-end in `cmd/tend` when it is
   something a user does.
6. **Verify with the real program**, not a stand-in, when the feature is about
   how agents behave.
7. `make check`, read the result, then commit. Update this file in that commit.

### Where herdr's code maps to

| herdr (`../herdr/src/`) | tend | notes |
|---|---|---|
| `ghostty/`, `terminal/`, `pane/terminal.rs` | `internal/vt` | herdr binds libghostty (C); tend's emulator is pure Go, written from scratch |
| `detect/`, `detect/manifests/` | `internal/detect`, `internal/agent` | manifests are byte-identical; `dialect.go` translates Rust regex syntax |
| `pty/` | `internal/pty` | |
| `workspace.rs`, `workspace/`, `layout.rs`, `session.rs` | `internal/session` | |
| `server/` | `internal/server` | |
| `protocol/`, `api/schema*` | `internal/proto` | tend's wire is much smaller |
| `client/` (the transport half) | `internal/client`, `internal/transport` | |
| `client/shell/` (the TUI) | `cmd/tend/tui*.go` | the largest gap is here |
| `ui/` | `internal/ui` | |
| `config/` | `internal/config` | |
| `cli/` | `cmd/tend/*.go` | |
| `selection.rs`, `client/shell/mouse.rs` | `internal/ui/selection.go`, `cmd/tend/tui_select.go`, `tui_view.go` | |
| `remote/` | `internal/transport/remote.go`, `cmd/tend/bridge.go` | |

---

## Ported, and checked

- **Terminal emulator** (`internal/vt`): DEC ANSI parser, grid with scrollback
  ring, reflow on width change, alternate screen, wide characters, SGR, mouse
  modes, OSC 0/2 (title), OSC 9 (progress), OSC 52 (clipboard write).
- **Agent detection**: all 22 manifests, identical to herdr's byte for byte
  (check with `diff -rq ../herdr/src/detect/manifests internal/detect/manifests`).
  Agents started inside a shell are adopted from the pty's foreground process.
- **Session structure**: spaces (herdr: workspaces), tabs, panes; split, close,
  zoom, resize by key and by dragging a divider, directional focus.
- **Where a new terminal starts** (herdr's `[terminal] new_cwd = "follow"`,
  `app/creation.rs`, `pane.rs` `follow_cwd`): a new tab starts where the
  focused pane's foreground program is working — the agent's directory, the
  shell's when that cannot be read — and a split where the pane split is.
  Before, both started in the server's own directory. Checked with the real
  binary: a tab opened after `cd` in the shell, and one opened while a
  program was running elsewhere in the foreground. herdr's other policies
  (`home`, `current`, a path) are not ported: follow is its default and the
  one asked for.
- **Server and client**: detached daemon on a Unix socket, started on demand by
  a bare `tend`; reconnect; version/feature mismatch notice with in-place
  server restart.
- **Tab bar** as herdr's `render_tab_bar`: each tab a block of its name and
  four columns (eight at least), the name centred, a column of the bar
  between blocks and before " + "; the tab in view on the accent, the others
  on surface0 (bright black with the terminal's colours, where the bar is the
  terminal's own background rather than a reversed band). Scroll buttons
  for more tabs than fit are not ported: the ones that do not fit are left
  off, as before.
- **Sidebar**: spaces as a folding tree of groups, git branch and ahead/behind
  per space, agents list flat or grouped, draggable divider between the two
  lists, its width dragged by its right edge, per-list scrolling with pinned headings, collapse/expand handles, a
  session-wide "N waiting" count.
- **Mouse**: clickable tabs and sidebar, context menus with hover highlight,
  rename in a modal, wheel scrolling.
- **Selection and copy**, on herdr's model: a program that asks for the mouse is
  given the whole gesture in its own coordinates and its OSC 52 copy reaches the
  clipboard (verified against the real Claude Code v2.1.278); in a plain pane
  tend selects, scrolling its own history under the drag; alt selects a block.
- **Remote sessions over ssh**: `tend attach -ssh user@host`, via `tend bridge`
  on the far side. Tested with a stand-in for ssh; not yet against a real sshd.
- **Shell completions**: bash, zsh, fish (`tend completion <shell>`).
- **Session persistence**: the arrangement is written to
  `$XDG_STATE_HOME/tend/<session>.json` (structure, checked every second) and
  `<session>.history.json` (scrollback, every 30s and at shutdown), and read
  back when a server starts. Spaces, groups, tabs, the split tree, each pane's
  *current* directory and its scrollback come back; programs do not — a
  restored pane is a new process. `[server] persist = false` turns it off.
  herdr's equivalent is `persist/`. Not ported from it: agent session resume
  (queue item 12).
- **Live handoff**: `tend handoff` (herdr: `server live-handoff`), and `r` on
  the mismatch notice when the server offers it. The server stops every pane's
  reader, writes a manifest (the session snapshot, plus per pane its pid, size
  and a `vt.RenderResume` blob that rebuilds scrollback, both screens, modes and
  cursor), starts the binary now on disk as `serve -inherit`, and waits for it
  to say it holds everything; then it lets go of the terminals without hanging
  them up and exits. The programs keep their pids. A replacement that fails to
  start or to answer is killed and the old server reads on, having lost
  nothing, since nobody read the terminals in between. Verified with the real
  Claude Code in a pane: same pid, same screen, detection still reporting its
  state afterwards. Also run on the owner's live session with Claude Code
  mid-session: the new server reported the pane's mouse modes (any-event, SGR)
  exactly as the old one had, which only `RenderResume` could have told it.
- **Automation API and CLI** (a large part of herdr's item 3): the socket
  answers `session.snapshot`, `workspace.list|get|create|rename|close`,
  `tab.list|get|create|rename|close`, `pane.list|get|read|send_text|send_keys|
  split|close|resize|wait_for_output`, `agent.list|get|read|prompt|send_keys|
  wait|start`, `events.wait`, `server.stop|live_handoff`, in herdr's names and
  shapes. The commands are `tend agent <list|get|read|prompt|send-keys|wait>`,
  `tend pane <list|get|read|send-text|send-keys|wait>` and `tend api <method>
  [json]` for anything without a command of its own. Keys are named as herdr
  names them (`internal/vt/keys.go`), encoded against the pane's own modes, and
  a prompt's text and its Enter are sent 300ms apart as herdr does. Verified
  against the real Claude Code with no client attached: prompted it, waited for
  it to come back idle, and read its answer out of the pane.
- **Plugin host** (the core of herdr's item 4): a plugin is a directory with a
  `tend-plugin.toml` — herdr's manifest with the name changed — declaring
  `build`, `startup`, `actions`, `events` and `panes`. `tend plugin
  list|link|unlink|enable|disable|reload|actions|run|open`, and over the socket
  `plugin.list|link|unlink|enable|disable|reload|action.list|action.invoke|
  pane.open`. The registry is `plugins.json` beside the settings file; linking
  records the directory rather than copying it, as herdr does. Commands run
  from the plugin's root with `TEND_PLUGIN_*` and the socket in their
  environment, in a process group of their own, killed after 30s. Verified end
  to end with a plugin written for the test (hook, action, pane); **not**
  verified with the owner's `herdr-sidebar`, which needs more than the host.
- **Copy mode** (herdr's `client/shell/copy_mode.rs`): `ctrl+b [` enters it,
  as in tmux and herdr — it replaces the plain scroll view, which it is with a
  cursor added. vi motions (`h j k l w b e W B E 0 ^ $ { } g G`, pages with
  ctrl+b/f/u/d), `v`/space and `V` to select, `y`/enter to copy (the cursor's
  line when nothing is selected), `/` `?` `n` `N` to search, escape clears a
  selection and then leaves. Motions and search run on the server
  (`pane.copy_motion`, `pane.copy_search`) with herdr's word classes and
  separator set; search is literal, smart-case, and matches across wrapped rows.
  Verified inside the real Claude Code: searched, selected and copied its text.
- **Worktrees** (herdr's `worktree.rs` and `app/api/worktrees.rs`): `tend
  worktree list|create|open|remove` and `worktree.list|create|open|remove`
  over the socket. A new worktree goes to `[worktrees] directory`
  (`~/.tend/worktrees`, herdr's is `~/.herdr/worktrees`) as
  `<repo>/<branch-slug>`; a branch nobody named gets herdr's generated
  `worktree/<adjective>-<noun>-<hex>`; an existing branch is checked out rather
  than recreated. It opens as a space rooted in the checkout and filed in a
  group named after the repository — herdr's "membership", which tend's groups
  already are. Removing follows herdr's order: forced, the space closes first;
  not forced, git is asked first and a dirty worktree keeps its space.
  Verified with real git end to end.
- **Moving and swapping** (herdr's `layout.rs` swap_panes, `workspace.rs`
  move_tab, `app/actions.rs` move_workspace): `ctrl+b H/J/K/L` swaps the focused
  pane with its neighbour — herdr's binding — keeping the split shape and its
  ratios; tabs and spaces move from their right-click menus; `pane.swap`,
  `tab.move`, `workspace.move` on both sockets. Resizing moved to herdr's
  resize mode, `ctrl+b r`, then h/j/k/l until escape; redraw moved to
  `ctrl+b R`. A new `session-changed` event (feature `session-changed`) tells
  other clients about swaps, moves, renames and regrouping, which before this
  a second client did not see until something unrelated made it look again.
- **Navigation keys**: `ctrl+b tab` / `shift+tab` cycle the tab's panes
  (herdr's `CyclePaneNext|Previous`), `ctrl+b ;` goes back to the pane focused
  before, `ctrl+b < >` step through the agents in sidebar order wherever they
  are, and `ctrl+b w` walks the sidebar (`g` is herdr's navigator, item 8).
  herdr leaves `LastPane`, `PreviousAgent` and `NextAgent` unbound by default;
  `;`, `<` and `>` are tend's choice.
- **Notifications and sound** (herdr's `terminal_notify.rs`, `sound.rs`,
  `app/actions.rs` toast rules): an agent that becomes blocked "needs
  attention", one that was working and went idle "finished", and nothing else
  is said — herdr's rule. `[notify] toasts` is `"tend"` (status line, the
  default), `"terminal"` (OSC 9, or kitty's OSC 99, wrapped for tmux, with the
  same backend detection and text sanitising as herdr) or `"off"`; the focused
  pane says nothing unless `focused = true`. `[sound]` plays a file through
  paplay/aplay/afplay/ffplay/play, or rings the terminal bell when none is
  named.
- **Settings, live**: `ctrl+b s` opens a settings screen (herdr's key) that
  changes the file and applies it at once; `ctrl+b R` re-reads the file — the
  client applies its half, `server.reload_config` makes the server re-read
  its own (detection interval, scrollback for new panes). An edit keeps the
  file the user wrote: one line replaced in place, comments and order intact,
  refused outright if the result would not parse (herdr's
  `config/io.rs::upsert_section_raw`).
- **Named themes** (herdr's `config/theme.rs` and `app/state.rs` Palette):
  `[ui.theme] name = "tokyo-night"` picks one of herdr's eighteen palettes,
  by any of herdr's spellings (`Tokyo Night`, `tokyonight`, `latte`, ...),
  with the values transcribed from herdr's source by a script. The palette
  colours what herdr colours with it — accent for focus, overlay0 for frames
  and secondary text, yellow/red/green for states — and the five individual
  colours still override it. The settings screen's first row is the theme,
  as in herdr, and each step along it writes and applies, which is the
  preview herdr's list gives. A palette with a panel colour draws the bars,
  menus and panels on it and what is chosen on its accent, as herdr does;
  the terminal palette, which has none, keeps reverse video. `auto_switch` with `dark_name`/`light_name`
  follows the outer terminal: tend turns on mode 2031 and asks for the scheme
  and the background colour, takes a scheme report over the background's
  luminance as herdr does, and keeps both kinds of answer out of the panes.
- **Outer window title** (herdr's `config/window_title.rs`,
  `app/window_title.rs`): `[ui] window_title` with herdr's tokens
  (`{hostname}`, `{workspace}`, `{tab}`, `{pane}`, `{terminal_title}`),
  escapes and default (`"{hostname}: {workspace}"`; `""` leaves the title
  alone). `{terminal_title}` drops a leading spinner frame as herdr does.
  `tend terminal title set|clear` and `client.window_title.set|clear` put a
  script's title over the template until cleared. herdr renders on the server;
  tend renders in the client from the snapshot, which carries the server's
  hostname, so `{hostname}` still names the machine the panes are on.
- **Tab bar status** (herdr's `config/tab_bar.rs`, `app/tab_bar_status.rs`):
  `[ui] tab_bar_right` with herdr's entry types (zoom, hostname, datetime,
  text, command), defaults (command every 5s, killed after 2s) and limits
  (16 entries, 80 characters, last line of output with escapes stripped by
  herdr's state machine), and `tab_bar_right_separator`. The server works out
  hostname, datetime and commands, as in herdr, and sends them in the
  snapshot; ZOOM is filled in by the client, whose zoom it is. A command runs
  in the focused pane's directory with `TEND_SOCKET_PATH`, `TEND_BIN_PATH` and
  `TEND_ACTIVE_{WORKSPACE,TAB,PANE}_ID`, in a process group killed on timeout
  or reload. The status gives way to the tabs on a bar narrower than herdr's
  minimum strip. `tab_bar_position = "bottom"` puts the bar above the status
  line and `hide_tab_bar_when_single_tab` gives its row back while a space
  has one tab; drawing, clicks and the pane area share one `TabBarRow`.
- **Your own commands on keys** (herdr's `[[keys.command]]`,
  `app/custom_commands.rs`): `shell` runs detached through a login shell,
  `pane` opens a pane that has the screen (zoomed) until the command ends and
  then closes and gives the view back, `plugin_action` invokes a plugin
  action by id. They run on the server, in the focused pane's current
  directory, with `TEND_ACTIVE_{WORKSPACE,TAB,PANE}_ID` and the socket. A key
  given to a command takes it from a default binding, and that is reported;
  `tend keys` and the help list them. The scrollback editor (`ctrl+b e`) is
  now the same kind of visit: zoomed, and closed with the editor.
- **Sidebar rows as tokens** (herdr's `config/sidebar.rs`,
  `config/sidebar/rules.rs`, `ui/sidebar/tokens.rs`): `[ui.sidebar.agents]`
  (`rows`, `rows_by_agent`, `row_gap`) and `[ui.sidebar.spaces]` with
  herdr's built-in tokens, `$name` for values a hook reported, styled tokens
  (`fg`, `bold`, `dim`) and rules (`equals`, `contains`, `starts_with`,
  `gt`, `lt`, `ignore_case`, `hide`), herdr's defaults and limits, and its
  way of fitting a row: fixed parts kept, text shared out, rightmost kept
  when not all fit. A hook's values now show where a `$token` puts them, as
  in herdr, rather than always after the agent's name. `[ui] sidebar = true`
  (tend's older key) still reads; whether the column starts shown is herdr's
  `sidebar_start_collapsed`, which the settings screen now writes.
- **Done, status marks and the attention order** (herdr's `seen`,
  `status_icon`, `agent_panel_sort`): an agent that goes from working or
  blocked to idle while its tab is not in view is "done" until the tab is
  looked at (the server tracks it, from the focus the client reports), shown
  in its own colour (the palette's teal) and first after blocked agents.
  Marks are herdr's — dots (● happening, ○ idle, · nothing known) or
  `status_indicators = "symbols"` (× ◐ ✓ ○ ·) — and mark the state, not
  which entry is in view, which the band already shows.
  `agent_panel_sort = "priority"` orders the agents blocked, done, working,
  idle, most recent change first. The client reads the window's focus (mode
  1004) and tells the server, so an agent that finishes while the window is
  behind something is done even in the tab on screen, and is announced even
  though its pane is the focused one; the terminal's theme is asked again
  when the window comes back.
- **Agent names** (herdr's `agent.rename`, `agent.start` name): a script
  names the agent in a pane (`tend agent rename p_2 reviewer`,
  `agent.start` with `name`) and addresses it by that name ahead of the
  agent's own label; herdr's name rule, `invalid_agent_name`,
  `duplicate_agent_name`, `not_an_agent`. Kept in the snapshot, so it
  survives a restart and a handoff.
- **herdr's event names** (`api/schema/events.rs`): events and plugin hooks
  go by herdr's names — `pane.created`, `pane.agent_status_changed`,
  `pane.output_changed` and the rest — so a hook written for herdr fires;
  tend's older `pane.opened`, `agent.state` and `pane.output` are read as
  aliases. Added: `workspace.closed|renamed|moved`, `tab.closed|renamed|
  moved`, `pane.moved`, `pane.agent_detected`, `worktree.created|opened|
  removed`.
- **Rebindable keys** (herdr's `config/keybinds.rs`): `[keys.bind]` maps a
  command to a key — `detach = "q"` — over the defaults, `tend keys` lists
  every command and the key it is on, and the help shows the keys in effect
  rather than the defaults. Binding a key something else has is allowed and
  reported. One table feeds the parser, the help and the listing, so they
  cannot drift.
- **Agent session resume** (herdr's `agent_resume.rs`): when a hook has said
  which conversation an agent is in, that reference is written into the state
  file, and a restored pane starts with the flag that continues it —
  `claude --resume <id>`, `codex resume <id>`, `omp --resume=<path>`, one per
  agent, herdr's table. Only the integration tend ships for that agent may
  name one, and the reference is refused if it is empty, has control
  characters, or is a path where an id belongs: it goes onto a command line.
- **Kitty graphics** (herdr's `kitty_graphics.rs`): `internal/vt` keeps what a
  pane's program transmitted and where it placed it — direct transfers of RGB,
  RGBA or PNG, in chunks, placed at the cursor in absolute rows so an image
  travels with its text — and the client draws the visible ones on its own
  terminal after each paint, with ids of its own, removing what scrolled away.
  Kitty, Ghostty and WezTerm get images; other terminals get nothing, which is
  what they would have shown anyway (`TEND_GRAPHICS=off` turns it off). A file
  transfer (`t=f`) is refused: the path would be chosen by whatever is running
  in the pane.
- **Updater** (herdr's `update.rs`): `tend update [-check]` reads the manifest
  for the configured channel — herdr's shape, version, notes, assets and
  checksums by platform — downloads this platform's asset beside the binary it
  will replace, refuses it if the checksum does not match, and installs it by
  rename, keeping the old one until the new is in place. `tend channel
  [stable|preview]` shows or sets the channel. The stable channel's manifest
  is published with each GitHub release (`latest.json`, at
  `releases/latest/download/latest.json`); preview has none unless set. A
  release build is offered only a newer release (herdr's semver compare); a
  build from a working tree, any different one. Tested against a local HTTP
  server, and `-check` against the published v0.3.0 manifest.
- **Update notice and release notes** (herdr's `auto_update`,
  `release_notes.rs`, `ui/release_notes.rs`, `global_menu.rs`): a release
  build's server checks when it starts and every half hour until it finds a
  newer release, gated by `[update] version_check` (on by default, read again
  each time); it saves the notes to `release-notes.json` beside the settings
  file, tells every client (a notice "v… available"), and says "update ready"
  in the snapshot until the release runs, surviving a restart. The client
  shows "update ready" at the right of the status bar and "● menu" on the
  sidebar's button, whose menu is herdr's global one (settings, keybinds,
  reload config, "update ready ●" / "what's new", detach); that entry opens
  herdr's 80×24 notes panel (markdown as herdr draws it, scroll, esc/enter
  close), and closing it marks the running release's notes read
  (`release_notes.dismiss`). Nothing is installed without being asked: the
  panel of a newer release offers "u update now" — tend's own, where herdr
  only says how — which runs `tend update -handoff` in a tab of its own.
  `TEND_FAKE_UPDATE_VERSION` is herdr's `HERDR_FAKE_UPDATE_VERSION`.
  Checked against GitHub: a v0.2.0 build's server found v0.3.0 and saved
  its notes.
- **Focus events**: a program that asked for mode 1004 is told when its pane
  gains or loses focus (`pane.focus`), and one that did not ask is not — an
  unasked-for report is a stray "[I" in somebody's shell.
- **Naming a pane**: `pane.rename` and "rename pane" in the pane menu. The
  name is the user's and the program's own terminal title no longer replaces
  it.
- **Double-click selects a word**, by herdr's word classes — the same ones
  copy mode moves by, so a double click and `w` cannot disagree — and copies
  it at once.
- **Event streaming**: `events.subscribe` turns the connection into a feed —
  one JSON object per line until the caller hangs up — beside `events.wait`,
  which answers one and returns. `tend events [-kinds …] [-pane …]` is that
  feed from a shell. A slow reader loses the middle rather than holding the
  server's publisher.
- **Lifecycle events**: `pane.focused`, `tab.focused`, `workspace.focused`,
  `tab.created` and `workspace.created`, over both sockets and to plugin
  hooks, under herdr's names. Focus is the client's, so a client reports it
  and every listener hears; the same pane twice says nothing, or a hook would
  run on every keystroke that moves focus.
- **Scrollback in an editor**: `ctrl+b e` writes the focused pane's history to
  a file and opens `$EDITOR` on it in a pane of its own — herdr's
  `EditScrollback`. The file is removed by the command that opened it, so
  nothing has to remember it.
- **Questions about a pane's place**: `pane.neighbor`, `pane.edges` and
  `pane.process_info` — which pane is beside it, which edges of the tab it
  touches, and what is running in it (the shell, the foreground process group,
  the tty and each process with its arguments, read from /proc). Geometry is
  asked of the layout at a nominal size, since which pane is beside which does
  not depend on any window. Checked against a real `sleep 40 | cat`.
- **Worktrees from the client**: the space menu has "new worktree…", which
  asks for the branch with a generated name already in the box (herdr's
  create overlay), "open worktree…", which lists the repository's worktrees,
  and "remove worktree", which refuses over changes that would be lost and
  names the command that would remove it anyway. `ctrl+b G` is the same
  prompt.
- **Explaining detection** (herdr's `agent explain`): `tend agent explain
  <target> [-screen]` runs the pane's manifest over its screen now and reports
  every rule, which matched, and which decided — or which hook is answering
  instead, when one is. Checked against the real Claude Code: `live_prompt_box`
  decided "idle", out of sixteen rules.
- **Agent metadata** (herdr's `terminal/metadata.rs`, `metadata_tokens.rs`):
  `pane.report_metadata` takes what a hook says about how to show a pane — the
  name the agent goes by, values to put beside it like the model or what is
  left of the context, labels for its states — with a lifetime, because "23%
  of context left" is true for a minute and misleading for an hour. It shows
  in the sidebar and in `pane.get`. Ordered per source like a state report,
  and refused about an agent that is not the one in the pane. Values are shown
  in key order: two reported in one message arrive in a map and have no order
  of their own.
- **Notifications from anything**: `notification.show` over the socket and
  `tend notify <title> [body]`, for a script, a hook or a plugin that has
  something to say and no screen to say it on. `[notify] toasts = "system"`
  adds desktop notifications through notify-send or osascript.
- **Moving a pane elsewhere**: `pane.move` takes a pane out of its tab and
  puts it beside another, in any tab or space, without remaking it — the
  process goes on running. A tab emptied by the move closes.
- **Layouts**: `tend layout save [tab] [-o file]` writes a tab's shape and
  what each pane runs; `tend layout apply <file>` builds it again in a space
  of its own. `layout.export` and `layout.apply` over the socket. tend's tree
  is n-ary where herdr's is binary, so the saved shape is tend's own.
- **Plugin builds**: `tend plugin link` runs the manifest's `[[build]]` steps
  and unlinks the plugin if one fails — a plugin whose binary does not exist
  is one whose every action fails later, somewhere else. `tend plugin build`
  runs them again.
- **Settings file** with validation, `tend config`.
- **Agent hooks**: `tend integration install|uninstall|status`, the automation
  socket methods `integration.*` and `pane.report_*`, and Unix assets for every
  herdr target. An installed hook reports over `$TEND_SOCKET_PATH` so the
  arbiter can prefer the agent's own account over screen detection.

## Different from herdr on purpose

| | herdr | tend | why |
|---|---|---|---|
| Terminal core | libghostty-vt via FFI | pure Go | owner's decision; no cgo |
| Agent state | screen detection **and** hooks installed into each agent | screen detection **and** hooks (`tend integration install`); arbitration + automation socket | — |
| Forced selection | none inside a mouse-holding program | alt+drag selects a block anywhere | fallback for programs that hold the mouse and do nothing with a drag |
| Clipboard | OSC 52 only | local tool (`wl-copy`/`xclip`/`xsel`/`pbcopy`) when not over ssh, plus OSC 52 always | the owner's terminal refuses OSC 52 |
| Space groups | only a repository's worktrees, grouped automatically | named groups of any spaces: "new group..." on a space makes one, "move to group..." (once there is a group) lists them, or leaves one, and a space dragged onto a group's heading or among its spaces joins it | the owner asked for spaces to be put together by hand |
| Companies | user-defined Spaces (`src/user_space.rs`, `src/app/api/spaces.rs`): named sets of workspaces, many-to-many, saved with the session, with `space.create/rename/delete/assign/unassign` handlers — and, in the checkout read, no screen and no schema types for them yet | the same model in `internal/session/company.go` (herdr's sanitising on restore, a closed space leaving every company), saved in the state file, sent in the snapshot, and `company.create/rename/delete/assign/unassign` on the client protocol; called companies because tend's workspaces are already called spaces. tend's own on top: prefix+O or the mark at the right of the "spaces" heading opens a panel to make, rename and delete them, tick which spaces each holds, and choose one; while one is chosen the sidebar lists its spaces and their agents only, next/previous space stays among them, a new space goes into it, and going to a space outside it (the navigator, an agent's notice) chooses a company that has it. A worktree's space — made or opened by worktree.create/worktree.open, from the menu or by starting work on an issue, an error or a ticket — goes into the companies of the space it came from, on the server, and back into them when a failed removal reopens it (#34). Which one is chosen is the client's, kept per session in the state directory; the heading shows "!" when an agent in a hidden space is waiting. Not on the automation socket yet | the owner's: many companies to work for, each with its clients' spaces, to be looked at one at a time |
| Files panel | none built in; the owner used the third-party `herdr-sidebar` plugin | `tend files`, docked on the left by prefix+f or the pane menu, in herdr-sidebar's three views (1 files, 2 search, 3 changes): tree with git letters; text search through `git grep` (tracked and new files, not ignored ones; case, whole word, regex, include/exclude globs; live, a moment after typing stops, off the draw loop), opening the editor on the line; following the pane beside it to another project (herdr-sidebar's CwdFollower rule: the focused sibling's live directory, else the one followed, else the lowest id; checked every two seconds; pane.list now carries herdr's workspace_id, tab_id, focused, cwd and foreground_cwd for it); the branch and a sync button on every view's header (herdr-sidebar's Git footer: click the branch or B to switch, a remote branch becoming a tracking local one; ⟳ or P to fetch, pull --ff-only, push, or publish to origin; in the background with GIT_TERMINAL_PROMPT=0 and ssh in batch mode); changes with diff, stage, commit (A drafts the message through `claude -p --model haiku` as herdr-sidebar's ✧ does, from the staged diff or else the unstaged one, 16 KB, 60 s, falling back to the file names; tend's session variables are kept from it so its hooks do not report the panel as an agent), discard (twice); find a file by name (`/`, ctrl+p); preview (space, or a click) in one reused tab, named after the file and gone to, as herdr-sidebar's default is (tend first split a pane beside the widest one to keep the panel in view; the owner asked for the tab, so the terminal being worked in keeps its width) — `tend view`, read-only, line numbers, a small lexical highlighter of tend's own for the common languages and markdown rather than a highlighting dependency, read again when the file changes, markdown rendered by tend's own line renderer (headings, emphasis, code, links, lists, tasks, quotes, fences, rules; m for the source) where herdr-sidebar asks glow, and PNG/JPEG/GIF drawn in half blocks with the standard library's decoders where herdr-sidebar has its own renderer — no graphics protocol needed, 24-bit colour is; video posters (ffmpeg) are not ported, pointed at the next file through a private OSC typed into its pane; the tree's context menu (m, right-click; herdr-sidebar's menu_entries: new file/folder, open with default app, stage, copy path/relative path as OSC 52, rename, delete — confirmed by typing yes, tend's addition —, reveal, change folder, which holds against following until a pane moves) and s to stage a file or folder from the tree; history (L: commits, a file's commits from the menu, stashes with apply, pop and a drop confirmed by typing yes, tags; each shown by git show with its diff; herdr-sidebar's branches, worktrees and remotes lists are the branch picker and tend's own worktree commands here); icons (herdr-sidebar's material theme as a Nerd Font's glyphs, or emoji; none by default, since glyphs the font lacks are boxes), on the tree and on herdr-sidebar's activity bar, which with icons on takes the header's place: the three views as icons on the left in chips, the one shown reaching half a block above and below, and a gear on the right opening the panel's settings (herdr-sidebar's settings overlay, as a screen of the panel's like its menu; `,` as well, since its `s` stages here) — and under it herdr-sidebar's layout: the project's name in capitals, the tree drawn as its row_line (its icon table, glyphs and colours as they are, names in the text's colour, a folder's letter a ●, the selection its DarkGray background rather than reversed text), its "m / ctrl+rclick for menus" line (right-click here, since tend does not take it), and branch and sync on the bottom line as its git footer; no « (tend puts the panel away with prefix+f); no hover highlight, since the panel is not sent the pointer's motion; settings as `[files]` in tend's settings file and rows on tend's settings screen (icons, follow, dotfiles, side — herdr-sidebar's left/right docking, pane.dock taking right, right by default at the owner's wish —, width), read again by an open panel when the file changes, and written from the gear's settings too; a folder of repositories (herdr-sidebar's multi-repo folders: those up to two levels below a folder that is not one, each one's changes under its name and branch, the tree marked from each, and branch, sync, history and commit acting on the repository of the selection); open in `$EDITOR` in a tab | tend's own, in the one binary, running where the files are |
| Handoff transport | pty descriptors sent as `SCM_RIGHTS` over a socket; the new server binds the socket afresh | descriptors inherited by the child (`exec.Cmd.ExtraFiles`) at fixed numbers, the **listening socket included** | inheritance needs no protocol, and handing the listener over means the socket file is never removed and recreated — there is no instant with nobody listening |
| Focus, scroll and zoom over the API | `pane.focus`, `agent.focus`, `pane.scroll`, `pane.zoom`, `pane.current` are server methods | `pane.focus` and `tab.focus` ask every attached client to show the pane, and each client moves itself (the answer says how many were asked); scroll, zoom and `pane.current` are not offered | in tend these are client state (AGENTS.md: what one person is looking at stays in the client), so the server can ask but not set, and cannot answer for a client that may not be attached |
| Names | workspace | space (in the UI; `workspace` in code and on the wire) | matches herdr's own UI wording |
| Claude / JSONC settings | `jsonc_parser` preserves comments and compact layout | `encoding/json`; comments lost and **keys re-sorted alphabetically** on rewrite (content otherwise identical — checked against the owner's real 44 KB `settings.json`: install adds one `SessionStart` entry, a second install adds nothing, uninstall restores it exactly) | avoid a new dependency; invalid JSON is an error, not silently stripped |
| Default theme | catppuccin | the terminal's own colours when no `name` is set | an unset theme keeps what tend has always looked like; the owner picks a palette in the settings screen or the file |
| Window title on detach | writes "herdr" | saves the window's title when it first writes one (`CSI 22;0t`) and puts it back on detach (`CSI 23;0t`) | detaching should leave the window as tend found it; a terminal without the title stack keeps tend's last title, which is no worse than herdr's name |
| Invalid tab bar entry | hidden, with a diagnostic | the settings file is refused at load, like every other value tend cannot use | one rule for every setting; a gap in the bar with the reason in a log nobody reads is harder to notice |
| Sidebar toolbar | none | a row of tools over the spaces (`internal/ui/toolbar.go`, `cmd/tend/tui_toolbar.go`): Files (the files panel, as prefix+f), Agents (the agent manager, prefix+A), Context (prefix+C) and Browser (prefix+B); `[ui.toolbar] enabled` / `items`; reached by a click, by prefix+w's walk (the tools are its first stops), its name on the status line on hover or focus | the owner's direction for tend past herdr: an environment for working with agents — files, a browser, captured context — with the server between tools and agents |
| Memory tools in the agent manager | none | a MEMORY section under the agents (docs/MEMORY.md, #28): ai-memory (AUR `ai-memory-bin`, else its README's Docker quick start without the optional model keys), found installed and running by 127.0.0.1:49374 answering when it is a container with no program on the PATH; Graphify (`pipx`, `uv tool` or `pip install graphifyy && graphify install`, as its README gives them); enter on one installed says what gives it to the agents (`ai-memory install-mcp`/`install-hooks`, `/graphify .`) | the owner's: integrate the memory others built rather than rebuild it |
| Agent manager | none (herdr installs hooks into agents, not agents) | prefix+A or the toolbar's Agents: `agents.catalog` on the server finds each agent CLI in `internal/agents` (its executables, from `internal/integration` where it knows them; its `--version`) on the machine the panes run on, and offers the first install method its vendor documents whose program is here (script, npm, brew, pip). Installing asks, showing the command, and runs it in a tab of its own that goes on as a shell; nothing installs by itself. The commands were read from each vendor's page on 2026-09-23, with the page kept beside each | the owner's direction for tend past herdr |
| Errors (GlitchTip) | none | prefix+E or the toolbar's Errors: the errors a GlitchTip or Sentry server collected (`internal/glitchtip`, Sentry's web API, which GlitchTip speaks: an issue's event and status are asked at GlitchTip's `/api/0/issues/{id}/…` and, on a 404 or 405, at the `/api/0/organizations/{org}/issues/{id}/…` Sentry documents; Sentry's `permalink` is the issue's page — checked against a stand-in written from Sentry's documentation, no real Sentry; organizations, projects, `organizations/{org}/issues/?query=is:<status>`, an issue's `events/latest/`, `PUT issues/{id}/` for its status). The client talks to it itself — a web service, not the projects' machine — with the server and an API token given in the panel's connect box, tested before they are kept, and written to the settings file (0600; `TEND_GLITCHTIP_TOKEN` also serves). Several servers are kept, a table each (`[errors.sources.<host>]`, beside the first one's `[errors] url/token`), so adding or removing one edits only its own lines: the gear or ctrl+k opens the servers box (use, add, remove — the table goes with its token, after enter confirms), a line of server chips and ctrl+g move between them, and `[errors] source` remembers the one shown. A project folder is tied to a server and a GlitchTip project (`projects = { "<folder>" = "org/slug" }` in that server's table, ctrl+l or the title's "link … here"): the panel opened from a pane working under the folder — its program's directory, from pane.get, and the repository it is in, a worktree counting as the repository it came from — shows that server and project whatever was remembered, the deepest tied folder winning. A line of project chips and the unresolved/resolved/ignored presets over a search; enter opens an error with its exception chain, the application's own frames marked with their code line, the request, tags and breadcrumbs. f types it into the pane the panel was opened from — its in-app frames last call first, the request, the environment, and that the paths are the server's — through the context, inert and unsubmitted; w starts the agent in a worktree `fix-<short id>` of the space's project; c puts it in the context; r, i, u resolve, ignore, reopen it — and from the list, without opening it, ctrl+x or the Resolve and Ignore buttons (Reopen, listing the resolved or ignored). An issue is closed from its list the same way, ctrl+x or Close issue, asking completed or not planned. Checked reading the owner's server (53 unresolved in 3 organizations); resolving was checked against a stand-in only | the owner's: the errors of the systems running, found and fixed from where the agents are |
| GitHub issues | none | prefix+I or the toolbar's Issues, after Orca's task page (read in `../orca`, `src/main/github/`): the server runs `gh` on its machine (`internal/github`, `github.issues`/`github.issue`), so tend keeps no token; the repository is the one the pane in view is working in (followDir), read from its git remotes, upstream before origin as Orca does, ctrl+t for the other; the list is GitHub's issue search (`gh api search/issues`, the last updated first, 100), with Orca's presets — open, assigned to me, created by me, closed — on tab, and what is typed added to the query in GitHub's own syntax, asked 400 ms after typing stops; enter opens an issue with its text and comments (`gh issue view --json`), drawn by the release notes' markdown; ctrl+o or o opens it in the desktop's browser. gh missing, not logged in, or no GitHub remote are said as such. On an issue, c comments (gh issue comment, the text on stdin), x closes — enter as completed, n as not planned — or reopens, and ctrl+n on the list files a new one (title, tab, text; gh issue create), all written in a box at the panel's foot, enter sending and ctrl+j breaking the line. w, or Start work, is Orca's "start workspace from issue": a worktree on `issue-<n>-<title slug>` from the project, through the automation socket's worktree.create as prefix+G is, opened as a space, and the agent started in a tab there with `[issues] prompt` (Orca's "Complete {url}" by default) as its first argument — `[issues] agent`, else the pane's agent, else claude; an issue with a worktree already is gone to instead. → shows the pull requests (Orca's PR mode: open, mine, needs my review, closed), each with its branch and review decision, and its checks filled in by a second call for the first 30 — with the checks in one call for a hundred, GitHub answered stablyai/orca with a 504; enter opens one with what it changes, whether it merges, its failing and running checks, its text and its thread of comments and reviews (HTML comments, bots' metadata, hidden as GitHub hides them); m merges it (s squash, m merge commit, r rebase), c comments, x closes or reopens, r makes a draft ready, w goes to the worktree its branch is in. An issue lists its pull requests — those GitHub links to it (closedByPullRequestsReferences, through GraphQL, as gh 2.67 does not carry it) and those on the project's issue-<n>-… branches — and p opens the first. Checked with the real gh on stablyai/orca and sousaakira/tend, reading only (issue #22725 shows its PR #22731, and p opens it); what is written was checked against a gh stand-in, and w with the real Claude Code. On an issue, l and a pick its labels and assignees from the repository's (Orca's edit sections: `repos/{o}/{r}/labels` and `/assignees`), enter marking and ctrl+s sending only what changed through one `gh issue edit` with `--add-label`/`--remove-label` and the assignee flags, so a label somebody else changed meanwhile is left alone; e changes its title. A folder that is no repository but has some under it — a project of several, the owner's mvno with its api, app and panel — lists them all (the files panel's rule, two levels down): one GitHub search with every `repo:`, which GitHub takes as any of them (checked: 3251 + 1028 = 4279 on two public repositories), a repository column, and a line of chips — all, then each — that ctrl+t or a click moves along; pull requests are each repository's `gh pr list` at once, put together by date, checks keyed by repository and number; everything done to an issue or a pull request is done in its own repository, work starts in its own checkout, and a new issue from all of them goes to the one its box's ctrl+t picks. Not yet: closing as a duplicate, more than 100 results, GitHub Enterprise and ssh host aliases | the owner's: managing projects from where the agents run |
| Tickets (Linear, Jira, ClickUp) | none | prefix+T or the toolbar's Tickets (#9), the errors panel's shape: accounts kept one table each (`[tickets.sources.<name>]`: kind, url and email for Jira, token — in the keyring when there is one — and the project folders tied), a box to add, show and remove them, a connect box choosing the tracker and testing the token (who it signs in as) before keeping it. `internal/tickets`: Linear's GraphQL (the key as the Authorization header itself; teams as scopes; filters on state type, isMe, title; a comment and the team's completed state by the issue's id), Jira's REST v2 (Cloud's email and token by basic auth, a Server/Data Center PAT as a bearer, a public Jira anonymously; projects as scopes; JQL per preset; Cloud's /search/jql first and the old /search on a 404; marking done takes the workflow's transition into a finished status), ClickUp's API v2 (the token as the Authorization header; workspaces as scopes; the user's tasks by their id; names filtered for a search, which the API does not do; done is the list's closed status). A ticket whole with its comments, rendered as markdown; c comments, x marks done after asking, f types it into the pane, w starts the agent in a worktree on ticket-<key>, ctrl+l ties the folder. Checked reading the real Apache Jira anonymously (open, done, search, one whole); Linear, ClickUp and every write against stand-ins written from their documentation, and end to end with a stand-in Jira | the owner's: the flow from ticket to PR for teams that keep their work in a tracker |
| Activity record, costs and policies | none | the server keeps `activity.jsonl` in the state directory (`internal/activity`, `server/activity.go`, #10): every few seconds it compares the panes whose process is running with an agent against the runs it has open, and appends a start (project, checkout, branch, worktree or not, HEAD, space, tab), the conversation a hook named (with what it had spent then, so a resumed one bills only this run), a stop (why, the files changed since HEAD — committed, uncommitted and new — and the commits) and any policy broken. A replacement after a handoff carries the runs on under their ids; a run whose server died is ended as lost, with no end time. Tokens are read from the agents' own files (`internal/usage`: Claude Code's transcript and its subagents, one count per reply by message id; Codex's last running total) and priced from Anthropic's public API prices, which `[activity.prices]` overrides. `[policy]` (`internal/policy`): protected branches (patterns allowed), require a worktree, a limit on agents at once; checked before a pane that runs an agent is opened and before agent.start types one (refused, `policy_denied`, and written down), and when an agent tend did not start appears (a notice and a line; never killed). `tend activity` reads the file with no server: per run, per project or per agent, as a table, CSV or JSON, `-prs` asking gh or GitLab what each branch became. Checked with the real Claude Code in a pane: start, the conversation its hook named, the stop at /exit, the file it wrote and a cost matching the transcript by hand | the owner's: governance for teams — what agents did, what it cost, and rules on what they may do |
| Tickets: the Issue Gateway | none | a fourth tracker kind, `issuegateway` (auth-com-br/issue-gateway#27), tend's own and not ported from herdr, which has no tickets panel to port it from: Auth's REST API at issue.auth.com.br (fixed: the connect box asks for the token alone; `TEND_ISSUE_GATEWAY_URL` or an account's `url` points it elsewhere), a bearer token; `/v1/me`, `/v1/scopes`, `/v1/issues` with the scope, the preset's name and what was typed — the gateway filters, searches and orders, tend only reads — one issue whole with its comments by its ref, a comment and closing posted to it; an error's `{"error":{"message"}}` is what the panel says. Checked against a stand-in written from the contract and against the real gateway | the owner's (issue-gateway#27): the tickets the owner's own gateway serves, in the same panel as the other trackers' |
| GitLab | none | the issues panel for a project on a GitLab (#9): a remote on gitlab.com or a host with "gitlab" in its name is a repository (subgroups kept; its slug host/group/…/name, which no GitHub owner can look like), and `internal/github/gitlab.go` asks GitLab's REST API (v4) into the same issue and pull request shapes — issues per preset (opened, scope assigned_to_me/created_by_me, closed, search), an issue with its notes (GitLab's own system notes left out), comment, close, reopen, create, labels, members as assignees (set by id), title; merge requests per preset (reviewer_username for "needs my review"; state=all without the open for closed), one whole with its head pipeline as its one check and detailed_merge_status/has_conflicts as mergeable, comment, merge (squash or merge; rebase is the project's setting and refused), close, reopen, ready (the draft mark taken off the title); an issue's merge requests are GitLab's related ones and those on its branches. A public project is read without a token; gitlab.com keeps an issue's notes and a project's labels to users, which the panel says. `tend gitlab login [host]` checks a token (GET /user) and keeps it in the keyring, else the settings (`[gitlab.tokens]`); GITLAB_TOKEN also serves. Checked reading gitlab-org/cli on the real gitlab.com anonymously (issues, one whole, merge requests open and closed, one whole with its pipeline, related merge requests, search); writing against a stand-in only | the owner's: the flow from ticket to PR for teams on GitLab |
| Agent sessions | none (herdr resumes the conversation a pane was in, `agent_resume.rs`, and lists no others) | prefix+S or the toolbar's Sessions: `agent_sessions.list` on the server reads the conversations its machine's agents keep (`internal/agentsessions`; Claude Code's so far, `~/.claude/projects/*/<id>.jsonl`, read loosely since the format is undocumented, cached by size and time — 39 real sessions, 56 MB, read in 0.36 s and again in 2 ms), titled by the name given (`agent-name`), else Claude Code's (`ai-title`), else the first prompt; the list searches every word typed in title, project and id; enter resumes in a new tab in the conversation's directory with the agent's resume flag (`internal/agent`), or goes to the pane it is open in; one held in a directory that is gone or not open to this user (the owner's held as root in /root) is marked ✗, and enter copies the command that resumes it where it can be instead of failing as `permission denied`; tab marks, ctrl+a marks what is shown, ctrl+o what is 30 days old, ctrl+d deletes after enter confirms (`agent_sessions.delete`: the file and its subagents' folder, never the project's memory, never one open in a pane) | the owner's: many projects, many conversations, no way to find or clear them |
| Handing a task to another agent | none | "continue in..." on an agent's pane menu and its sidebar row, and ctrl+g in the sessions list (docs/MEMORY.md, #27): `agent_sessions.handoff` on the server reads the conversation the pane's hook reported (source `tend:claude`), or one by id — Claude Code's so far — for its title, first and last five requests (a screenshot's description left out, a picture alone "(an image)"), the project's files its edit tools wrote, and its last words, with the branch and `git status --short` where it was held (git without optional locks); the client writes a note from it without asking a model, starts the chosen agent in a new tab of the same space there, and puts the note first in the context, sent — typed, not submitted — when the user presses enter. Checked against a real conversation of the owner's for the reading, and end to end with a stand-in conversation, hook report and agents | the owner's: memory across agents, the part only tend is placed to do |
| Context buffer | none | the server keeps what tools captured (`internal/capture` says what an item is — url, element, text, file — and how it reads to an agent; `server/context.go` keeps the newest 50 for as long as the server runs). In over the automation socket (`context.add/list/remove/clear/send`, which a browser extension will call), `tend context add|list|clear`, and "Add to context" on a file in the files panel. The context panel (prefix+C or the toolbar) lists them with the chosen one's parts, copies one, and sends one to the pane this tab works with — the focused agent, else an agent in the tab, then the space — typed and not submitted. No tool talks to an agent directly | the owner's direction: the server between every tool and every agent |
| Browser | none | the foundation (`server/browser.go`, `docs/BROWSER.md`): browsers attach over the automation socket (`browser.attach`, a stream of open/navigate/select commands) and hand back what they pick (`browser.context`, `browser.send_to_agent`), which goes into the context buffer. prefix+B or the toolbar's Browser asks for a page and sends it to an attached browser, or opens it on this machine (`[browser] command`, else the desktop's). `tend browser status|open|select|attach`, the last a stand-in for the browser. With none attached, tend's browser opens (`internal/browserext`): Chromium or Edge in a profile of the session's own with tend's extension loaded — picking elements (its icon, Alt+Shift+T), with notes and a message, into the context, where the context panel opens as they arrive and sends them on (`S`, Send all) — and `tend browser bridge` registered there as its native messaging host; started through `tend browser keep`, which loads the extension over DevTools' `Extensions.loadUnpacked` on `--remote-debugging-pipe` and holds the pipe while the browser lives — not also `--load-extension`, which Chromium and Edge still read and which dropped the first bridge when both were given; Google's Chrome stopped reading that switch in 2025, and a machine with only Chrome got the desktop's browser without the extension; tried with the real Chromium 153, Edge 153 and Google Chrome 154, the bridge attaching from each; Brave 153 loads the extension but never starts the bridge, and is not tried | the owner's direction: pages rendered by a real browser, what is picked in them reaching agents through the server |
| Update manifest | on herdr.dev (`latest.json`, `preview.json`) | an asset of the latest GitHub release, at GitHub's fixed `releases/latest/download/latest.json` | tend's releases are on GitHub and nowhere else; the URL follows each release with nothing more to publish |
| Installing an update | "detach, run `herdr update`, then run Herdr again to reconnect" | "run `tend update -handoff` in any pane; what is running keeps running" | tend's handoff replaces the server under its programs, so nothing needs leaving |
| Sidebar "menu" button | opens the global menu | the same — before, it opened the current space's menu, which is on a right-click on the space | ported with the release notes, whose only way in is that menu |
| Release notes markdown | `**` drawn as it is | `**bold**` drawn in bold | tend's notes are its GitHub release text, which uses it |
| Integration assets | `.sh` and `.ps1` | Unix `.sh` / `.js` / `.ts` / Hermes plugin only | Windows PowerShell assets not ported yet; platform code is compile-gated when they are |

---

## Not ported — the work queue

Ordered by how much each changes daily use. Sizes are herdr's, as a guide to
effort, not a target.

### 1. Live handoff — what is left of it

The handoff itself is ported (see "Ported, and checked"). What herdr builds on
top of it is not:

- **Handoff as part of updating** is ported: `tend update -handoff` installs
  and hands every running session to the new build (herdr's `--handoff`). A
  server too old to hand off is named and left running; herdr offers to stop
  it, tend leaves that to the user. `make install` then `tend handoff` is
  still the way for a build made from source.
- **Remote attach preparation** (`remote/attach.rs`) is ported for Unix
  hosts over plain ssh: the far side is probed (platform, each tend there and
  its build, gzip); a tend of this build is used wherever it is; otherwise,
  on a machine of the same kind, this binary is offered (`[Y/n]`, herdr's
  default), copied gzipped with progress to `~/.local/bin/tend`, run once
  before it replaces anything, and the running server of that session is
  handed to it. Verified against a real host (Ubuntu, glibc 2.35), which is
  how it was found that builds must be static. Not ported: herdr's download of
  a release for a machine of another kind (there are no releases yet), and
  herdr's prompt to stop a server too old to hand off. Every `-ssh` command
  runs `~/.local/bin/tend` there when it exists, and the one on the PATH
  otherwise.
- **Saved machines** (herdr's multi-endpoint client: `client/endpoint/`,
  `client/shell/endpoint_sidebar.rs`, `endpoint_navigation.rs`, `herdr
  machine`) are ported in part. Ported: the catalog (`machines.json` in the
  state directory, herdr's fields and limits) and `tend machine list | add |
  rename | remove | enable | disable`, adding a machine preparing it first as
  attaching would; with one saved, the sidebar lists machines — "Local" first,
  each with its spaces beneath it, folding, and the reachability glyph herdr
  shows — each machine not in view watched over a connection of its own
  (events only, ssh in herdr's non-interactive mode, reconnecting with
  backoff), and a click on another machine's space, or enter on it from the
  sidebar walk, shows that machine through the connection already open, so
  the switch is immediate. The agent list is herdr's aggregate one
  (`endpoint_agents.rs`, `aggregate_navigation.rs`): every machine's agents,
  each row naming its machine through the `machine` token ("Local" for this
  one), a click going there, and in priority order the attention queue
  across machines with unreachable ones last — state sequences are each
  server's own, so "most recent" between machines is approximate. Agents on
  other machines are announced as this one's are, their cards naming the
  machine, and a click on one (or open-notification) goes to it. Next and
  previous space and agent go across machines as herdr's
  `handle_endpoint_navigation` does, stepping over unreachable ones. Left: an
  agent view set by a script orders only the shown machine's agents (herdr
  filters every machine's with it); herdr's notices about a machine itself
  (`endpoint_notices.rs`: unsupported, timed out) — tend shows the state on
  the machine's row. The navigator (prefix+g) is herdr's federated one: a
  row per machine with its spaces beneath, a query matching a machine's name
  keeping all of it, and choosing a row on another machine going there. The
  catalog is read
  again every second while a client runs (herdr's `catalog_reload.rs`):
  machines added, removed, renamed, turned on or off are applied without a
  restart. Different on purpose: the machine being shown is not taken away
  when it is removed — herdr moves the client to Local — but leaves the list
  once the client goes elsewhere; and a catalog that fails to read is left
  as it was rather than read as empty.
- A server from before this feature cannot hand off — it has no such method —
  and is replaced only by a restart. `tend handoff` says so rather than doing it.
- Known limit: a pane resized between the manifest being written and the
  replacement taking over keeps the old size until the client next sends one,
  which it does on reconnect.

### 2. Agent integrations (hooks) — mostly done

herdr installs hooks into each agent so the agent reports its own state, which
is more reliable than reading the screen.

**Ported in tend:**
- Arbitration (`internal/agent` Arbiter) and server report methods
- JSON automation socket (`internal/api`) at `$TEND_RUNTIME_DIR/api/<session>.sock`
- Pane env (`TEND_ENV`, `TEND_SOCKET_PATH`, `TEND_PANE_ID`, `TEND_BIN_PATH`)
- Handing that socket across `tend handoff`
- `tend integration install|uninstall|status` and API
  `integration.list|install|uninstall`
- Unix assets and installers for every herdr target (claude, codex, cursor,
  copilot, devin, droid, kimi, pi, omp, opencode, kilo, hermes, qodercli,
  qwen, letta, mastracode, antigravity-cli, grok)

**Verified, and how:** every asset is herdr's with the name translated (diffed
file by file; three differ only in the capital T). The Claude hook script was
run in a pane of a live server with a `SessionStart` payload in Claude's format
and the session came back over `pane.list` as `agent_session`. **Not verified:**
any hook fired by a real agent — that needs the integration installed in the
owner's real agent config, which is theirs to do (`tend integration install
claude`). Session references follow herdr's rule: only the official source for
an agent may name one, by id, except pi and omp which resume from a path.

**Still not ported:**
- The arbiter leaves out herdr's bookkeeping for suppressed and stale
  full-lifecycle sessions, its window after an observed process exit, and
  agent names (`terminal/state.rs`)
- Windows `.ps1` assets and Windows path branches
- Comment-preserving JSONC rewrites (see divergence table)
- herdr's kimi min-version gate (`enforce_agent_version`) and a few
  opencode/hermes edge cases around validity checks for "outdated"
- Note the versioning rule in `../herdr/CLAUDE.md` ("Integration asset
  versions") when bumping `TEND_INTEGRATION_VERSION` markers

### 3. Automation API and CLI — the core is done, the rest is open

What a script needs is ported (see "Ported, and checked"). What is left:

- **Scroll and the current pane** (`agent.focus`, `pane.scroll`,
  `pane.current`): absent, see "Different from herdr on purpose".
  `pane.focus` and `tab.focus` are ported as requests to the clients.
- The graphics API. (`agent.view.set` and `agent.view.clear` are ported:
  herdr's filter and sort, its limits, the view kept by the server and
  applied by each client, with its label in the list's heading.) (`command.invoke` is left out
  on purpose; see item 11.)
- The published schema is ported: `tend api schema` (summary, -json,
  -output PATH), a JSON Schema of every method's params read by reflection
  off the named types the handlers decode into (internal/api/params.go), so
  the two cannot disagree, with the event names and the response shapes; a
  test fails when a Method constant is not in it. Left: the results are
  described as objects with a "type", not field by field as herdr's are,
  since tend's handlers build them as maps; and a field is marked required
  only where a handler depends on it (schema:"required"), since a missing
  field is its zero value to every handler.
- herdr: `api/schema*` (9.4k), `cli/agent.rs`, `cli/pane.rs`, `cli/tab.rs`,
  `cli/workspace.rs`, `cli/api.rs`; user docs `socket-api.mdx`,
  `cli-reference.mdx`, `agent-automation.mdx`.

### 4. Plugins — the host is done; what the sidebar plugin needs is not

The host is ported (see "Ported, and checked"). Left:

- **The owner's `herdr-sidebar`** (`~/.config/herdr/plugins/github/`): every
  API method it calls now exists in tend (`pane.layout`, `pane.send_input`,
  `pane.rename`, `pane.focus` and `tab.focus` were the missing ones) and its
  events fire by herdr's names. What is left is naming, which is the
  owner's call: its manifest is `herdr-plugin.toml`, and it reads
  `HERDR_SOCKET_PATH`, `HERDR_PANE_ID`, `HERDR_PLUGIN_EVENT_JSON` and
  `HERDR_PLUGIN_STATE_DIR` where tend sets `TEND_*`. tend does not export
  herdr-named variables (AGENTS.md: no herdr name in the product). Not run
  against tend yet.
  The owner chose a built-in panel over running it: `tend files` (prefix+f),
  see "Different from herdr on purpose". The plugin stays unported.
- `tend plugin install owner/repo[/subdir] [-ref] [-yes]` and `uninstall`
  are ported (herdr's GitHub shorthand only; clone, preview, confirm, build,
  keep under tend's state directory), and so is `plugin.log.list` (`tend
  plugin log`, the last 200 runs). The marketplace is herdr's website
  (herdr.dev/plugins, indexing GitHub repositories by topic), not code in
  the binary; installing from GitHub is ported. Left: `min_herdr_version` enforcement (tend's builds have no
  ordering to compare against). `link_handlers` are ported: a regex over
  the URL and an action of the plugin, checked as herdr checks them, the
  first match over plugins in id order run with TEND_PLUGIN_CLICKED_URL and
  TEND_PLUGIN_LINK_HANDLER_ID (pane.link_activate); links themselves are
  herdr's ctrl+click and ctrl+hover, web URLs only, trimmed by herdr's
  rules, found in the client's own copy of the screen, opened on the
  client's machine when no plugin takes them. OSC 8 hyperlinks are kept per
  cell (a 16-bit index into the screen's table, in what was padding, so
  cells did not grow and the hot paths still allocate nothing), rendered
  back for the client, and preferred over the text under the pointer —
  Claude Code writes markdown links this way when the terminal says it can,
  as VTE_VERSION inherited by the owner's panes does. The hover shows
  only while the terminal reports motion, which tend asks for only when
  something wants it.
- herdr: `plugin_command.rs`, `plugin_paths.rs`, `cli/plugin.rs`,
  `persist/plugin_registry.rs`, `app/api/plugins/`, `api/schema/plugins.rs`;
  user docs `plugins.mdx`, `marketplace.mdx`.

### 5. Copy mode and scrollback tools — copy mode done, the rest is open

Copy mode is ported (see "Ported, and checked"). Left:

- herdr refuses a motion when the pane's content changed since the client last
  looked (`stale_content`): the revision is taken when copy mode starts, and
  every motion the server answers carries it. tend does not, and porting it
  as it is needs a decision: in tend every motion, j and k included, is the
  server's, and rows are counted from the top of the history, so output
  appended below does not move them. Refusing on any change would stop copy
  mode dead over an agent that is writing. What does move rows under the
  cursor is the history dropping its oldest lines when full, a reflow on
  resize, and a clear; a revision that counts only those would be the
  useful half of herdr's rule.
- Every visible match of a search is marked (underlined, the search's own
  case rule); a match wrapped across a row's edge is found but not marked.

### 6. Worktrees — done over the CLI and socket; no overlay yet

Ported (see "Ported, and checked"). Left:

- The new-worktree prompt shows the checkout's path as the branch is typed
  (herdr's create overlay, which lists nothing else), on a client on the
  session's own machine. Opening one is herdr's open overlay: a popup of
  the checkouts two lines each with open/detached/root, a / filter, a
  count, opening… and git's error in place.
- A forced removal that git then refuses brings the space back — name,
  group, tabs, each pane a shell in its directory — rather than herdr's
  restore of the very runtimes it paused: tend has ended those programs by
  then.
- `trust_repository` is ported (`-trust` on `tend worktree`, the parameter
  on `worktree.*`): git runs with `-c safe.directory=<repo>` for that call
  only. Not verified against a repository actually owned by another user,
  which a test cannot make without root.

### 7. Moving and swapping — done over the API; the mouse and a few keys are not

Ported (see "Ported, and checked"). Left:

- Moving a pane into another tab or space is `pane.move` over the socket,
  with no key or menu — as in herdr, which offers it only there too.
  `workspace.move_block` is ported.
- Dragging a tab along the bar and a space down the sidebar are ported, each
  with a mark where it will land. A group of spaces is not dragged as one;
  `workspace.move_block` does that over the socket.
- **The rest of herdr's default keys.** tend's prefix keys are tmux's where
  herdr's differ: herdr detaches on `q` (tend `d`), renames tabs on `shift+t`
  and spaces on `shift+w` (tend `,` and `.`), splits on `v` and `-`, cycles
  panes on `tab`, toggles the sidebar on `b`, reloads the config on `shift+r`.
  Moved to herdr's so far: `H J K L` (swap), `r` (resize mode), `s`
  (settings), `N` (new space), `G` (new worktree), `R` (reload). Still tmux's:
  `d` detach (herdr `q`), `,` and `.` rename (herdr `shift+t`, `shift+w`),
  `|` and `-` split (herdr `v` and `-`), `a` agents (herdr `b` sidebar).

### 8. Navigation extras — done

Ported (see "Ported, and checked"). Left:

- The navigator (`OpenNavigator`, prefix+g) is ported for one machine:
  herdr's popup over the screen with every space, tab and pane, its rules for
  what a query matches and a filter keeps (`navigator_rows`), its keys
  (j/k, ctrl+d/u, / to search, b w i d a to filter, space to open a space,
  enter, esc), its tree drawing, and the mouse (hover marks, click goes, a
  click on a space's caret opens it, a click outside closes). tend's own
  sidebar walk moved from g to w. With saved machines it lists every
  machine, as herdr's `aggregate_navigation.rs` does (see "Saved machines").
  Left: the pane's foreground directory, which tend does not track apart
  from its directory.
- `FocusAgent(index)`: jump to the nth agent. herdr binds it only through
  `[keys.indexed] agents = "<modifier>"` (modifier+1…9, no prefix), which
  tend cannot read: a terminal sends ctrl+digit as the digit, and after the
  prefix 1-9 pick tabs.

### 9. Notifications and sound — done, minus the parts that need assets

Ported (see "Ported, and checked"). Left, and deliberately:

- **Bundled sounds.** herdr ships two mp3s and decodes them itself (`sound.rs`
  is 482 lines mostly for that). tend carries no assets, so a sound is a file
  the user names, else the desktop's sound theme (freedesktop's complete and
  message-new-instant through paplay or pw-play; macOS's Glass and Ping),
  else the bell — `"bell"` asks for it. The theme was put before the bell
  when the bell proved silent in GNOME Terminal, the owner's, which rings it
  as the desktop's alert sound, off by default: a finished agent made no
  sound. Checked on the owner's machine: paplay plays complete.oga.
- Notification cards are ported (herdr's render_notification_card and
  notification_policy): a card in a corner (notify.position, herdr's four,
  bottom-right by default) with a dot for the kind, the title and the body;
  one at a time, 8 s for attention and 5 s otherwise, eight waiting at most,
  newer news of a pane replacing older, a click going to the pane. herdr's
  delay and re-check are ported too (notification_policy): an agent's news
  is held notify.delay (1 s) and said, by every means, only if its pane is
  still blocked or done then; a finished pane still working is looked at
  every 50 ms for a second; a script's word goes at once. (Per-agent sound — `[sound.agents]`, droid muted by default — and
  `open-notification`, herdr's unbound `open_notification_target`, are
  ported.)
- tend keeps a cooldown per pane besides herdr's re-check, written down
  beside the rule: an agent flickering between states within the delay is
  still one notice.

### 10. Settings UI, onboarding, live reload — screen and reload done

Ported (see "Ported, and checked"). Left:

- **Onboarding** is ported (`ui/onboarding.rs`, `render_onboarding_overlay`,
  `complete_onboarding`): while `onboarding` is missing or true, a client
  opens on herdr's welcome (its words, tend's name, this client's prefix and
  help keys), which takes every key and goes on with enter, → or l, or a
  click on continue; that writes `onboarding = false` before every table
  (herdr's `upsert_top_level_bool`) and opens the settings screen at the
  first integration row. `TEND_TEST_SKIP_ONBOARDING` turns it off for the
  tests, as herdr's `HERDR_TEST_*` variables do. Left: release notes
  (`ui/release_notes.rs`) and product announcements, which need something
  published to announce (item 14).
- **Integrations in the screen** are rows after the settings, one per agent
  on this machine, that install, update or remove its hooks; not offered to
  a client attached over ssh, whose machine is not the agents'. herdr's
  update badge is not drawn.
- Live reload of the prefix key is applied, but a client started with one
  prefix keeps any pane input already bound elsewhere; herdr rebinds
  everything through its keybind table (item 11).

### 11. Configurable chrome — keys done, the rest of the chrome is not

Keys are rebindable (see "Ported, and checked"). Left:

- **Sidebar tokens**: a `$name` in a space row shows what
  `workspace.report_metadata` said about the space (herdr's; not persisted,
  as a value about a space is true while its reporter runs).
- **Tab bar**: datetime uses a strftime written for tend covering the
  common directives; herdr's `time` crate takes a few more, and `%z`/`%Z` are
  refused by both.
- A title set with `tend terminal title set` survives a handoff but not a
  restart, as herdr's api_window_title does.
- **Themes**: `[ui.theme.custom]` with herdr's tokens and colour forms, and
  its `light`/`dark` variants under auto_switch, are ported; with no name,
  the overrides go over catppuccin, herdr's default palette. `mauve`,
  `blue`, `peach`, `surface1` and `subtext0` are accepted and drawn by
  nothing, since tend has no element herdr colours with them.
- A key is one byte or one escape sequence after the prefix, so `ctrl+q` and
  `alt+x` cannot be bound: the terminal sends control bytes tend forwards to
  the pane. herdr reads key events with modifiers through crossterm.
- herdr's remaining default keys, listed under item 7.
- **Custom commands**: `popup` is herdr's popup (below), sized by
  `width`/`height`. A key can only be one after the
  prefix; herdr's direct chords (`alt+g` without the prefix) cannot be read,
  for the reason above. `command.invoke` over the socket is not ported: in
  herdr it takes an id from the thin client's command manifest, and a script
  on tend's socket can run the command itself.

- **Popups** are ported (herdr's app/popup.rs and popup_size.rs): one at
  a time, over the tab it was opened from, in no layout, centred, sized in
  cells or percent (half the area by default), with agent detection off;
  it has the keyboard and the mouse while it is up and goes when its
  program ends, on popup.close, or when its tab closes; a handoff closes
  it first. Opened by a user's command of type popup and a plugin pane
  placed as a popup. Different on purpose: the prefix keys still work
  while one is up, since tend reads them before a pane does and a popup
  whose program hangs would otherwise have no way out but the socket.

### 12. Agent session resume — done

Ported (see "Ported, and checked"). Left:

- Nothing known. herdr's `dedupe_key` (two panes in one conversation: the
  first resumes, the other gets a shell) and `codex resume <id>` through
  `agent.start` naming the conversation are both ported.

### 13. Kitty graphics — the path works; the hard parts of herdr's are not here

Ported (see "Ported, and checked"). Left:

- **Unicode placeholders**, animation, `a=q` queries, cropping (`x y w h`) and
  cell-precise offsets: parsed and ignored rather than honoured.
- **Pane layers and client surfaces** (`plugin`-drawn images, `pane.graphics.*`
  over the API, `api/server/pane_graphics_stream.rs`, 1115 lines): a plugin
  cannot draw an image into a pane.
- **Sizing against the cell size**: herdr asks the terminal how big a cell is
  and scales; tend passes on the sender's `c`/`r` and leaves the rest to the
  terminal.
- Images are fetched on the paint goroutine when a pane's revision moves. A
  slow server would show as a slow redraw for that frame; herdr streams them.
- Verified with a stand-in program, not with a real image viewer.

### 14. Updater with channels — done, but for package managers and Windows

Ported (see "Ported, and checked"): the manifest, `tend update`, the
background check, the notice, "update ready", and the release notes.

Publishing a release, which each one needs for the check to see it:

1. Tag it (`git tag -a v0.4.0 -m v0.4.0`) and push the tag.
2. `make dist` — the four binaries and SHA256SUMS, built on the tag.
3. Write the notes in markdown (`### New`, `- item`, `` `code` ``,
   `**bold**`): they are both the GitHub release's text and what the
   notes panel shows.
4. `make manifest NOTES=notes.md` — `dist/latest.json`.
5. `gh release create v0.4.0 --notes-file notes.md dist/*` — latest.json
   with the binaries, or no tend will hear of the release.
6. Put the notes in CHANGELOG.md under `## v0.4.0 — date`, the headings one
   level down and without the install and platforms boilerplate, moving
   what was under "Unreleased" into them.

The repository is `auth-com-br/tend` (the release tooling's default). Build
from a clean checkout of the tag (`git worktree add --detach … v0.4.0`), so
nothing uncommitted reaches a release. `.github/workflows/` can only be
pushed with a token that has the `workflow` scope; over ssh it goes as any
other file.

Left: Homebrew, mise and Nix guidance (`update_install_command`), the
preview channel's own manifest shape (`PreviewManifest`, build ids), the
manifest's `announcement`, and the Windows installer path. Signing is not
done.

### 15. Windows — large, and cannot be run from Linux

- herdr: `platform/windows.rs`, `platform/windows/` (ConPTY, named pipes);
  herdr validates on a Windows VM.
- tend today: Unix only. The pty (`internal/pty/pty_unix.go`), terminal resize
  (`cmd/tend/resize_unix.go`) and server spawn (`cmd/tend/spawn_unix.go`) have no
  Windows counterpart, and the transport is a Unix socket. An agent on
  Linux can get it to cross-compile and no further; say so rather than claiming
  it works.
- What is kept true from here: `GOOS=windows go build ./...` and `GOOS=windows
  go vet ./...` both pass, so whoever starts the port starts from a tree that
  compiles. Tests that need a pty carry `//go:build unix`.

### Smaller gaps in what already exists

- Pointer motion is ported as herdr forwards it: the client turns on
  any-motion reporting (1003) at the outer terminal while a pane in view runs
  a program that asked for it, and forwards each move to the pane under the
  pointer. herdr leaves 1003 on always; tend only while something wants it,
  since each cell crossed is a report. Verified with the real Claude Code,
  which asks for 1000/1002/1003/1006 once past the folder-trust prompt.
- `tend attach -ssh` interactively, and the mismatch notice's restart over
  it, have not been tried against a real sshd yet. Checked against one
  (root@10.8.0.110): `tend ls -ssh`, `tend new -ssh`, and the automation
  commands over `-ssh`, which is where a bug was found and fixed (they read
  the flag and answered from the local session); and `tend --remote host`
  (herdr's launch form) attaching, drawing, a new tab, and detaching, where a
  second was found and fixed (the client sent its own shell, which the host
  did not have).
- Copy mode does not refuse a motion over content that changed underneath, as
  herdr does (`stale_content`).
- herdr keeps the chrome a user arranged — the sidebar's width, the split
  between its lists, the groups folded — in a preferences file
  (`persist_chrome_preferences`); tend keeps them for as long as the client
  runs. The sidebar's width is dragged by its right edge as in herdr
  (`set_sidebar_width_from_column`, 18 to 36, a double click back to
  `[ui] sidebar_width`), and starts at the setting each time.
- The files panel opens in every tab by itself, herdr-sidebar's auto_open
  (`ensure.rs`, run there on tab.created, workspace.created and
  pane.focused): each time the client shows a tab without it, it docks one
  beside the pane in focus and keeps the focus there; a tab where the panel
  was closed — prefix+f, q, or its pane closed — keeps it closed until
  prefix+f opens it there again (herdr-sidebar's snooze, kept by the client
  rather than in files, so a new client starts without them). `[files]
  auto_open`, on by default as there, and on the settings screen and the
  gear. `pane.dock`'s `unless` makes the check and the dock one step on the
  server, so two clients on one tab dock one panel.
- The files panel starts a new build of itself when one is installed
  (`internal/explorer/upgrade.go`): a `tend files` watches its binary on its
  two-second tick and, once a new one has settled and the panel is only
  showing something, execs it in the same pane with the tree's folders, the
  selection and the view carried in `TEND_FILES_STATE`. herdr has no such
  thing to copy; its sidebar is a plugin herdr restarts. `tend view`
  (the preview) does not do it yet.

---

## Lessons already paid for

- **The client draws from terminals another goroutine is writing.** `paint`
  read the panes' screens outside the lock while the reader goroutine wrote
  them, and a resize invalidated the painter from its own goroutine. Under
  load that crashed the client — the gate caught a SIGSEGV once. Drawing now
  happens under the lock and the painter is touched only by the goroutine that
  owns it; the terminal write stays outside, so a slow write never holds a
  pane's output up. A test resizes a real client forty times while it draws,
  with the binary built `-race`: the detector in the test process cannot see
  into the process it starts.

Each of these cost hours. They are here so they cost nobody else any.

- **Read herdr before designing.** See `AGENTS.md`. Eight attempts, then one
  hour.
- **A program that holds the mouse does its own selecting.** Forward the whole
  gesture — press, every drag, release — in the *pane's* coordinates and the
  encoding it asked for. Forwarding the raw report displaces every click by the
  sidebar and the border.
- **A full-screen agent keeps no scrollback in tend.** Its earlier output never
  reaches the terminal; it repaints from its own memory. Nothing client-side
  can select past its window.
- **Clients re-read the session only when told to.** A pane's agent, its state
  and its mouse mode live in the session, not in its output. Anything that
  changes them must publish an event, or every client shows the old answer.
- **A scroll view at offset zero is a frozen picture.** If the server answers a
  scroll request with the present, leave the scroll view.
- **`Fd()` on an `*os.File` is not free**: it detaches the descriptor from the
  runtime poller and races with a reader. Use `SyscallConn().Control`.
- **A timeout is not an answer.** Probing a Unix socket, `ECONNREFUSED` means
  nobody is there; a timeout means you do not know. Deleting a socket on "do
  not know" puts two servers on one session.
- **Staying put is a candidate.** Any "how far did it move" search that leaves
  out zero will find a move in a still picture.
- **The gate has been hollow before.** `make fmt` once called a `gofmt` that was
  not on PATH; empty output from a missing binary read as "already formatted".
