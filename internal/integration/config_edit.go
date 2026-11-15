package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/auth-com-br/tend/internal/jsonc"
)

// MarshalJSONPretty serializes v the way herdr's installers do: two-space indent,
// no trailing newline, HTML-safe escaping disabled so hook commands stay readable.
func MarshalJSONPretty(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

// EnsureHooksObject returns the top-level "hooks" object, creating it when
// absent. settings must be a JSON object; otherwise the error names the file
// path so installers can surface a parse failure to the user.
func EnsureHooksObject(
	settings map[string]any,
	settingsPath string,
	rootDescription string,
	hooksDescription string,
) (map[string]any, error) {
	if settings == nil {
		return nil, fmt.Errorf("%s at %s must be a JSON object", rootDescription, settingsPath)
	}

	hooksValue, ok := settings["hooks"]
	if !ok {
		hooks := map[string]any{}
		settings["hooks"] = hooks
		return hooks, nil
	}

	hooks, ok := hooksValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s at %s must be a JSON object", hooksDescription, settingsPath)
	}
	return hooks, nil
}

// HooksObjectIfPresent returns the "hooks" object when it already exists.
func HooksObjectIfPresent(
	settings map[string]any,
	settingsPath string,
	rootDescription string,
	hooksDescription string,
) (map[string]any, bool, error) {
	if settings == nil {
		return nil, false, fmt.Errorf("%s at %s must be a JSON object", rootDescription, settingsPath)
	}

	hooksValue, ok := settings["hooks"]
	if !ok {
		return nil, false, nil
	}

	hooks, ok := hooksValue.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%s at %s must be a JSON object", hooksDescription, settingsPath)
	}
	return hooks, true, nil
}

// EnsureCommandHook appends a nested Claude/Codex-style hook group when the
// exact command is not already registered for event. Repeated calls are a
// no-op so installers can safely rewrite settings. matcher may be empty.
func EnsureCommandHook(
	hooks map[string]any,
	event string,
	command string,
	timeout uint64,
	matcher string,
) error {
	entries, err := hookEntries(hooks, event)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hookEntriesValue, ok := entryMap["hooks"]
		if !ok {
			continue
		}
		hookEntries, ok := hookEntriesValue.([]any)
		if !ok {
			continue
		}
		for _, hook := range hookEntries {
			if IsMatchingCommandHook(hook, command) {
				return nil
			}
		}
	}

	entry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
				"timeout": timeout,
			},
		},
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	hooks[event] = append(entries, entry)
	return nil
}

// RemoveCommandHook deletes nested command hooks matching command from event.
func RemoveCommandHook(hooks map[string]any, event string, command string) (bool, error) {
	entriesValue, ok := hooks[event]
	if !ok {
		return false, nil
	}

	entries, ok := entriesValue.([]any)
	if !ok {
		return false, fmt.Errorf("hook entries for %s must be an array", event)
	}

	removed := false
	filtered := entries[:0]
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}

		hookEntriesValue, ok := entryMap["hooks"]
		if !ok {
			filtered = append(filtered, entry)
			continue
		}

		hookEntries, ok := hookEntriesValue.([]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}

		before := len(hookEntries)
		kept := hookEntries[:0]
		for _, hook := range hookEntries {
			if IsMatchingCommandHook(hook, command) {
				continue
			}
			kept = append(kept, hook)
		}
		hookEntries = kept
		if len(hookEntries) != before {
			removed = true
		}
		if len(hookEntries) == 0 {
			continue
		}
		entryMap["hooks"] = hookEntries
		filtered = append(filtered, entryMap)
	}

	if len(filtered) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = filtered
	}
	return removed, nil
}

// RemoveSimpleCommandHook deletes Cursor's minimal `{ "command": "..." }`
// entries. Keep this separate from RemoveCommandHook so uninstall does not
// rewrite unrelated hooks in another agent's native format.
func RemoveSimpleCommandHook(hooks map[string]any, event string, command string) (bool, error) {
	entriesValue, ok := hooks[event]
	if !ok {
		return false, nil
	}

	entries, ok := entriesValue.([]any)
	if !ok {
		return false, fmt.Errorf("hook entries for %s must be an array", event)
	}

	before := len(entries)
	filtered := entries[:0]
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		cmd, _ := entryMap["command"].(string)
		if cmd == command {
			continue
		}
		filtered = append(filtered, entry)
	}

	removed := len(filtered) != before
	if len(filtered) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = filtered
	}
	return removed, nil
}

// EnsureSimpleCommandHook appends Cursor's minimal `{ "command": "..." }`
// entry when the exact command is not already present.
func EnsureSimpleCommandHook(hooks map[string]any, event, command string) error {
	entries, err := hookEntries(hooks, event)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := entryMap["command"].(string)
		if cmd == command {
			return nil
		}
	}
	hooks[event] = append(entries, map[string]any{"command": command})
	return nil
}

// DirectCommandField is the Copilot settings key for the shell command:
// "bash" on Unix. Windows would use "powershell"; tend is Unix-only for now.
func DirectCommandField() string {
	return "bash"
}

// IsMatchingDirectCommandEntry reports whether a Copilot-style flat entry
// refers to command via command, bash, or powershell.
func IsMatchingDirectCommandEntry(entry any, command string) bool {
	entryMap, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"command", "bash", "powershell"} {
		if v, _ := entryMap[key].(string); v == command {
			return true
		}
	}
	return false
}

// EnsureDirectCommandHook upserts a Copilot-style flat command entry using
// DirectCommandField and timeoutSec. Matching entries are rewritten in place
// so an older command/bash field migrates to the canonical key.
func EnsureDirectCommandHook(hooks map[string]any, event, command string, timeoutSec uint64, matcher string) error {
	entries, err := hookEntries(hooks, event)
	if err != nil {
		return err
	}
	field := DirectCommandField()
	for i, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hookType, _ := entryMap["type"].(string)
		if hookType != "command" || !IsMatchingDirectCommandEntry(entry, command) {
			continue
		}
		delete(entryMap, "command")
		delete(entryMap, "bash")
		delete(entryMap, "powershell")
		entryMap[field] = command
		entryMap["timeoutSec"] = timeoutSec
		if matcher != "" {
			entryMap["matcher"] = matcher
		} else {
			delete(entryMap, "matcher")
		}
		entries[i] = entryMap
		hooks[event] = entries
		return nil
	}

	entry := map[string]any{
		"type":       "command",
		field:        command,
		"timeoutSec": timeoutSec,
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	hooks[event] = append(entries, entry)
	return nil
}

// RemoveDirectCommandHook deletes Copilot-style flat command entries matching
// command (via command/bash/powershell).
func RemoveDirectCommandHook(hooks map[string]any, event, command string) (bool, error) {
	entriesValue, ok := hooks[event]
	if !ok {
		return false, nil
	}
	entries, ok := entriesValue.([]any)
	if !ok {
		return false, fmt.Errorf("hook entries for %s must be an array", event)
	}
	before := len(entries)
	filtered := entries[:0]
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		hookType, _ := entryMap["type"].(string)
		if hookType == "command" && IsMatchingDirectCommandEntry(entry, command) {
			continue
		}
		filtered = append(filtered, entry)
	}
	removed := len(filtered) != before
	if len(filtered) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = filtered
	}
	return removed, nil
}

// RemoveDirectHookCommands removes every Copilot command variant for hookPath.
func RemoveDirectHookCommands(hooks map[string]any, event, hookPath, action string) (bool, error) {
	removed := false
	for _, command := range HookCommandVariants(hookPath, action) {
		got, err := RemoveDirectCommandHook(hooks, event, command)
		if err != nil {
			return removed, err
		}
		removed = removed || got
	}
	return removed, nil
}

// RemoveHookCommands removes every command variant tend may have written for
// hookPath, including legacy bash paths from older installs.
func RemoveHookCommands(hooks map[string]any, event string, hookPath string, action string) (bool, error) {
	removed := false
	for _, command := range HookCommandVariants(hookPath, action) {
		got, err := RemoveCommandHook(hooks, event, command)
		if err != nil {
			return removed, err
		}
		removed = removed || got
	}
	return removed, nil
}

// IsMatchingCommandHook reports whether hook is a nested command hook for
// command.
func IsMatchingCommandHook(hook any, command string) bool {
	hookMap, ok := hook.(map[string]any)
	if !ok {
		return false
	}
	hookType, _ := hookMap["type"].(string)
	hookCommand, _ := hookMap["command"].(string)
	return hookType == "command" && hookCommand == command
}

func hookEntries(hooks map[string]any, event string) ([]any, error) {
	entriesValue, ok := hooks[event]
	if !ok {
		entries := []any{}
		hooks[event] = entries
		return entries, nil
	}

	entries, ok := entriesValue.([]any)
	if !ok {
		return nil, fmt.Errorf("hook entries for %s must be an array", event)
	}
	return entries, nil
}

// EnsureFlatCommandHook appends a MastraCode-style flat command entry
// `{type, command, timeout, description}` when the command is not already present.
func EnsureFlatCommandHook(hooks map[string]any, event, command string, timeoutMS uint64, description string) error {
	entries, err := hookEntries(hooks, event)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hookType, _ := entryMap["type"].(string)
		hookCommand, _ := entryMap["command"].(string)
		if hookType == "command" && hookCommand == command {
			return nil
		}
	}
	hooks[event] = append(entries, map[string]any{
		"type":        "command",
		"command":     command,
		"timeout":     timeoutMS,
		"description": description,
	})
	return nil
}

// RemoveFlatCommandHook deletes flat `{type:command, command:…}` entries.
func RemoveFlatCommandHook(hooks map[string]any, event, command string) (bool, error) {
	entriesValue, ok := hooks[event]
	if !ok {
		return false, nil
	}
	entries, ok := entriesValue.([]any)
	if !ok {
		return false, fmt.Errorf("hook entries for %s must be an array", event)
	}
	before := len(entries)
	filtered := entries[:0]
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		hookType, _ := entryMap["type"].(string)
		hookCommand, _ := entryMap["command"].(string)
		if hookType == "command" && hookCommand == command {
			continue
		}
		filtered = append(filtered, entry)
	}
	removed := len(filtered) != before
	if len(filtered) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = filtered
	}
	return removed, nil
}

// ParseJSONObject parses content as a JSON object. description names the file
// in error messages (e.g. "qodercli settings").
func ParseJSONObject(content, path, description string) (map[string]any, error) {
	// JSONC, numbers as their text: a file with comments is read, and a
	// value tend did not change is written back as it was (WriteJSONPretty).
	root, err := jsonc.Decode([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s at %s must be a JSON object", description, path)
	}
	return obj, nil
}

// WriteJSONPretty writes v to path. A file already there is patched, so
// its comments, key order and layout stay where the change is not (issue
// #61; herdr's jsonc-parser rewrite); a new file, or one the patch cannot
// keep, is written whole with two-space indent and a trailing newline.
func WriteJSONPretty(path string, v any) error {
	if old, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(old)) > 0 {
		if out, err := jsonc.Patch(old, v); err == nil {
			return os.WriteFile(path, out, 0o644)
		}
	}
	data, err := MarshalJSONPretty(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Kimi config.toml markers. Install rewrites the tend-owned block; uninstall
// also strips a leftover herdr block so a prior herdr install does not linger.
const (
	KimiConfigBlockBegin       = "# >>> tend kimi integration"
	KimiConfigBlockEnd         = "# <<< tend kimi integration"
	legacyKimiConfigBlockBegin = "# >>> herdr kimi integration"
	legacyKimiConfigBlockEnd   = "# <<< herdr kimi integration"
)

// BuildKimiConfigWithHooks replaces any tend/herdr kimi block with a fresh one.
func BuildKimiConfigWithHooks(content, hookPath string, events []KimiHookEvent) string {
	result := strings.TrimRight(RemoveKimiConfigBlock(content), "\n")
	if result != "" {
		result += "\n\n"
	}
	var b strings.Builder
	b.WriteString(result)
	b.WriteString(KimiConfigBlockBegin)
	b.WriteByte('\n')
	for _, ev := range events {
		b.WriteString(kimiHookTable(ev.Event, ev.Matcher, hookPath, ev.Action))
	}
	b.WriteString(KimiConfigBlockEnd)
	b.WriteByte('\n')
	return b.String()
}

// KimiHookEvent is one [[hooks]] row written into kimi's config.toml.
type KimiHookEvent struct {
	Event   string
	Matcher string // empty means no matcher line
	Action  string
}

func kimiHookTable(event, matcher, hookPath, action string) string {
	command := HookCommand(hookPath, action)
	var b strings.Builder
	b.WriteString("[[hooks]]\n")
	b.WriteString("event = ")
	b.WriteString(tomlBasicString(event))
	b.WriteByte('\n')
	if matcher != "" {
		b.WriteString("matcher = ")
		b.WriteString(tomlBasicString(matcher))
		b.WriteByte('\n')
	}
	b.WriteString("command = ")
	b.WriteString(tomlBasicString(command))
	b.WriteByte('\n')
	b.WriteString("timeout = 10\n\n")
	return b.String()
}

// RemoveKimiConfigBlock strips tend and legacy herdr kimi integration blocks.
func RemoveKimiConfigBlock(content string) string {
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := make([]string, 0)
	inBlock := false
	removed := false
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if trim == KimiConfigBlockBegin || trim == legacyKimiConfigBlockBegin {
			inBlock = true
			removed = true
			continue
		}
		if inBlock {
			if trim == KimiConfigBlockEnd || trim == legacyKimiConfigBlockEnd {
				inBlock = false
			}
			continue
		}
		lines = append(lines, line)
	}
	if !removed {
		return content
	}
	result := joinTOMLLines(lines, trailingNewline)
	for strings.HasSuffix(result, "\n\n") {
		result = result[:len(result)-1]
	}
	if result == "\n" {
		return ""
	}
	return result
}

func joinTOMLLines(lines []string, trailingNewline bool) string {
	result := strings.Join(lines, "\n")
	if trailingNewline || result == "" {
		result += "\n"
	}
	return result
}

// BuildCodexConfigWithHooks ensures top-level [features] has hooks = true and
// drops the deprecated codex_hooks key. Nested tables like
// [profiles.work.features] are left alone — herdr only migrates the top-level
// block so user profile overrides stay intact.
func BuildCodexConfigWithHooks(content string) string {
	trailingNewline := strings.HasSuffix(content, "\n")
	// Match Rust str::lines(): split on \n and drop the final empty element
	// that a terminating newline would otherwise leave.
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
		if trailingNewline && len(lines) > 0 {
			lines = lines[:len(lines)-1]
		}
	}
	inTopLevelFeatures := false
	var featuresHeaderIndex *int
	var hooksIndex *int
	var deprecatedHooksIndexes []int

	for index, line := range lines {
		if header, ok := tomlTableHeader(line); ok {
			inTopLevelFeatures = header == "[features]"
			if inTopLevelFeatures && featuresHeaderIndex == nil {
				idx := index
				featuresHeaderIndex = &idx
			}
			continue
		}
		if !inTopLevelFeatures {
			continue
		}
		if isTOMLKey(line, "codex_hooks") {
			deprecatedHooksIndexes = append(deprecatedHooksIndexes, index)
		} else if isTOMLKey(line, "hooks") {
			idx := index
			hooksIndex = &idx
		}
	}

	if hooksIndex != nil {
		lines[*hooksIndex] = "hooks = true"
	}

	for i := len(deprecatedHooksIndexes) - 1; i >= 0; i-- {
		index := deprecatedHooksIndexes[i]
		lines = append(lines[:index], lines[index+1:]...)
		// hooksIndex is only used above; after removal we either return via
		// insert or join, so no need to adjust it.
	}

	if hooksIndex == nil {
		if featuresHeaderIndex != nil {
			insertAt := *featuresHeaderIndex + 1
			lines = append(lines[:insertAt], append([]string{"hooks = true"}, lines[insertAt:]...)...)
			return joinTOMLLines(lines, trailingNewline)
		}

		result := strings.TrimRight(content, "\n")
		if result != "" {
			result += "\n\n"
		}
		result += "[features]\nhooks = true\n"
		return result
	}

	return joinTOMLLines(lines, trailingNewline)
}

func tomlTableHeader(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	var headerEnd int
	if strings.HasPrefix(trimmed, "[[") {
		idx := strings.Index(trimmed, "]]")
		if idx < 0 {
			return "", false
		}
		headerEnd = idx + 2
	} else {
		idx := strings.Index(trimmed, "]")
		if idx < 0 {
			return "", false
		}
		headerEnd = idx + 1
	}
	header := trimmed[:headerEnd]
	rest := strings.TrimLeft(trimmed[headerEnd:], " \t")
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", false
	}
	return header, true
}

func isTOMLKey(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, key) {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(trimmed[len(key):], " \t"), "=")
}

func tomlBasicString(value string) string {
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, ch := range value {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if ch <= 0x1f || ch == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, ch)
			} else {
				b.WriteRune(ch)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
