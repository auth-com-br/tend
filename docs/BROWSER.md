# tend and a browser

tend does not render pages. A real browser does, and talks to the tend
server, which stands between it and the agents: what the browser picks goes
into the session's context buffer, and from there to whichever agent the
user sends it to. No browser talks to an agent directly.

```
browser + extension ──(bridge)──► tend server ──► context buffer ──► agent pane
                     ◄── commands ──┘
```

## The browser tend opens

The toolbar's Browser (prefix+B) — or `tend browser launch [url]` — opens a
page in tend's browser when no browser is attached to the session yet: a
Chromium-family browser in a profile of the session's own
(`~/.local/share/tend/browser/<session>/`), started with tend's extension
loaded. Nothing is installed by hand, and the user's own browser and
profile are not touched.

- **The extension** (`internal/browserext/extension/`, built into tend and
  written out each time the browser opens, so it is always the running
  tend's) connects to the native messaging host `dev.tend.browser` as it
  starts, and follows the session's commands. Its manifest carries a fixed
  key, so its ID is always `kafdikfjfbpngnlobakdlnepmciniffa`.
- **Picking and notes**: its toolbar icon or Alt+Shift+T turns picking on in
  the page. The element under the pointer is outlined with its selector over
  it; a click takes it and opens a note on it (Enter saves, Shift+Enter a new
  line, Esc skips), and picking goes on, so several are taken in a row, each
  numbered on the page. Esc, or Done picking, stops. A floating chat button
  holds them all: each with its note, editable there, sent to tend on its
  own or removed. Over them is a message box, for what is wanted of all of
  them together; Copy all copies the message and them, in the text tend
  hands an agent, and Send all to tend puts the message (as a `text` item)
  and them into the session's context. A note still being written when
  either is pressed goes with it. The extension never types into an agent
  itself: the client opens the context panel as they arrive (or says so, if
  another panel is up), and the user sends them from there — `s` one,
  `S` or Send all everything, oldest first, to the pane they were in —
  having seen what goes. What is taken
  is kept per tab and page, so a reload finds it again; it goes when the
  browser closes. It is all drawn in a shadow root, apart from the page's
  styles.
- **The bridge** (`tend browser bridge`) is that host: the browser starts it,
  from a script in the profile, with the environment the browser was started
  with (`TEND_BROWSER_SESSION`, and `TEND_BROWSER_REMOTE` for a session over
  ssh). It speaks native messaging — JSON after four bytes of its length —
  on its standard streams, attaches to the session as a browser, passes each
  command on, and takes the extension's requests to the socket: only the
  browser's own (`browser.context`, `browser.send_to_agent`,
  `browser.status`). Its registration is in the profile's
  `NativeMessagingHosts/`, where a browser started on that profile looks.
- **Which browser**: `[browser] program` if set, else the first of chromium,
  chromium-browser, microsoft-edge, vivaldi, google-chrome-for-testing,
  google-chrome. It is started by `tend browser keep`, which loads the
  extension through DevTools' `Extensions.loadUnpacked` over
  `--remote-debugging-pipe` — the way Google left when Chrome stopped
  reading `--load-extension` in 2025. Chromium and Edge still read that
  switch, but giving both loaded the extension twice: the second load
  replaced the first and dropped its bridge, so a page sent in that gap
  never opened. The keeper holds the pipe until the browser ends (the
  browser ends if it closes); no port is opened. Tried on 2026-09-24,
  headless, through the keeper, with the bridge attaching: Chromium 153,
  Google Chrome 154, Edge 153. Brave 153 loads the extension but never
  starts the bridge, so it is not tried. With none of them, the page opens
  in the desktop's browser without the extension, and tend says so.
  `[browser] command` opens a browser of your own instead, also without it.

A second page for the same session opens as a tab in the browser already
running on that profile.

## The protocol

Everything is on the session's automation socket (`tend api schema` lists
it), one JSON object a line, as every other method there.

### A browser attaches

```json
{"id":"1","method":"browser.attach","params":{"name":"chrome"}}
```

The reply is `{"type":"browser_attached"}`, and from then on the connection
carries the commands the browser is to follow, one a line, until it hangs up:

```json
{"type":"browser_command","action":"open","url":"https://example.com/login"}
{"type":"browser_command","action":"navigate","url":"https://example.com/login"}
{"type":"browser_command","action":"select","on":true}
```

- `open`: the page in a new tab.
- `navigate`: the tab in front goes to the page.
- `select`: element picking on or off — the page highlights what is under
  the pointer, and a click captures it.

Only `http://` and `https://` pages are ever sent.

### Telling it what to do

Any tool, or a person with `tend browser`, sends these; each answers
`{"type":"browser_sent","browsers":N}`, or the error `no_browser` when none
is attached:

| method | params |
|---|---|
| `browser.open` | `url` |
| `browser.navigate` | `url` |
| `browser.select` | `on` (bool) |
| `browser.status` | — (answers the names of the browsers attached) |

The client's own "open a page" (the toolbar's Browser, prefix+B) sends
`browser.open` first, and opens the page on the user's own machine — in
`[browser] command`, or the desktop's browser — when no browser is attached.

### What it captures

The browser hands back what was picked:

```json
{"id":"2","method":"browser.context","params":{
  "url":"https://example.com/login","title":"Login",
  "selector":"#email","tag":"input","text":"",
  "attributes":{"id":"email","name":"email","type":"email"}}}
```

It goes into the context buffer as an `element` from `browser` (a `kind` of
`url` or `text` can be given instead), where the context panel (prefix+C)
lists it and sends it to an agent. Each item can carry a `note` — what the
user wants done with it — which leads it in what the agent is handed.
Several can go at once under `items`, with a `message` that goes in first
as a `text` item; they are checked together and kept all or none. Each
arrival is announced to the clients (the `context-arrived` event), which
open the context panel.
`browser.send_to_agent` takes the same params, or several under `items`, a
`message` to lead them, and a `pane_id`, and goes straight on: the items are kept, and typed together
into that pane — or, with no `pane_id`, the one the user was last in —
without being submitted.

Nothing typed this way can act. Control characters are dropped, so an
escape in a page's text cannot end a bracketed paste early; and a program
that has not asked for bracketed paste — a shell, not an agent — gets the
text on one line, each break shown as ⏎, since a break would be Enter.

## Trying it without a browser

```bash
tend browser attach -name stand-in      # in one terminal: prints commands
tend browser status                     # stand-in
tend browser open https://example.com   # the stand-in prints it
tend api browser.context '{"url":"https://example.com","selector":"h1","text":"Example Domain"}'
```

## Not done

- Firefox: it gives extensions native messaging too, but loads an unsigned
  one only for a session and only through its developer tools, so tend's
  browser is Chromium-family.
- Picking a stretch of text rather than an element, and screenshots, which
  the context's kinds leave room for.
- Knowing when the agent has read what was sent: the extension marks an item
  sent when tend has typed it, not when anybody has pressed Enter.
