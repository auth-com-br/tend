package agentfile

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAnObjectComesBackWithItsKeysInTheirPlaces: OpenCode applies the last
// permission rule that matches, so a rewrite that sorted the keys would
// change what the agent may do; and a settings file kept by hand would come
// back shuffled.
func TestAnObjectComesBackWithItsKeysInTheirPlaces(t *testing.T) {
	in := `{
  "zeta": 1,
  "bash": {"*": "ask", "git *": "allow", "git push *": "deny"},
  "alpha": 12345678901234567890
}`
	o, err := ParseObject([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	o.Set("new", []byte(`"x"`))
	o.Set("zeta", []byte(`2`))
	out, err := o.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	order := []string{`"zeta": 2`, `"*": "ask"`, `"git *": "allow"`, `"git push *": "deny"`, `"alpha": 12345678901234567890`, `"new": "x"`}
	at := 0
	for _, want := range order {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", want, got)
		}
		at += i
	}
}

// TestDeletingAKeyKeepsTheOthersInOrder: removing a rule must not move the
// ones around it.
func TestDeletingAKeyKeepsTheOthersInOrder(t *testing.T) {
	o, _ := ParseObject([]byte(`{"a":1,"b":2,"c":3}`))
	o.Delete("b")
	if got := strings.Join(o.Keys(), ","); got != "a,c" {
		t.Fatalf("keys %s", got)
	}
}

// TestAFileWithCommentsIsRefused: tend does not write JSONC, whose comments
// it would lose.
func TestAFileWithCommentsIsRefused(t *testing.T) {
	if _, err := ParseObject([]byte("{\n // a comment\n \"a\": 1\n}")); err == nil {
		t.Fatal("a JSONC file was taken as JSON")
	}
	if _, err := ParseObject([]byte(`[1]`)); err == nil {
		t.Fatal("an array was taken as an object")
	}
}

// TestAShellRuleIsWrittenAsTyped: Marshal does not escape &, < and >, so
// `Bash(a && b)` stays readable in the file.
func TestAShellRuleIsWrittenAsTyped(t *testing.T) {
	raw, _ := Marshal("Bash(make && make install > log)")
	if string(raw) != `"Bash(make && make install > log)"` {
		t.Fatalf("got %s", raw)
	}
}

// TestAnEditWritesOnlyWhatChanged: a file laid out by hand — four spaces,
// a list on one line — comes back with only the value changed written
// again (issue #61); a key moved still moves.
func TestAnEditWritesOnlyWhatChanged(t *testing.T) {
	src := "{\n    \"b\": [1, 2, 3],\n    \"a\": {\"x\": 1}\n}\n"
	o, err := ParseObject([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	o.Set("a", json.RawMessage(`{"x":2}`))
	out, err := o.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n    \"b\": [1, 2, 3],\n    \"a\": {\"x\": 2}\n}\n"; string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}

	o, _ = ParseObject([]byte(src))
	o.Delete("b")
	o.Set("b", json.RawMessage(`[1,2,3]`))
	out, err = o.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(out), `"a"`) > strings.Index(string(out), `"b"`) {
		t.Fatalf("b was moved last and came back first:\n%s", out)
	}
}
