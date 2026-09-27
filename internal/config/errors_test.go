package config

import (
	"strings"
	"testing"
)

// TestErrorSourcesAreTheOldServerAndTheAddedOnes: the server in [errors]
// is a source like those in [errors.sources], listed first, the added ones
// by name, one without a token left out, and names come from the host. If
// it regresses, a user who connected one server before loses it on
// upgrading, or a half-written source is offered and fails.
func TestErrorSourcesAreTheOldServerAndTheAddedOnes(t *testing.T) {
	cfg, err := parse(`[errors]
url = "https://glitchtip.rvx.dev.br"
token = "a"
source = "zeta"

[errors.sources.zeta]
url = "https://z.example.com"
token = "z"

[errors.sources.alpha]
url = "https://a.example.com"
token = "b"

[errors.sources.half]
url = "https://h.example.com"
`, "test")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range cfg.ErrorSources() {
		got = append(got, s.Name)
	}
	if strings.Join(got, " ") != "glitchtip-rvx-dev-br alpha zeta" || !cfg.ErrorSources()[0].Legacy || cfg.Errors.Source != "zeta" {
		t.Errorf("sources %v, %+v", got, cfg.ErrorSources())
	}
	for url, want := range map[string]string{"https://GlitchTip.Example.com:8000/x": "glitchtip-example-com-8000", "": "glitchtip"} {
		if got := ErrorSourceName(url); got != want {
			t.Errorf("%q named %q, want %q", url, got, want)
		}
	}
}

// TestRemovingASectionLeavesTheRestAsWritten: a table goes with its keys
// and the blank line before it; the tables around it, their comments and
// their order stay. If it regresses, removing one server rewrites or
// damages the user's settings file.
func TestRemovingASectionLeavesTheRestAsWritten(t *testing.T) {
	in := "# mine\n[errors]\nurl = \"x\"\n\n[errors.sources.a]\nurl = \"a\"\ntoken = \"t\"\n\n[issues]\n# keep\nagent = \"claude\"\n"
	want := "# mine\n[errors]\nurl = \"x\"\n\n[issues]\n# keep\nagent = \"claude\"\n"
	if got := DropSection(in, "errors.sources.a"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	last := "[errors]\nurl = \"x\"\n\n[errors.sources.a]\nurl = \"a\"\n"
	if got := DropSection(last, "errors.sources.a"); got != "[errors]\nurl = \"x\"\n" {
		t.Errorf("the last table: %q", got)
	}
	if got := DropSection(in, "errors.sources.none"); got != in {
		t.Errorf("a table not there changed the file: %q", got)
	}
}

// TestAProjectFolderChoosesItsServer: a folder tied to a server, and a
// project on it, is found from the folder itself and from any folder under
// it; the deepest folder wins, so a project inside another can report to
// another server; a folder that only starts with the same letters is not
// under it; and the table written reads back the same. If it regresses,
// the panel opens on the wrong server for the project being worked in, or
// "/work/api" catches "/work/api-old".
func TestAProjectFolderChoosesItsServer(t *testing.T) {
	cfg, err := parse(`[errors]
url = "https://one.example.com"
token = "a"
projects = { "/work" = "" }

[errors.sources.two]
url = "https://two.example.com"
token = "b"
projects = `+InlineTable(map[string]string{"/work/api": "shop/api", "/srv/x": ""})+`
`, "test")
	if err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{
		"/work/api": "two shop/api", "/work/api/src/models": "two shop/api",
		"/work/api-old": "one-example-com ", "/work": "one-example-com ", "/srv/x": "two ", "/elsewhere": "none",
	} {
		src, project, ok := cfg.ErrorSourceFor(dir)
		got := "none"
		if ok {
			got = src.Name + " " + project
		}
		if got != want {
			t.Errorf("%s: %q, want %q", dir, got, want)
		}
	}
	if src, _, ok := cfg.ErrorSourceFor("", "/work/api/x"); !ok || src.Name != "two" {
		t.Errorf("the second of two folders was not looked at: %v %v", src.Name, ok)
	}
	if got := InlineTable(map[string]string{"/b": "", "/a \"q\"": "o/p"}); got != `{ "/a \"q\"" = "o/p", "/b" = "" }` {
		t.Errorf("inline table: %s", got)
	}
}

// TestTicketAccountsAreKeptAndFoundByFolder: tracker accounts are read by
// name, one without a token or a kind left out, and a project folder finds
// its account and scope, the deepest winning. If it regresses, the tickets
// panel loses an account, or opens on the wrong one.
func TestTicketAccountsAreKeptAndFoundByFolder(t *testing.T) {
	cfg, err := parse(`[tickets]
source = "linear-akira"

[tickets.sources.linear-akira]
kind = "linear"
token = "keyring:tickets:linear-akira"
projects = { "/work" = "" }

[tickets.sources.jira-acme]
kind = "jira"
url = "https://acme.atlassian.net"
email = "me@acme.com"
token = "t"
projects = { "/work/app" = "APP" }

[tickets.sources.half]
kind = "clickup"
`, "test")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range cfg.TicketSources() {
		names = append(names, s.Name)
	}
	if strings.Join(names, " ") != "jira-acme linear-akira" || cfg.Tickets.Source != "linear-akira" {
		t.Errorf("sources %v", names)
	}
	if s, scope, ok := cfg.TicketSourceFor("/work/app/src"); !ok || s.Name != "jira-acme" || scope != "APP" {
		t.Errorf("app: %v %q %v", s.Name, scope, ok)
	}
	if s, _, ok := cfg.TicketSourceFor("/work/site"); !ok || s.Name != "linear-akira" {
		t.Errorf("site: %v %v", s.Name, ok)
	}
}
