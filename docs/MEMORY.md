# Memory across sessions and agents — a study

Issue #3 asked how tend could give agents memory that outlives a session
and crosses from one agent to another: close Claude Code, open Codex in the
same project, and nothing of what was being done is there. This is the
study, and what it recommends tend builds, integrates, or leaves alone.

Source: the video *MEMÓRIA INFINITA de Graça pra Qualquer IA de Programar*
(Kauã Miguel, 08/09/2026) and its notes, and the projects it shows, read
on 26/09/2026.

## What a model remembers

Nothing. A conversation is the whole history sent again with every
message; when the session ends, so does the memory. Everything below is a
way of writing things down where the next session will read them.

## The four kinds of memory, and who already does each

| Kind | What it holds | Who does it well | Notes |
|---|---|---|---|
| Instructions in levels | the rules: global (`~/.claude/CLAUDE.md`), project (`CLAUDE.md`, `AGENTS.md`), subfolder | the agents themselves | Claude Code reads `CLAUDE.md`, Codex and OpenCode `AGENTS.md`; a `CLAUDE.md` holding `@AGENTS.md` serves both, as this repository does |
| Who you are | notes about you and your work | an Obsidian vault over MCP ([mcpvault](https://github.com/bitbonsai/mcpvault)) | personal; nothing for tend to add |
| The code's shape | functions, classes, who calls whom | [Graphify](https://github.com/safishamsi/graphify) — tree-sitter, 37 languages, no LLM for code, offline; a graph report, a JSON graph and an MCP server; hooks nudge Claude Code to query it before reading files | Apache-2.0/MIT; its token savings are its own benchmark's and depend on the project |
| What happened | the decision yesterday, the bug dropped, where work stopped | [ai-memory](https://github.com/akitaonrails/ai-memory) — lifecycle hooks record prompts, tool calls and session boundaries, sanitized, into a git-backed markdown wiki with SQLite search, read back over MCP, for 20+ agents; no LLM needed (one only improves summaries); Docker on 127.0.0.1:49374 or native | MIT; installs MCP config and hooks into each agent's global config, with backups |

## The questions, answered

### 1. Handing a task from one agent to another — **build it in tend**

This is the one only tend is placed to do well. It already knows which
conversation each pane's agent is in (the session its hook reports, kept
across restarts), can read Claude Code's conversations (the Sessions
panel), and can type text into a pane without it acting (`capture.Inert`,
the context panel).

What to build: on an agent's pane, **"continue in…"** another agent. tend
writes a handoff note from what it can read without calling a model — the
task (the conversation's first prompt and title), the last few requests,
the files the agent changed (its edit tool calls), where it stopped (its
last message), and the branch and `git status` — and starts the chosen
agent in a new tab of the same space, with the note as its first message,
looked over in the context panel before it is sent.

It needs no service, no API key and nothing installed in the agents, and
it works the day it ships. Claude Code's conversation is the source; other
agents' are added as tend learns to read them.

### 2. A project memory — **integrate ai-memory, do not rebuild it**

ai-memory already does what a tend-made project log would: hooks on 20+
agents, a sanitized record, a wiki per project, search, and MCP to read it
back. Rebuilding it would be a year of work to arrive where it is.

What tend adds: install and see it. The agent manager (`ctrl+b A`) grows a
**memory** section offering ai-memory with its documented install, run in
a tab after the user agrees, as agent CLIs are; and shows whether it is
running. tend keeps its own record only if the audit issue (#10) is built
for companies, and then of what was *done* (starts, stops, commits, PRs),
not a second memory of what was *said*.

### 3. Instructions in levels — **a small tool, later**

Useful and small: from the files panel, open the global, project and
subfolder instruction files, and offer to make `CLAUDE.md` point at
`AGENTS.md` (`@AGENTS.md`) so one file serves every agent. Not before the
handoff: it saves minutes, the handoff saves the task.

### 4. A graph of the code — **offer Graphify, do not build one**

Graphify does it with tree-sitter, offline, for 37 languages, and has
hooks and an MCP server. tend offers it in the same memory section of the
agent manager. Whether it saves tokens on the owner's real projects is
measured there before it is recommended to anyone.

### 5. Obsidian and MCP — **leave to the user**

A personal vault is personal. tend documents it and does not install it.

### 6. Where it shows

- "continue in…" on an agent pane's menu and in the Sessions panel;
- memory tools (ai-memory, Graphify) in the agent manager;
- instruction files in the files panel.

No new toolbar button: each fits a panel that already exists.

## Order

1. Handoff between agents — tend's own, and the one thing nobody else can do.
   **Built** (#27): "continue in..." on an agent's pane, ctrl+g in Sessions.
2. ai-memory and Graphify in the agent manager.
3. Instruction files in the files panel.
