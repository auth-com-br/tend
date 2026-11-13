package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// talksToTheNetwork is every file that can open a connection off this
// machine, and when it does (#48: tend collects nothing, and talks to the
// network only when asked or in its documented release check). A file that
// imports net/http or dials TCP and is not here fails the test below:
// before it is added, its connection goes into docs/PRIVACY.md.
//
// It reads the source; what tend runs that reaches the network (gh, git,
// ssh, claude) is in docs/PRIVACY.md, and every one of those runs when the
// user asks.
var talksToTheNetwork = map[string]string{
	"internal/update/update.go":        "the release check (off with [update] version_check) and `tend update`",
	"internal/github/gitlab.go":        "the issues panel, for a GitLab project, when it is opened",
	"internal/glitchtip/glitchtip.go":  "the errors panel, when it is opened",
	"internal/tickets/tickets.go":      "the tickets panel, when it is opened",
	"internal/tickets/clickup.go":      "the tickets panel (ClickUp)",
	"internal/tickets/jira.go":         "the tickets panel (Jira)",
	"internal/tickets/linear.go":       "the tickets panel (Linear)",
	"internal/tickets/issuegateway.go": "the tickets panel (Issue Gateway)",
	"internal/mcp/probe.go":            "testing an MCP server, when asked to",
	"internal/web/push.go":             "push to a phone that turned notifications on",
	"internal/web/server.go":           "the phone page's server: it listens, it does not call out",
	"internal/web/phone.go":            "the phone page's routes: it listens, it does not call out",
	"cmd/tend/web.go":                  "`tend web`: it listens, it does not call out",
	"internal/agents/agents.go":        "whether a memory tool listens on 127.0.0.1: this machine only",
}

// TestOnlyTheFilesListedTalkToTheNetwork: no file reaches the network that
// is not in the list above with when it does. If it regresses — a new
// library, a new check at start — tend quietly starts calling somewhere,
// which is what #48 promised it would not.
func TestOnlyTheFilesListedTalkToTheNetwork(t *testing.T) {
	root := filepath.Join("..", "..")
	dial := regexp.MustCompile(`net\.(Dial|DialTimeout|Listen)\("(tcp|tcp4|tcp6|udp)"|net\.Dialer\{|DialContext\(`)
	found := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				if imp.Path.Value == `"net/http"` {
					found[rel] = true
				}
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if dial.Match(src) {
				found[rel] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var unlisted, gone []string
	for f := range found {
		if _, ok := talksToTheNetwork[f]; !ok {
			unlisted = append(unlisted, f)
		}
	}
	for f := range talksToTheNetwork {
		if !found[f] {
			gone = append(gone, f)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(gone)
	if len(unlisted) > 0 {
		t.Errorf("these reach the network and are not listed (add each, with when, here and in docs/PRIVACY.md):\n  %s", strings.Join(unlisted, "\n  "))
	}
	if len(gone) > 0 {
		t.Errorf("listed but no longer reaching the network (take them off):\n  %s", strings.Join(gone, "\n  "))
	}
}
