package integration

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/auth-com-br/tend/internal/jsonc"
)

//go:embed assets/claude/tend-agent-state.sh
var claudeHookAsset string

// ClaudeHookInstallName is the filename written under ~/.claude/hooks/.
const ClaudeHookInstallName = "tend-agent-state.sh"

// ClaudeIntegrationVersion is stamped in the hook header; bump when the asset
// changes in a way that needs reinstall.
const ClaudeIntegrationVersion = 10

// Claude's documented SessionStart sources. Grok imports Claude hooks but uses
// new/load; filter before it starts an unnecessary hook process.
const sessionStartMatcher = "^(startup|resume|clear|compact|fork)$"

// ClaudeInstallPaths are the files InstallClaude wrote or updated.
type ClaudeInstallPaths struct {
	HookPath     string
	SettingsPath string
}

// ClaudeUninstallResult reports what UninstallClaude removed.
type ClaudeUninstallResult struct {
	HookPath        string
	SettingsPath    string
	RemovedHookFile bool
	UpdatedSettings bool
}

type hookRemoval struct {
	event   string
	actions []string
}

// HOOK_REMOVALS matches herdr's claude_settings.rs: deprecated lifecycle hooks
// tend (and herdr) once installed, removed on every install/uninstall.
var claudeHookRemovals = []hookRemoval{
	{event: "PostToolUse", actions: []string{"working"}},
	{event: "PostToolUseFailure", actions: []string{"working"}},
	{event: "SubagentStop", actions: []string{"working"}},
	{event: "PermissionRequest", actions: []string{"blocked"}},
	{event: "SessionStart", actions: []string{"idle", "session"}},
	{event: "UserPromptSubmit", actions: []string{"working"}},
	{event: "PreToolUse", actions: []string{"working"}},
	{event: "Stop", actions: []string{"idle"}},
	{event: "SessionEnd", actions: []string{"release"}},
}

// InstallClaude writes the SessionStart hook and updates settings.json.
func InstallClaude() (ClaudeInstallPaths, error) {
	dir, err := ClaudeDir()
	if err != nil {
		return ClaudeInstallPaths{}, err
	}
	if err := requireDir(dir, "claude"); err != nil {
		return ClaudeInstallPaths{}, err
	}

	hooksDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return ClaudeInstallPaths{}, err
	}

	hookPath := filepath.Join(hooksDir, ClaudeHookInstallName)
	if err := os.WriteFile(hookPath, []byte(claudeHookAsset), 0o755); err != nil {
		return ClaudeInstallPaths{}, err
	}
	if err := MakeExecutable(hookPath); err != nil {
		return ClaudeInstallPaths{}, err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	existing := "{}"
	if data, err := os.ReadFile(settingsPath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return ClaudeInstallPaths{}, err
	}

	updated, err := installClaudeSettings(existing, settingsPath, hookPath)
	if err != nil {
		return ClaudeInstallPaths{}, err
	}
	if updated != existing {
		if err := os.WriteFile(settingsPath, []byte(updated), 0o644); err != nil {
			return ClaudeInstallPaths{}, err
		}
	}

	return ClaudeInstallPaths{HookPath: hookPath, SettingsPath: settingsPath}, nil
}

// UninstallClaude removes tend/herdr Claude hook entries and the hook file.
func UninstallClaude() (ClaudeUninstallResult, error) {
	dir, err := ClaudeDir()
	if err != nil {
		return ClaudeUninstallResult{}, err
	}
	hookPath := filepath.Join(dir, "hooks", ClaudeHookInstallName)
	settingsPath := filepath.Join(dir, "settings.json")
	result := ClaudeUninstallResult{
		HookPath:     hookPath,
		SettingsPath: settingsPath,
	}

	if data, err := os.ReadFile(settingsPath); err == nil {
		updated, err := uninstallClaudeSettings(string(data), settingsPath, hookPath)
		if err != nil {
			return result, err
		}
		if updated != string(data) {
			if err := os.WriteFile(settingsPath, []byte(updated), 0o644); err != nil {
				return result, err
			}
			result.UpdatedSettings = true
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}

	removed, err := RemoveFileIfExists(hookPath)
	if err != nil {
		return result, err
	}
	result.RemovedHookFile = removed
	return result, nil
}

// installClaudeSettings merges the SessionStart hook into Claude settings.
//
// As herdr's jsonc_parser rewrite does, the file is changed only where the
// hook goes (rewriteSettings, internal/jsonc): the person's order, layout
// and comments stay. A file that does not parse is a clear error, never
// rewritten from a guess.
func installClaudeSettings(content, settingsPath, hookPath string) (string, error) {
	original, err := parseSettingsObject(content, settingsPath)
	if err != nil {
		return "", err
	}
	desired := cloneJSONMap(original)

	hooks, err := EnsureHooksObject(desired, settingsPath, "claude settings", "claude settings hooks")
	if err != nil {
		return "", err
	}
	canonical := canonicalSessionStartEntry(hookPath)
	if _, err := applyClaudeRemovals(hooks, hookPath, canonical); err != nil {
		return "", err
	}
	if err := EnsureCommandHook(hooks, "SessionStart", HookCommand(hookPath, "session"), 10, sessionStartMatcher); err != nil {
		return "", err
	}

	if reflect.DeepEqual(desired, original) {
		return content, nil
	}
	return rewriteSettings(content, desired)
}

func uninstallClaudeSettings(content, settingsPath, hookPath string) (string, error) {
	original, err := parseSettingsObject(content, settingsPath)
	if err != nil {
		return "", err
	}
	desired := cloneJSONMap(original)

	hooks, ok, err := HooksObjectIfPresent(desired, settingsPath, "claude settings", "claude settings hooks")
	if err != nil {
		return "", err
	}
	if !ok {
		return content, nil
	}
	removed, err := applyClaudeRemovals(hooks, hookPath, nil)
	if err != nil {
		return "", err
	}
	if !removed {
		return content, nil
	}
	if reflect.DeepEqual(desired, original) {
		return content, nil
	}
	return rewriteSettings(content, desired)
}

func applyClaudeRemovals(hooks map[string]any, hookPath string, canonical map[string]any) (bool, error) {
	removed := false
	for _, policy := range claudeHookRemovals {
		var keep map[string]any
		if policy.event == "SessionStart" {
			keep = canonical
		}
		commands := removalCommands(policy, hookPath)
		did, err := removeEventCommands(hooks, policy.event, commands, keep)
		if err != nil {
			return removed, err
		}
		removed = removed || did
	}
	return removed, nil
}

func removalCommands(policy hookRemoval, hookPath string) []string {
	var commands []string
	for _, action := range policy.actions {
		commands = append(commands, HookCommandVariants(hookPath, action)...)
	}
	return commands
}

func removeEventCommands(hooks map[string]any, event string, commands []string, canonical map[string]any) (bool, error) {
	raw, ok := hooks[event]
	if !ok {
		return false, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return false, fmt.Errorf("hook entries for %s must be an array", event)
	}

	removed := false
	canonicalPreserved := false
	kept := make([]any, 0, len(entries))

	for _, entry := range entries {
		if !canonicalPreserved && canonical != nil && jsonDeepEqual(entry, canonical) {
			canonicalPreserved = true
			kept = append(kept, entry)
			continue
		}
		entryMap, ok := entry.(map[string]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		hookEntries, ok := entryMap["hooks"].([]any)
		if !ok {
			kept = append(kept, entry)
			continue
		}
		before := len(hookEntries)
		filtered := hookEntries[:0]
		for _, hook := range hookEntries {
			match := false
			for _, command := range commands {
				if IsMatchingCommandHook(hook, command) {
					match = true
					break
				}
			}
			if !match {
				filtered = append(filtered, hook)
			}
		}
		if len(filtered) != before {
			removed = true
		}
		if len(filtered) == 0 {
			continue
		}
		entryMap["hooks"] = filtered
		kept = append(kept, entryMap)
	}

	if len(kept) == 0 && canonical == nil {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	return removed, nil
}

func canonicalSessionStartEntry(hookPath string) map[string]any {
	return map[string]any{
		"matcher": sessionStartMatcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": HookCommand(hookPath, "session"),
				"timeout": uint64(10),
			},
		},
	}
}

func parseSettingsObject(content, settingsPath string) (map[string]any, error) {
	root, err := jsonc.Decode([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("claude settings at %s must be a JSON object", settingsPath)
	}
	return obj, nil
}

// rewriteSettings is content changed to hold settings: patched, so the
// person's comments, order and layout stay (issue #61), or written whole
// when the patch cannot keep them.
func rewriteSettings(content string, settings map[string]any) (string, error) {
	if strings.TrimSpace(content) != "" {
		if out, err := jsonc.Patch([]byte(content), settings); err == nil {
			return string(out), nil
		}
	}
	return marshalSettings(settings)
}

func marshalSettings(settings map[string]any) (string, error) {
	data, err := MarshalJSONPretty(settings)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func cloneJSONMap(in map[string]any) map[string]any {
	data, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	// Numbers as their text, as parseSettingsObject reads them, so a clone
	// is equal to what it was cloned from.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return map[string]any{}
	}
	return out
}

func jsonDeepEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var av, bv any
	if err := json.Unmarshal(ab, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(bb, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}
