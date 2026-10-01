package main

import (
	"fmt"
	"sort"
	"strings"
)

// commands is every subcommand and what it does, in the order the usage text
// lists them. The completion scripts are generated from it, so a command added
// to the dispatcher and not here is caught by a test rather than by somebody
// pressing tab and finding it missing.
var commands = []struct{ name, help string }{
	{"attach", "open a session, starting a server if there is none"},
	{"ls", "list panes, or sessions"},
	{"new", "open a pane in a session"},
	{"kill", "close panes, or stop a session's server"},
	{"handoff", "replace a session's server, keeping its programs"},
	{"agent", "drive an agent: read, prompt, wait"},
	{"pane", "drive a pane: read, type, wait"},
	{"notify", "tell whoever is watching the session"},
	{"context", "add to, list or clear what the agents are to be handed"},
	{"gitlab", "keep a token for a GitLab: login, logout, status"},
	{"mcp", "the agents' MCP servers: list them, test them"},
	{"browser", "the session's browsers: status, open, select, attach"},
	{"terminal", "set or clear the outer window's title"},
	{"layout", "save or rebuild a tab's arrangement"},
	{"events", "follow a session's events"},
	{"api", "call the automation socket directly"},
	{"plugin", "install and run plugins"},
	{"worktree", "give an agent a checkout of its own"},
	{"files", "the file explorer, for its panel"},
	{"machine", "save other machines to keep an eye on"},
	{"view", "a file read-only with syntax colour"},
	{"follow", "stream a session's events"},
	{"serve", "run a session server in the foreground"},
	{"bridge", "carry a session over stdin and stdout, for ssh"},
	{"config", "show or write the settings file"},
	{"integration", "install or remove agent hooks"},
	{"update", "install the published build"},
	{"channel", "show or set the update channel"},
	{"keys", "list every command and the key it is on"},
	{"activity", "what agents did and what it cost"},
	{"agents", "list the agents tend can recognise"},
	{"detect", "classify a screen capture"},
	{"explain", "classify a screen capture and say why"},
	{"screen", "print a pane's screen"},
	{"watch", "run a command on a pty and report its agent state"},
	{"session", "run several commands as panes in one process"},
	{"completion", "print a shell completion script"},
	{"version", "print the version"},
}

// completionShells is what `tend completion` can write for.
var completionShells = []string{"bash", "fish", "zsh"}

// sessionNames is the shell fragment that lists sessions, shared by the three
// scripts so they cannot disagree about what a session name is.
const sessionNames = `tend ls -sessions 2>/dev/null | tail -n +2 | cut -d' ' -f1`

func runCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tend completion <%s>", strings.Join(completionShells, "|"))
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion(), nil
	case "zsh":
		return zshCompletion(), nil
	case "fish":
		return fishCompletion(), nil
	}
	sorted := append([]string{}, completionShells...)
	sort.Strings(sorted)
	return "", fmt.Errorf("no completion for %q; try one of: %s", shell, strings.Join(sorted, ", "))
}

func commandNames() []string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.name
	}
	return names
}

func bashCompletion() string {
	return `# tend completion for bash. Load with: source <(tend completion bash)
_tend() {
    local cur prev
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    if [ "$prev" = "-s" ]; then
        COMPREPLY=( $(compgen -W "$(` + sessionNames + `)" -- "$cur") )
        return
    fi
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "` + strings.Join(commandNames(), " ") + `" -- "$cur") )
        return
    fi
    if [ "${COMP_WORDS[1]}" = "completion" ]; then
        COMPREPLY=( $(compgen -W "` + strings.Join(completionShells, " ") + `" -- "$cur") )
    fi
}
complete -F _tend tend
`
}

func zshCompletion() string {
	var b strings.Builder
	b.WriteString("#compdef tend\n# tend completion for zsh. Load with: source <(tend completion zsh)\n")
	b.WriteString("_tend() {\n    local -a commands\n    commands=(\n")
	for _, c := range commands {
		// A colon separates the name from its description, so one inside the
		// description has to be escaped or zsh reads it as a third field.
		// And an apostrophe ends the single-quoted string it sits in, so it is
		// written the only way that shell allows: close, escape, reopen.
		help := strings.ReplaceAll(c.help, ":", "\\:")
		help = strings.ReplaceAll(help, "'", `'\''`)
		fmt.Fprintf(&b, "        '%s:%s'\n", c.name, help)
	}
	b.WriteString("    )\n")
	b.WriteString("    if [[ ${words[CURRENT-1]} == -s ]]; then\n")
	b.WriteString("        local -a sessions\n        sessions=(${(f)\"$(" + sessionNames + ")\"})\n")
	b.WriteString("        _describe 'session' sessions\n        return\n    fi\n")
	b.WriteString("    if (( CURRENT == 2 )); then\n        _describe 'command' commands\n        return\n    fi\n")
	b.WriteString("    if [[ ${words[2]} == completion ]]; then\n        _values 'shell' " + strings.Join(completionShells, " ") + "\n    fi\n")
	// compdef only exists once the completion system has been started, which a
	// bare shell has not done. Sourcing this there would fail on its last line
	// after defining everything, so it is started if it has to be.
	b.WriteString("}\n(( $+functions[compdef] )) || { autoload -Uz compinit && compinit }\n")
	b.WriteString("compdef _tend tend\n")
	return b.String()
}

func fishCompletion() string {
	var b strings.Builder
	b.WriteString("# tend completion for fish. Load with: tend completion fish | source\n")
	b.WriteString("complete -c tend -f\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "complete -c tend -n __fish_use_subcommand -a %s -d '%s'\n",
			c.name, strings.ReplaceAll(c.help, "'", "\\'"))
	}
	b.WriteString("complete -c tend -s s -x -a '(" + sessionNames + ")' -d 'session'\n")
	b.WriteString("complete -c tend -n '__fish_seen_subcommand_from completion' -a '" +
		strings.Join(completionShells, " ") + "'\n")
	return b.String()
}
