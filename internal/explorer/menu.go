package explorer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/auth-com-br/tend/internal/vt"
)

// The tree's context menu is herdr-sidebar's (`actions.rs`, menu_entries):
// make a file or a folder, open with the system's app, stage, copy the path,
// rename, delete, reveal in the file manager, change the folder shown. m
// opens it on the entry under the cursor, as does a right-click on one.

type menuAction int

const (
	actNewFile menuAction = iota
	actNewFolder
	actOpenExternal
	actStage
	actCopyPath
	actCopyRelative
	actRename
	actDelete
	actReveal
	actChangeFolder
	actFileHistory
	// actAddContext puts the entry's path in the session's context buffer,
	// tend's own (internal/capture): for the context panel to hand an agent.
	actAddContext
	// The agents' instructions (instructions.go), tend's own.
	actInstrGlobal
	actInstrProject
	actInstrHere
	actInstrShare
)

type menuItem struct {
	action menuAction
	label  string
}

// menuEntries is what a target offers: a file, a folder (isDir), or the
// project itself (none, from empty space) which only takes creation and
// the folder actions. Stage is offered only inside a repository, so the
// menu never offers what can only fail.
func menuEntries(target *Node, inRepo bool) []menuItem {
	items := []menuItem{{actNewFile, "New file…"}, {actNewFolder, "New folder…"}}
	if target != nil && !target.Dir {
		items = append(items, menuItem{actOpenExternal, "Open with default app"})
	}
	if target != nil && inRepo {
		items = append(items, menuItem{actStage, "Stage changes"})
		if !target.Dir {
			items = append(items, menuItem{actFileHistory, "File history"})
		}
	}
	if target != nil {
		items = append(items,
			menuItem{actCopyPath, "Copy path"},
			menuItem{actCopyRelative, "Copy relative path"},
			menuItem{actRename, "Rename…"},
			menuItem{actDelete, "Delete"},
			// tend's own, after herdr-sidebar's: the entry into the context
			// buffer, for the context panel to hand an agent.
			menuItem{actAddContext, "Add to context"},
		)
	}
	return append(items,
		menuItem{actReveal, "Reveal in file manager"},
		menuItem{actChangeFolder, "Change folder…"},
	)
}

// contextMenu is the menu while it is up.
type contextMenu struct {
	target *Node
	items  []menuItem
	cursor int
}

// prompt is a line of text being typed for an action: a name, a path.
type prompt struct {
	label string
	text  string
	done  func(*Model, string)
}

func (m *Model) openMenu(target *Node) {
	m.menu = contextMenu{target: target, items: append(menuEntries(target, m.git.Top != ""), m.instructionItems(target)...)}
	m.mode = modeMenu
}

func (m *Model) menuKey(k Key) {
	c := &m.menu
	switch k.Name {
	case "esc", "q", "m", "ctrl+c":
		m.mode = modeList
	case "up", "k":
		c.cursor = (c.cursor + len(c.items) - 1) % len(c.items)
	case "down", "j":
		c.cursor = (c.cursor + 1) % len(c.items)
	case "enter", "space":
		m.mode = modeList
		m.runMenu(c.items[c.cursor].action, c.target)
	}
}

// menuRows is where the menu's items are drawn: from row 2, one each.
func (m *Model) menuMouse(ev Mouse) {
	if !ev.Press {
		return
	}
	i := ev.Y - 2
	if i < 0 || i >= len(m.menu.items) {
		m.mode = modeList
		return
	}
	m.mode = modeList
	m.runMenu(m.menu.items[i].action, m.menu.target)
}

func (m *Model) drawMenu(g *vt.Grid) {
	c := &m.menu
	title := m.projectName()
	if c.target != nil {
		title = c.target.Name
	}
	put(g, 1, 0, truncate(title, m.cols-2), styleAccent, m.cols)
	for i, item := range c.items {
		y := 2 + i
		if y >= m.rows-1 {
			break
		}
		style := styleNormal
		if item.action == actDelete {
			style = styleRed
		}
		if i == c.cursor {
			fill(g, y, 0, m.cols, styleSel)
			style = styleSel
		}
		put(g, 2, y, truncate(item.label, m.cols-3), style, m.cols)
	}
	put(g, 1, m.rows-1, truncate("enter choose  esc", m.cols-2), styleDim, m.cols)
}

// dirFor is the directory a new entry goes in: the folder itself, or the
// one a file is in, or the project.
func (m *Model) dirFor(target *Node) string {
	switch {
	case target == nil:
		return m.tree.Root
	case target.Dir:
		return m.tree.Path(target)
	}
	return filepath.Dir(m.tree.Path(target))
}

// validName is a name, not a path: herdr-sidebar's validate_name.
func validName(input string) (string, bool) {
	name := strings.TrimSpace(input)
	ok := name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`)
	return name, ok
}

func (m *Model) ask(label, text string, done func(*Model, string)) {
	m.prompt = prompt{label: label, text: text, done: done}
	m.mode = modePrompt
}

func (m *Model) promptKey(k Key) {
	p := &m.prompt
	switch k.Name {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "enter":
		m.mode = modeList
		p.done(m, p.text)
	case "backspace":
		p.text = dropLastRune(p.text)
	case "ctrl+u":
		p.text = ""
	case "ctrl+w":
		p.text = dropLastWord(p.text)
	default:
		if k.Rune != 0 {
			p.text += string(k.Rune)
		}
	}
}

// clipboardOut is where the copy sequence goes: the panel's terminal, whose
// OSC 52 tend passes on to the clipboard of the machine the client is on.
var clipboardOut io.Writer = os.Stdout

func (m *Model) runMenu(action menuAction, target *Node) {
	switch action {
	case actNewFile, actNewFolder:
		dir := m.dirFor(target)
		label := "new file in " + m.relToRootOrDot(dir)
		if action == actNewFolder {
			label = "new folder in " + m.relToRootOrDot(dir)
		}
		folder := action == actNewFolder
		m.ask(label, "", func(m *Model, input string) {
			name, ok := validName(input)
			if !ok {
				m.say("a name, not a path", true)
				return
			}
			path := filepath.Join(dir, name)
			if _, err := os.Lstat(path); err == nil {
				m.say(name+" already exists", true)
				return
			}
			var err error
			if folder {
				err = os.Mkdir(path, 0o755)
			} else {
				err = os.WriteFile(path, nil, 0o644)
			}
			if err != nil {
				m.fail(err)
				return
			}
			m.Refresh()
			m.reveal(filepath.ToSlash(m.relToRoot(path)))
			m.say("made "+name, false)
		})
	case actOpenExternal:
		m.fail(openWithSystem(m.tree.Path(target)))
	case actReveal:
		dir := m.tree.Root
		if target != nil {
			dir = m.dirFor(target)
		}
		m.fail(openWithSystem(dir))
	case actStage:
		m.stagePath(target)
	case actInstrGlobal:
		path, err := globalInstructions()
		if err != nil {
			m.fail(err)
			return
		}
		m.openInstructions(path)
	case actInstrProject:
		path, _ := instructionFile(m.tree.Root)
		m.openInstructions(path)
	case actInstrHere:
		path, _ := instructionFile(m.dirFor(target))
		m.openInstructions(path)
	case actInstrShare:
		said, err := share(m.tree.Root)
		if err != nil {
			m.say(err.Error(), true)
			return
		}
		m.Refresh()
		m.say(said, false)
	case actFileHistory:
		m.openHistory(histCommits, m.tree.repoPath(m.git, target.Rel))
	case actAddContext:
		adder, ok := m.opener.(ContextAdder)
		if !ok {
			m.say("not in a tend session: no context to add to", true)
			return
		}
		if err := adder.AddContext("file", m.tree.Path(target)); err != nil {
			m.say("context: "+err.Error(), true)
			return
		}
		m.say("added "+target.Rel+" to the context (prefix+C)", false)
	case actCopyPath, actCopyRelative:
		path := m.tree.Path(target)
		if action == actCopyRelative {
			path = target.Rel
		}
		fmt.Fprintf(clipboardOut, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(path)))
		m.say("copied "+path, false)
	case actRename:
		old := m.tree.Path(target)
		m.ask("rename "+target.Name+" to", target.Name, func(m *Model, input string) {
			name, ok := validName(input)
			if !ok {
				m.say("a name, not a path", true)
				return
			}
			if name == filepath.Base(old) {
				return
			}
			path := filepath.Join(filepath.Dir(old), name)
			if _, err := os.Lstat(path); err == nil {
				m.say(name+" already exists", true)
				return
			}
			if err := os.Rename(old, path); err != nil {
				m.fail(err)
				return
			}
			m.Refresh()
			m.reveal(filepath.ToSlash(m.relToRoot(path)))
			m.say("renamed to "+name, false)
		})
	case actDelete:
		what := target.Name
		if target.Dir {
			what += " and everything in it"
		}
		path, isDir := m.tree.Path(target), target.Dir
		// Typed, not a key pressed twice: deleting a folder takes everything
		// under it, and a folder is not in git's keeping to bring back.
		m.ask("delete "+what+"? type yes", "", func(m *Model, input string) {
			if strings.TrimSpace(strings.ToLower(input)) != "yes" {
				m.say("not deleted", false)
				return
			}
			var err error
			if isDir {
				err = os.RemoveAll(path)
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				m.fail(err)
				return
			}
			m.Refresh()
			m.say("deleted "+filepath.Base(path), false)
		})
	case actChangeFolder:
		m.ask("show folder", m.tree.Root, func(m *Model, input string) {
			dir := strings.TrimSpace(input)
			if strings.HasPrefix(dir, "~/") {
				if home, err := os.UserHomeDir(); err == nil {
					dir = filepath.Join(home, dir[2:])
				}
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(m.tree.Root, dir)
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				m.say(dir+" is not a folder", true)
				return
			}
			m.Reroot(dir)
			// A folder chosen by hand holds until a pane beside moves.
			m.follow.manual = true
		})
	}
}

func (m *Model) relToRootOrDot(dir string) string {
	rel := m.relToRoot(dir)
	if rel == "." {
		return m.projectName()
	}
	return rel
}

// stagePath stages a file or everything under a folder, from the tree.
func (m *Model) stagePath(target *Node) {
	if target == nil || m.git.Top == "" {
		return
	}
	if err := m.git.Stage(m.tree.repoPath(m.git, target.Rel)); err != nil {
		m.fail(err)
		return
	}
	m.say("staged "+target.Rel, false)
	m.Refresh()
}

// openWithSystem hands a path to the desktop: xdg-open, or open on a Mac.
// It runs on the machine the panel runs on, which over --remote is not the
// one in front of the user, and says so rather than seeming to do nothing.
func openWithSystem(path string) error {
	program := "xdg-open"
	if runtime.GOOS == "darwin" {
		program = "open"
	}
	if _, err := exec.LookPath(program); err != nil {
		return errors.New("no " + program + " on this machine")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" && runtime.GOOS != "darwin" {
		return errors.New("no desktop on this machine to open it on")
	}
	cmd := exec.Command(program, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ContextAdder is an opener that can put an item in the session's context
// buffer: tend's session, over its automation socket.
type ContextAdder interface {
	AddContext(kind, path string) error
}
