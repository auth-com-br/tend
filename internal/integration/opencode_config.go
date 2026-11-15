package integration

import (
	"fmt"
	"github.com/auth-com-br/tend/internal/jsonc"
	"os"
	"path/filepath"
)

const tuiConfigName = "tui.jsonc"

// TUIConfigPath returns configDir/tui.jsonc.
func TUIConfigPath(configDir string) string {
	return filepath.Join(configDir, tuiConfigName)
}

// ValidateTUIPluginConfig fails when tui.jsonc's "plugin" or cli.json's
// "plugins" exists but is not an array — install must not overwrite a broken
// config.
func ValidateTUIPluginConfig(configDir string) error {
	if err := validatePluginConfig(TUIConfigPath(configDir), "plugin"); err != nil {
		return err
	}
	return validatePluginConfig(filepath.Join(configDir, "cli.json"), "plugins")
}

func validatePluginConfig(configPath, key string) error {
	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return nil
	}
	root, err := readOpencodeConfigObject(configPath)
	if err != nil {
		return err
	}
	if raw, ok := root[key]; ok {
		if _, ok := raw.([]any); !ok {
			return invalidPluginList(configPath)
		}
	}
	return nil
}

// AddTUIPlugin registers pluginSpec under tui.jsonc's "plugin" array.
func AddTUIPlugin(configDir, pluginSpec string) (string, error) {
	return addPlugin(TUIConfigPath(configDir), "plugin", pluginSpec)
}

// AddCLIPlugin registers pluginSpec under cli.json's "plugins" array. Returns
// ("", nil) when OpenCode still has V1 migration sources (tui.json / kv.json)
// and cli.json is absent — writing early would skip OpenCode's own import.
func AddCLIPlugin(configDir, stateDir, pluginSpec string) (string, error) {
	path := filepath.Join(configDir, "cli.json")
	if _, err := os.Stat(path); os.IsNotExist(err) && cliMigrationPending(configDir, stateDir) {
		return "", nil
	}
	return addPlugin(path, "plugins", pluginSpec)
}

func cliMigrationPending(configDir, stateDir string) bool {
	if _, err := os.Stat(filepath.Join(configDir, "tui.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(stateDir, "kv.json")); err == nil {
		return true
	}
	return false
}

func addPlugin(configPath, key, pluginSpec string) (string, error) {
	var root map[string]any
	if data, err := os.ReadFile(configPath); err == nil {
		parsed, err := parseOpencodeConfigObject(string(data), configPath)
		if err != nil {
			return "", err
		}
		root = parsed
	} else if os.IsNotExist(err) {
		root = map[string]any{}
	} else {
		return "", err
	}

	plugins, err := pluginList(root, key, configPath)
	if err != nil {
		return "", err
	}
	for _, entry := range plugins {
		if pluginEntryMatches(entry, pluginSpec) {
			return configPath, nil
		}
	}
	root[key] = append(plugins, pluginSpec)
	if err := WriteJSONPretty(configPath, root); err != nil {
		return "", err
	}
	return configPath, nil
}

// RemoveTUIPlugin drops pluginSpec from tui.jsonc's "plugin" array.
func RemoveTUIPlugin(configDir, pluginSpec string) (bool, error) {
	return removePlugin(TUIConfigPath(configDir), "plugin", pluginSpec)
}

// RemoveCLIPlugin drops pluginSpec from cli.json's "plugins" array.
func RemoveCLIPlugin(configDir, pluginSpec string) (bool, error) {
	return removePlugin(filepath.Join(configDir, "cli.json"), "plugins", pluginSpec)
}

func removePlugin(configPath, key, pluginSpec string) (bool, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	root, err := readOpencodeConfigObject(configPath)
	if err != nil {
		return false, err
	}
	raw, ok := root[key]
	if !ok {
		return false, nil
	}
	plugins, ok := raw.([]any)
	if !ok {
		return false, invalidPluginList(configPath)
	}
	kept := make([]any, 0, len(plugins))
	removed := false
	for _, entry := range plugins {
		if pluginEntryMatches(entry, pluginSpec) {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false, nil
	}
	if len(kept) == 0 {
		delete(root, key)
	} else {
		root[key] = kept
	}
	if err := WriteJSONPretty(configPath, root); err != nil {
		return false, err
	}
	return true, nil
}

// TUIPluginIsConfigured reports whether pluginSpec is listed in tui.jsonc.
func TUIPluginIsConfigured(configDir, pluginSpec string) bool {
	return pluginIsConfigured(TUIConfigPath(configDir), "plugin", pluginSpec)
}

// CLIPluginIsConfigured reports whether pluginSpec is listed in cli.json.
func CLIPluginIsConfigured(configDir, pluginSpec string) bool {
	return pluginIsConfigured(filepath.Join(configDir, "cli.json"), "plugins", pluginSpec)
}

func pluginIsConfigured(configPath, key, pluginSpec string) bool {
	root, err := readOpencodeConfigObject(configPath)
	if err != nil {
		return false
	}
	plugins, err := pluginList(root, key, configPath)
	if err != nil {
		return false
	}
	for _, entry := range plugins {
		if pluginEntryMatches(entry, pluginSpec) {
			return true
		}
	}
	return false
}

func pluginList(root map[string]any, key, configPath string) ([]any, error) {
	raw, ok := root[key]
	if !ok {
		return nil, nil
	}
	plugins, ok := raw.([]any)
	if !ok {
		return nil, invalidPluginList(configPath)
	}
	return plugins, nil
}

func pluginEntryMatches(entry any, pluginSpec string) bool {
	switch v := entry.(type) {
	case string:
		return v == pluginSpec
	case map[string]any:
		if pkg, ok := v["package"].(string); ok {
			return pkg == pluginSpec
		}
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s == pluginSpec
			}
		}
	}
	return false
}

func readOpencodeConfigObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseOpencodeConfigObject(string(data), path)
}

func parseOpencodeConfigObject(content, path string) (map[string]any, error) {
	// tui.jsonc is JSONC: comments are read, and kept when it is written
	// back (WriteJSONPretty patches it).
	root, err := jsonc.Decode([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenCode TUI config at %s: %w", path, err)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OpenCode TUI config at %s must be a JSON object", path)
	}
	return obj, nil
}

func invalidPluginList(path string) error {
	return fmt.Errorf("OpenCode TUI config plugin list at %s must be an array", path)
}
