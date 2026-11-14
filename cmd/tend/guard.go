package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/integration"
	"github.com/auth-com-br/tend/internal/policy"
)

const guardUsage = `usage: tend guard install      add the guard's hook to Claude Code (~/.claude/settings.json)
       tend guard uninstall    take it out again
       tend guard check <command>   what the guard would say about a command, here
       tend guard              the hook itself: Claude Code's PreToolUse input on stdin

Before an agent runs a shell command, the guard looks for what would destroy
work outside it — rm -r outside the project, a force push, git reset --hard,
DROP DATABASE, a Docker volume, a disk — and, as [policy] guard says, has the
agent ask you ("ask") or refuses it ("deny"). It is off until guard is set.
`

func runGuard(args []string) error {
	if len(args) == 0 {
		return guardHook(os.Stdin, os.Stdout)
	}
	switch args[0] {
	case "install", "uninstall":
		self, err := os.Executable()
		if err != nil {
			return err
		}
		if r, err := filepath.EvalSymlinks(self); err == nil {
			self = r
		}
		command := shellQuote(self) + " guard"
		path, err := integration.ClaudeGuardSettings()
		if err != nil {
			return err
		}
		var changed bool
		if args[0] == "install" {
			changed, err = integration.InstallClaudeGuard(path, command)
		} else {
			changed, err = integration.UninstallClaudeGuard(path, command)
		}
		if err != nil {
			return err
		}
		switch {
		case args[0] == "install" && changed:
			fmt.Printf("%s the guard's hook is in %s\n", tag(), path)
			if cfg, err := config.Load(); err == nil && !cfg.Policy.Guard.On() {
				fmt.Printf("%s it does nothing until [policy] guard is \"ask\" or \"deny\" in tend's settings\n", tag())
			}
		case args[0] == "install":
			fmt.Printf("%s the guard's hook was already in %s\n", tag(), path)
		case changed:
			fmt.Printf("%s the guard's hook is out of %s\n", tag(), path)
		default:
			fmt.Printf("%s the guard's hook was not in %s\n", tag(), path)
		}
		return nil
	case "check":
		line := strings.Join(args[1:], " ")
		if strings.TrimSpace(line) == "" {
			return errors.New("usage: tend guard check <command>")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd()
		dangers := cfg.Policy.Guard.Judge(line, guardWhere(cwd))
		if len(dangers) == 0 {
			fmt.Println("allowed")
			return nil
		}
		for _, d := range dangers {
			fmt.Printf("%s: %s\n", d.Rule, d.Message)
		}
		return fmt.Errorf("the guard would %s it", verdictWord(cfg.Policy.Guard.Mode))
	case "-h", "--help", "help":
		fmt.Print(guardUsage)
		return nil
	}
	return errors.New(guardUsage)
}

func verdictWord(mode string) string {
	if mode == policy.GuardDeny {
		return "refuse"
	}
	return "ask you about"
}

// guardWhere is what a command is judged against: the repository the
// directory is in (else the directory), home, and the temporary
// directories, under which anything may go.
func guardWhere(cwd string) policy.Where {
	home, _ := os.UserHomeDir()
	project := cwd
	if out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output(); err == nil {
		project = strings.TrimSpace(string(out))
	}
	temp := []string{"/tmp", "/var/tmp"}
	if d := os.TempDir(); d != "" {
		temp = append(temp, d)
	}
	return policy.Where{Project: project, Cwd: cwd, Home: home, Temp: temp}
}

// hookInput is what Claude Code hands a PreToolUse hook.
type hookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
	Cwd string `json:"cwd"`
}

// guardHook answers one PreToolUse: nothing when the command may run as
// Claude Code would run it, else a decision. Anything that goes wrong —
// unreadable input, a broken settings file — answers nothing: a guard that
// stopped every command over its own fault would be switched off by the
// first person it stopped.
func guardHook(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return nil
	}
	var input hookInput
	if json.Unmarshal(raw, &input) != nil || input.ToolName != "Bash" || strings.TrimSpace(input.ToolInput.Command) == "" {
		return nil
	}
	cfg, err := config.Load()
	if err != nil || !cfg.Policy.Guard.On() {
		return nil
	}
	cwd := input.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	dangers := cfg.Policy.Guard.Judge(input.ToolInput.Command, guardWhere(cwd))
	if len(dangers) == 0 {
		return nil
	}
	reasons := make([]string, len(dangers))
	for i, d := range dangers {
		reasons[i] = d.Message
	}
	decision := policy.GuardAsk
	if cfg.Policy.Guard.Mode == policy.GuardDeny {
		decision = policy.GuardDeny
	}
	reason := "tend guard: " + strings.Join(reasons, "; ")
	if decision == policy.GuardDeny {
		reason += ". If it is meant, the person can run it, or allow it in [policy] guard_allow."
	}
	answer, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason,
	}})
	_, _ = out.Write(append(answer, '\n'))

	// Written down in the session the agent runs in, when it runs in one.
	if socket, pane := os.Getenv(api.EnvSocketPath), os.Getenv(api.EnvPaneID); socket != "" && pane != "" {
		done := "refused"
		if decision == policy.GuardAsk {
			done = "asked"
		}
		msg := fmt.Sprintf("%q: %s", oneLine(input.ToolInput.Command), strings.Join(reasons, "; "))
		_, _ = socketCaller(socket).Call(api.MethodPaneReportGuard, map[string]any{
			"pane_id": pane, "rule": dangers[0].Rule, "outcome": done, "message": msg,
		})
	}
	return nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		return string(r[:117]) + "…"
	}
	return s
}
