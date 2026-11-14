package integration

import (
	"os"
	"path/filepath"
	"reflect"
)

// The guard's hook (tend guard, internal/policy's guard.go) is Claude Code's
// PreToolUse on Bash: Claude asks it before each shell command, and it
// answers deny, ask, or nothing. It is installed only when asked (tend guard
// install), not with the state hook: it runs before every command of every
// Claude Code on the machine, inside tend or not, and that is a thing to
// choose.

// GuardMatcher is the tool the guard is asked about.
const GuardMatcher = "Bash"

// GuardHookTimeout bounds the hook, in seconds: it reads a command and a
// settings file, and answers at once.
const GuardHookTimeout = 10

// ClaudeGuardSettings is the settings file the guard's hook goes in.
func ClaudeGuardSettings() (string, error) {
	dir, err := ClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// InstallClaudeGuard adds the guard's PreToolUse hook, running command, to
// the settings file at path. It reports whether the file changed.
func InstallClaudeGuard(path, command string) (bool, error) {
	return editClaudeSettings(path, func(settings map[string]any) error {
		hooks, err := EnsureHooksObject(settings, path, "claude settings", "claude settings hooks")
		if err != nil {
			return err
		}
		return EnsureCommandHook(hooks, "PreToolUse", command, GuardHookTimeout, GuardMatcher)
	})
}

// UninstallClaudeGuard takes the guard's hook out again.
func UninstallClaudeGuard(path, command string) (bool, error) {
	return editClaudeSettings(path, func(settings map[string]any) error {
		hooks, ok, err := HooksObjectIfPresent(settings, path, "claude settings", "claude settings hooks")
		if err != nil || !ok {
			return err
		}
		_, err = RemoveCommandHook(hooks, "PreToolUse", command)
		return err
	})
}

func editClaudeSettings(path string, edit func(map[string]any) error) (bool, error) {
	content := "{}"
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	original, err := parseSettingsObject(content, path)
	if err != nil {
		return false, err
	}
	desired := cloneJSONMap(original)
	if err := edit(desired); err != nil {
		return false, err
	}
	if reflect.DeepEqual(desired, original) {
		return false, nil
	}
	out, err := marshalSettings(desired)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}
