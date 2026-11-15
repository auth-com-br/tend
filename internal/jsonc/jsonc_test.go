package jsonc

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// settings is a Claude Code settings file as a person keeps one: comments,
// keys in their own order, a blank line, a trailing comma.
const settings = `{
  // my model, chosen after a week of trying
  "model": "opus",
  "permissions": {
    "allow": ["Bash(git status)", "Read"],
    "defaultMode": "acceptEdits"
  },

  /* hooks — careful */
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit",
        "hooks": [{ "type": "command", "command": "mine" }]
      }
    ],
    "Stop": [
      { "hooks": [{ "type": "command", "command": "say done" }] }, // speaks
    ]
  },
  "zeta": 1
}
`

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	v, err := Decode([]byte(s))
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, s)
	}
	return v.(map[string]any)
}

func patch(t *testing.T, src string, want any) string {
	t.Helper()
	out, err := Patch([]byte(src), want)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	return string(out)
}

// unchanged says every line of the original but the ones named is still in
// the result, in order.
func unchanged(t *testing.T, before, after string, except ...string) {
	t.Helper()
	rest := after
	for _, line := range strings.Split(before, "\n") {
		skip := false
		for _, e := range except {
			skip = skip || strings.Contains(line, e)
		}
		if skip {
			continue
		}
		i := strings.Index(rest, line)
		if i < 0 {
			t.Fatalf("the line %q is gone or moved:\n%s", line, after)
		}
		rest = rest[i+len(line):]
	}
}

// TestAddingAHookChangesOnlyWhereItGoes: tend guard install on a file with
// comments and an order of its own. If it regresses, installing a hook
// costs the person their comments, as encoding/json did.
func TestAddingAHookChangesOnlyWhereItGoes(t *testing.T) {
	v := decode(t, settings)
	hooks := v["hooks"].(map[string]any)
	hooks["PreToolUse"] = append(hooks["PreToolUse"].([]any), map[string]any{
		"matcher": "Bash", "hooks": []any{map[string]any{"type": "command", "command": "tend guard"}},
	})
	out := patch(t, settings, v)
	unchanged(t, settings, out)
	if !strings.Contains(out, `"command": "tend guard"`) || !strings.Contains(out, "\n      {\n        \"hooks\"") {
		t.Fatalf("the new entry is not laid out as its neighbour:\n%s", out)
	}
}

// TestTakingOutAHookTakesOnlyItsLines: uninstall, the entry in the middle
// and the one at the end; the comment on the line after stays.
func TestTakingOutAHookTakesOnlyItsLines(t *testing.T) {
	v := decode(t, settings)
	hooks := v["hooks"].(map[string]any)
	delete(hooks, "Stop")
	out := patch(t, settings, v)
	if strings.Contains(out, "say done") || strings.Contains(out, "speaks") {
		t.Fatalf("Stop is still there:\n%s", out)
	}
	// PreToolUse is the last key now, and its comma goes with Stop.
	unchanged(t, settings, out, "Stop", "say done", "    ],", "    ]\n")
	if !strings.Contains(out, "    ]\n  },") {
		t.Fatalf("PreToolUse should end the hooks, its comma gone:\n%s", out)
	}
	if !strings.Contains(out, "/* hooks — careful */") || !strings.Contains(out, "// my model") {
		t.Fatalf("a comment went with it:\n%s", out)
	}

	v = decode(t, settings)
	v["permissions"].(map[string]any)["allow"] = []any{"Read"}
	out = patch(t, settings, v)
	if !strings.Contains(out, `"allow": ["Read"],`) {
		t.Fatalf("the first item was not taken out alone:\n%s", out)
	}
	unchanged(t, settings, out, "allow")
}

// TestChangingAValueChangesThatValue, and a key added goes after the last,
// in the file's separator and indentation.
func TestChangingAValueChangesThatValue(t *testing.T) {
	v := decode(t, settings)
	v["model"] = "sonnet"
	v["alpha"] = map[string]any{"x": true}
	out := patch(t, settings, v)
	if !strings.Contains(out, `"model": "sonnet",`) {
		t.Fatalf("model:\n%s", out)
	}
	if !strings.Contains(out, "\"zeta\": 1,\n  \"alpha\": {\n    \"x\": true\n  }\n}") {
		t.Fatalf("the new key is not after the last:\n%s", out)
	}
	unchanged(t, settings, out, "model", `"zeta"`)
}

// TestFilesOfOtherShapes: an empty object, one on a line, and nothing left.
func TestFilesOfOtherShapes(t *testing.T) {
	if out := patch(t, "{}\n", map[string]any{"a": 1}); out != "{\"a\": 1}\n" {
		t.Errorf("empty, one line: %q", out)
	}
	if out := patch(t, "{\n}\n", map[string]any{"a": 1}); out != "{\n  \"a\": 1\n}\n" {
		t.Errorf("empty, on lines: %q", out)
	}
	if out := patch(t, `{"a":1,"b":2}`, map[string]any{"a": 1}); out != `{"a":1}` {
		t.Errorf("compact: %q", out)
	}
	if out := patch(t, `{"a":1,"b":2}`, map[string]any{"b": 2}); out != `{"b":2}` {
		t.Errorf("compact, first: %q", out)
	}
	if out := patch(t, "{\n  \"a\": 1\n}\n", map[string]any{}); out != "{}\n" {
		t.Errorf("nothing left: %q", out)
	}
}

// TestAnythingPatchedReadsAsWhatWasMeant: arrays reordered and changed in
// the middle still come out right — by splices where it can, by writing
// the part again where it cannot — never as something else.
func TestAnythingPatchedReadsAsWhatWasMeant(t *testing.T) {
	src := `{"list": [1, 2, 3, 4], "n": {"deep": [{"a": 1}, {"b": 2}]}}`
	for _, want := range []map[string]any{
		{"list": []any{4, 3, 2, 1}, "n": map[string]any{"deep": []any{}}},
		{"list": []any{1, 9, 3}, "n": map[string]any{"deep": []any{map[string]any{"b": 2}}}},
		{"list": "not a list any more"},
		{"list": []any{1, 2, 3, 4, 5, 6}, "n": nil},
	} {
		out, err := Patch([]byte(src), want)
		if err != nil {
			t.Errorf("%v: %v", want, err)
			continue
		}
		got, _ := Decode(out)
		norm, _ := Normalize(want)
		if !reflect.DeepEqual(got, norm) {
			t.Errorf("%v came out as %s", want, out)
		}
	}
}

// TestCommentsAndTrailingCommasAreRead as the agents read them, and broken
// text is refused.
func TestCommentsAndTrailingCommasAreRead(t *testing.T) {
	v := decode(t, settings)
	if v["model"] != "opus" || v["zeta"] != json.Number("1") {
		t.Fatalf("read %v", v)
	}
	for _, bad := range []string{`{"a": }`, `{"a": 1`, `{"a" 1}`, `[1 2]`, "{\"a\": \"x\ny\"}"} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
	if _, err := Patch([]byte(`{"a": 1, "a": 2}`), map[string]any{"a": 3}); err == nil {
		t.Error("a key the file repeats was patched")
	}
}
