package config

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/auth-com-br/tend/internal/detect"
)

// What each row of the sidebar shows: herdr's [ui.sidebar.agents] and
// [ui.sidebar.spaces] (`config/sidebar.rs`, `config/sidebar/rules.rs`).
//
//	[ui.sidebar.agents]
//	rows = [["state_icon", "machine", "workspace", "tab"], ["agent", "context"]]
//	row_gap = 0
//	[ui.sidebar.agents.rows_by_agent]
//	claude = [["state_icon", "workspace"], ["terminal_title_stripped"]]
//	[ui.sidebar.spaces]
//	rows = [["state_icon", "workspace"], ["branch", "git_status"]]
//
// A token is a name, a $name for a value a hook reported, or a table that
// styles it: { token = "workspace", fg = "#89b4fa", bold = true, dim = false,
// rules = [{ gt = 80, fg = "#f38ba8" }] }. A rule matches the token's text —
// equals, contains, starts_with, or a number above gt or below lt — and
// restyles it or, with hide = true, leaves it out.
//
// tend used to write `sidebar = true` in [ui] for whether the column is
// shown, which is the same key herdr uses for this table. Both forms are
// read: a boolean is tend's old setting, a table is herdr's rows. Whether
// the column starts shown is `sidebar_start_collapsed`, herdr's key.

// Limits, herdr's.
const (
	maxSidebarRows      = 16
	maxSidebarRowTokens = 16
	maxTokenRules       = 16
	maxCustomTokenName  = 32
)

// Built-in token names.
var (
	AgentTokenNames = []string{"state_icon", "state_text", "machine", "workspace", "tab",
		"pane", "agent", "terminal_title", "terminal_title_stripped", "context", "spent", "cost", "limit"}
	SpaceTokenNames = []string{"state_icon", "state_text", "workspace", "branch", "git_status"}
)

// DefaultAgentRows is herdr's, with how full the agent's context is beside
// its name: an agent that runs out compacts or stops, and the moment to
// know is before (issue #46). Where nothing is known it is left out, and the
// row is herdr's.
func DefaultAgentRows() [][]SidebarToken {
	return [][]SidebarToken{
		{{Name: "state_icon"}, {Name: "machine"}, {Name: "workspace"}, {Name: "tab"}},
		{{Name: "agent"}, {Name: "context"}},
	}
}

// DefaultSpaceRows is herdr's layout for a space.
func DefaultSpaceRows() [][]SidebarToken {
	return [][]SidebarToken{
		{{Name: "state_icon"}, {Name: "workspace"}},
		{{Name: "branch"}, {Name: "git_status"}},
	}
}

// Sidebar is [ui] sidebar in either of its forms.
type Sidebar struct {
	// Shown is tend's old boolean, nil when the file used the table form or
	// said nothing.
	Shown  *bool
	Agents SidebarAgents `toml:"agents"`
	Spaces SidebarSpaces `toml:"spaces"`
}

// SidebarAgents is how agent rows are laid out.
type SidebarAgents struct {
	Rows        [][]SidebarToken            `toml:"rows"`
	RowsByAgent map[string][][]SidebarToken `toml:"rows_by_agent"`
	RowGap      int                         `toml:"row_gap"`
}

// SidebarSpaces is how space rows are laid out.
type SidebarSpaces struct {
	Rows   [][]SidebarToken `toml:"rows"`
	RowGap int              `toml:"row_gap"`
}

// AgentRows is the layout for an agent: its own, if one was given for it,
// or the general one, or herdr's default.
func (s Sidebar) AgentRows(agent string) [][]SidebarToken {
	if rows, ok := s.Agents.RowsByAgent[agent]; ok && agent != "" {
		return rows
	}
	if s.Agents.Rows != nil {
		return s.Agents.Rows
	}
	return DefaultAgentRows()
}

// SpaceRows is the layout for a space.
func (s Sidebar) SpaceRows() [][]SidebarToken {
	if s.Spaces.Rows != nil {
		return s.Spaces.Rows
	}
	return DefaultSpaceRows()
}

// UnmarshalTOML reads a boolean (tend's) or a table (herdr's).
func (s *Sidebar) UnmarshalTOML(v any) error {
	switch value := v.(type) {
	case bool:
		s.Shown = &value
		return nil
	case map[string]any:
		// Written back out and read into the struct, so the tables inside get
		// the same decoding, and the same refusal of unknown keys, as the
		// rest of the file.
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(value); err != nil {
			return err
		}
		var table struct {
			Agents SidebarAgents `toml:"agents"`
			Spaces SidebarSpaces `toml:"spaces"`
		}
		md, err := toml.Decode(buf.String(), &table)
		if err != nil {
			return err
		}
		for _, key := range md.Undecoded() {
			// A token's own fields were read by its UnmarshalTOML, which the
			// decoder does not count; anything else unread is a mistake.
			if len(key) >= 2 && (key[1] == "rows" || key[1] == "rows_by_agent") {
				continue
			}
			return fmt.Errorf("ui.sidebar has no setting %q", key.String())
		}
		s.Agents, s.Spaces = table.Agents, table.Spaces
		return nil
	}
	return fmt.Errorf("ui.sidebar is a %T; use true, false, or [ui.sidebar.agents] and [ui.sidebar.spaces]", v)
}

// RGB is a token colour.
type RGB struct{ R, G, B uint8 }

// TokenStyle patches the style a token would have had. Nil fields keep it.
type TokenStyle struct {
	FG   *RGB
	Bold *bool
	Dim  *bool
}

// TokenRule restyles, or hides, a token whose text matches.
type TokenRule struct {
	equals, contains, startsWith *string
	gt, lt                       *float64
	ignoreCase                   bool
	Style                        TokenStyle
	Hide                         bool
}

// SidebarToken is one token in a row.
type SidebarToken struct {
	// Name is a built-in token's name, or "$name" for a reported value.
	Name  string
	Style TokenStyle
	Rules []TokenRule
}

// Custom is the reported value's name, for a $name token.
func (t SidebarToken) Custom() (string, bool) {
	return strings.CutPrefix(t.Name, "$")
}

// UnmarshalTOML reads "workspace" or { token = "workspace", ... }.
func (t *SidebarToken) UnmarshalTOML(v any) error {
	switch value := v.(type) {
	case string:
		t.Name = value
		return nil
	case map[string]any:
		for key := range value {
			switch key {
			case "token", "fg", "bold", "dim", "rules":
			default:
				return fmt.Errorf("sidebar token has no field %q", key)
			}
		}
		name, ok := value["token"].(string)
		if !ok {
			return errors.New("a styled sidebar token needs token = \"<name>\"")
		}
		t.Name = name
		style, err := parseTokenStyle(value)
		if err != nil {
			return err
		}
		t.Style = style
		if raw, ok := value["rules"]; ok {
			list, ok := raw.([]map[string]any)
			if !ok {
				if anys, isAnys := raw.([]any); isAnys {
					for _, a := range anys {
						m, isMap := a.(map[string]any)
						if !isMap {
							return errors.New("sidebar token rules are tables")
						}
						list = append(list, m)
					}
				} else {
					return errors.New("sidebar token rules are a list of tables")
				}
			}
			if len(list) > maxTokenRules {
				return fmt.Errorf("sidebar tokens may contain at most %d rules", maxTokenRules)
			}
			if len(list) > 0 && (name == "state_icon" || name == "git_status") {
				return errors.New("sidebar rules require a text-valued token")
			}
			for _, m := range list {
				rule, err := parseTokenRule(m)
				if err != nil {
					return err
				}
				t.Rules = append(t.Rules, rule)
			}
		}
		return nil
	}
	return fmt.Errorf("a sidebar token is a name or a table, not a %T", v)
}

func parseTokenStyle(m map[string]any) (TokenStyle, error) {
	var s TokenStyle
	if raw, ok := m["fg"]; ok {
		text, isText := raw.(string)
		if !isText {
			return s, errors.New("sidebar token fg must be #RGB or #RRGGBB")
		}
		c, err := parseTokenColor(text)
		if err != nil {
			return s, err
		}
		s.FG = &c
	}
	for key, target := range map[string]**bool{"bold": &s.Bold, "dim": &s.Dim} {
		if raw, ok := m[key]; ok {
			b, isBool := raw.(bool)
			if !isBool {
				return s, fmt.Errorf("sidebar token %s is true or false", key)
			}
			*target = &b
		}
	}
	return s, nil
}

// parseTokenColor takes herdr's strict #RGB or #RRGGBB.
func parseTokenColor(text string) (RGB, error) {
	hex, ok := strings.CutPrefix(text, "#")
	if !ok || (len(hex) != 3 && len(hex) != 6) {
		return RGB{}, errors.New("sidebar token fg must be #RGB or #RRGGBB")
	}
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return RGB{}, errors.New("sidebar token fg must be #RGB or #RRGGBB")
	}
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

func parseTokenRule(m map[string]any) (TokenRule, error) {
	var r TokenRule
	conditions := 0
	for key, raw := range m {
		switch key {
		case "equals", "contains", "starts_with":
			text, ok := raw.(string)
			if !ok {
				return r, fmt.Errorf("sidebar rule %s takes text", key)
			}
			conditions++
			switch key {
			case "equals":
				r.equals = &text
			case "contains":
				r.contains = &text
			default:
				r.startsWith = &text
			}
		case "gt", "lt":
			var n float64
			switch num := raw.(type) {
			case int64:
				n = float64(num)
			case float64:
				n = num
			default:
				return r, fmt.Errorf("sidebar rule %s takes a number", key)
			}
			if math.IsInf(n, 0) || math.IsNaN(n) {
				return r, errors.New("sidebar numeric rule threshold must be finite")
			}
			conditions++
			if key == "gt" {
				r.gt = &n
			} else {
				r.lt = &n
			}
		case "ignore_case":
			b, ok := raw.(bool)
			if !ok {
				return r, errors.New("sidebar rule ignore_case is true or false")
			}
			r.ignoreCase = b
		case "hide":
			b, ok := raw.(bool)
			if !ok {
				return r, errors.New("sidebar rule hide is true or false")
			}
			r.Hide = b
		case "fg", "bold", "dim":
		default:
			return r, fmt.Errorf("sidebar rule has no field %q", key)
		}
	}
	if conditions != 1 {
		return r, errors.New("sidebar rule requires exactly one of equals, contains, starts_with, gt, lt")
	}
	if _, ok := m["ignore_case"]; ok && (r.gt != nil || r.lt != nil) {
		return r, errors.New("ignore_case applies only to sidebar text conditions")
	}
	style, err := parseTokenStyle(m)
	if err != nil {
		return r, err
	}
	r.Style = style
	return r, nil
}

// matches is herdr's rule test.
func (r TokenRule) matches(value string) bool {
	fold := func(s string) string {
		if r.ignoreCase {
			return strings.ToLower(s)
		}
		return s
	}
	switch {
	case r.equals != nil:
		return fold(value) == fold(*r.equals)
	case r.contains != nil:
		return strings.Contains(fold(value), fold(*r.contains))
	case r.startsWith != nil:
		return strings.HasPrefix(fold(value), fold(*r.startsWith))
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return false
	}
	if r.gt != nil {
		return n > *r.gt
	}
	return r.lt != nil && n < *r.lt
}

// StyleFor is the style a token's text gets: the first rule that matches,
// over the token's own style, or false when that rule hides it.
func (t SidebarToken) StyleFor(value string) (TokenStyle, bool) {
	for _, r := range t.Rules {
		if !r.matches(value) {
			continue
		}
		if r.Hide {
			return TokenStyle{}, false
		}
		out := t.Style
		if r.Style.FG != nil {
			out.FG = r.Style.FG
		}
		if r.Style.Bold != nil {
			out.Bold = r.Style.Bold
		}
		if r.Style.Dim != nil {
			out.Dim = r.Style.Dim
		}
		return out, true
	}
	return t.Style, true
}

// checkSidebar refuses token names that do not exist and layouts past
// herdr's limits.
func checkSidebar(s Sidebar) error {
	if err := checkRows("ui.sidebar.agents.rows", s.Agents.Rows, AgentTokenNames); err != nil {
		return err
	}
	var known map[string]bool
	if len(s.Agents.RowsByAgent) > 0 {
		// herdr refuses an id it does not know; here the ids are the bundled
		// detection manifests', which is what a pane's agent is called.
		known = map[string]bool{}
		if catalog, err := detect.Bundled(); err == nil {
			for _, id := range catalog.IDs() {
				known[id] = true
			}
		}
	}
	for agent, rows := range s.Agents.RowsByAgent {
		if len(known) > 0 && !known[agent] {
			return fmt.Errorf("unknown agent id %q in ui.sidebar.agents.rows_by_agent", agent)
		}
		if err := checkRows("ui.sidebar.agents.rows_by_agent."+agent, rows, AgentTokenNames); err != nil {
			return err
		}
	}
	if err := checkRows("ui.sidebar.spaces.rows", s.Spaces.Rows, SpaceTokenNames); err != nil {
		return err
	}
	if s.Agents.RowGap < 0 || s.Spaces.RowGap < 0 {
		return errors.New("ui.sidebar row_gap cannot be negative")
	}
	return nil
}

func checkRows(where string, rows [][]SidebarToken, builtins []string) error {
	if len(rows) > maxSidebarRows {
		return fmt.Errorf("%s: sidebar layouts may contain at most %d rows", where, maxSidebarRows)
	}
	for _, row := range rows {
		if len(row) > maxSidebarRowTokens {
			return fmt.Errorf("%s: sidebar rows may contain at most %d tokens", where, maxSidebarRowTokens)
		}
		for _, t := range row {
			if name, custom := t.Custom(); custom {
				if name == "" || len(name) > maxCustomTokenName || strings.TrimFunc(name, func(r rune) bool {
					return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
				}) != "" {
					return fmt.Errorf("%s: invalid custom sidebar token %q", where, t.Name)
				}
				continue
			}
			known := false
			for _, b := range builtins {
				known = known || b == t.Name
			}
			if !known {
				return fmt.Errorf("%s: unknown sidebar token %q; built-ins are %s, and custom tokens start with $",
					where, t.Name, strings.Join(builtins, ", "))
			}
		}
	}
	return nil
}
