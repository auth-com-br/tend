package explorer

import (
	"strconv"
	"strings"
)

// A diff is shown by the panel as git prints it, a line at a time; the
// desktop's files panel draws it side by side, and for that it needs what
// each line is and which line of the old and the new file it is. ParseDiff
// reads git's unified format (and jj's --git, which is the same) into that,
// and the text stays beside it, so the two clients show one diff.

// DiffLineKind is what a line of a hunk is.
type DiffLineKind int

const (
	// DiffContext is a line both files have.
	DiffContext DiffLineKind = iota
	// DiffAdded is only in the new file, DiffRemoved only in the old.
	DiffAdded
	DiffRemoved
	// DiffNoNewline is git's "\ No newline at end of file", about the line
	// before it.
	DiffNoNewline
)

// DiffLine is one line of a hunk.
type DiffLine struct {
	Kind DiffLineKind
	// Old and New are the line's numbers in the old and the new file,
	// counted from one; zero where the line is not in that file.
	Old, New int
	// Text is the line without the column git marks it with.
	Text string
}

// Hunk is one "@@ -a,b +c,d @@" of a file's diff and its lines.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	// Section is what git prints after the second @@: the function the
	// hunk is in, when git can tell.
	Section string
	Lines   []DiffLine
}

// FilePatch is one file of a diff.
type FilePatch struct {
	// OldPath and NewPath are the file before and after, as the repository
	// names them; "" for the side a new or deleted file has none of. They
	// differ for a rename.
	OldPath, NewPath string
	// Binary is a change git describes but does not show.
	Binary bool
	Hunks  []Hunk
}

// Path is the file the patch is about: its new name, or the old one of a
// file deleted.
func (f FilePatch) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Diff is a change as git printed it and as it parses.
type Diff struct {
	Text  string
	Files []FilePatch
}

// newDiff is a diff from git's text.
func newDiff(text string) Diff { return Diff{Text: text, Files: ParseDiff(text)} }

// ParseDiff reads a unified diff: one patch per file, each hunk with its
// lines numbered. What comes before the first file — a commit's message,
// the --stat git show prints — is not a patch and is left to the text.
//
// A hunk's lines are read by the counts in its header, not by how they
// start: a removed line reading "--- x" is a line of the hunk, and a parser
// that looked at the marks alone would take it for the next file's header.
func ParseDiff(text string) []FilePatch {
	var files []FilePatch
	var cur *FilePatch
	var hunk *Hunk
	oldLeft, newLeft := 0, 0
	oldAt, newAt := 0, 0
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	startFile := func() {
		files = append(files, FilePatch{})
		cur, hunk = &files[len(files)-1], nil
	}
	for i, line := range lines {
		if hunk != nil && (oldLeft > 0 || newLeft > 0 || strings.HasPrefix(line, `\`)) {
			l := DiffLine{}
			switch {
			case strings.HasPrefix(line, "+"):
				l = DiffLine{Kind: DiffAdded, New: newAt, Text: line[1:]}
				newAt++
				newLeft--
			case strings.HasPrefix(line, "-"):
				l = DiffLine{Kind: DiffRemoved, Old: oldAt, Text: line[1:]}
				oldAt++
				oldLeft--
			case strings.HasPrefix(line, `\`):
				l = DiffLine{Kind: DiffNoNewline, Text: strings.TrimSpace(line[1:])}
			default:
				// A context line; some tools strip its leading blank when
				// the line is empty, so an empty line is one too.
				if line != "" {
					line = line[1:]
				}
				l = DiffLine{Kind: DiffContext, Old: oldAt, New: newAt, Text: line}
				oldAt++
				newAt++
				oldLeft--
				newLeft--
			}
			hunk.Lines = append(hunk.Lines, l)
			continue
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			startFile()
			cur.OldPath, cur.NewPath = gitHeaderPaths(strings.TrimPrefix(line, "diff --git "))
		case strings.HasPrefix(line, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			if cur == nil || len(cur.Hunks) > 0 {
				// A diff with no "diff --git" line, as diff -u writes.
				startFile()
			}
			cur.OldPath = patchPath(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ ") && cur != nil && hunk == nil:
			cur.NewPath = patchPath(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "new file mode") && cur != nil:
			cur.OldPath = ""
		case strings.HasPrefix(line, "deleted file mode") && cur != nil:
			cur.NewPath = ""
		case strings.HasPrefix(line, "rename from ") && cur != nil:
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to ") && cur != nil:
			cur.NewPath = strings.TrimPrefix(line, "rename to ")
		case (strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch")) && cur != nil:
			cur.Binary = true
		case strings.HasPrefix(line, "@@ ") && cur != nil:
			h, ok := parseHunkHeader(line)
			if !ok {
				continue
			}
			cur.Hunks = append(cur.Hunks, h)
			hunk = &cur.Hunks[len(cur.Hunks)-1]
			oldLeft, newLeft = h.OldLines, h.NewLines
			oldAt, newAt = h.OldStart, h.NewStart
		}
	}
	return files
}

// parseHunkHeader reads "@@ -a,b +c,d @@ section"; a count left out is one.
func parseHunkHeader(line string) (Hunk, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	ranges, section, ok := strings.Cut(rest, " @@")
	if !ok {
		return Hunk{}, false
	}
	oldR, newR, ok := strings.Cut(ranges, " ")
	if !ok || !strings.HasPrefix(oldR, "-") || !strings.HasPrefix(newR, "+") {
		return Hunk{}, false
	}
	h := Hunk{Section: strings.TrimSpace(section)}
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = parseRange(oldR[1:])
	h.NewStart, h.NewLines, ok2 = parseRange(newR[1:])
	return h, ok1 && ok2
}

func parseRange(s string) (start, count int, ok bool) {
	a, b, has := strings.Cut(s, ",")
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if has {
		if count, err = strconv.Atoi(b); err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

// gitHeaderPaths reads "a/x b/y". A path with a space in it makes the line
// ambiguous, which the ---/+++ and rename lines that follow then settle.
func gitHeaderPaths(s string) (string, string) {
	if strings.HasPrefix(s, "a/") {
		if i := strings.Index(s, " b/"); i >= 0 {
			return s[2:i], s[i+3:]
		}
	}
	a, b, _ := strings.Cut(s, " ")
	return a, b
}

// patchPath is a ---/+++ path without git's a/ or b/, "" for /dev/null, and
// without the tab and timestamp diff -u adds.
func patchPath(s, prefix string) string {
	if i := strings.IndexByte(s, '\t'); i >= 0 {
		s = s[:i]
	}
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// DiffRow is a row of a hunk drawn side by side: the old file's line on
// the left, the new file's on the right, either nil where that side has
// none.
type DiffRow struct {
	Left, Right *DiffLine
}

// SideBySide lays a hunk out in rows: a context line on both sides, and a
// run of removals beside the run of additions after it, line for line, as
// an editor's side-by-side diff pairs a changed line with what it became.
func (h Hunk) SideBySide() []DiffRow {
	var rows []DiffRow
	lines := h.Lines
	for i := 0; i < len(lines); {
		l := &lines[i]
		switch l.Kind {
		case DiffRemoved, DiffAdded:
			var removed, added []*DiffLine
			for i < len(lines) && lines[i].Kind == DiffRemoved {
				removed = append(removed, &lines[i])
				i++
			}
			for i < len(lines) && (lines[i].Kind == DiffAdded || lines[i].Kind == DiffNoNewline) {
				if lines[i].Kind == DiffAdded {
					added = append(added, &lines[i])
				}
				i++
			}
			for j := 0; j < max(len(removed), len(added)); j++ {
				var row DiffRow
				if j < len(removed) {
					row.Left = removed[j]
				}
				if j < len(added) {
					row.Right = added[j]
				}
				rows = append(rows, row)
			}
		case DiffNoNewline:
			i++
		default:
			rows = append(rows, DiffRow{Left: l, Right: l})
			i++
		}
	}
	return rows
}
