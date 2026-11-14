// Package preset reads a layout preset: spaces, their tabs, how each tab is
// split, and what each pane runs, written by hand and opened at once — what
// tmuxp is to tmux (herdr#1359 asks it of herdr, which has none).
//
//	# ~/.config/tend/layouts/api.toml
//	[[space]]
//	name = "api"
//	dir = "~/code/api"
//
//	  [[space.tab]]
//	  name = "work"
//	  split = "columns"            # side by side; "rows" is one above another
//
//	    [[space.tab.pane]]
//	    agent = "claude"           # started as tend starts agents: policies hold
//	    size = 0.6
//
//	    [[space.tab.pane]]
//	    split = "rows"
//	      [[space.tab.pane.pane]]
//	      run = "npm run dev"      # typed into the pane's shell
//	      [[space.tab.pane.pane]]
//	      dir = "server"           # relative to the space's directory
//
// A tab with one pane may say run or agent itself. Saved layouts
// (`tend layout save`) stay what they are: a file a program wrote, for
// one tab; a preset is for a person to write, for a whole working setup.
package preset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/auth-com-br/tend/internal/session"
)

// File is a preset.
type File struct {
	Spaces []Space `toml:"space"`
}

// Space is one space of a preset.
type Space struct {
	Name string `toml:"name"`
	Dir  string `toml:"dir"`
	Tabs []Tab  `toml:"tab"`
}

// Tab is one tab: a pane tree, its root here.
type Tab struct {
	Name string `toml:"name"`
	Node
}

// Node is a pane, or a split of panes.
type Node struct {
	Split string  `toml:"split"`
	Size  float64 `toml:"size"`
	Panes []Node  `toml:"pane"`

	Run   string `toml:"run"`
	Agent string `toml:"agent"`
	Dir   string `toml:"dir"`
	// Title names the pane, as renaming it does.
	Title string `toml:"title"`
}

// Leaf is what one pane of a built tab is to do once it exists, in the
// order the tree lists its panes.
type Leaf struct {
	Dir   string
	Run   string
	Agent string
	Title string
}

// Built is a tab ready for layout.apply: the tree, a pane per leaf (ids 1…n,
// which layout.apply reads as names, not as the panes it makes), and what
// each leaf does after.
type Built struct {
	Name   string
	Tree   session.LayoutSnapshot
	Panes  []session.PaneSnapshot
	Leaves []Leaf
}

// Load reads a preset: a path, or a name in dir (a.toml for "a").
func Load(nameOrPath, dir string) (File, string, error) {
	path := nameOrPath
	if !strings.ContainsRune(path, os.PathSeparator) && !strings.HasSuffix(path, ".toml") {
		path = filepath.Join(dir, path+".toml")
	}
	var f File
	meta, err := toml.DecodeFile(path, &f)
	if err != nil {
		return File{}, path, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		// A misspelt key would otherwise be a pane quietly running nothing.
		return File{}, path, fmt.Errorf("%s: tend does not know %q", path, undecoded[0].String())
	}
	if len(f.Spaces) == 0 {
		return File{}, path, fmt.Errorf("%s: no [[space]] in it", path)
	}
	return f, path, nil
}

// Expand makes a directory absolute: ~ is home, a relative one is under base.
func Expand(dir, base string) string {
	switch {
	case dir == "":
		return base
	case dir == "~" || strings.HasPrefix(dir, "~/"):
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(dir, "~"))
	case filepath.IsAbs(dir):
		return dir
	default:
		return filepath.Join(base, dir)
	}
}

// Build turns a tab into what layout.apply takes, its directories under the
// space's.
func Build(t Tab, spaceDir string) (Built, error) {
	b := Built{Name: t.Name}
	tree, err := b.node(t.Node, spaceDir, "tab "+t.Name)
	if err != nil {
		return Built{}, err
	}
	b.Tree = tree
	return b, nil
}

func (b *Built) node(n Node, dir, where string) (session.LayoutSnapshot, error) {
	if len(n.Panes) == 0 {
		if n.Split != "" {
			return session.LayoutSnapshot{}, fmt.Errorf("%s: split %q with no panes in it", where, n.Split)
		}
		if n.Run != "" && n.Agent != "" {
			return session.LayoutSnapshot{}, fmt.Errorf("%s: a pane runs a command or an agent, not both", where)
		}
		id := uint64(len(b.Leaves) + 1)
		d := Expand(n.Dir, dir)
		b.Leaves = append(b.Leaves, Leaf{Dir: d, Run: n.Run, Agent: n.Agent, Title: n.Title})
		b.Panes = append(b.Panes, session.PaneSnapshot{ID: id, Dir: d})
		return session.LayoutSnapshot{Pane: id}, nil
	}
	if n.Run != "" || n.Agent != "" {
		return session.LayoutSnapshot{}, errors.New(where + ": a split's panes run things, the split does not")
	}
	split := n.Split
	switch split {
	case "", "columns":
		split = "columns"
	case "rows":
	default:
		return session.LayoutSnapshot{}, fmt.Errorf("%s: split %q is not columns or rows", where, n.Split)
	}
	if len(n.Panes) == 1 {
		// A split of one is that one pane.
		return b.node(n.Panes[0], Expand(n.Dir, dir), where)
	}
	out := session.LayoutSnapshot{Dir: split}
	sized, total := 0, 0.0
	for i, kid := range n.Panes {
		k, err := b.node(kid, Expand(n.Dir, dir), fmt.Sprintf("%s, pane %d", where, i+1))
		if err != nil {
			return session.LayoutSnapshot{}, err
		}
		out.Kids = append(out.Kids, k)
		if kid.Size > 0 {
			sized++
			total += kid.Size
		}
	}
	if sized > 0 {
		// The panes given a size have it; the rest share what is left.
		if total >= 1 && sized < len(n.Panes) {
			return session.LayoutSnapshot{}, fmt.Errorf("%s: the sizes add up to %.2f and leave nothing for the other panes", where, total)
		}
		rest := 0.0
		if sized < len(n.Panes) {
			rest = (1 - total) / float64(len(n.Panes)-sized)
		}
		for _, kid := range n.Panes {
			if kid.Size > 0 {
				out.Sizes = append(out.Sizes, kid.Size)
			} else {
				out.Sizes = append(out.Sizes, rest)
			}
		}
		sum := 0.0
		for _, s := range out.Sizes {
			sum += s
		}
		for i := range out.Sizes {
			out.Sizes[i] /= sum
		}
	}
	return out, nil
}

// Leaves lists a built tab's panes as layout.export gives the tree back:
// left to right and top to bottom, which is the order Build made them in.
func Leaves(tree session.LayoutSnapshot) []uint64 {
	if len(tree.Kids) == 0 {
		return []uint64{tree.Pane}
	}
	var out []uint64
	for _, k := range tree.Kids {
		out = append(out, Leaves(k)...)
	}
	return out
}
