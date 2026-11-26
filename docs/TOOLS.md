# tend's tools

Everything tend has beyond panes, one section each. The
[README](../README.md) has the short list; this is the detail. Inside tend,
`?` in any panel lists that panel's keys, and **menu → all tools...** lists
the tools themselves.

Keys written `ctrl+b X` are pressed after the prefix. Most tools are also a
button in the row over the spaces; `ctrl+b w` walks that row too, and
`[ui.toolbar] enabled = false` takes it away.

- [Files](#files) · [Agents](#agents) · [Sleeping agents](#sleeping-agents) ·
  [MCP servers](#mcp-servers) · [Agent permissions](#agent-permissions)
- [Sessions](#sessions) · [Handing work to another agent](#handing-work-to-another-agent) ·
  [Context](#context) · [Browser](#browser)
- [Issues and pull requests](#issues-and-pull-requests) · [Errors](#errors) ·
  [Tickets](#tickets)
- [Companies](#companies) · [The navigator](#the-navigator) ·
  [Your phone](#your-phone) · [Activity and policies](#activity-and-policies)
- [Agent teams](#agent-teams) · [Agents driving agents](#agents-driving-agents) ·
  [Layouts](#layouts) · [The guard](#the-guard)
- [WhatsApp](#whatsapp) · [The mouse and links](#the-mouse-and-links)

## Files

`ctrl+b f` (or "files panel" on a pane's right-click menu) docks it on the
right of the tab (or the left, with `dock = "left"`); again goes to it, and
once more puts it away. It runs on the machine the session is on, so over
`--remote` it shows the server's project, and it follows the pane beside it
to whatever project that pane moves to (`tend files -still` stays put).
`1` `2` `3` switch its views, or with `icons` set, a click on their icons on
the bar at the top, whose gear (or `,`) opens the panel's settings; `?` lists
the keys.

- **files** — the project as a tree, with git's letter beside each changed
  file. Enter opens a file in `$EDITOR` in a tab of its own; space, or a
  click, previews it read-only in a tab of its own with syntax colour —
  markdown rendered (`m` for the source), PNG, JPEG and GIF drawn in the
  terminal (`tend view` is that preview on its own). `s` stages a file or a
  folder; `m` or a right-click is the menu: new file or folder, rename,
  delete (type yes), copy the path, open with the system's app, reveal,
  change folder, a file's history. `/` or `ctrl+p` finds a file by name.
- **search** — `ctrl+f` searches the files' text, with case, whole word,
  regex, and include/exclude globs; Enter opens the editor on the line.
- **changes** — staged and unstaged, each with its diff (opened on a folder
  of projects, each repository's under its name and branch); `s` stages,
  `x x` discards, `c` commits, `A` drafts the message with the local `claude`
  (haiku), or from the file names without it.
- **reviewing an agent's work** — in a diff, `r` writes a comment about the
  file and the line on screen and sends it to the agent in the panel's tab
  (the one the panel follows), submitted, as one line: "Review of
  src/a.go around line 12: …". When the work is committed, in the agent's
  worktree `M M` merges its branch into the branch the main worktree is on
  (with a merge commit; refused with uncommitted work on either side, and a
  conflict is undone, leaving main as it was), and `R R` pushes the branch
  and opens a pull request with `gh`, titled from its commits.
- **git bar** — the line under the header: click the branch (or `B`) to
  switch, `⟳` (or `P`) to sync — pull when behind, fast-forward only, push
  when ahead, publish a new branch to origin — without ever asking for a
  password on the panel. `L` is the history: commits, stashes (apply, pop,
  drop) and tags, each with its diff.

The panel's menu (`m`, or a right-click) also opens the instructions your
agents read — global, the project's, a folder's — and "Share instructions
with every agent" makes one file serve Claude Code and the others
(`CLAUDE.md` reading `AGENTS.md`).

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

In a [Jujutsu](https://jj-vcs.github.io/) repository (a `.jj` folder, beside
a `.git` or not) the panel asks jj instead of git: the changes are the
working-copy commit's (`@`), one group with nothing to stage, since jj has
none; `x` restores a file (`jj restore`), `c` commits (`jj commit`), the
branch picker lists bookmarks (switching makes a change on top of one), and
sync is `jj git fetch` and `jj git push`. A new worktree there is a jj
workspace.

## Agents

`ctrl+b a` opens the agent list down the left: every agent in the session,
grouped by space and tab, with what it is doing. `ctrl+b w` walks it and
Enter jumps — which is the point, since the agent that stopped is rarely the
one you are looking at.

`ctrl+b A` is the agent manager: the agent CLIs tend knows of — Claude Code,
Codex, Gemini CLI, Cursor, OpenCode, Copilot, the DeepSeek Harness and more —
which of them this machine has, with their version, and for the rest the
install command their vendor documents, run in a tab of its own once you have
seen it and said yes.

To start one in a group's folder, right-click the group and choose **new
agent here...**.

### What waits for you

An agent that finishes while you are looking elsewhere is marked ✓ — finished
and unseen — until you go to its tab.

- `ctrl+b u` goes to the **next agent waiting for you**: a blocked one first,
  then the finished and unseen, the one waiting longest first. Press it again
  for the next.
- Glanced at one and want to come back to it later? Right-click its pane or
  its line in the list and choose **mark unread**. The mark stays while you
  look around — even on the agent on screen — and goes when you come to its
  tab from another (or with **mark read**).
- A worker whose every "finished" is noise: right-click it and **mute
  notifications**. It is still shown in the list and on its tab; it raises
  no card, no desktop notification, no sound and no phone push, until you
  unmute it. Kept across restarts.
- With `[notify] toasts = "system"`, clicking the desktop notification takes
  tend to the agent, where the desktop's notify-send can say it was clicked;
  without notify-send (GNOME without libnotify's tools), the notification is
  raised over D-Bus and is not clickable.
- When an agent stops to ask you something, its notification says what it
  asks — "Bash command: npm test — Do you want to proceed?" — and where, so
  you know which one can wait.

### What each agent is spending

For Claude Code and Codex, the sidebar says beside the agent's name how full
its context window is — `claude · ctx 42%` — in the working colour from 80%
and the blocked one from 95%, the numbers each agent's own `/context` or
`/status` shows. Passing 80% and 95% is also a notification, and so is a
Codex agent nearing (90%) or hitting its usage limit, with when it resets.
The phone page shows the same under each agent, with the tokens, the
estimated cost and the limit.

They are read from the file the agent keeps its conversation in, which the
agent's hook names (`tend integration install`), and only while the activity
record is on (`[activity] record`, on unless turned off). The sidebar's
other tokens for it are `spent` (tokens in and out, the prompt cache's
re-reads left out), `cost` (an estimate from public API prices, `≥` when a
model has none) and `limit` (Codex only: Claude Code writes its limit
nowhere tend can read):

```toml
[ui.sidebar.agents]
rows = [["state_icon", "workspace", "tab"], ["agent", "context", "cost"]]
```

A Codex agent stopped by its usage limit is told of when the limit resets.
Right-click it and choose **continue when the limit resets**, and tend also
tells it to carry on then — in the words Claude Code uses on itself, which
continues on its own after a reset and needs nothing from tend. The arming
lasts until the reset, or until the server restarts.

What the agents spent, by company, for the day or the week:

```bash
tend activity -by company -since today
tend activity -by company -since week     # since Monday
```

A space in two companies counts in both; runs recorded before this release
are under "(no company)".

## Sleeping agents

After a restart, agents come back **asleep**: each pane is there, with the
agent's name and `z` beside it, and the agent starts — in the conversation it
was in — the moment you go to the pane. Nothing is started that you did not
go to, so twenty agents kept across projects do not all load at once.

- An agent idle with nobody looking at it goes to sleep by itself after 30
  minutes.
- To put one to sleep yourself: right-click its pane, or its line in the
  agents list, and choose **sleep (free its memory)**; or **menu → sleep an
  agent...** to pick one, or every idle one at once. tend says how much
  memory came back.

```toml
[agents]
sleep_after = "30m"     # "0" never puts an idle agent to sleep
sleep_restored = true   # false starts every agent again after a restart
```

## MCP servers

`ctrl+b M` (or **menu → MCP servers**) shows the MCP servers of Claude Code,
Codex, Gemini CLI, Cursor, OpenCode and the DeepSeek Harness as one grid: a
row per server, a column per agent, ✓ on, ○ off, · not set up. Switch a
server on or off in any agent, copy it to another agent, **test it** (tend
talks to it and says whether it answers, how fast, and how many tools it has
— or why not), add one from a single line, and remove one. Right-click a
server for every action in words.

Each agent's file is edited safely (its own lock, a backup a day) and secrets
never leave the machine the agents run on. `tend mcp list` and
`tend mcp test` do the same from the command line.

## Agent permissions

`ctrl+b P` (or **menu → agent permissions**) shows what each agent may do
without asking: the permission rules of Claude Code, Codex, Cursor, Gemini
CLI, OpenCode and the DeepSeek Harness, a tab an agent, for your whole
machine and for the project of the pane you are in.

Change a rule between allow, ask and deny, move it between files, add one,
remove one, and set the agent's modes. Any of the files opens in an editor
chosen as you open it: nano, vim and the like open beside your pane, and the
panel comes back when you close them; gedit, VS Code and the like open in a
window.

```toml
[permissions]
editor = "nano"          # the one offered first; else $VISUAL, $EDITOR, nano

[[permissions.files]]    # an agent tend does not know: listed and opened
agent = "Kimi"
path = "~/.kimi/config.toml"
```

`tend perm list` prints it all.

## Sessions

`ctrl+b S` lists the conversations your agents keep on this machine (Claude
Code's so far): type to search them, Enter to resume one in a new tab where
it was held, and mark old ones to delete them. One open in a pane is never
deleted.

## Handing work to another agent

To hand a task on — Codex after Claude Code, or a fresh Claude — right-click
the agent's pane (or its row in the sidebar) and choose **continue in...**,
or press `ctrl+g` on a conversation in the sessions list. tend starts that
agent where the work was, and puts a note of the task in the context — what
was asked, the files changed, where it stopped, the uncommitted work — for
you to look over and send.

## Context

`ctrl+b C` is what tools have captured for the agents — a page, an element
picked in one, text, a file. Put things there with `tend context add`, with
"Add to context" on a file in the files panel, or from any tool over the
automation socket (`context.add`). Then copy one, or send it — or all of it,
`S` — to the agent this tab works with, where it is typed in for you to read
over and send.

## Browser

`ctrl+b B` asks for a page — the last five opened are listed under the field
— and opens it in tend's browser: Chromium (or Edge) in a profile of the
session's own, with tend's extension already in it. Its icon, or Alt+Shift+T,
lets you pick elements on the page — outlined as you point, numbered as you
take them — and write a note on each. A chat button in the corner holds them
all, to copy at once or send to tend one by one or together, with a message
over them all; the context panel opens with them, to look over and send on to
the agent. Nothing to install; your own browser is left alone. More in
[BROWSER.md](BROWSER.md).

## Issues and pull requests

`ctrl+b I` lists the GitHub issues of the project you are in, through `gh`:
pick a filter with tab, type to search (GitHub's syntax works: `label:bug`),
Enter to read one with its comments. In a folder of several repositories it
lists them all, with a repository column, and `ctrl+t` narrows it to one.

| on an issue | |
|---|---|
| `w` | start work: a worktree on `issue-<n>-…` and your agent in it, told to complete the issue |
| `c` `x` | comment, close (`ctrl+x` closes from the list) |
| `e` `l` `a` | change its title, labels, assignees |
| `p` | open its first pull request |
| `ctrl+n` | file a new issue |
| `→` | the pull requests, with their checks and reviews; open one to merge it (`m`), comment, close it or mark a draft ready |

A project on a GitLab — gitlab.com or your company's — works the same, with
its issues and merge requests: public projects are read without signing in;
`tend gitlab login [host]` keeps a token (in the system keyring when there is
one) for writing and for private projects.

## Errors

`ctrl+b E` shows what your systems' GlitchTip or Sentry caught. Connect the
server (for Sentry, `https://sentry.io`) and an API token from the panel — as
many servers as you have: the gear (or `ctrl+k`) adds and removes them, and
`ctrl+g` or a click on the server chips moves between them. `ctrl+l` (or
"link … here" on the title) ties the project you are in to the server and
project shown, so the panel opens there from then on.

| on an error | |
|---|---|
| Enter | read its stack |
| `f` | hand it to the agent you are working with |
| `w` | fix it in a worktree of its own |
| `r` | resolve it (`ctrl+x` resolves from the list) |

## Tickets

`ctrl+b T` is the same for Linear, Jira, ClickUp and Auth's Issue Gateway
(issue.auth.com.br): connect an account with a token from it (the gear keeps
several), and the open tickets, yours and the done are listed. Open one to
read it with its comments; `c` comments, `x` marks it done in the tracker,
`f` hands it to your agent, `w` works on it in a worktree, and `ctrl+l` makes
the project you are in open on that account.

## Companies

The mark at the right of the "spaces" heading (or `ctrl+b O`) is for
companies: make one for each company you work for, tick which spaces it
holds, and choose it — the sidebar then lists that company's spaces and their
agents only, and a space you make goes into it. "all spaces" brings the rest
back; a `!` beside the mark says an agent in a hidden space is waiting.
Choosing one changes only what you see: every space keeps running. Going back
to a company takes you to the space you were last in there, not to its first.

Spaces can be kept in named groups (**new group...** on a space's menu). A
group stays until you delete it, can have a folder its new spaces start in,
and its menu moves it, with its spaces, to another company.

## The navigator

`ctrl+b g` is every space, tab and pane in one list over the screen, each
pane with its agent's state. `/` searches by name or directory, `b` `w` `i`
`d` keep only blocked, working, idle or done agents (`a` shows all again),
space opens a space, Enter goes there.

## Your phone

`tend web` serves a page for a phone on your home network or your VPN: the
agents of every company with what each is doing, one agent's screen live, a
box to send it a message, the keys an agent asks for (Enter, Esc, 1–9,
arrows, Tab), and wake and sleep. It buzzes when an agent needs you or
finishes.

```bash
tend web                  # prints the address to open on the phone
tend web -set-password    # a login and password, instead of that address
tend web -new-token       # unpair every phone paired before
```

Or from inside tend: **menu → phone view...** turns it on and off, and shows
the addresses to open on the phone — a click copies one. tend's server starts
it and keeps it: turned on, it comes back when the server restarts, after a
reboot or an update, until it is turned off again.

It reaches agents only, with a fixed list of actions — never a shell.
`[web] listen` sets the address and port (`0.0.0.0:7777`).

### Notifications with the page closed

A phone can be told when an agent needs you even with the page closed — the
agent, where it is and what it asks ("Bash command: npm test — Do you want to
proceed?"), and a tap opens it. Browsers allow that only on a secure page,
so it needs HTTPS:

```toml
[web]
https = true
# cert = "~/certs/box.crt"   # a certificate of your own (a VPN's), or none:
# key = "~/certs/box.key"    # tend signs one with an authority of its own
```

Without a certificate of your own, tend makes an authority of its own the
first time and signs the page's certificate with it. That authority is
limited to private networks and local names: on a phone it vouches for this
machine and for nothing on the internet. `tend web` prints how to install it
on the phone, once: open `https://<address>/ca.crt`, accept the warning that
one time, and install it as a CA certificate (Android: Settings › Security ›
Encryption & credentials › Install a certificate; iPhone: install the
profile, then turn it on in General › About › Certificate Trust Settings).

Then tap the bell 🔕 at the top of the page and allow notifications. It tells
when an agent becomes blocked, and when one finishes while nobody is looking
at it. The notification travels through your phone's push service (Google's
or Apple's), encrypted for your phone alone: the service carries it without
being able to read it. Tap the bell again to stop.

Without HTTPS the page is plain HTTP: use it on a network you trust, or
through a VPN, which encrypts it anyway.

## Activity and policies

The session server keeps a record of every agent: when it started and
stopped, in which project and on which branch, what the branch says it was
for (an issue, an error, a ticket), which files changed in its checkout while
it ran, and — for Claude Code and Codex — the tokens its conversation spent
and what that is likely to have cost. `tend activity` reads it, with no
server needed:

```bash
tend activity                          # every run, newest first
tend activity -project . -since 7d     # this project, this week
tend activity -by project              # totals per project (or -by agent, -by company)
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

## Agent teams

```bash
tend claude-teams            # in a tend pane; every argument goes to claude
```

starts Claude Code with its agent teams on, and each teammate it starts opens
as a tend pane beside it — named, its state shown, closed when the team is.
Claude Code puts teammates in panes only under tmux; tend gives that one
Claude Code a `tmux` of its own to talk to, and leaves the other panes and
any real tmux alone.

## Agents driving agents

```bash
claude mcp add tend -- tend mcp serve
```

gives an agent tend's own MCP server: tools to list the session's agents,
start one in a tab of its own space (`start_agent`, with a first task),
message one and wait for its answer (`send_message` with `wait`), read its
screen, and show it to you. The session's policies and companies hold, as
they do for anything else that starts an agent.

## Tasks

The session keeps a list of the work ahead — tasks with a title, notes, a
due date and a status (todo, doing, done) — which every client sees: the
desktop window's planner, a script, an agent.

```bash
tend task add -due 2026-11-30 write the report for Acme
tend task add -space w_2 -agent claude -at 09:00 -prompt "review yesterday's PRs" morning review
tend task list            # the ones not done; -all for every one, -json for a script
tend task show 2
tend task done 1
tend task rm 1
```

A task with an agent and a time is started by the server at that time,
attached or not: a new tab in the task's space (or a space named after the
task, when it has none), in that space's folder, the agent typed in as
`agent.start` types one — so the policies decide whether it may start — and
the prompt given once the agent is ready. The task then records the pane
and is "doing"; a start that fails says why on the task and in a
notification. A time that passed while the server was down is started when
it comes back, once. Times are `2026-11-27 09:00`, a date, `09:00` (the next
one), `+30m`, or RFC 3339.

The same tasks are `task.list`, `task.create`, `task.update` and
`task.delete` on the automation socket, and `list_tasks`, `add_task` and
`complete_task` in tend's own MCP server (`tend mcp serve`), so an agent can
keep the plan too.

## Layouts

`tend layout open work` builds a whole setup from `layouts/work.toml` beside
the settings file — spaces, their tabs, how each tab is split, and what each
pane runs:

```toml
[[space]]
name = "api"
dir = "~/code/api"

  [[space.tab]]
  name = "work"
    [[space.tab.pane]]
    agent = "claude"        # started as tend starts agents
    size = 0.6
    [[space.tab.pane]]
    split = "rows"          # one above the other; "columns" side by side
      [[space.tab.pane.pane]]
      run = "npm run dev"   # typed into the pane's shell
      [[space.tab.pane.pane]]
      dir = "server"        # under the space's
```

`tend layout save` and `tend layout apply` keep and rebuild a single tab as it
is. `ctrl+b y` types into every pane of the tab at once (SYNC on the status
bar) until pressed again.

In `[ui]`: `sidebar_side = "right"` puts the sidebar on the right,
`tab_bar_position = "bottom"` the tabs at the bottom, and
`dim_unfocused = true` draws the panes you are not typing into faint.

## The guard

Agents run without asking — Claude Code's `--dangerously-skip-permissions`,
the auto modes — sometimes delete what they should not. With

```toml
[policy]
guard = "ask"     # or "deny"; "off" by default
```

and `tend guard install` once (a hook in Claude Code's settings), a shell
command that would destroy work outside the agent's — `rm -r` outside the
project, a force push, `git reset --hard`, `DROP DATABASE`, a Docker volume, a
disk — is put to you, or refused with the reason. `guard_deny` and
`guard_allow` take patterns of your own; `tend guard check '<command>'` says
what it would do; `tend activity -violations` lists what it stopped. It
catches the common accident, not everything: a command an agent builds at run
time is not seen through.

## WhatsApp

The agents can read your WhatsApp conversations and draft replies to your
customers; only you send them. It is `tend-whatsapp`, a program of its own
beside tend (its dependencies would have made tend 16 MB heavier), built
from the repository with `make whatsapp-install`; `tend whatsapp …` runs it.

**Read this before linking.** tend links your number as a companion
device, as WhatsApp Web does, through [whatsmeow](https://github.com/tulir/whatsmeow),
which WhatsApp did not write and does not allow. It is against WhatsApp's
terms, and **the number can be banned** for it. The conversations are your
customers' personal data: under the LGPD, handing them to AI agents (which
send what they read to their providers) is processing you answer for. `tend
whatsapp link` says all of this and asks before pairing.

```sh
tend whatsapp link              # QR code in the terminal; scan it from WhatsApp → Linked devices
tend whatsapp link -phone 5511… # or a code to type on the phone instead
tend whatsapp status
claude mcp add whatsapp -- tend whatsapp serve    # the tools, for an agent
tend whatsapp pending           # replies the agents proposed
tend whatsapp approve 3         # shows it, asks you to type "send", sends it
tend whatsapp reject 3 [why]
tend whatsapp unlink [-forget]  # -forget also erases what is kept here
```

The agent's tools are `list_chats`, `read_chat` (a chat by name, phone
number or JID; `before` pages back), `search` (accents and case aside) and
`propose_reply` (with an optional message to quote). There is no tool that
sends: a proposal waits, and the tend session is notified ("a reply to Maria
waits for you"). `approve` asks at a terminal, which an agent's shell tool
does not have; that is a bar, not a lock — anything running as you can reach
the daemon's socket, as it can tend's.

A daemon holds the one connection (a number linked once has one) and starts
on demand; `tend whatsapp stop` stops it. It keeps the messages it receives,
live and from the history the phone sends after linking, in
`~/.local/state/tend/whatsapp/` (0600, in a 0700 directory), and nowhere
else; media is not downloaded, only noted (`[image] caption`). Its socket,
`$XDG_RUNTIME_DIR/tend/whatsapp/daemon.sock`, speaks one JSON object per
line (the methods are listed in `whatsapp/internal/daemon/protocol.go`), for
a desktop panel to come.

## The mouse and links

The mouse works throughout. Click a tab to switch to it, `+` to make one,
click a pane to focus it, drag a border to resize, scroll to look back. In
the agent list, clicking a row goes there — a pane, a tab, or a whole space.
A pane running something that wants the mouse itself, an editor say, gets the
clicks instead. A right-click on a pane has **copy** (when text is selected)
and **paste**.

What you select is copied, and on Linux it also goes to the primary
selection, so a middle click pastes it anywhere, as in every other program
(`[ui] copy_to_primary = false` to stop it).

Copy mode's keys are vi's out of the box; `[keys.copy_mode]` gives any of
its actions keys of your own (`left = "h left"`), and `tend keys` lists them
all.

The tab bar can say each tab's number before the name you gave it
(`[ui] tab_numbers = true`: "2 build") and, before that, the state of its
agent — ◐ working, × waiting for you, ✓ finished unseen — which it does
whenever the sidebar is hidden (`tab_status = "auto"`; `"on"`, `"off"`).

ctrl+click on a web link a pane printed — a URL, or words a program linked
(OSC 8), as Claude Code does — opens it in the browser on your own machine
(over `--remote` too), unless a plugin's link handler claims it; with ctrl
held, the link under the pointer is underlined.

The sidebar's right edge can be dragged to make it wider or narrower (18 to
36 columns); a double click on it puts it back to `[ui] sidebar_width`.
