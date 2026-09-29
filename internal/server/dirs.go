package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/worktree"
)

// maxListedDirs bounds one listing. A folder with more subfolders than this
// is not one anybody picks from by scrolling, and the answer has to fit in
// a frame.
const maxListedDirs = 5000

// listDirs answers dir.list: the folders in a folder, for the folder picker.
// It holds no lock — it reads the disk and nothing of the session.
//
// A link to a folder is listed as a folder, since that is where it takes
// you. Hidden folders are listed too; the picker decides whether to show
// them.
func listDirs(path string) (proto.DirListResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "~"
	}
	abs, err := filepath.Abs(worktree.ExpandHome(path))
	if err != nil {
		return proto.DirListResult{}, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return proto.DirListResult{}, fmt.Errorf("no such folder: %s", abs)
		}
		return proto.DirListResult{}, err
	}
	out := proto.DirListResult{Path: abs, Dirs: []string{}}
	if parent := filepath.Dir(abs); parent != abs {
		out.Parent = parent
	}
	for _, e := range entries {
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(abs, e.Name())); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			out.Dirs = append(out.Dirs, e.Name())
		}
		if len(out.Dirs) == maxListedDirs {
			break
		}
	}
	slices.SortFunc(out.Dirs, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out, nil
}
