package integration

import (
	"fmt"
	"os"
	"path/filepath"
)

// Install writes hooks for target into the agent's config directory and
// returns the human messages herdr prints for the same action.
func Install(target Target) ([]string, error) {
	if !target.Supported() {
		return nil, fmt.Errorf("%s integration is not supported on this platform", target.Label())
	}
	switch target {
	case TargetPi:
		path, err := InstallPi()
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("installed pi integration to %s", path)}, nil
	case TargetOmp:
		installed, err := InstallOmp()
		if err != nil {
			return nil, err
		}
		var messages []string
		if installed.RemovedLegacyPiExtension {
			messages = append(messages, fmt.Sprintf(
				"removed legacy pi integration from omp extension directory at %s",
				filepath.Join(filepath.Dir(installed.ExtensionPath), PiExtensionInstallName),
			))
		}
		messages = append(messages, fmt.Sprintf("installed omp integration to %s", installed.ExtensionPath))
		return messages, nil
	case TargetClaude:
		installed, err := InstallClaude()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed claude integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured claude settings at %s", installed.SettingsPath),
		}, nil
	case TargetCodex:
		installed, err := InstallCodex()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed codex integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured codex hooks at %s", installed.HooksPath),
			fmt.Sprintf("ensured codex config at %s", installed.ConfigPath),
			// Codex 0.169 runs a new or changed hook only once the person
			// trusts it — the "Hooks need review" screen at its start, or
			// /hooks — and one left untrusted never runs, silently: tend
			// then never hears Codex's conversations from it. Found with the
			// real Codex; the trust is the person's to give, since a
			// trusted hook runs outside Codex's sandbox.
			"codex runs it only once you trust it: answer \"Trust all\" on Codex's \"Hooks need review\" screen, or trust it in /hooks",
		}, nil
	case TargetCopilot:
		installed, err := InstallCopilot()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed copilot integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured copilot settings at %s", installed.SettingsPath),
		}, nil
	case TargetDevin:
		installed, err := InstallDevin()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed devin integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured devin settings at %s", installed.SettingsPath),
		}, nil
	case TargetDroid:
		installed, err := InstallDroid()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed droid integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured droid settings at %s", installed.SettingsPath),
		}, nil
	case TargetKimi:
		installed, err := InstallKimi()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed kimi integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured kimi config at %s", installed.ConfigPath),
		}, nil
	case TargetOpencode:
		installed, err := InstallOpencode()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed opencode integration plugin to %s", installed.PluginPath),
			fmt.Sprintf("installed opencode tui plugin to %s", installed.TUIPluginPath),
		}, nil
	case TargetKilo:
		installed, err := InstallKilo()
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("installed kilo integration to %s", installed.PluginPath)}, nil
	case TargetHermes:
		installed, err := InstallHermes()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed hermes integration plugin to %s", installed.PluginDir),
			fmt.Sprintf("ensured hermes config at %s", installed.ConfigPath),
		}, nil
	case TargetQoderCLI:
		installed, err := InstallQodercli()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed qodercli integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured qodercli settings at %s", installed.SettingsPath),
		}, nil
	case TargetQwen:
		installed, err := InstallQwen()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed qwen integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured qwen settings at %s", installed.SettingsPath),
		}, nil
	case TargetCursor:
		installed, err := InstallCursor()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed cursor integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured cursor hooks at %s", installed.HooksPath),
		}, nil
	case TargetMastracode:
		installed, err := InstallMastracode()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed mastracode integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured mastracode hooks at %s", installed.HooksPath),
		}, nil
	case TargetAntigravityCLI:
		installed, err := InstallAntigravityCLI()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed antigravity-cli integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured antigravity-cli hooks at %s", installed.HooksPath),
		}, nil
	case TargetGrok:
		installed, err := InstallGrok()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed grok integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured grok hook config at %s", installed.ConfigPath),
		}, nil
	case TargetLetta:
		installed, err := InstallLetta()
		if err != nil {
			return nil, err
		}
		return []string{
			fmt.Sprintf("installed letta integration hook to %s", installed.HookPath),
			fmt.Sprintf("ensured letta settings at %s", installed.SettingsPath),
		}, nil
	default:
		return nil, fmt.Errorf("integration: unknown target %q", target)
	}
}

// Uninstall removes hooks for target and returns human messages.
func Uninstall(target Target) ([]string, error) {
	if !target.Supported() {
		return nil, fmt.Errorf("%s integration is not supported on this platform", target.Label())
	}
	switch target {
	case TargetPi:
		result, err := UninstallPi()
		if err != nil {
			return nil, err
		}
		if result.RemovedExtension {
			return []string{fmt.Sprintf("removed pi integration at %s", result.ExtensionPath)}, nil
		}
		return []string{fmt.Sprintf("no pi integration found at %s", result.ExtensionPath)}, nil
	case TargetOmp:
		result, err := UninstallOmp()
		if err != nil {
			return nil, err
		}
		if result.RemovedExtension {
			return []string{fmt.Sprintf("removed omp integration at %s", result.ExtensionPath)}, nil
		}
		return []string{fmt.Sprintf("no omp integration found at %s", result.ExtensionPath)}, nil
	case TargetClaude:
		result, err := UninstallClaude()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("claude", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetCodex:
		result, err := UninstallCodex()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("codex", result.HookPath, result.HooksPath, result.RemovedHookFile, result.UpdatedHooks), nil
	case TargetCopilot:
		result, err := UninstallCopilot()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("copilot", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetDevin:
		result, err := UninstallDevin()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("devin", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetDroid:
		result, err := UninstallDroid()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("droid", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetKimi:
		result, err := UninstallKimi()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("kimi", result.HookPath, result.ConfigPath, result.RemovedHookFile, result.UpdatedConfig), nil
	case TargetOpencode:
		result, err := UninstallOpencode()
		if err != nil {
			return nil, err
		}
		var messages []string
		if result.RemovedPlugin {
			messages = append(messages, fmt.Sprintf("removed opencode plugin at %s", result.PluginPath))
		} else {
			messages = append(messages, fmt.Sprintf("no opencode plugin found at %s", result.PluginPath))
		}
		if result.RemovedTUIPlugin {
			messages = append(messages, fmt.Sprintf("removed opencode tui plugin at %s", result.TUIPluginPath))
		}
		return messages, nil
	case TargetKilo:
		result, err := UninstallKilo()
		if err != nil {
			return nil, err
		}
		if result.RemovedPlugin {
			return []string{fmt.Sprintf("removed kilo integration at %s", result.PluginPath)}, nil
		}
		return []string{fmt.Sprintf("no kilo integration found at %s", result.PluginPath)}, nil
	case TargetHermes:
		result, err := UninstallHermes()
		if err != nil {
			return nil, err
		}
		var messages []string
		if result.RemovedPluginDir {
			messages = append(messages, fmt.Sprintf("removed hermes plugin at %s", result.PluginDir))
		} else {
			messages = append(messages, fmt.Sprintf("no hermes plugin found at %s", result.PluginDir))
		}
		if result.UpdatedConfig {
			messages = append(messages, fmt.Sprintf("removed tend hermes plugin from %s", result.ConfigPath))
		}
		return messages, nil
	case TargetQoderCLI:
		result, err := UninstallQodercli()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("qodercli", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetQwen:
		result, err := UninstallQwen()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("qwen", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	case TargetCursor:
		result, err := UninstallCursor()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("cursor", result.HookPath, result.HooksPath, result.RemovedHookFile, result.UpdatedHooks), nil
	case TargetMastracode:
		result, err := UninstallMastracode()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("mastracode", result.HookPath, result.HooksPath, result.RemovedHookFile, result.UpdatedHooks), nil
	case TargetAntigravityCLI:
		result, err := UninstallAntigravityCLI()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("antigravity-cli", result.HookPath, result.HooksPath, result.RemovedHookFile, result.UpdatedHooks), nil
	case TargetGrok:
		result, err := UninstallGrok()
		if err != nil {
			return nil, err
		}
		messages := uninstallHookMessages("grok", result.HookPath, result.ConfigPath, result.RemovedHookFile, result.RemovedConfigFile)
		return messages, nil
	case TargetLetta:
		result, err := UninstallLetta()
		if err != nil {
			return nil, err
		}
		return uninstallHookMessages("letta", result.HookPath, result.SettingsPath, result.RemovedHookFile, result.UpdatedSettings), nil
	default:
		return nil, fmt.Errorf("integration: unknown target %q", target)
	}
}

func uninstallHookMessages(label, hookPath, settingsPath string, removedHook, updatedSettings bool) []string {
	var messages []string
	if removedHook {
		messages = append(messages, fmt.Sprintf("removed %s hook at %s", label, hookPath))
	} else {
		messages = append(messages, fmt.Sprintf("no %s hook found at %s", label, hookPath))
	}
	if updatedSettings {
		messages = append(messages, fmt.Sprintf("removed tend %s hook entry from %s", label, settingsPath))
	} else {
		messages = append(messages, fmt.Sprintf("no tend %s hook entry found in %s", label, settingsPath))
	}
	return messages
}

// StatusKind is whether an integration's on-disk hook matches what this build ships.
type StatusKind string

const (
	StatusNotInstalled StatusKind = "not_installed"
	StatusCurrent      StatusKind = "current"
	StatusOutdated     StatusKind = "outdated"
)

// Status describes one integration on disk.
type Status struct {
	Target           Target
	Path             string
	State            StatusKind
	InstalledVersion *uint32
	ExpectedVersion  uint32
	Available        bool
}

// Info is what the automation socket reports for integration.list.
type Info struct {
	Target    string     `json:"target"`
	Label     string     `json:"label"`
	Command   string     `json:"command"`
	Available bool       `json:"available"`
	State     StatusKind `json:"state"`
}

// ExpectedVersion is the version stamped in this build's asset for target.
func ExpectedVersion(target Target) uint32 {
	switch target {
	case TargetPi:
		return PiIntegrationVersion
	case TargetOmp:
		return OmpIntegrationVersion
	case TargetClaude:
		return ClaudeIntegrationVersion
	case TargetCodex:
		return CodexIntegrationVersion
	case TargetCopilot:
		return CopilotIntegrationVersion
	case TargetDevin:
		return DevinIntegrationVersion
	case TargetDroid:
		return DroidIntegrationVersion
	case TargetKimi:
		return KimiIntegrationVersion
	case TargetOpencode:
		return OpenCodeIntegrationVersion
	case TargetKilo:
		return KiloIntegrationVersion
	case TargetHermes:
		return HermesIntegrationVersion
	case TargetQoderCLI:
		return QodercliIntegrationVersion
	case TargetQwen:
		return QwenIntegrationVersion
	case TargetCursor:
		return CursorIntegrationVersion
	case TargetMastracode:
		return MastracodeIntegrationVersion
	case TargetAntigravityCLI:
		return AntigravityCLIIntegrationVersion
	case TargetGrok:
		return GrokIntegrationVersion
	case TargetLetta:
		return LettaIntegrationVersion
	default:
		return 0
	}
}

// MarkerPath is where the version marker for target is expected to live.
func MarkerPath(target Target) (string, error) {
	switch target {
	case TargetPi:
		dir, err := PiExtensionDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, PiExtensionInstallName), nil
	case TargetOmp:
		dir, err := OmpExtensionDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, OmpExtensionInstallName), nil
	case TargetClaude:
		dir, err := ClaudeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", ClaudeHookInstallName), nil
	case TargetCodex:
		dir, err := CodexDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, CodexHookInstallName), nil
	case TargetCopilot:
		dir, err := CopilotDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", CopilotHookInstallName), nil
	case TargetDevin:
		dir, err := DevinDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, DevinHookInstallName), nil
	case TargetDroid:
		dir, err := DroidDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", DroidHookInstallName), nil
	case TargetKimi:
		dir, err := KimiDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", KimiHookInstallName), nil
	case TargetOpencode:
		dir, err := OpencodeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "plugins", OpenCodePluginInstallName), nil
	case TargetKilo:
		dir, err := KiloDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "plugin", KiloPluginInstallName), nil
	case TargetHermes:
		dir, err := HermesPluginDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, HermesPluginInitInstallName), nil
	case TargetQoderCLI:
		dir, err := QodercliDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", QodercliHookInstallName), nil
	case TargetQwen:
		dir, err := QwenDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", QwenHookInstallName), nil
	case TargetCursor:
		dir, err := CursorDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, CursorHookInstallName), nil
	case TargetMastracode:
		dir, err := MastracodeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", MastracodeHookInstallName), nil
	case TargetAntigravityCLI:
		dir, err := AntigravityCLIDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", AntigravityCLIHookInstallName), nil
	case TargetGrok:
		dir, err := GrokDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", GrokHookInstallName), nil
	case TargetLetta:
		dir, err := LettaDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hooks", LettaHookInstallName), nil
	default:
		return "", fmt.Errorf("integration: unknown target %q", target)
	}
}

// Statuses returns the on-disk state of every supported target.
func Statuses() []Status {
	out := make([]Status, 0, len(allTargets))
	for _, target := range allTargets {
		if !target.Supported() {
			continue
		}
		out = append(out, statusOf(target))
	}
	return out
}

// ListInfos is integration.list: recommendations with availability.
func ListInfos() []Info {
	statuses := Statuses()
	out := make([]Info, 0, len(statuses))
	for _, st := range statuses {
		cmds := st.Target.Commands()
		cmd := ""
		if len(cmds) > 0 {
			cmd = cmds[0]
		}
		out = append(out, Info{
			Target:    st.Target.Label(),
			Label:     st.Target.Label(),
			Command:   cmd,
			Available: st.Available || st.State != StatusNotInstalled,
			State:     st.State,
		})
	}
	return out
}

func statusOf(target Target) Status {
	expected := ExpectedVersion(target)
	st := Status{
		Target:          target,
		ExpectedVersion: expected,
		Available:       TargetAvailable(target),
		State:           StatusNotInstalled,
	}
	path, err := MarkerPath(target)
	if err != nil {
		return st
	}
	st.Path = path
	content, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if ver, ok := ReadInstalledVersion(content); ok {
		st.InstalledVersion = &ver
		if ver >= expected {
			st.State = StatusCurrent
		} else {
			st.State = StatusOutdated
		}
		return st
	}
	// File exists but has no recognisable marker: treat as outdated legacy.
	st.State = StatusOutdated
	return st
}

// TargetAvailable reports whether the agent's binary (or install layout) is
// present, which is how recommendations decide what to offer.
func TargetAvailable(target Target) bool {
	if !target.Supported() {
		return false
	}
	for _, name := range target.Commands() {
		if commandAvailable(name) {
			return true
		}
	}
	return false
}

func commandAvailable(command string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
