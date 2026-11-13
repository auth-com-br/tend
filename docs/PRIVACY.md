# What tend sends, and where

tend collects nothing. There is no telemetry, no analytics, no crash report
sent anywhere, and no account. What follows is every connection tend opens
off your machine, and when. Everything else stays on it: the session, the
scrollback, the activity record, the settings.

## On its own

| What | Where to | When | Off |
|---|---|---|---|
| The release check | `github.com/auth-com-br/tend` and GitHub's release files | a release build only, when its server starts and every 30 minutes after, until it finds a newer release; nothing is sent but the request for the release list | `[update] version_check = false` |

That is the only one. Measured with the release build under
`strace -e trace=connect` for a whole session: with the check on, the only
network connections were the name lookup and GitHub; with it off, none.

## When you ask

| What | Where to | When |
|---|---|---|
| `tend update` | GitHub's release files | you run it, or press `u` in the release notes |
| The issues and pull-request panels | GitHub (through your `gh`), or a GitLab's API | you open them, search, or act on an item |
| The errors panel | your GlitchTip or Sentry server | you open it |
| The tickets panel | your Linear, Jira, ClickUp or Issue Gateway | you open it |
| The files panel's sync, merge and pull request | your git remote, and GitHub through `gh` | you press the key |
| Drafting a commit message (`A`) | Anthropic, through your own `claude` | you press it |
| Testing an MCP server | that server | you test it |
| Installing an agent | the agent's own installer | you install it |
| Saved machines (`tend attach -ssh`, the sidebar's machines) | your machines, over ssh | you saved them or attach |
| Phone notifications | your phone's push service (Google's, Apple's, Mozilla's), encrypted end to end so the service reads nothing | only for a phone that turned them on |
| Plugins | whatever the plugin does | you installed it |

The phone page itself (`tend web`) listens; it does not call out.

## Kept honest

`cmd/tend/network_test.go` lists every file that can open a connection off
the machine, with when it does. A file that starts to — a new library, a new
check at start — fails the test until it is listed here and there.
