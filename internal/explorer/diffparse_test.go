package explorer

import "testing"

// TestADiffParsesIntoNumberedLines: each line of a hunk has its kind and
// its numbers in the old and new files, a removed line that looks like a
// file header is still a line of the hunk, a missing newline is marked, and
// the files of a commit come apart with renames and new files named. If it
// regresses, the desktop's side-by-side diff puts lines beside the wrong
// ones, or loses a file.
func TestADiffParsesIntoNumberedLines(t *testing.T) {
	text := "commit abc\n\n    a message\n\n a.go | 2 +-\n" +
		"diff --git a/a.go b/a.go\n" +
		"index 1..2 100644\n" +
		"--- a/a.go\n" +
		"+++ b/a.go\n" +
		"@@ -1,4 +1,4 @@ func main() {\n" +
		" one\n" +
		"--- two\n" +
		"+++ two\n" +
		" three\n" +
		"-four\n" +
		"\\ No newline at end of file\n" +
		"+four\n" +
		"diff --git a/old.txt b/new.txt\n" +
		"similarity index 90%\n" +
		"rename from old.txt\n" +
		"rename to new.txt\n" +
		"diff --git a/n.txt b/n.txt\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/n.txt\n" +
		"@@ -0,0 +1 @@\n" +
		"+only\n" +
		"diff --git a/pic.png b/pic.png\n" +
		"Binary files a/pic.png and b/pic.png differ\n"
	files := ParseDiff(text)
	if len(files) != 4 {
		t.Fatalf("files: %+v", files)
	}
	a := files[0]
	if a.OldPath != "a.go" || a.NewPath != "a.go" || len(a.Hunks) != 1 {
		t.Fatalf("a.go: %+v", a)
	}
	h := a.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 4 || h.NewStart != 1 || h.NewLines != 4 || h.Section != "func main() {" {
		t.Errorf("header: %+v", h)
	}
	want := []DiffLine{
		{DiffContext, 1, 1, "one"},
		{DiffRemoved, 2, 0, "-- two"},
		{DiffAdded, 0, 2, "++ two"},
		{DiffContext, 3, 3, "three"},
		{DiffRemoved, 4, 0, "four"},
		{DiffNoNewline, 0, 0, "No newline at end of file"},
		{DiffAdded, 0, 4, "four"},
	}
	if len(h.Lines) != len(want) {
		t.Fatalf("lines: %+v", h.Lines)
	}
	for i, l := range want {
		if h.Lines[i] != l {
			t.Errorf("line %d = %+v, want %+v", i, h.Lines[i], l)
		}
	}
	if r := files[1]; r.OldPath != "old.txt" || r.NewPath != "new.txt" || len(r.Hunks) != 0 {
		t.Errorf("rename: %+v", r)
	}
	if n := files[2]; n.OldPath != "" || n.Path() != "n.txt" || n.Hunks[0].NewLines != 1 || n.Hunks[0].Lines[0] != (DiffLine{DiffAdded, 0, 1, "only"}) {
		t.Errorf("new file: %+v", n)
	}
	if b := files[3]; !b.Binary || b.Path() != "pic.png" {
		t.Errorf("binary: %+v", b)
	}
}

// TestAHunkLaysOutSideBySide: context is on both sides, and a run of
// removals sits beside the additions that replaced it, the longer run
// leaving the other side empty. If it regresses, the desktop draws a
// changed line beside an unrelated one.
func TestAHunkLaysOutSideBySide(t *testing.T) {
	files := ParseDiff("--- a/x\n+++ b/x\n@@ -1,5 +1,3 @@\n keep\n-a\n-b\n+A\n tail\n-gone\n")
	if len(files) != 1 {
		t.Fatalf("files: %+v", files)
	}
	rows := files[0].Hunks[0].SideBySide()
	text := func(l *DiffLine) string {
		if l == nil {
			return "·"
		}
		return l.Text
	}
	var got []string
	for _, r := range rows {
		got = append(got, text(r.Left)+"|"+text(r.Right))
	}
	want := []string{"keep|keep", "a|A", "b|·", "tail|tail", "gone|·"}
	if len(got) != len(want) {
		t.Fatalf("rows: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %s, want %s", i, got[i], want[i])
		}
	}
}
