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

## install

```bash
curl -fsSL https://tend.auth.com.br/install.sh | sh
```

On Windows, tend runs inside WSL: [docs/WINDOWS.md](docs/WINDOWS.md) walks
through it. From a checkout: `make install`.

## try it

```bash
tend
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

Run an agent in a pane, as you would in any terminal. tend reads its screen
and marks the pane working, blocked or idle. `ctrl+b d` detaches and leaves
everything running; `tend` again is back where you were.

A **space** holds tabs, a tab holds panes. The mouse works throughout: click,
drag a border, scroll, right-click for a menu.

## keys

`ctrl+b` is the prefix: press it, then the key. `ctrl+b ?` lists every key.

| key | |
|---|---|
| `\|` `-` | split beside, below |
| `h` `j` `k` `l`, arrows | move focus |
| `H` `J` `K` `L` | swap panes |
| `r` | resize (then `h` `j` `k` `l`, esc) |
| `z` | zoom a pane to the window |
| `[` | scroll back through its history |
| `x` | close a pane |
| `c` `n` `p` `1`–`9` | tabs: new, next, previous, by number |
| `N` `(` `)` | spaces: new, previous, next |
| `,` `.` | rename this tab, this space |
| `a` | show every agent, grouped |
| `u` | go to the next agent waiting for you |
| `w` | walk the sidebar from the keyboard |
| `g` | find any space, tab or pane, and jump |
| `m` | the menu |
| `s` | settings |
| `d` | detach, leaving everything running |

## tools

Each is a key after the prefix, and most are a button over the spaces list.
[docs/TOOLS.md](docs/TOOLS.md) has each one in detail.

| key | tool | |
|---|---|---|
| `f` | [Files](docs/TOOLS.md#files) | the project's tree, a search, and git: diff, stage, commit, push |
| `A` | [Agents](docs/TOOLS.md#agents) | the agent CLIs tend knows, which you have, and installing the rest |
| `M` | [MCP servers](docs/TOOLS.md#mcp-servers) | every agent's MCP servers in one grid: on, off, test, add, copy |
| `P` | [Agent permissions](docs/TOOLS.md#agent-permissions) | what each agent may do without asking, and its files in your editor |
| `S` | [Sessions](docs/TOOLS.md#sessions) | your agents' past conversations: search, resume, delete |
| `I` | [Issues](docs/TOOLS.md#issues-and-pull-requests) | GitHub and GitLab issues and pull requests; `w` starts an agent on one |
| `E` | [Errors](docs/TOOLS.md#errors) | what GlitchTip or Sentry caught; hand one to an agent, resolve it |
| `T` | [Tickets](docs/TOOLS.md#tickets) | Linear, Jira, ClickUp: read, comment, hand to an agent, mark done |
| `B` | [Browser](docs/TOOLS.md#browser) | a browser where you pick elements on a page and send them to the agent |
| `C` | [Context](docs/TOOLS.md#context) | what was captured for the agents, to look over and send |
| `O` | [Companies](docs/TOOLS.md#companies) | show only one company's spaces and agents |

And without a key:

| | |
|---|---|
| [Sleeping agents](docs/TOOLS.md#sleeping-agents) | after a restart an agent starts only when you go to it, and an idle one gives its memory back |
| [Your phone](docs/TOOLS.md#your-phone) | **menu → phone view...**, or `tend web`: follow the agents and message them from a phone |
| [Handing work on](docs/TOOLS.md#handing-work-to-another-agent) | **continue in...** on a pane's menu starts another agent where this one stopped |
| [Activity and policies](docs/TOOLS.md#activity-and-policies) | `tend activity`: what agents did and what it cost; `[policy]`: where they may work |

## commands

Everything works against the same server, from anywhere:

```bash
tend new -- claude          # open a pane; prints its id
tend new -split 1 -dir rows -- codex
tend ls                     # what is running
tend kill 2                 # close one pane
tend kill -server           # stop the session
tend --remote host          # a session on another machine, over ssh
tend shortcut               # put tend in your desktop's application launcher
```

The panes belong to the server, not to the command that opened them: every
line above is a separate process, and the agents keep working between them.
`-s NAME` runs more than one session side by side. `tend help` lists the
rest.

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

## updates

tend looks for a newer release in the background and says so: "update ready"
at the right of the status bar. **menu → update ready** shows what is new,
and `u` there installs it without stopping what is running (`tend update
-handoff`). `[update] version_check = false` turns the check off. **about
tend**, first in the menu, says which tend is running.

## privacy

tend collects nothing: no telemetry, no analytics, no account. On its own it
talks to the network once — the release check above, which a setting turns
off — and otherwise only when you ask (a panel, an update, a push to your
phone). [docs/PRIVACY.md](docs/PRIVACY.md) lists every connection and when it
happens, and a test fails if a new one appears.

tend used to live at `github.com/sousaakira/tend`. Old links, clones and
installs follow it here.

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

To debug how an agent's state is read, capture its screen once and iterate
against the file instead of against a live agent:

```bash
bin/tend agents -v                        # the agents tend can detect
bin/tend watch -- claude                  # run one and report its state live
bin/tend watch -capture out.raw -- codex  # ...and keep the raw bytes
bin/tend detect -agent claude out.raw     # replay a capture offline
bin/tend explain -agent claude out.raw    # ...and see how every rule voted
bin/tend screen out.raw                   # ...and what the terminal core made of it
bin/tend follow                           # state changes of a running session
```

`watch` runs one command on a pseudo-terminal, mirrors its output, and prints
state changes to stderr, each with the rule that decided it:

```
[tend] watching claude as "claude" on a 120x40 terminal
[tend] working   osc_title_working
[tend] blocked   bash_permission_prompt
[tend] idle      live_prompt_box
```

`tend serve` runs a session's server in the foreground, which is for watching
it rather than for using it. `make dist` cross-compiles the release binaries
for Linux and macOS.

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
