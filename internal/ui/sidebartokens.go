package ui

import (
	"strconv"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"
)

// A sidebar entry as herdr lays it out: rows of tokens, each row fitted to
// the column (`ui/sidebar/tokens.rs`, `ui/sidebar.rs` resolved_token_spans).
// Which tokens and in what order is the user's ([ui.sidebar.*]); what each
// token says comes from the session; how a row that does not fit gives way is
// herdr's — the fixed parts stay, and the text-valued tokens shrink, the
// rightmost dropping out first when even a character each is too much.

// TokenKind says what a resolved token is, which decides its default look.
type TokenKind uint8

const (
	// TokenStateIcon is the entry's state mark.
	TokenStateIcon TokenKind = iota
	// TokenStateText is the state in words.
	TokenStateText
	// TokenPrimary is the entry's name: the space it is in.
	TokenPrimary
	// TokenSecondary is the rest of what locates it: machine, tab, pane,
	// agent, branch.
	TokenSecondary
	// TokenValue is a title or a value a hook reported.
	TokenValue
	// TokenGitStatus is how far a checkout is ahead and behind.
	TokenGitStatus
	// TokenGauge is a value that runs out — how full an agent's context
	// is, how much of its usage limit is gone — coloured as it nears the
	// end (Level).
	TokenGauge
)

// SidebarToken is one resolved token.
type SidebarToken struct {
	Kind          TokenKind
	Text          string
	Ahead, Behind int
	// Level is how near a gauge is to running out: 0 not near, 1 near, 2
	// there.
	Level uint8
	Style config.TokenStyle
}

// AgentTokenValues is what an agent's tokens can say. An empty value is a
// token with nothing to show, and is left out.
type AgentTokenValues struct {
	StateText, Machine, Workspace, Tab, Pane, Agent string
	TerminalTitle, TerminalTitleStripped            string
	// Context, Spent, Cost and Limit say what the agent's conversation is
	// spending (FormatUsage); ContextLevel and LimitLevel colour the
	// gauges.
	Context, Spent, Cost, Limit string
	ContextLevel, LimitLevel    uint8
	Custom                      map[string]string
}

// SpaceTokenValues is what a space's tokens can say.
type SpaceTokenValues struct {
	StateText, Workspace, Branch string
	Ahead, Behind                int
	Custom                       map[string]string
}

// ResolveAgentRows fills a layout in for one agent.
func ResolveAgentRows(rows [][]config.SidebarToken, v AgentTokenValues) [][]SidebarToken {
	return resolveRows(rows, v.Custom, func(name string) (SidebarToken, bool) {
		switch name {
		case "state_icon":
			return SidebarToken{Kind: TokenStateIcon}, true
		case "state_text":
			return SidebarToken{Kind: TokenStateText, Text: v.StateText}, true
		case "machine":
			return SidebarToken{Kind: TokenSecondary, Text: v.Machine}, true
		case "workspace":
			return SidebarToken{Kind: TokenPrimary, Text: v.Workspace}, true
		case "tab":
			return SidebarToken{Kind: TokenSecondary, Text: v.Tab}, true
		case "pane":
			return SidebarToken{Kind: TokenSecondary, Text: v.Pane}, true
		case "agent":
			return SidebarToken{Kind: TokenSecondary, Text: v.Agent}, true
		case "terminal_title":
			return SidebarToken{Kind: TokenValue, Text: v.TerminalTitle}, true
		case "terminal_title_stripped":
			return SidebarToken{Kind: TokenValue, Text: v.TerminalTitleStripped}, true
		case "context":
			return SidebarToken{Kind: TokenGauge, Text: v.Context, Level: v.ContextLevel}, true
		case "spent":
			return SidebarToken{Kind: TokenValue, Text: v.Spent}, true
		case "cost":
			return SidebarToken{Kind: TokenValue, Text: v.Cost}, true
		case "limit":
			return SidebarToken{Kind: TokenGauge, Text: v.Limit, Level: v.LimitLevel}, true
		}
		return SidebarToken{}, false
	})
}

// ResolveSpaceRows fills a layout in for one space.
func ResolveSpaceRows(rows [][]config.SidebarToken, v SpaceTokenValues) [][]SidebarToken {
	return resolveRows(rows, v.Custom, func(name string) (SidebarToken, bool) {
		switch name {
		case "state_icon":
			return SidebarToken{Kind: TokenStateIcon}, true
		case "state_text":
			return SidebarToken{Kind: TokenStateText, Text: v.StateText}, true
		case "workspace":
			return SidebarToken{Kind: TokenPrimary, Text: v.Workspace}, true
		case "branch":
			return SidebarToken{Kind: TokenSecondary, Text: v.Branch}, true
		case "git_status":
			// Said only when there is something to say: in step is the
			// ordinary state of every checkout.
			if v.Ahead == 0 && v.Behind == 0 {
				return SidebarToken{}, false
			}
			return SidebarToken{Kind: TokenGitStatus, Ahead: v.Ahead, Behind: v.Behind}, true
		}
		return SidebarToken{}, false
	})
}

// resolveRows is herdr's agent_rows/space_rows: every token that has a value
// and is not hidden by a rule, and no row left with nothing in it.
func resolveRows(rows [][]config.SidebarToken, custom map[string]string, builtin func(string) (SidebarToken, bool)) [][]SidebarToken {
	var out [][]SidebarToken
	for _, row := range rows {
		var line []SidebarToken
		for _, configured := range row {
			var token SidebarToken
			if name, ok := configured.Custom(); ok {
				value, found := custom[name]
				if !found {
					continue
				}
				token = SidebarToken{Kind: TokenValue, Text: value}
			} else {
				var ok bool
				if token, ok = builtin(configured.Name); !ok {
					continue
				}
			}
			textual := token.Kind != TokenStateIcon && token.Kind != TokenGitStatus
			if textual && token.Text == "" {
				continue // nothing to show
			}
			style := configured.Style
			if textual {
				var shown bool
				if style, shown = configured.StyleFor(token.Text); !shown {
					continue
				}
			}
			token.Style = style
			line = append(line, token)
		}
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

// tokenSeparator is herdr's: a space after the state mark and before the
// git status, a dot between everything else.
func tokenSeparator(previous, current SidebarToken) string {
	if previous.Kind == TokenStateIcon || current.Kind == TokenGitStatus {
		return " "
	}
	return " · "
}

func gitStatusText(t SidebarToken) string {
	var s string
	if t.Ahead > 0 {
		s = "↑" + strconv.Itoa(t.Ahead)
	}
	if t.Ahead > 0 && t.Behind > 0 {
		s += " "
	}
	if t.Behind > 0 {
		s += "↓" + strconv.Itoa(t.Behind)
	}
	return s
}

// tokenStyles are the default looks, worked out by the row that draws.
type tokenStyles struct {
	icon, stateText, primary, secondary, value, separator, ahead, behind vt.Style
	// near and out are a gauge nearing its end and at it.
	near, out vt.Style
	iconText  string
}

// drawTokenLine writes one row of tokens from x, within limit, fitting them
// as herdr does. It returns where it stopped.
func drawTokenLine(dst *vt.Grid, line []SidebarToken, x, y, limit int, st tokenStyles) int {
	maxWidth := limit - x
	if maxWidth <= 0 || len(line) == 0 {
		return x
	}
	fixed := make([]int, len(line))
	flexible := make([]int, len(line))
	for i, t := range line {
		switch t.Kind {
		case TokenStateIcon:
			fixed[i] = runewidth.StringWidth(st.iconText)
		case TokenGitStatus:
			fixed[i] = runewidth.StringWidth(gitStatusText(t))
		default:
			flexible[i] = runewidth.StringWidth(t.Text)
		}
	}
	separators := func(active []bool) int {
		width, previous := 0, -1
		for i, on := range active {
			if !on {
				continue
			}
			if previous >= 0 {
				width += runewidth.StringWidth(tokenSeparator(line[previous], line[i]))
			}
			previous = i
		}
		return width
	}
	minimum := func(active []bool) int {
		width := separators(active)
		for i, on := range active {
			if on {
				width += fixed[i]
				if flexible[i] > 0 {
					width++
				}
			}
		}
		return width
	}
	active := make([]bool, len(line))
	for i := range active {
		active[i] = true
	}
	if minimum(active) > maxWidth {
		// Everything flexible out, then back in from the right while it
		// fits: herdr keeps the rightmost it can.
		for i := range active {
			if flexible[i] > 0 {
				active[i] = false
			}
		}
		for i := len(line) - 1; i >= 0; i-- {
			if flexible[i] == 0 {
				continue
			}
			active[i] = true
			if minimum(active) > maxWidth {
				active[i] = false
			}
		}
	}

	budgets := make([]int, len(line))
	used := separators(active)
	for i, on := range active {
		if !on {
			continue
		}
		used += fixed[i]
		if flexible[i] > 0 {
			budgets[i] = 1
			used++
		}
	}
	for remaining := maxWidth - used; remaining > 0; {
		grew := false
		for i := range budgets {
			if budgets[i] > 0 && budgets[i] < flexible[i] {
				budgets[i]++
				remaining--
				grew = true
				if remaining == 0 {
					break
				}
			}
		}
		if !grew {
			break
		}
	}

	previous := -1
	for i, t := range line {
		if !active[i] {
			continue
		}
		if previous >= 0 {
			x = writeString(dst, x, y, tokenSeparator(line[previous], t), st.separator, limit)
		}
		previous = i
		switch t.Kind {
		case TokenStateIcon:
			x = writeString(dst, x, y, st.iconText, patchStyle(st.icon, t.Style), limit)
		case TokenGitStatus:
			if t.Ahead > 0 {
				x = writeString(dst, x, y, "↑"+strconv.Itoa(t.Ahead), patchStyle(st.ahead, t.Style), limit)
			}
			if t.Ahead > 0 && t.Behind > 0 {
				x = writeString(dst, x, y, " ", patchStyle(st.separator, t.Style), limit)
			}
			if t.Behind > 0 {
				x = writeString(dst, x, y, "↓"+strconv.Itoa(t.Behind), patchStyle(st.behind, t.Style), limit)
			}
		default:
			base := st.value
			switch t.Kind {
			case TokenStateText:
				base = st.stateText
			case TokenPrimary:
				base = st.primary
			case TokenSecondary:
				base = st.secondary
			case TokenGauge:
				switch t.Level {
				case 1:
					base = st.near
				case 2:
					base = st.out
				}
			}
			x = writeString(dst, x, y, truncate(t.Text, budgets[i]), patchStyle(base, t.Style), limit)
		}
	}
	return x
}

// patchStyle applies what a token's configuration says over its default.
func patchStyle(s vt.Style, p config.TokenStyle) vt.Style {
	if p.FG != nil {
		s.FG = vt.RGBColor(p.FG.R, p.FG.G, p.FG.B)
	}
	if p.Bold != nil {
		if *p.Bold {
			s.Attrs |= vt.AttrBold
		} else {
			s.Attrs &^= vt.AttrBold
		}
	}
	if p.Dim != nil {
		if *p.Dim {
			s.Attrs |= vt.AttrDim
		} else {
			s.Attrs &^= vt.AttrDim
		}
	}
	return s
}
