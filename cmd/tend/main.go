// Command tend is the tend CLI.
//
// The subcommands here exercise the pieces that exist: the terminal core, and
// manifest-driven agent detection. The server and the TUI client are not built
// yet, so nothing here manages a session — `watch` runs one command and reports
// what its terminal says about it.
package main

import (
	"fmt"
	"os"
	"strings"
)

// version is stamped at build time from git; see the Makefile. The fallback
// is what a "go build ./cmd/tend" with no flags produces, which is worth
// saying plainly rather than claiming a version it does not have.
var version = "development build"

const usage = `tend — terminal runtime for coding agents

usage: tend [command] [options]
       tend --remote <ssh-target> [--session <name>]

with no command, tend opens a session — starting a server for it if there is
not one already — and draws it.

commands:
  attach            open a session and draw it (the default)
  ls                list a session's panes, or every session
  new               open a pane in a session
  kill              close panes, or stop a session
  handoff           replace a session's server, keeping its programs
  agent             drive an agent: read it, prompt it, wait for it
  pane              drive a pane: read it, type into it, wait for it
  notify            tell whoever is watching the session something
  context           add to, list or clear what the agents are to be handed
  gitlab            keep a token for a GitLab, for its issues and merge requests
  browser           the session's browsers: status, open, select, attach
  terminal          set or clear the outer window's title
  layout            save a tab's arrangement, or build it again
  events            follow a session's events as they happen
  api               call the automation socket directly
  plugin            install and run plugins
  worktree          give an agent a checkout of its own, as a space
  files             the file explorer, as it runs in its panel (prefix+f)
  machine           save other machines to keep an eye on
  view              a file read-only with syntax colour (the panel's preview)
  follow            print a session's events, for diagnosis
  serve             run a session server in the foreground
  config            show or create the settings file
  integration       install or remove agent hooks

commands that need no server:
  update            install the build published on your channel
  channel           show or set the update channel
  keys              list every command and the key it is on
  agents            list the agents tend can detect
  detect            report an agent's state from captured terminal output
  explain           show how every rule voted, for one capture
  screen            render captured terminal output as tend parses it
  watch             run a command on a pty and report its agent state
  session           run several commands as panes in one process
  bridge            carry a session over stdin and stdout, for ssh
  completion        print a shell completion script (bash, zsh, fish)
  version           print the version

run "tend <command> -h" for a command's options.
`

// launchArgs reads herdr's way of starting: `tend --remote <ssh-target>
// [--session <name>]`, and `tend --session <name>`, each as `--flag value` or
// `--flag=value`. They mean attach, to that machine and that session; with
// no arguments at all, tend attaches to the default session here. As in
// herdr, they go only with the default launch, not with a command.
func launchArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return []string{"attach"}, nil
	}
	if !strings.HasPrefix(args[0], "--remote") && !strings.HasPrefix(args[0], "--session") {
		return args, nil
	}
	out := []string{"attach"}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		var flag string
		switch name {
		case "--remote":
			flag = "-ssh"
		case "--session":
			flag = "-s"
		default:
			return nil, fmt.Errorf("--remote and --session go only with the default launch; %q is not one of them", args[i])
		}
		if !inline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, fmt.Errorf("%s needs a value", name)
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s needs a value", name)
		}
		out = append(out, flag, value)
	}
	return out, nil
}

func main() {
	// Bare "tend" opens a session, starting a server if there is none. Asking
	// someone to run a daemon before they can use the thing is a step that
	// exists only because of how it is built.
	args, err := launchArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "tend: %v\n", err)
		os.Exit(2)
	}

	switch cmd := args[0]; cmd {
	case "update":
		err = runUpdate(args[1:])
	case "channel":
		err = runChannel(args[1:])
	case "keys":
		err = runKeys(args[1:])
	case "agents":
		err = runAgents(args[1:])
	case "detect":
		err = runDetect(args[1:], false)
	case "explain":
		err = runDetect(args[1:], true)
	case "screen":
		err = runScreen(args[1:])
	case "watch":
		err = runWatch(args[1:])
	case "session":
		err = runSession(args[1:])
	case "serve":
		err = runServe(args[1:])
	case "ls":
		err = runList(args[1:])
	case "new":
		err = runNew(args[1:])
	case "kill":
		err = runKill(args[1:])
	case "handoff":
		err = runHandoff(args[1:])
	case "agent":
		err = runAgent(args[1:])
	case "pane":
		err = runPane(args[1:])
	case "notify":
		err = runNotify(args[1:])
	case "context":
		err = runContext(args[1:])
	case "gitlab":
		err = runGitLab(args[1:])
	case "browser":
		err = runBrowser(args[1:])
	case "terminal":
		err = runTerminal(args[1:])
	case "layout":
		err = runLayout(args[1:])
	case "events":
		err = runEvents(args[1:])
	case "api":
		err = runAPI(args[1:])
	case "plugin":
		err = runPlugin(args[1:])
	case "worktree":
		err = runWorktree(args[1:])
	case "files":
		err = runFiles(args[1:])
	case "machine":
		err = runMachine(args[1:])
	case "view":
		err = runView(args[1:])
	case "attach":
		err = runAttach(args[1:])
	case "follow":
		err = runFollow(args[1:])
	case "config":
		err = runConfig(args[1:])
	case "integration":
		err = runIntegration(args[1:])
	case "bridge":
		err = runBridge(args[1:])
	case "completion":
		err = runCompletion(args[1:])
	case "version":
		fmt.Printf("tend %s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "tend: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tend: %v\n", err)
		os.Exit(1)
	}
}
