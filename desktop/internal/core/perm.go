package core

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The permissions screen: the TUI's permissions manager
// (cmd/tend/tui_perm.go) in a window — what each agent may do without
// asking, a tab an agent, for the project of the pane in focus when it
// opened. The rules are read and written by the session's server, where the
// agents and their files are (internal/perm); what is listed and how is
// internal/tools', the TUI's own.

// PermState is the screen as QML draws it.
type PermState struct {
	Where   string          `json:"where"`
	Tabs    []ui.PermTab    `json:"tabs"`
	Tab     int             `json:"tab"`
	Notes   []string        `json:"notes,omitempty"`
	Lines   []PermLineState `json:"lines"`
	Empty   string          `json:"empty,omitempty"`
	Filter  string          `json:"filter,omitempty"`
	Loading bool            `json:"loading,omitempty"`
	Message string          `json:"message,omitempty"`
	Alert   bool            `json:"alert,omitempty"`
	Confirm string          `json:"confirm,omitempty"`
	Detail  []string        `json:"detail,omitempty"`
	// Decisions are what the agent's rules can say, with what each does;
	// Files every file of the agent and RuleFiles those a rule can go in
	// (indexes into Files); Hint how a rule is written for it.
	Decisions []PermChoice `json:"decisions"`
	Files     []string     `json:"files"`
	RuleFiles []int        `json:"ruleFiles"`
	Hint      string       `json:"hint,omitempty"`
	// Editors are the editors the server's machine has, once asked for.
	Editors []string `json:"editors,omitempty"`
}

// PermChoice is a value and what it says.
type PermChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// PermLineState is a mode or a rule. A mode has its Choices, which a click
// picks from; a rule its decision, file (an index into Files) and marks.
type PermLineState struct {
	ui.PermLine
	File    int      `json:"file"`
	Choices []string `json:"choices,omitempty"`
	Value   string   `json:"value,omitempty"`
	Help    string   `json:"help,omitempty"`
}

// PermAction is what the screen asks for: Do one of "reload", "tab"
// (Index), "filter" (Text), "change" (Row: the next decision), "decision"
// (Row, Text: that decision), "mode" (Row, Text: the value, "" unset),
// "remove" (Row), "move" (Row, Index: a file), "add" (Text, Decision,
// Index: a file), "detail" (Row), "help", "editors", "open" (Index: a file
// or -1 for the row's, EditorAt: one of Editors, else the default) and
// "answer" (Yes).
type PermAction struct {
	Do       string `json:"do"`
	Row      int    `json:"row"`
	Index    int    `json:"index"`
	Text     string `json:"text"`
	Decision string `json:"decision"`
	EditorAt *int   `json:"editorAt"`
	Yes      bool   `json:"yes"`
}

type permScreen struct {
	state PermState
	list  proto.PermListResult
	// pane is the pane in focus when it opened: its project is the one
	// shown, and an editor opens beside it.
	pane    uint64
	agent   string
	filter  string
	items   []tools.PermItem
	editors []proto.PermEditor
}

// loadPerm reads the permissions again, keeping the tab; message, when
// set, is said once it is done.
func (s *Session) loadPerm(message string) {
	s.mu.Lock()
	c, p := s.client, s.tools.perm
	s.mu.Unlock()
	if p == nil {
		return
	}
	if !c.Supports(proto.MethodPermList) {
		s.permSay("this server is older than the permissions manager; "+tools.HandoffCommand(s.name)+" moves it to this build", true)
		return
	}
	list, err := c.PermList(p.pane, "")
	s.mu.Lock()
	if s.tools.perm != p {
		s.mu.Unlock()
		return
	}
	p.state.Loading = false
	if err != nil {
		p.state.Message, p.state.Alert = "could not read the agents' permissions: "+tools.ErrorText(err), true
	} else {
		p.list = list
		p.layout()
		p.state.Message, p.state.Alert = message, false
	}
	s.mu.Unlock()
	s.stateChanged()
}

// layout lays the screen out from the list, as the TUI's permLayoutLocked.
func (p *permScreen) layout() {
	a, ok := tools.PermAgentOf(p.list, p.agent)
	if ok {
		p.agent = a.ID
	}
	st := &p.state
	st.Where = tools.PermWhere(p.list.Project)
	st.Tabs = tools.PermTabs(p.list)
	for i, agent := range p.list.Agents {
		if agent.ID == a.ID {
			st.Tab = i
		}
	}
	st.Notes = tools.PermNotes(a)
	st.Filter = p.filter
	st.Empty = tools.PermEmptyLabel(a)
	st.Hint = tools.PermRuleHint[a.ID]
	st.Decisions, st.Files, st.RuleFiles = nil, nil, nil
	for _, d := range a.Decisions {
		st.Decisions = append(st.Decisions, PermChoice{Value: d, Label: tools.PermDecisionWords[d]})
	}
	fileIndex := map[string]int{}
	for i, f := range a.Files {
		fileIndex[f.Path] = i
		st.Files = append(st.Files, tools.PermFileLabel(f))
		if !f.ReadOnly && !f.ModesOnly {
			st.RuleFiles = append(st.RuleFiles, i)
		}
	}
	p.items, st.Lines = nil, nil
	if !ok {
		return
	}
	items, lines := tools.PermList(a, p.list.Project, p.filter)
	p.items = items
	for i, l := range lines {
		ls := PermLineState{PermLine: l, File: -1}
		if m := items[i].Mode; m != nil {
			ls.File, ls.Choices, ls.Value, ls.Help = fileIndex[m.File], m.Choices, m.Value, m.Help
		} else if f, ok := fileIndex[items[i].Rule.File]; ok {
			ls.File = f
		}
		st.Lines = append(st.Lines, ls)
	}
}

func (s *Session) permSay(msg string, alert bool) {
	s.mu.Lock()
	if p := s.tools.perm; p != nil {
		p.state.Message, p.state.Alert, p.state.Loading = msg, alert, false
	}
	s.mu.Unlock()
	s.stateChanged()
}

// permWrite sends one write and reads the list again, saying what was
// done and when the agent sees it.
func (s *Session) permWrite(p *permScreen, method string, w proto.PermWriteParams, done string) error {
	s.mu.Lock()
	c := s.client
	a, _ := tools.PermAgentOf(p.list, p.agent)
	w.Pane, w.Agent = p.pane, a.ID
	s.mu.Unlock()
	if err := c.PermWrite(method, w); err != nil {
		s.permSay(tools.ErrorText(err), true)
		return nil
	}
	if a.Effect != "" {
		done += " — " + a.Effect
	}
	s.loadPerm(done)
	return nil
}

// Perm does what the screen asks.
func (s *Session) Perm(act PermAction) error {
	s.mu.Lock()
	p, c := s.tools.perm, s.client
	if p == nil {
		s.mu.Unlock()
		return nil
	}
	a, _ := tools.PermAgentOf(p.list, p.agent)
	var item tools.PermItem
	if act.Row >= 0 && act.Row < len(p.items) {
		item = p.items[act.Row]
	}
	s.mu.Unlock()
	refused := func() error {
		s.permSay("tend does not edit this rule: open its file in your editor", false)
		return nil
	}
	switch act.Do {
	case "reload":
		s.loadPerm("")
	case "answer":
		return s.answer(act.Yes, func() { p.state.Confirm = "" })
	case "tab":
		s.mu.Lock()
		if act.Index >= 0 && act.Index < len(p.list.Agents) {
			p.agent = p.list.Agents[act.Index].ID
			p.state.Detail, p.state.Message = nil, ""
			p.layout()
		}
		s.mu.Unlock()
		s.stateChanged()
	case "filter":
		s.mu.Lock()
		p.filter = act.Text
		p.layout()
		s.mu.Unlock()
		s.stateChanged()
	case "help":
		s.mu.Lock()
		p.state.Detail = helpBefore(ui.PermHelp, "WHAT YOU CAN DO")
		s.mu.Unlock()
		s.stateChanged()
	case "detail":
		if item.Rule == nil {
			return nil
		}
		s.mu.Lock()
		p.state.Detail = tools.PermDetail(a, *item.Rule)
		s.mu.Unlock()
		s.stateChanged()
	case "change", "decision":
		r := item.Rule
		if r == nil {
			return nil
		}
		if r.ReadOnly {
			return refused()
		}
		next := act.Text
		if act.Do == "change" {
			next = tools.PermNextDecision(a, r.Decision)
		}
		if next == "" || next == r.Decision {
			s.permSay(a.Name+" has no other decision for a rule", false)
			return nil
		}
		return s.permWrite(p, proto.MethodPermSetDecision, proto.PermWriteParams{File: r.File, Decision: r.Decision, Text: r.Text, ToDecision: next},
			r.Text+" is "+next+" now")
	case "mode":
		m := item.Mode
		if m == nil {
			return nil
		}
		done := tools.PermModeKey(m.Key) + " is " + act.Text
		if act.Text == "" {
			done = tools.PermModeKey(m.Key) + " is not set"
		}
		return s.permWrite(p, proto.MethodPermSetMode, proto.PermWriteParams{File: m.File, Key: m.Key, Value: act.Text}, done)
	case "remove":
		r := item.Rule
		if r == nil {
			return nil
		}
		if r.ReadOnly {
			return refused()
		}
		rule := *r
		s.ask("remove "+rule.Decision+" "+rule.Text+" from "+tools.TildeHome(rule.File)+"?",
			func(q string) { p.state.Confirm = q },
			func() error {
				return s.permWrite(p, proto.MethodPermRemove, proto.PermWriteParams{File: rule.File, Decision: rule.Decision, Text: rule.Text}, "removed "+rule.Text)
			})
	case "move":
		r := item.Rule
		if r == nil || act.Index < 0 || act.Index >= len(a.Files) {
			return nil
		}
		if r.ReadOnly {
			return refused()
		}
		to := a.Files[act.Index].Path
		if to == r.File {
			return nil
		}
		return s.permWrite(p, proto.MethodPermMove, proto.PermWriteParams{File: r.File, To: to, Decision: r.Decision, Text: r.Text},
			"moved "+r.Text+" to "+tools.TildeHome(to))
	case "add":
		if act.Text == "" || act.Decision == "" || act.Index < 0 || act.Index >= len(a.Files) {
			return nil
		}
		f := a.Files[act.Index]
		return s.permWrite(p, proto.MethodPermAdd, proto.PermWriteParams{File: f.Path, Decision: act.Decision, Text: act.Text},
			"added "+act.Decision+" "+act.Text+" to "+tools.TildeHome(f.Path))
	case "editors":
		res, err := c.PermEditors()
		if err != nil {
			s.permSay(tools.ErrorText(err), true)
			return nil
		}
		s.mu.Lock()
		p.editors = res.Editors
		p.state.Editors = nil
		for _, e := range res.Editors {
			p.state.Editors = append(p.state.Editors, tools.PermEditorLabel(e))
		}
		s.mu.Unlock()
		s.stateChanged()
	case "open":
		path := ""
		switch {
		case act.Index >= 0 && act.Index < len(a.Files):
			path = a.Files[act.Index].Path
		case item.Rule != nil:
			path = item.Rule.File
		case item.Mode != nil:
			path = item.Mode.File
		default:
			for _, f := range a.Files {
				if f.Exists && path == "" {
					path = f.Path
				}
			}
			if path == "" && len(a.Files) > 0 {
				path = a.Files[0].Path
			}
		}
		editor := ""
		s.mu.Lock()
		if i := act.EditorAt; i != nil && *i >= 0 && *i < len(p.editors) {
			editor = p.editors[*i].Command
		}
		s.mu.Unlock()
		return s.permOpen(p, path, editor)
	}
	return nil
}

// permOpen opens a file in an editor ("" the default): a graphical one in
// a window of its own, the screen staying up; a terminal one in a pane
// beside the one the screen was opened over, which the window goes to.
// The TUI brings its panel back when that editor closes; here the screen
// is opened again from the menu, and reads the file again then.
func (s *Session) permOpen(p *permScreen, path, editor string) error {
	if path == "" {
		return nil
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	res, err := c.PermOpen(proto.PermOpenParams{Pane: p.pane, Path: path, Editor: editor})
	if err != nil {
		s.permSay(tools.ErrorText(err), true)
		return nil
	}
	if res.Window {
		s.permSay("opened "+tools.TildeHome(path)+" in a window — reload once you save", false)
		return nil
	}
	s.OpenPanel("")
	return s.JumpToPane(res.Pane)
}

// helpBefore is a panel's help up to a heading: the TUI's help says which
// keys do what, which in the window are the buttons in view.
func helpBefore(help []string, heading string) []string {
	for i, l := range help {
		if l == heading {
			return help[:i]
		}
	}
	return help
}
