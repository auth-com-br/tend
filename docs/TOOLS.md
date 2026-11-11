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
- [The mouse and links](#the-mouse-and-links)

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
- When an agent stops to ask you something, its notification says what it
  asks — "Bash command: npm test — Do you want to proceed?" — and where, so
  you know which one can wait.

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

It reaches agents only, with a fixed list of actions — never a shell. It is
plain HTTP: use it on a network you trust, or through a VPN. `[web] listen`
sets the address and port (`0.0.0.0:7777`).

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

## The mouse and links

The mouse works throughout. Click a tab to switch to it, `+` to make one,
click a pane to focus it, drag a border to resize, scroll to look back. In
the agent list, clicking a row goes there — a pane, a tab, or a whole space.
A pane running something that wants the mouse itself, an editor say, gets the
clicks instead. A right-click on a pane has **copy** (when text is selected)
and **paste**.

ctrl+click on a web link a pane printed — a URL, or words a program linked
(OSC 8), as Claude Code does — opens it in the browser on your own machine
(over `--remote` too), unless a plugin's link handler claims it; with ctrl
held, the link under the pointer is underlined.

The sidebar's right edge can be dragged to make it wider or narrower (18 to
36 columns); a double click on it puts it back to `[ui] sidebar_width`.
