package tools

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The MCP manager's logic: the agents' servers laid out as a grid, a row a
// server and a column an agent, what a write and a test say, and the line a
// new server is typed on. Both clients draw the grid their own way from
// what is here.

// MCPSummary is the line under a freshly read grid: how many servers, and
// any agent whose file could not be read.
func MCPSummary(list proto.MCPListResult) string {
	keys := map[string]bool{}
	for _, e := range list.Entries {
		keys[strings.ToLower(e.Name)] = true
	}
	s := strconv.Itoa(len(keys)) + " servers in " + strconv.Itoa(len(MCPColumns(list))) + " agents (global settings; a project's own are not shown)"
	for _, a := range list.Agents {
		if a.Error != "" {
			s = a.Name + ": " + a.Error
		}
	}
	return s
}

// MCPColumns is the agents shown: those installed here or with servers.
func MCPColumns(list proto.MCPListResult) []proto.MCPAgent {
	has := map[string]bool{}
	for _, e := range list.Entries {
		has[e.Agent] = true
	}
	var out []proto.MCPAgent
	for _, a := range list.Agents {
		if a.Present || has[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// MCPGrid lays the servers out: a row per name (as Key matches them), a
// column per agent, each cell on, off or absent, and ≠ on a server whose
// definition is not the one most agents of the row have.
func MCPGrid(list proto.MCPListResult) (labels, cols []string, rows []ui.MCPRow, keys []string, entries map[string]map[string]proto.MCPEntry) {
	for _, a := range MCPColumns(list) {
		labels = append(labels, a.ID)
		cols = append(cols, a.ID)
	}
	entries = map[string]map[string]proto.MCPEntry{}
	names := map[string]string{}
	for _, e := range list.Entries {
		k := strings.ToLower(strings.TrimSpace(e.Name))
		if entries[k] == nil {
			entries[k] = map[string]proto.MCPEntry{}
			names[k] = e.Name
			keys = append(keys, k)
		}
		entries[k][e.Agent] = e
	}
	sort.Strings(keys)
	for _, k := range keys {
		row := ui.MCPRow{Name: names[k], Cells: make([]ui.MCPCell, len(cols)), Differs: make([]bool, len(cols))}
		count := map[string]int{}
		transports := map[string]int{}
		for _, e := range entries[k] {
			count[e.Fingerprint]++
			transports[e.Transport]++
		}
		common, most := "", 0
		for fp, n := range count {
			if n > most || n == most && fp < common {
				common, most = fp, n
			}
		}
		for tr, n := range transports {
			if n > transports[row.Transport] || row.Transport == "" {
				row.Transport = tr
			}
		}
		for i, id := range cols {
			e, ok := entries[k][id]
			if !ok {
				continue
			}
			row.Cells[i] = ui.MCPOff
			if e.Enabled {
				row.Cells[i] = ui.MCPOn
			}
			row.Differs[i] = len(count) > 1 && e.Fingerprint != common
		}
		rows = append(rows, row)
	}
	return labels, cols, rows, keys, entries
}

// MCPWriteText says what a write did.
func MCPWriteText(did string, res proto.MCPWriteResult) string {
	var parts []string
	if len(res.Done) > 0 {
		parts = append(parts, did+" "+strings.Join(res.Done, ", ")+" — it takes effect in their next sessions")
	}
	for agent, why := range res.Failed {
		parts = append(parts, agent+": "+why)
	}
	parts = append(parts, res.Warnings...)
	return strings.Join(parts, "; ")
}

// ParseMCPSpec reads the line a server is added with:
//
//	name https://host/mcp [-H 'Name: value']...   streamable HTTP
//	name sse https://host/sse [-H ...]            the older SSE
//	name [KEY=value]... -- command [args]...      a program, on stdio
//
// The -- may be left out when the command is the first word that is not
// KEY=value. Words are split as a shell splits them, quotes and all.
func ParseMCPSpec(line string) (string, proto.MCPDef, error) {
	words, err := ShellWords(line)
	if err != nil {
		return "", proto.MCPDef{}, err
	}
	if len(words) < 2 {
		return "", proto.MCPDef{}, errors.New("give a name, then a URL or -- and a command")
	}
	name, rest := words[0], words[1:]
	def := proto.MCPDef{Transport: "stdio"}
	if rest[0] == "sse" || rest[0] == "http" {
		def.Transport, rest = rest[0], rest[1:]
		if len(rest) == 0 {
			return "", def, fmt.Errorf("%s needs a URL", def.Transport)
		}
	}
	if strings.Contains(rest[0], "://") {
		if def.Transport == "stdio" {
			def.Transport = "http"
		}
		def.URL, rest = rest[0], rest[1:]
		for len(rest) > 0 {
			if rest[0] != "-H" || len(rest) < 2 {
				return "", def, fmt.Errorf("after the URL only -H 'Name: value' is understood, not %q", rest[0])
			}
			k, val, ok := strings.Cut(rest[1], ":")
			if !ok {
				return "", def, fmt.Errorf("a header is Name: value, not %q", rest[1])
			}
			if def.Headers == nil {
				def.Headers = map[string]string{}
			}
			def.Headers[strings.TrimSpace(k)] = strings.TrimSpace(val)
			rest = rest[2:]
		}
		return name, def, nil
	}
	if def.Transport != "stdio" {
		return "", def, fmt.Errorf("%s needs a URL", def.Transport)
	}
	for len(rest) > 0 {
		if rest[0] == "--" {
			rest = rest[1:]
			break
		}
		k, val, ok := strings.Cut(rest[0], "=")
		if !ok || k == "" || strings.ContainsAny(k, "/ ") {
			break
		}
		if def.Env == nil {
			def.Env = map[string]string{}
		}
		def.Env[k] = val
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return "", def, errors.New("no command to start")
	}
	def.Command, def.Args = rest[0], rest[1:]
	return name, def, nil
}

// ShellWords splits a line as a shell would: by spaces, with '…' and "…"
// keeping theirs, and \ escaping the next character outside single quotes.
func ShellWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord, quote, escaped := false, rune(0), false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// MCPTarget is what a server runs or reaches, as it can be shown.
func MCPTarget(e proto.MCPEntry) string {
	if e.URL != "" {
		return e.URL
	}
	return strings.TrimSpace(e.Command + " " + strings.Join(e.Args, " "))
}

// MCPResultText is a test's result on one line.
func MCPResultText(r proto.MCPTestResult, err error) string {
	switch {
	case err != nil:
		return "✗ " + ErrorText(err)
	case r.Status == "ok":
		s := fmt.Sprintf("ok %dms, %d tools", r.Millis, r.Tools)
		if r.Server != "" {
			s += " (" + strings.TrimSpace(r.Server+" "+r.Version) + ")"
		}
		return s
	case r.Status == "auth":
		return "login needed — " + r.Error
	}
	s := "✗ " + r.Error
	if r.Stderr != "" {
		s += " — " + r.Stderr
	}
	return s
}
