package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/auth-com-br/tend/internal/integration"
)

// Codex keeps its servers as tables in config.toml — [mcp_servers.<name>],
// with subtables such as [mcp_servers.<name>.env] — anywhere in a file the
// user also edits by hand, with comments. So it is read with a TOML parser
// and edited by lines: only the lines of the one server change, and every
// edit is parsed back to check that nothing else did. It has its own
// switch: enabled = false in the server's table.

type codexAgent struct{ backups string }

func newCodex(backups string) Adapter { return &codexAgent{backups: backups} }

func (*codexAgent) ID() string   { return "codex" }
func (*codexAgent) Name() string { return "Codex" }

// Supports: Codex speaks stdio and streamable HTTP, not the older SSE.
func (*codexAgent) Supports(t Transport) bool { return t == Stdio || t == HTTP }

func (*codexAgent) Path() (string, error) {
	dir, err := integration.CodexDir()
	return filepath.Join(dir, "config.toml"), err
}

func (c *codexAgent) opts(path string) writeOpts {
	return writeOpts{backups: c.backups, backupName: "codex-" + filepath.Base(path), mode: 0o600}
}

// servers is the decoded server tables, by name.
func codexServers(data []byte) (map[string]map[string]any, error) {
	var doc struct {
		Servers map[string]map[string]any `toml:"mcp_servers"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("is not valid TOML: %w", err)
	}
	if doc.Servers == nil {
		doc.Servers = map[string]map[string]any{}
	}
	return doc.Servers, nil
}

func (c *codexAgent) read() ([]byte, string, error) {
	path, err := c.Path()
	if err != nil {
		return nil, "", err
	}
	data, _, err := readFile(path, 0)
	return data, path, err
}

func codexDef(t map[string]any) (Def, []string) {
	var extras []string
	for k := range t {
		switch k {
		case "command", "args", "env", "url", "http_headers", "enabled":
		default:
			extras = append(extras, k)
		}
	}
	sort.Strings(extras)
	if url, _ := t["url"].(string); url != "" {
		return Def{Transport: HTTP, URL: url, Headers: tomlStrMap(t["http_headers"])}, extras
	}
	cmd, _ := t["command"].(string)
	d := Def{Transport: Stdio, Command: cmd, Env: tomlStrMap(t["env"])}
	if args, ok := t["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				d.Args = append(d.Args, s)
			}
		}
	}
	return d, extras
}

func tomlStrMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

func (c *codexAgent) List() ([]Entry, error) {
	data, path, err := c.read()
	if err != nil || data == nil {
		return nil, err
	}
	servers, err := codexServers(data)
	if err != nil {
		return nil, fmt.Errorf("%s %w", path, err)
	}
	var out []Entry
	for name, t := range servers {
		d, extras := codexDef(t)
		e := Entry{Agent: "codex", Name: name, Enabled: true, Def: d, Extras: extras}
		if on, ok := t["enabled"].(bool); ok && !on {
			e.Enabled, e.Via = false, "native"
		}
		out = append(out, e)
	}
	return out, nil
}

// Raw is the server's decoded table as JSON: Codex's servers are never
// stashed, so this is for showing, not for restoring.
func (c *codexAgent) Raw(name string) (string, json.RawMessage, error) {
	data, _, err := c.read()
	if err != nil {
		return "", nil, err
	}
	servers, err := codexServers(data)
	if err != nil {
		return "", nil, err
	}
	for n, t := range servers {
		if Key(n) == Key(name) {
			raw, err := json.Marshal(t)
			return n, raw, err
		}
	}
	return "", nil, fmt.Errorf("Codex: %w: %s", ErrNoServer, name)
}

func (*codexAgent) AddRaw(string, json.RawMessage) error {
	return errors.New("Codex servers are switched off in its own file, not stashed")
}

func (c *codexAgent) Add(name string, d Def) error {
	if !c.Supports(d.Transport) {
		return fmt.Errorf("Codex cannot reach a server over %s; it speaks stdio and streamable HTTP", d.Transport)
	}
	path, err := c.Path()
	if err != nil {
		return err
	}
	return editFile(path, c.opts(path), func(data []byte) ([]byte, error) {
		servers, err := codexServers(data)
		if err != nil {
			return nil, err
		}
		for n := range servers {
			if Key(n) == Key(name) {
				return nil, fmt.Errorf("Codex %w: %s", ErrExists, n)
			}
		}
		text := string(data)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if text != "" {
			text += "\n"
		}
		out := text + codexBlock(name, d)
		return checkCodex(data, []byte(out), name, func(after map[string]any) bool { return after != nil })
	})
}

// codexBlock is a server written as Codex's own `codex mcp add` writes it.
func codexBlock(name string, d Def) string {
	var b strings.Builder
	head := "mcp_servers." + tomlKey(name)
	fmt.Fprintf(&b, "[%s]\n", head)
	if d.Transport == HTTP {
		fmt.Fprintf(&b, "url = %s\n", tomlString(d.URL))
		writeTable(&b, head+".http_headers", d.Headers)
		return b.String()
	}
	fmt.Fprintf(&b, "command = %s\n", tomlString(d.Command))
	args := make([]string, len(d.Args))
	for i, a := range d.Args {
		args[i] = tomlString(a)
	}
	fmt.Fprintf(&b, "args = [%s]\n", strings.Join(args, ", "))
	writeTable(&b, head+".env", d.Env)
	return b.String()
}

func writeTable(b *strings.Builder, head string, m map[string]string) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(b, "\n[%s]\n", head)
	for _, k := range keys {
		fmt.Fprintf(b, "%s = %s\n", tomlKey(k), tomlString(m[k]))
	}
}

func (c *codexAgent) Remove(name string) error {
	path, err := c.Path()
	if err != nil {
		return err
	}
	return editFile(path, c.opts(path), func(data []byte) ([]byte, error) {
		lines := strings.SplitAfter(string(data), "\n")
		found, blocks := codexBlocks(lines, name)
		if found == "" {
			return nil, c.missing(data, name)
		}
		drop := map[int]bool{}
		for _, r := range blocks {
			for i := r[0]; i < r[1]; i++ {
				drop[i] = true
			}
		}
		var out strings.Builder
		for i, l := range lines {
			if !drop[i] {
				out.WriteString(l)
			}
		}
		return checkCodex(data, []byte(tidyBlankLines(out.String())), found, func(after map[string]any) bool { return after == nil })
	})
}

func (c *codexAgent) SetEnabled(name string, on bool) error {
	path, err := c.Path()
	if err != nil {
		return err
	}
	return editFile(path, c.opts(path), func(data []byte) ([]byte, error) {
		lines := strings.SplitAfter(string(data), "\n")
		found, blocks := codexBlocks(lines, name)
		if found == "" {
			return nil, c.missing(data, name)
		}
		main := blocks[0] // the server's own table comes first
		var out []string
		out = append(out, lines[:main[0]+1]...)
		written := false
		for i := main[0] + 1; i < main[1]; i++ {
			if isTOMLKeyLine(lines[i], "enabled") {
				if !on {
					out = append(out, "enabled = false\n")
				}
				written = true
				continue
			}
			out = append(out, lines[i])
		}
		if !on && !written {
			// Right under the header: the first thing anybody reading the
			// table sees is that it is off.
			out = append(out[:main[0]+1], append([]string{"enabled = false\n"}, out[main[0]+1:]...)...)
		}
		out = append(out, lines[main[1]:]...)
		return checkCodex(data, []byte(strings.Join(out, "")), found, func(after map[string]any) bool {
			v, has := after["enabled"].(bool)
			if on {
				return !has || v
			}
			return has && !v
		})
	})
}

// missing says why a server could not be edited: not there, or written in
// a form these line edits do not touch (an inline table, dotted keys).
func (c *codexAgent) missing(data []byte, name string) error {
	if servers, err := codexServers(data); err == nil {
		for n := range servers {
			if Key(n) == Key(name) {
				return fmt.Errorf("Codex's %s is written in a form tend does not edit; change it in config.toml", n)
			}
		}
	}
	return fmt.Errorf("Codex: %w: %s", ErrNoServer, name)
}

// checkCodex parses an edit back and checks that the one server is as
// wanted and that nothing else in the file changed.
func checkCodex(before, after []byte, name string, want func(map[string]any) bool) ([]byte, error) {
	var b, a map[string]any
	if _, err := toml.Decode(string(before), &b); err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(after), &a); err != nil {
		return nil, fmt.Errorf("the edit would not read back: %w", err)
	}
	bs, _ := b["mcp_servers"].(map[string]any)
	as, _ := a["mcp_servers"].(map[string]any)
	target, _ := as[name].(map[string]any)
	if !want(target) {
		return nil, errors.New("the edit did not do what was meant; config.toml is left as it was")
	}
	delete(bs, name)
	delete(as, name)
	delete(b, "mcp_servers")
	delete(a, "mcp_servers")
	if !reflect.DeepEqual(b, a) || !reflect.DeepEqual(emptyIfNil(bs), emptyIfNil(as)) {
		return nil, errors.New("the edit would have changed more than the one server; config.toml is left as it was")
	}
	return after, nil
}

func emptyIfNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// codexBlocks finds a server's tables: its own first, then its subtables,
// each as a [start, end) range of lines. found is the name as written.
func codexBlocks(lines []string, name string) (found string, blocks [][2]int) {
	start := -1
	for i, l := range lines {
		path, ok := tomlHeaderPath(l)
		if !ok {
			continue
		}
		if start >= 0 {
			blocks = append(blocks, [2]int{start, i})
			start = -1
		}
		if len(path) >= 2 && path[0] == "mcp_servers" && Key(path[1]) == Key(name) {
			if found == "" {
				found = path[1]
			}
			if path[1] == found {
				start = i
			}
		}
	}
	if start >= 0 {
		blocks = append(blocks, [2]int{start, len(lines)})
	}
	// The server's own table first, so SetEnabled edits it and not a
	// subtable that happens to come earlier in the file.
	sort.SliceStable(blocks, func(i, j int) bool {
		pi, _ := tomlHeaderPath(lines[blocks[i][0]])
		pj, _ := tomlHeaderPath(lines[blocks[j][0]])
		return len(pi) < len(pj)
	})
	if len(blocks) > 0 {
		if p, _ := tomlHeaderPath(lines[blocks[0][0]]); len(p) != 2 {
			return "", nil // only subtables: the server itself is written elsewhere
		}
	}
	return found, blocks
}

// tomlHeaderPath reads a table header — [a.b."c d"] — into its keys; an
// array-of-tables header counts as a header too, ending a table.
func tomlHeaderPath(line string) ([]string, bool) {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "[") {
		return nil, false
	}
	s = strings.TrimPrefix(s, "[")
	array := strings.HasPrefix(s, "[")
	s = strings.TrimPrefix(s, "[")
	var path []string
	for {
		s = strings.TrimLeft(s, " \t")
		var key string
		switch {
		case strings.HasPrefix(s, `"`), strings.HasPrefix(s, `'`):
			q := s[0]
			end := 1
			for end < len(s) && s[end] != q {
				if q == '"' && s[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(s) {
				return nil, false
			}
			key = s[1:end]
			if q == '"' {
				var unq string
				if json.Unmarshal([]byte(s[:end+1]), &unq) == nil {
					key = unq
				}
			}
			s = s[end+1:]
		default:
			end := strings.IndexAny(s, ".] \t")
			if end <= 0 {
				return nil, false
			}
			key, s = s[:end], s[end:]
		}
		path = append(path, key)
		s = strings.TrimLeft(s, " \t")
		if strings.HasPrefix(s, ".") {
			s = s[1:]
			continue
		}
		if array {
			return path, strings.HasPrefix(s, "]]")
		}
		return path, strings.HasPrefix(s, "]")
	}
}

func isTOMLKeyLine(line, key string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, key) {
		return false
	}
	return strings.HasPrefix(strings.TrimLeft(t[len(key):], " \t"), "=")
}

// tidyBlankLines leaves at most one blank line between tables where a
// removed table had stood between two.
func tidyBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

// tomlKey is a key bare when it can be, quoted when it cannot.
func tomlKey(k string) string {
	if k == "" {
		return `""`
	}
	for _, r := range k {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return tomlString(k)
		}
	}
	return k
}

// tomlString is a TOML basic string: JSON's escaping is TOML's for every
// character a value here can hold.
func tomlString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
