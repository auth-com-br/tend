package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/preset"
	"github.com/auth-com-br/tend/internal/session"
)

// presetDir is where presets named without a path are looked for: beside
// the settings file, in layouts/.
func presetDir() string {
	path, err := config.Path()
	if err != nil {
		return "layouts"
	}
	return filepath.Join(filepath.Dir(path), "layouts")
}

// openPreset builds every space and tab of a preset in the session, then
// starts what each pane runs. Commands are typed into each pane's shell, as
// tmuxp does, so a pane whose command ends is a prompt rather than gone;
// agents go through agent.start, so a policy that forbids one says so.
func openPreset(sessionName, nameOrPath string) error {
	file, path, err := preset.Load(nameOrPath, presetDir())
	if err != nil {
		return err
	}
	here, _ := os.Getwd()
	for si, sp := range file.Spaces {
		name := sp.Name
		if name == "" {
			name = fmt.Sprintf("%s %d", filepath.Base(path), si+1)
		}
		dir := preset.Expand(sp.Dir, here)
		ws, err := apiCall(sessionName, api.MethodWorkspaceCreate, map[string]any{"name": name, "dir": dir}, false)
		if err != nil {
			return fmt.Errorf("space %s: %w", name, err)
		}
		info, _ := ws["workspace"].(map[string]any)
		wsID := text(info["workspace_id"])
		tabs := sp.Tabs
		if len(tabs) == 0 {
			// A space with no tab says only where it is: one shell there.
			tabs = []preset.Tab{{}}
		}
		for ti, tab := range tabs {
			if tab.Name == "" {
				tab.Name = fmt.Sprintf("tab %d", ti+1)
			}
			built, err := preset.Build(tab, dir)
			if err != nil {
				return fmt.Errorf("%s: space %s: %w", path, name, err)
			}
			applied, err := apiCall(sessionName, api.MethodLayoutApply, map[string]any{
				"workspace_id": wsID, "name": built.Name, "tree": built.Tree, "panes": built.Panes,
			}, true)
			if err != nil {
				return fmt.Errorf("space %s, %s: %w", name, built.Name, err)
			}
			tabID := text(applied["tab_id"])
			made, err := apiCall(sessionName, api.MethodLayoutExport, map[string]any{"tab_id": tabID}, false)
			if err != nil {
				return err
			}
			var tree struct {
				Tree session.LayoutSnapshot `json:"tree"`
			}
			raw, _ := json.Marshal(made)
			if err := json.Unmarshal(raw, &tree); err != nil {
				return err
			}
			panes := preset.Leaves(tree.Tree)
			if len(panes) != len(built.Leaves) {
				return fmt.Errorf("space %s, %s: built %d panes for %d", name, built.Name, len(panes), len(built.Leaves))
			}
			for i, leaf := range built.Leaves {
				pane := fmt.Sprintf("p_%d", panes[i])
				if leaf.Title != "" {
					if _, err := apiCall(sessionName, api.MethodPaneRename, map[string]any{"pane_id": pane, "label": leaf.Title}, false); err != nil {
						return err
					}
				}
				switch {
				case leaf.Agent != "":
					params := map[string]any{"pane_id": pane, "agent": leaf.Agent}
					if leaf.Title != "" {
						params["name"] = leaf.Title
					}
					if _, err := apiCall(sessionName, api.MethodAgentStart, params, false); err != nil {
						return fmt.Errorf("space %s, %s: %s: %w", name, built.Name, leaf.Agent, err)
					}
				case leaf.Run != "":
					if _, err := apiCall(sessionName, api.MethodPaneSendText, map[string]any{"pane_id": pane, "text": leaf.Run, "submit": true}, false); err != nil {
						return err
					}
				}
			}
			fmt.Fprintf(os.Stderr, "%s %s: %s, %d pane(s)\n", tag(), name, built.Name, len(built.Leaves))
		}
	}
	return nil
}
