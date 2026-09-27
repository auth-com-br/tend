# tend

From the ticket and the production error to the pull request, with agents,
without leaving the terminal.

tend takes a GitHub issue or a GlitchTip error, opens a worktree for it and
hands it to Claude Code, Codex or whichever agent you use. You follow every
agent on one screen — each pane marked working, blocked or idle — and close
the issue or resolve the error when it is fixed, from the same place.

Under it is a terminal runtime built for agents: a background server that
keeps them running when you detach, spaces and tabs for your projects, and a
socket API so agents can spawn panes and wait on each other.

Written in Go, single binary, no cgo. Made by
[Auth Tecnologia Ltda](https://auth.com.br), Belo Horizonte, Brazil —
[tend.auth.com.br](https://tend.auth.com.br).

> Status: usable. Panes are drawn, keys and the mouse reach them, text reflows
> when you resize, scrollback is readable, the client reconnects by itself, and
> detaching leaves everything running.

## try it

```bash
make build

bin/tend agents -v              # the agents tend can detect
bin/tend watch -- claude        # run an agent and report its state live
bin/tend watch -capture out.raw -- codex
bin/tend detect -agent claude out.raw    # replay a capture offline
bin/tend explain -agent claude out.raw   # ...and see how every rule voted
bin/tend screen out.raw                  # ...and what the terminal core made of it

bin/tend session -- claude -- codex      # several panes in one process
```

against a running server:

```bash
bin/tend
```

That is the whole thing. It opens a session, starting a server for it if there
is not one already, and draws it.

```
┌ 1 claude ●──────────────────────────┐┌ 2 codex ▲───────────────────────────┐
│⠋ Pondering the refactor…            ││ Do you want to proceed?             │
│                                     ││  ❯ 1. Yes                           │
│                                     ││    2. No                            │
└─────────────────────────────────────┘└─────────────────────────────────────┘
 work · main · agents   1:claude ●  2:codex ▲
```

`ctrl+b` is the prefix and `ctrl+b ?` lists every key.

| | |
|---|---|
| `\|` `-` | split beside, below |
| `hjkl` arrows | move focus |
| `HJKL` | resize |
| `z` | zoom a pane to the window |
| `[` | scroll back through its history |
| `x` | close a pane |
| `c` `n` `p` `1-9` | tabs: new, next, previous, by number |
| `s` `(` `)` | spaces: new, previous, next |
| `a` | show every agent, grouped |
| `g` | find any space, tab or pane: search, filter by state, jump |
| `w` | walk the agent list from the keyboard |
| `,` `.` | rename this tab, this space |
| `f` | the files panel: the project, git, a diff, a search |
| `d` | detach, leaving everything running |

A **space** holds tabs, a tab holds panes. Tabs belong to their space, so
`ctrl+b n` stays where you are and `ctrl+b )` is how you leave.

`ctrl+b a` opens the agent list down the left: every agent in the session,
grouped by space and tab, with what it is doing. `ctrl+b w` walks it and Enter
jumps — which is the point, since the agent that stopped is rarely the one you
are looking at.

Over the spaces is a row of tools. Files opens and closes the files panel,
as `ctrl+b f` does. Agents (or `ctrl+b A`) is the agent manager: the agent
CLIs tend knows of — Claude Code, Codex, Gemini CLI, OpenCode, Copilot and
more — which of them this machine has, with their version, and for the rest
the install command their vendor documents, run in a tab of its own once
you have seen it and said yes. Context (or `ctrl+b C`) is what
tools have captured for the agents — a page, an element picked in one, text,
a file: put things there with `tend context add`, with "Add to context" on a
file in the files panel, or from any tool over the automation socket
(`context.add`); then copy one, or send it — or all of it, `S` — to the
agent this tab works with, where it is typed in for you to read over and send. Sessions (or `ctrl+b S`) lists the
conversations your agents keep on this machine (Claude Code's so far): type to
search them, enter to resume one in a new tab where it was held, and mark old
ones to delete them. Tickets (or `ctrl+b T`) is the same for Linear, Jira, ClickUp and a
self-hosted Issue Gateway: connect an account with a token from it (the gear
keeps several), and the open tickets, yours and the done are listed; open one to read it with its comments, `c` to
comment, `x` to mark it done in the tracker, `f` to hand it to your agent,
`w` to work on it in a worktree, and `ctrl+l` to make the project you are in
open on that account.

The files panel's menu (`m`, or a right-click) opens the instructions your
agents read — global, the project's, a folder's — and "Share instructions
with every agent" makes one file serve Claude Code and the others
(`CLAUDE.md` reading `AGENTS.md`). To hand a task to another agent — Codex after Claude
Code, or a fresh Claude — right-click the agent's pane (or its row in the
sidebar) and choose "continue in...", or press `ctrl+g` on a conversation
in the list: tend starts that agent where the work was, and puts a note of
the task in the context — what was asked, the files changed, where it
stopped, the uncommitted work — for you to look over and send it. Issues (or `ctrl+b I`) lists the GitHub issues of the
project you are in, through `gh`: pick a filter with tab, type to search
(GitHub's syntax works: `label:bug`), enter to read one with its comments.
In a folder of several repositories, it lists them all, with a repository
column, and `ctrl+t` narrows it to one. On an issue, `c` comments, `x` closes it, `e`, `l` and `a` change its title,
labels and assignees, and `w` starts work on it — a worktree on
`issue-<n>-…` and your agent in it, told to complete the issue. `ctrl+n` files
a new one. `→` shows the pull requests, with their checks and reviews;
open one to merge it (`m`), comment, close it or mark a draft ready. An issue
lists its pull requests, and `p` opens the first. A project on a GitLab —
gitlab.com or your company's — works the same, with its issues and merge
requests: public projects are read without signing in; `tend gitlab login
[host]` keeps a token (in the system keyring when there is one) for writing
and for private projects.
Errors (or `ctrl+b E`) shows what your systems' GlitchTip or Sentry caught: connect the
server (for Sentry, `https://sentry.io`) and an API token from the panel — as many servers as you have: the gear
(or `ctrl+k`) adds and removes them, and `ctrl+g` or a click on the server
chips moves between them; `ctrl+l` (or "link … here" on the title) ties the
project you are in to the server and project shown, so the panel opens
there from then on — then open an error to see its stack,
`f` to hand it to the agent you are working with, `w` to fix it in a worktree
of its own, and `r` to resolve it — or resolve it from the list with `ctrl+x`,
without opening it, as `ctrl+x` closes a GitHub issue from its list. Browser (or `ctrl+b B`) asks for a page — the last five opened are listed
under the field, a click or the arrows away — and opens it in tend's
browser: Chromium (or Edge) in a profile of the session's own, with tend's
extension already in it. Its icon, or Alt+Shift+T, lets you pick elements on
the page — outlined as you point, numbered as you take them — and write a
note on each; a chat button in the corner holds them all, to copy at once or
send to tend one by one or together, with a message over them all; the
context panel opens with them, to look over and send on to the agent. Nothing to install; your own browser is left alone
(`docs/BROWSER.md`). `ctrl+b w` walks them too, and `[ui.toolbar] enabled =
false` takes the row away.

The mark at the right of the "spaces" heading (or `ctrl+b O`) is for
companies: make one for each company you work for, tick which spaces it
holds, and choose it — the sidebar then lists that company's spaces and
their agents only, and a space you make goes into it. "all spaces" brings
the rest back; a `!` beside the mark says an agent in a hidden space is
waiting. Choosing one changes only what you see: every space keeps running.

The sidebar's right edge can be dragged to make it wider or narrower (18 to
36 columns); a double click on it puts it back to `[ui] sidebar_width`.

### the files panel

`ctrl+b f` (or "files panel" on a pane's right-click menu) docks it on the
right of the tab (or the left, with `dock = "left"`); again goes to it, and once more puts it away. It runs on the
machine the session is on, so over `--remote` it shows the server's project,
and it follows the pane beside it to whatever project that pane moves to
(`tend files -still` stays put). `1` `2` `3` switch its views, or with
`icons` set, a click on their icons on the bar at the top, whose gear (or `,`)
opens the panel's settings; `?` lists the keys.

- **files** — the project as a tree, with git's letter beside each changed
  file. Enter opens a file in `$EDITOR` in a tab of its own; space, or a
  click, previews it read-only in a tab of its own with syntax colour —
  markdown rendered (`m` for the source), PNG, JPEG and GIF drawn in the
  terminal (`tend view` is that preview on its own). `s` stages a file or a folder;
  `m` or a right-click is the menu: new file or folder, rename, delete (type
  yes), copy the path, open with the system's app, reveal, change folder, a
  file's history. `/` or `ctrl+p` finds a file by name.
- **search** — `ctrl+f` searches the files' text, with case, whole word,
  regex, and include/exclude globs; Enter opens the editor on the line.
- **changes** — staged and unstaged, each with its diff (opened on a folder
  of projects, each repository's under its name and branch); `s` stages, `x x`
  discards, `c` commits, `A` drafts the message with the local `claude`
  (haiku), or from the file names without it.
- **git bar** — the line under the header: click the branch (or `B`) to
  switch, `⟳` (or `P`) to sync — pull when behind, fast-forward only, push
  when ahead, publish a new branch to origin — without ever asking for a
  password on the panel. `L` is the history: commits, stashes (apply, pop,
  drop) and tags, each with its diff.

Its settings are `[files]` in the settings file, and on the settings screen:

```toml
[files]
icons = "nerd"   # "none" (default), "nerd" for a Nerd Font, or "emoji"
width = 32       # columns it opens at
dock = "right"   # or "left"
follow = true    # follow the pane beside it
auto_open = true # open in every tab by itself; false: only on prefix+f
hidden = false   # hide dotfiles
```

`ctrl+b g` is the navigator: every space, tab and pane in one list over the
screen, each pane with its agent's state. `/` searches by name or directory,
`b` `w` `i` `d` keep only blocked, working, idle or done agents (`a` shows all
again), space opens a space, Enter goes there.

ctrl+click on a web link a pane printed — a URL, or words a program linked
(OSC 8), as Claude Code does — opens it in the browser on your own machine
(over `--remote` too), unless a plugin's link handler claims it;
with ctrl held, the link under the pointer is underlined.

The mouse works throughout. Click a tab to switch to it, `+` to make one,
click a pane to focus it, drag a border to resize, scroll to look back. In the
agent list, clicking a row goes there — a pane, a tab, or a whole space. A pane
running something that wants the mouse itself, an editor say, gets the clicks
instead.

### what agents did, and rules for them

The session server keeps a record of every agent: when it started and
stopped, in which project and on which branch, what the branch says it was
for (an issue, an error, a ticket), which files changed in its checkout while
it ran, and — for Claude Code and Codex — the tokens its conversation spent
and what that is likely to have cost. `tend activity` reads it, with no
server needed:

```bash
tend activity                          # every run, newest first
tend activity -project . -since 7d     # this project, this week
tend activity -by project              # totals per project (or -by agent)
tend activity -prs                     # and the pull request each branch became
tend activity -format csv -o runs.csv  # or -format json
tend activity -violations              # agents that broke a policy
```

Costs are estimates from the models' public API prices; what a subscription
or a cloud provider charges is not that, and `[activity.prices]` replaces
them. The record is `activity.jsonl` in tend's state directory, readable by
you alone; `[activity] record = false` turns it off.

Rules for agents go in `[policy]`:

```toml
[policy]
protected_branches = ["main", "master"]  # agents do not work on these
require_worktree = true                  # only in a worktree of a repository
max_agents = 4                           # at once, in a session
```

tend refuses to start an agent that would break one — from the issues,
errors and tickets panels, `tend new`, or the automation socket — and says
why. One started by hand in a shell is not stopped: tend says so, and writes
it down for `tend activity -violations`.

## settings

Optional, at `~/.config/tend/config.toml`. `tend config -init` writes a
commented copy of the defaults; `tend config` says where it is and whether it
loads.

```toml
[keys]
prefix = "ctrl+a"

[pane]
shell = ["/bin/zsh"]
scrollback = 20000

[ui]
mouse = true

[ui.theme]
border_focused = "blue"
blocked = "#ff5f5f"
```

A misspelled setting is an error rather than being ignored, since a setting
quietly dropped is one you believe is in effect.

The rest of the commands work against the same server, from anywhere:

```bash
bin/tend new -- claude          # open a pane; prints its id
bin/tend new -split 1 -dir rows -- codex
bin/tend ls                     # what is running
bin/tend follow                 # watch state changes, for diagnosis
bin/tend kill 2                 # close one pane
bin/tend kill -server           # stop the session
```

`attach` and `new` start a server when there is none; everything else reports
a missing session instead, since creating one on the way to listing it would
report an empty session rather than the absence of one. `tend serve` runs a
server in the foreground, which is for watching it rather than for using it.

The panes belong to the server, not to the command that opened them: every
line above is a separate process, and the agents keep working between them.
`-s NAME` runs more than one session side by side.

`watch` runs one command on a pseudo-terminal, mirrors its output, and prints
state changes to stderr:

```
[tend] watching claude as "claude" on a 120x40 terminal
[tend] working   osc_title_working
[tend] blocked   bash_permission_prompt
[tend] idle      live_prompt_box
```

`-capture` writes the raw bytes, which `detect`, `explain` and `screen` replay
offline. That loop is how a detection rule gets debugged: capture the state
once, then iterate against the file instead of against a live agent.

`session` runs each command as a pane in one server and reports their state as
it changes. It does not draw the panes — that is the client's job, and the
client does not exist yet — but it is the multiplexer underneath:

```
PANE  COMMAND  AGENT   STATE    TITLE
1     claude   claude  working  my-project
2     codex    codex   blocked  ~/proj
3     htop     -       -        -
```

## install

```bash
curl -fsSL https://tend.auth.com.br/install.sh | sh
```

On Windows, tend runs inside WSL: [docs/WINDOWS.md](docs/WINDOWS.md) walks
through it.

tend used to live at `github.com/sousaakira/tend`. Old links, clones and
installs follow it here: GitHub redirects the repository, and an installed
tend keeps finding its updates.

tend looks for a newer release in the background and says so: a notice, and
"update ready" at the right of the status bar. The sidebar's "menu" → update
ready shows what is new in it, and `u` there installs it (`tend update
-handoff`, in a tab of its own) without stopping what is running; the sidebar's "menu" has what
is new in it. `[update] version_check = false` turns the check off. "about tend",
first in that menu, says which tend is running, who makes it, where it lives
and how to sponsor it — and the server's version when an update left the server behind,
with the `tend handoff` that brings it along (the item is dotted then).
`ctrl+b ?` has the version at its top too.

Or from a checkout:

```bash
make install     # into GOBIN, or ~/.local/bin
make dist        # cross-compiled binaries for linux and macOS
```

## development

```bash
make dev      # fast loop: cached, fail-fast, no vet (~50ms warm)
make watch    # re-run `make dev` on every .go change
make check    # gofmt + vet + full tests — run before committing
make bench    # parser hot-path benchmarks; internal/vt must stay 0 allocs/op
```

`make dev` narrows to `./internal/...` and turns off the vet pass that
`go test` runs by default, which is where most of the time goes. `make check`
puts both back.

Requires Go 1.27+.

## architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## working on tend

tend is a port of herdr to Go, and the porting is ongoing.

- [CONTRIBUTING.md](CONTRIBUTING.md) — setting up, the gate, tests, commits
  and pull requests. Start here.
- [AGENTS.md](AGENTS.md) — how to work here: the rules, the gate, the traps.
  Written for AI agents; it is the right first read for anyone.
- [docs/PORTING.md](docs/PORTING.md) — what has been ported, what has not, and
  where each missing piece lives in herdr's source. The work queue.

## license

Apache-2.0, © Auth Tecnologia Ltda. Security problems go to
seguranca@auth.com.br ([SECURITY.md](SECURITY.md)); contributions are signed
off ([CONTRIBUTING.md](CONTRIBUTING.md)). tend is an independent Go implementation of the architecture of
[herdr](https://github.com/herdrdev/herdr); see [NOTICE](NOTICE) for attribution.

If tend is useful to you, you can support it on
[GitHub Sponsors](https://github.com/sponsors/sousaakira).
