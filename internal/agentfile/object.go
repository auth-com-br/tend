package agentfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/auth-com-br/tend/internal/jsonc"
)

// Object is a JSON object that keeps its keys in the order the file has
// them, each value as its own bytes. The permissions manager needs the
// order: OpenCode applies the last rule that matches, so its `permission`
// object means something else sorted. And a settings file the person keeps
// by hand comes back with its keys where they were, only the value tend
// changed written again — numbers and all else as they were.
type Object struct {
	keys []string
	vals map[string]json.RawMessage
	// src is the file the object was read from, which Bytes patches.
	src []byte
}

// ErrNotObject is a file, or a value, that is not a JSON object.
var ErrNotObject = errors.New("is not a JSON object")

// ParseObject reads a JSON object. An empty file is an empty object; a file
// with comments (JSONC) is refused, as tend does not write those.
func ParseObject(data []byte) (*Object, error) {
	o := &Object{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	o.src = append([]byte(nil), data...)
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("is not plain JSON (comments are not edited here; change it by hand): %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, ErrNotObject
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("is not plain JSON (comments are not edited here; change it by hand): %w", err)
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("is not plain JSON (comments are not edited here; change it by hand): %w", err)
		}
		o.Set(key, raw)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("is not plain JSON: %w", err)
	}
	// Anything after the object is not JSON either.
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("is not plain JSON: something follows the object")
	}
	return o, nil
}

// ObjectOf reads a value as an object; a missing or null value is an empty
// one.
func ObjectOf(raw json.RawMessage) (*Object, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return &Object{vals: map[string]json.RawMessage{}}, nil
	}
	if t[0] != '{' {
		return nil, ErrNotObject
	}
	return ParseObject(t)
}

// Get is a key's value, and whether the object has it.
func (o *Object) Get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Set gives a key a value: in its place when the object has it, last when
// it is new. A key the file repeats keeps its first place and its last
// value, which is how JSON readers take it.
func (o *Object) Set(key string, v json.RawMessage) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// Delete removes a key.
func (o *Object) Delete(key string) {
	if _, ok := o.vals[key]; !ok {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
			break
		}
	}
}

// Keys are the object's keys, in order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Len is how many keys the object has.
func (o *Object) Len() int { return len(o.keys) }

// Raw is the object as compact JSON, to be Set into another.
func (o *Object) Raw() (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		if err := json.Compact(&buf, o.vals[k]); err != nil {
			return nil, fmt.Errorf("%q: %w", k, err)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Bytes is the object as a file: two-space indentation, which is what the
// agents write, and a final newline. What it writes is read back first, so
// a mistake here cannot leave an agent unable to start.
func (o *Object) Bytes() ([]byte, error) {
	raw, err := o.Raw()
	if err != nil {
		return nil, err
	}
	if patched, ok := o.patched(raw); ok {
		return patched, nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	if _, err := ParseObject(out.Bytes()); err != nil {
		return nil, fmt.Errorf("the edit would not read back: %w", err)
	}
	return out.Bytes(), nil
}

// patched is the file the object was read from with only what changed
// written again (issue #61): its layout, and the order of what was not
// touched, as it was. It is taken only when it says exactly what raw says,
// keys in raw's order — the order is what a rule means in OpenCode, and a
// rule moved must not come back unmoved.
func (o *Object) patched(raw json.RawMessage) ([]byte, bool) {
	if len(o.src) == 0 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var want any
	if err := dec.Decode(&want); err != nil {
		return nil, false
	}
	out, err := jsonc.Patch(o.src, want)
	if err != nil {
		return nil, false
	}
	got, err1 := jsonc.Canonical(out)
	meant, err2 := jsonc.Canonical(raw)
	if err1 != nil || err2 != nil || got != meant {
		return nil, false
	}
	if _, err := ParseObject(out); err != nil {
		return nil, false
	}
	return out, true
}

// Marshal encodes a value without escaping <, > and & — a URL with a query,
// or a shell rule with a redirection, would otherwise come back as &.
func Marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes())), nil
}
