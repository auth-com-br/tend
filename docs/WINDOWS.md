# tend on Windows

tend runs on Linux and macOS. On Windows it runs inside **WSL** — the Linux
that Windows 10 and 11 carry — where it is the Linux build, doing everything
it does on Linux. There is no native Windows build yet; see the end of this
page for why and what would decide it.

> This guide follows from how tend and WSL work. It has not yet been walked
> through end to end on a Windows machine; if a step does not do what it
> says, open an issue with what you saw.

## 1. WSL and a Linux

In PowerShell, as administrator:

```powershell
wsl --install -d Ubuntu
```

Restart when asked, open **Ubuntu** from the Start menu, and choose a user
name and password. Windows 11 (and Windows 10 with a recent WSL) includes
**WSLg**, which lets Linux windows — tend's browser — open on the Windows
desktop.

## 2. A terminal

Use **Windows Terminal** (installed with Windows 11; from the Microsoft
Store on Windows 10), with the Ubuntu profile. It passes the mouse, 24-bit
colour and the clipboard sequences tend uses; the old console window does
not do all of them.

Set the Ubuntu profile's font to one with the box-drawing characters —
Cascadia Mono, which Windows Terminal ships, does.

## 3. tend

In the Ubuntu window:

```bash
curl -fsSL https://tend.auth.com.br/install.sh | sh
```

It installs to `~/.local/bin`, which Ubuntu puts on `PATH` for new
windows; open a new tab, then run `tend`.

Keep your projects **inside** WSL (`~/work/…`), not under `/mnt/c/…`: the
Windows drive is reached through a translation layer that makes git, and
every agent reading the tree, many times slower.

## 4. The agents

Install them inside WSL, as on Linux — `ctrl+b A` (the agent manager) lists
them and runs each vendor's install command. An agent installed on Windows
is not seen from inside WSL.

## 5. What works, and how

| | In WSL |
|---|---|
| Sessions, panes, detach and attach, handoff | as on Linux |
| Mouse, colours | through Windows Terminal |
| Copy (`y` in copy mode, a selection) | through the terminal's clipboard sequence (OSC 52), which Windows Terminal passes to the Windows clipboard |
| GitHub issues and pull requests | install `gh` in WSL (`sudo apt install gh`) and `gh auth login` there |
| Errors (GlitchTip, Sentry) | as on Linux; the token stays in the settings file unless a keyring runs in WSL |
| Sounds | through WSLg's audio, when `paplay` is installed (`sudo apt install pulseaudio-utils`) |
| tend's browser (`ctrl+b B`) | install Chromium in WSL (`sudo apt install chromium` or the snap); it opens on the Windows desktop through WSLg, with tend's extension. Chrome or Edge **installed on Windows** cannot reach tend inside WSL: the extension's bridge has to run where tend runs |
| Desktop notifications | not in WSL; tend's own toasts show in the terminal |

## Why not a native Windows build yet

A native build needs Windows' own terminal API (ConPTY) in place of
Unix's pseudo-terminals, named pipes in place of Unix sockets, and Windows
paths throughout — the original tend is ported from does this, and
`docs/PORTING.md` (item 15) lists what is missing. It is a large piece of
work that can only be tested on Windows.

What would decide it: how many people who want tend use Windows without
WSL. The waitlist for team features asks, and the count will say whether a
native build is worth more than everything else waiting.
