# Changelog

What changed in each release of tend, newest first. The same text is each
release's notes on GitHub and what "what's new" shows inside tend.

## Unreleased

## v0.9.0 — 2026-09-27

#### New
- **Tickets: Linear, Jira and ClickUp** (`ctrl+b T`, or 🎫 on the toolbar). Connect an account with a token from it — tested before it is kept, in your system keyring when there is one — and see the open tickets, yours and the done, with a search. Open one to read it with its comments; `c` comments, `x` marks it done in the tracker (Jira through its workflow, Linear to the team's completed state, ClickUp to the list's closed status), `f` hands it to your agent, `w` starts your agent on it in a worktree. Keep several accounts, and `ctrl+l` makes the project you are in open on its own.
- **Sponsor tend.** A way to support the project, in the README and in "about tend".

#### Fixed
- **tend's browser in Chromium and Edge** loaded its extension twice, and a page sent to it right away could be lost. It loads once now.

## v0.8.0 — 2026-09-26

#### New
- **Hand a task to another agent.** Right-click an agent's pane (or its row in the sidebar) and choose **continue in…**, or press `ctrl+g` on a conversation in Sessions: tend starts Codex, Claude, Cursor or Gemini where the work was, and puts a note of the task in the context — what was asked, the files changed, where it stopped, the uncommitted work — for you to look over and send. No model writes the note; it is read from the conversation.
- **GitLab.** The issues panel works for projects on gitlab.com or your company's GitLab: issues and merge requests, comments, closing, labels, assignees, merging, and starting work in a worktree. Public projects are read without signing in; `tend gitlab login` keeps a token for the rest.
- **Sentry.** The errors panel connects to Sentry (`https://sentry.io`) as it does to GlitchTip.
- **Memory tools.** The agent manager (`ctrl+b A`) offers ai-memory and Graphify under MEMORY, each with its documented install, and says when ai-memory is running.
- **Your agents' instructions.** The files panel's menu opens the global, project and folder instructions, and "Share instructions with every agent" makes `CLAUDE.md` read `AGENTS.md`, so one file serves them all.
- **Tokens in the system keyring.** A GlitchTip or GitLab token goes to GNOME Keyring or KWallet (through `secret-tool`) or the macOS Keychain when there is one, and the settings file only names it.

#### Also
- `make stats` shows how tend is taken up, from what GitHub shows publicly.
- Windows: a guide to running tend in WSL (docs/WINDOWS.md).

## v0.7.2 — 2026-09-26

#### New
- **Several GlitchTip servers.** The errors panel keeps as many servers as you report to. The gear (or `ctrl+k`) opens the servers box: add one, show one, or remove one — its token goes with it. With more than one, a line of server chips and `ctrl+g` move between them, and the last one shown is remembered.
- **Each project opens on its own errors.** In the errors panel, `ctrl+l` (or "link … here" on the title) ties the project you are in to the server and project shown. From then on, the panel opened anywhere in that project — a worktree of it too — shows those errors first, whatever you looked at last. `ctrl+l` again unties it.

#### Fixed
- **The installer checks what it downloads.** `curl … | sh` now compares the binary with the release's `SHA256SUMS`, as `tend update` always has, and installs nothing when they differ.

## v0.7.1 — 2026-09-25

#### tend has a new home
tend is now made by **Auth Tecnologia Ltda** (Belo Horizonte, Brazil) and lives at **[github.com/auth-com-br/tend](https://github.com/auth-com-br/tend)**, with its site at **tend.auth.com.br**. Nothing changes in how it works, and nothing needs doing: old links and clones are redirected, and this release is found by the tend you already have. From this release on, tend looks for its updates at the new address directly.

- About (first in the sidebar's menu) says who makes tend and where it lives now.
- Contributing asks for signed-off commits (`git commit -s`); see CONTRIBUTING.md. Security problems go to seguranca@auth.com.br, as SECURITY.md says.
- For Go users: the module is now `github.com/auth-com-br/tend`.

## v0.7.0 — 2026-09-25

#### New
- **Companies.** Working for several companies, each with its clients' projects? Click the mark at the right of the **spaces** heading (or prefix+O): make a company for each, tick which spaces it holds, and choose one — the sidebar then lists that company's spaces and their agents only, next/previous space stays inside it, and a space you make goes into it. **all spaces** brings everything back. A `!` beside the mark tells you an agent in a hidden space is waiting, so choosing a company never silences one. It only changes what you see: every space keeps running. The companies are kept with the session, and tend reopens on the one you last chose.

#### Fixed
- **The keys' help on a short terminal.** On a terminal about twenty rows high, the last keys of `ctrl+b ?` — detach among them — ran off the bottom of the box. The help now uses as many columns as it needs.

## v0.6.3 — 2026-09-25

#### New
- **Errors, from GlitchTip** (prefix+E, or 🐞 on the toolbar). Connect your GlitchTip server and an API token from the panel — the token is tested before it is kept, and stored readable by you alone — and see the errors your systems report, by project and status, with a search. Open one to read its stack, your own code's lines marked with the code itself, the request and what happened before. `f` hands it to the agent you are working with, typed in for you to look over; `w` starts an agent on it in a worktree of its own; `r` resolves it, `i` ignores it.
- **Close what is already fixed from the list.** `ctrl+x` resolves an error, or closes a GitHub issue (as completed, or `n` for not planned), without opening it; the lists have buttons for it too.
- **A ✕ on every panel.** Sessions, issues, errors, agents, context, about, release notes, the keys' help and the navigator close with a click on the ✕ at their top right, as esc closes them.

## v0.6.2 — 2026-09-25

#### New
- **A project of several repositories.** Open the issues panel (prefix+I) in a folder that holds several repositories, and it lists them all: every repository's issues and pull requests together, with a repository column. `ctrl+t`, or a click on the line at the top, narrows it to one. Work on an issue (`w`) starts in that issue's own repository, and a new issue (`ctrl+n`) goes to the repository you pick with `ctrl+t` in its box.
- **About tend.** The first item of the sidebar's menu opens a panel with the version, the server's version, the author, the site, the source and the licence. When an update left the server behind, it says so — the item gets a dot — with the `tend handoff` that brings it along. `ctrl+b ?` shows the version at its top too.

#### Fixed
- Against a server not yet moved to the new build, the issues panel only said "no GitHub remote". tend now notices the older server and says to run `tend handoff`.

## v0.6.1 — 2026-09-25

#### New
- **The last five pages, a click away.** The browser prompt (prefix+B, or 🌐 on the toolbar) lists the last five pages you opened: click one to open it again, or pick it with the arrows. They are kept across restarts.

#### Fixed
- **Google Chrome comes up with tend's extension.** Chrome stopped loading extensions given on its command line, so on a machine with only Chrome the page opened in your everyday Chrome without the extension. tend now starts the browser through a small keeper that loads the extension the way Google provides for this, over a private DevTools pipe (no port is opened). Checked with Chromium, Google Chrome 154 and Edge. Brave is still not supported.
- A long address typed in the browser prompt was cut at 64 characters, and one pasted over the prompt's `https://` came out `https://https://…`. The arrow keys no longer close the prompt.

## v0.6.0 — 2026-09-25

#### New
- **GitHub issues** (prefix+I, or Issues on the toolbar). The issues of the project you are in, through `gh` — nothing to log in to in tend. Filters on tab (open, assigned to me, created by me, closed), search as you type in GitHub's own syntax (`label:bug`), enter to read one with its comments. On a fork, upstream's issues first; `ctrl+t` switches to origin.
- **Work on an issue.** `c` comments, `x` closes (as completed or not planned) or reopens, `e`, `l` and `a` change its title, labels and assignees, and `ctrl+n` files a new one. `w` starts work: a worktree on `issue-<n>-<title>`, opened as a space, with your agent in it told to complete the issue — or, when the issue has one already, it goes there.
- **Pull requests.** `→` in the same panel lists them with their branch, checks and review. Open one to see what it changes, whether it merges, the checks that fail, its text and reviews; `m` merges it (squash, merge commit or rebase), `r` makes a draft ready, `w` goes to its worktree. An issue lists its pull requests, and `p` opens the first.
- **CONTRIBUTING.md** for anyone who wants to help.

#### Fixed
- The files panel's `git status` took the index lock, so your own `git add` or `commit` could fail with "index.lock: File exists" while it polled.
- Branch names made from titles keep accented letters: "começar" is `comecar`, not `come-ar`.

## v0.5.0 — 2026-09-24

#### New
- **Agent sessions** (prefix+S, or Sessions on the toolbar). Every Claude Code conversation on the machine, by the name it was given or the title Claude Code made for it, with its project, age and size. Type to search; `enter` resumes one in a new tab, in its directory — or goes to the pane it is already open in. `tab` marks, `ctrl+a` marks what is shown, `ctrl+o` marks what is 30 days old, `ctrl+d` deletes after asking. A conversation open in a pane is never deleted.
- **New tabs open where you are working.** A new tab, or a split, starts in the directory of the program in the pane you are on — the agent's project — instead of wherever tend was first started.
- **The browser sends to tend's context.** Send and Send all in the extension put what you picked, and the message over it, into the context; the context panel opens by itself, and `S` or **Send all** there sends everything to the agent.
- **Sounds you can hear.** With no sound file set, a finished agent plays the desktop's "complete" sound and one waiting for you its "message" sound. `[sound] done = "bell"` keeps the terminal bell.

#### Fixed
- Send all in the browser did nothing: it typed into whichever pane had focus. Against a server older than the extension, the browser now says to run `tend handoff`.
- Conversations held in a directory you cannot enter (as root, in `/root`) failed to resume with "permission denied"; they are marked, and `enter` copies the command that resumes them.
- The terminal bell was silent in GNOME Terminal, so a finished agent made no sound.

## v0.4.0 — 2026-09-24

#### New
- **Update notice and release notes.** A newer release is announced with a notice, "update ready" on the status bar and a dot on the sidebar's menu; the menu opens its notes, and `u` there installs it with `tend update -handoff`, in a tab of its own. `[update] version_check = false` turns the check off.
- **Sidebar toolbar.** Files, Agents, Context and Browser over the spaces, also on prefix+f, prefix+A, prefix+C and prefix+B.
- **Agent manager** (prefix+A). The agent CLIs tend knows — Claude Code, Codex, Gemini CLI, OpenCode, Copilot and more — which are installed, with their version, and the vendor's install command for the rest, run in a tab once you have seen it.
- **Context** (prefix+C). What tools captured for the agents — a page, an element, text, a file — with a note on each; copy one, or send it to the agent this tab works with, typed in for you to read over. `tend context add`, and "Add to context" in the files panel.
- **tend's browser** (prefix+B). Chromium or Edge in a profile of its own, with tend's extension already installed: pick elements on a page, write a note on each, and from the chat button copy them all or send them to the agent, one by one or together with a message.
- **Files panel** opens by itself in every tab (`[files] auto_open`), keeps up with a new build without being reopened, and previews files in a tab of their own.
- **Sidebar** width is dragged by its right edge; **tabs** are blocks with a gap between them.

#### Fixed
- Context sent to a pane not taking a paste (a shell) ran its lines as commands; what is typed now cannot act.
- A tab gone back to returns to the pane you left there.
- The client could crash on start when an event arrived before its connection was kept.

## v0.3.0 — 2026-09-23

(no notes)

## v0.2.0 — 2026-09-22

(no notes)

## v0.1.0 — 2026-09-21

(no notes)
