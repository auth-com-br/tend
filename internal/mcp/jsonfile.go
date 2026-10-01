package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// JSON files are edited as maps of raw values: the top level, and the
// object that holds the servers. Only the one entry being changed is
// encoded again; every other value keeps its bytes, so a large number in
// ~/.claude.json does not come back as a float, and nothing tend does not
// understand is touched. What is lost is the order of the top-level keys,
// which come back sorted — the same trade the hooks' settings.json edits
// already make (docs/PORTING.md).

// decodeObject reads a JSON object; an empty file is an empty object, as
// Antigravity leaves its config.
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(data)) == 0 {
		return obj, nil
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("is not plain JSON (comments are not edited here; change it by hand): %w", err)
	}
	return obj, nil
}

// encodeObject writes an object back with two-space indentation, which is
// what each of these agents writes, and without escaping <, > and & — a
// URL with a query would otherwise come back as &.
func encodeObject(obj map[string]json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// marshal encodes a value the way encodeObject does.
func marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes())), nil
}

// readServers is the server map under key in a JSON file, by name, raw; a
// file that is not there has none.
func readServers(path, key string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	top, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s %w", path, err)
	}
	return serversIn(top, key)
}

func serversIn(top map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	servers := map[string]json.RawMessage{}
	raw, ok := top[key]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return servers, nil
	}
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("%q is not an object of servers: %w", key, err)
	}
	return servers, nil
}

// editServers changes the server map under key in a JSON file's bytes, and
// nothing else in it.
func editServers(data []byte, key string, edit func(servers map[string]json.RawMessage) error) ([]byte, error) {
	top, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	servers, err := serversIn(top, key)
	if err != nil {
		return nil, err
	}
	if err := edit(servers); err != nil {
		return nil, err
	}
	raw, err := marshal(servers)
	if err != nil {
		return nil, err
	}
	top[key] = raw
	out, err := encodeObject(top)
	if err != nil {
		return nil, err
	}
	// What is written must read back: the rule internal/config's own
	// writes follow, so a mistake here cannot leave an agent unable to
	// start.
	if _, err := decodeObject(out); err != nil {
		return nil, fmt.Errorf("the edit would not read back: %w", err)
	}
	return out, nil
}

// findName is the name a server is kept under in servers, matched as Key
// matches names; "" when there is none.
func findName(servers map[string]json.RawMessage, name string) string {
	if _, ok := servers[name]; ok {
		return name
	}
	for n := range servers {
		if Key(n) == Key(name) {
			return n
		}
	}
	return ""
}

// object decodes a raw server into its fields.
func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	err := json.Unmarshal(raw, &obj)
	return obj, err
}

// str, strs and strMap read one field of a server, empty when absent or of
// another type: a field tend cannot read is left as it is, and shown as
// what is missing.
func str(obj map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(obj[key], &s)
	return strings.TrimSpace(s)
}

func strs(obj map[string]json.RawMessage, key string) []string {
	var s []string
	_ = json.Unmarshal(obj[key], &s)
	return s
}

func strMap(obj map[string]json.RawMessage, key string) map[string]string {
	var m map[string]string
	_ = json.Unmarshal(obj[key], &m)
	if len(m) == 0 {
		return nil
	}
	return m
}

// boolField is a field that is a bool, and whether it is there.
func boolField(obj map[string]json.RawMessage, key string) (bool, bool) {
	raw, ok := obj[key]
	if !ok {
		return false, false
	}
	var b bool
	if json.Unmarshal(raw, &b) != nil {
		return false, false
	}
	return b, true
}
