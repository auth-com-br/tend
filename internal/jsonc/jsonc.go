// Package jsonc rewrites a JSON or JSONC file to hold a new value while
// changing only the text that has to change: comments, key order,
// indentation, blank lines and trailing commas everywhere else stay as the
// person wrote them.
//
// herdr does this with the jsonc-parser crate's CST (claude_settings.rs):
// it edits the tree in place and then checks that the text it wrote reads
// back as the value it meant, refusing otherwise. tend does the same
// without a dependency: Parse keeps each value's place in the text, Patch
// works out the smallest splices that turn the old value into the new one —
// a scalar replaced, an item taken out with its comma, an item added after
// the last in the separator and indentation the container already uses —
// and the result is read back and compared before it is returned. When the
// splices cannot be found (an array reordered, a file that will not parse),
// Patch says so and the caller writes the file the old way.
package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Kind is what a node is.
type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Node is a value and where it is in the text: [Start, End).
type Node struct {
	Kind       Kind
	Start, End int
	// Members of an object, in the file's order; Elems of an array.
	Members []Member
	Elems   []*Node
}

// Member is one key of an object, and its value.
type Member struct {
	Key        string
	KeyStart   int
	Value      *Node
	TrailComma int // where the comma after it is, or -1
}

// ErrSyntax is text that is not JSON or JSONC.
var ErrSyntax = errors.New("jsonc: not valid JSON")

type parser struct {
	src []byte
	pos int
}

// Parse reads JSON or JSONC: // and /* */ comments, and a trailing comma
// before a closing bracket, as the agents' own readers take them.
func Parse(src []byte) (*Node, error) {
	p := &parser{src: src}
	p.skip()
	n, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.pos != len(src) {
		return nil, p.fail("text after the value")
	}
	return n, nil
}

func (p *parser) fail(what string) error {
	line := 1 + bytes.Count(p.src[:min(p.pos, len(p.src))], []byte{'\n'})
	return fmt.Errorf("%w: %s at line %d", ErrSyntax, what, line)
}

// skip passes whitespace and comments.
func (p *parser) skip() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '*':
			end := bytes.Index(p.src[p.pos+2:], []byte("*/"))
			if end < 0 {
				p.pos = len(p.src)
				return
			}
			p.pos += end + 4
		default:
			return
		}
	}
}

func (p *parser) value() (*Node, error) {
	if p.pos >= len(p.src) {
		return nil, p.fail("a value missing")
	}
	start := p.pos
	switch c := p.src[p.pos]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		if err := p.str(); err != nil {
			return nil, err
		}
		return &Node{Kind: String, Start: start, End: p.pos}, nil
	case c == 't' || c == 'f' || c == 'n':
		for _, word := range []string{"true", "false", "null"} {
			if bytes.HasPrefix(p.src[p.pos:], []byte(word)) {
				p.pos += len(word)
				k := Bool
				if word == "null" {
					k = Null
				}
				return &Node{Kind: k, Start: start, End: p.pos}, nil
			}
		}
		return nil, p.fail("an unknown word")
	case c == '-' || (c >= '0' && c <= '9'):
		for p.pos < len(p.src) && strings.IndexByte("+-0123456789.eE", p.src[p.pos]) >= 0 {
			p.pos++
		}
		return &Node{Kind: Number, Start: start, End: p.pos}, nil
	}
	return nil, p.fail(fmt.Sprintf("an unexpected %q", p.src[p.pos]))
}

func (p *parser) str() error {
	p.pos++ // the opening quote
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case '\\':
			p.pos += 2
		case '"':
			p.pos++
			return nil
		case '\n':
			return p.fail("a line break in a string")
		default:
			p.pos++
		}
	}
	return p.fail("an unterminated string")
}

func (p *parser) object() (*Node, error) {
	n := &Node{Kind: Object, Start: p.pos}
	p.pos++
	for {
		p.skip()
		if p.pos >= len(p.src) {
			return nil, p.fail("an unclosed object")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			n.End = p.pos
			return n, nil
		}
		if p.src[p.pos] != '"' {
			return nil, p.fail("a key that is not a string")
		}
		keyStart := p.pos
		if err := p.str(); err != nil {
			return nil, err
		}
		var key string
		if err := json.Unmarshal(p.src[keyStart:p.pos], &key); err != nil {
			return nil, p.fail("a key that does not read")
		}
		p.skip()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return nil, p.fail("a missing colon")
		}
		p.pos++
		p.skip()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		m := Member{Key: key, KeyStart: keyStart, Value: v, TrailComma: -1}
		p.skip()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			m.TrailComma = p.pos
			p.pos++
		} else if p.pos < len(p.src) && p.src[p.pos] != '}' {
			return nil, p.fail("a missing comma")
		}
		n.Members = append(n.Members, m)
	}
}

func (p *parser) array() (*Node, error) {
	n := &Node{Kind: Array, Start: p.pos}
	p.pos++
	for {
		p.skip()
		if p.pos >= len(p.src) {
			return nil, p.fail("an unclosed array")
		}
		if p.src[p.pos] == ']' {
			p.pos++
			n.End = p.pos
			return n, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		n.Elems = append(n.Elems, v)
		p.skip()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
		} else if p.pos < len(p.src) && p.src[p.pos] != ']' {
			return nil, p.fail("a missing comma")
		}
	}
}

// Decode reads JSONC into Go's generic values, numbers as json.Number so a
// value read and written again is the same text.
func Decode(src []byte) (any, error) {
	n, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return n.decode(src)
}

func (n *Node) decode(src []byte) (any, error) {
	switch n.Kind {
	case Object:
		out := make(map[string]any, len(n.Members))
		for _, m := range n.Members {
			v, err := m.Value.decode(src)
			if err != nil {
				return nil, err
			}
			out[m.Key] = v
		}
		return out, nil
	case Array:
		out := make([]any, 0, len(n.Elems))
		for _, e := range n.Elems {
			v, err := e.decode(src)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(src[n.Start:n.End]))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSyntax, err)
	}
	return v, nil
}

// Normalize makes a value comparable with what Decode reads: a round trip
// through JSON, numbers kept as json.Number.
func Normalize(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- the patch ---------------------------------------------------------------

type edit struct {
	start, end int
	text       string
}

// ErrNoPatch is a change Patch would not make by splices; the caller writes
// the whole file instead.
var ErrNoPatch = errors.New("jsonc: no patch keeps the file as it was")

// Patch is src changed to hold want, the rest of the text as it was. want is
// any value encoding/json can write; objects tend adds are written with
// their keys sorted, in the file's own indentation.
func Patch(src []byte, want any) ([]byte, error) {
	root, err := Parse(src)
	if err != nil {
		return nil, err
	}
	desired, err := Normalize(want)
	if err != nil {
		return nil, err
	}
	pt := &patcher{src: src, unit: indentUnit(src)}
	if err := pt.node(root, desired); err != nil {
		return nil, err
	}
	out := apply(src, pt.edits)
	// Read back and compared, as herdr's verify_updated does: a patch that
	// says something else than it meant is not written.
	got, err := Decode(out)
	if err != nil {
		return nil, fmt.Errorf("%w: the patched text does not read: %v", ErrNoPatch, err)
	}
	if !reflect.DeepEqual(got, desired) {
		return nil, fmt.Errorf("%w: the patched text reads otherwise", ErrNoPatch)
	}
	return out, nil
}

type patcher struct {
	src   []byte
	unit  string
	edits []edit
}

func apply(src []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), src...)
	for _, e := range edits {
		out = append(out[:e.start], append([]byte(e.text), out[e.end:]...)...)
	}
	return out
}

func (pt *patcher) node(n *Node, want any) error {
	have, err := n.decode(pt.src)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(have, want) {
		return nil
	}
	switch w := want.(type) {
	case map[string]any:
		if n.Kind == Object {
			return pt.object(n, w)
		}
	case []any:
		if n.Kind == Array {
			return pt.array(n, w)
		}
	}
	return pt.replace(n, want)
}

// replace writes a value over a node, laid out at the node's depth.
func (pt *patcher) replace(n *Node, want any) error {
	text, err := pt.render(want, lineIndent(pt.src, n.Start))
	if err != nil {
		return err
	}
	pt.edits = append(pt.edits, edit{n.Start, n.End, text})
	return nil
}

func (pt *patcher) object(n *Node, want map[string]any) error {
	kept := make([]bool, len(n.Members))
	seen := map[string]bool{}
	for i, m := range n.Members {
		if seen[m.Key] {
			// A key the file repeats is one tend cannot change without
			// guessing which the agent reads.
			return fmt.Errorf("%w: the key %q is in the file twice", ErrNoPatch, m.Key)
		}
		seen[m.Key] = true
		if v, ok := want[m.Key]; ok {
			kept[i] = true
			if err := pt.node(m.Value, v); err != nil {
				return err
			}
		}
	}
	var added []string
	for k := range want {
		if !seen[k] {
			added = append(added, k)
		}
	}
	sort.Strings(added)
	items := make([]item, len(n.Members))
	for i, m := range n.Members {
		items[i] = item{start: m.KeyStart, end: m.Value.End}
	}
	var adds []string
	inner := lineIndent(pt.src, n.Start) + pt.unit
	if len(n.Members) > 0 {
		inner = lineIndent(pt.src, n.Members[0].KeyStart)
	}
	sep := ": "
	if len(n.Members) > 0 {
		sep = string(pt.src[keyEnd(pt.src, n.Members[0].KeyStart):n.Members[0].Value.Start])
	}
	for _, k := range added {
		key, _ := json.Marshal(k)
		v, err := pt.render(want[k], inner)
		if err != nil {
			return err
		}
		adds = append(adds, string(key)+sep+v)
	}
	return pt.container(n, items, kept, adds, inner)
}

func (pt *patcher) array(n *Node, want []any) error {
	have := make([]any, len(n.Elems))
	for i, e := range n.Elems {
		v, err := e.decode(pt.src)
		if err != nil {
			return err
		}
		have[i] = v
	}
	// The same length is an array changed item by item: each patched in
	// its place, comments and all.
	if len(have) == len(want) {
		for i, e := range n.Elems {
			if err := pt.node(e, want[i]); err != nil {
				return err
			}
		}
		return nil
	}
	// Otherwise the changes tend makes to hook and rule lists: items taken
	// out, and items added at the end. Each item kept is the next one
	// wanted that it equals; what is wanted after the last match is added.
	kept := make([]bool, len(n.Elems))
	w := 0
	for i := range n.Elems {
		if w < len(want) && reflect.DeepEqual(have[i], want[w]) {
			kept[i] = true
			w++
		}
	}
	items := make([]item, len(n.Elems))
	for i, e := range n.Elems {
		items[i] = item{start: e.Start, end: e.End}
	}
	inner := lineIndent(pt.src, n.Start) + pt.unit
	if len(n.Elems) > 0 {
		inner = lineIndent(pt.src, n.Elems[0].Start)
	}
	var adds []string
	for _, v := range want[w:] {
		text, err := pt.render(v, inner)
		if err != nil {
			return err
		}
		adds = append(adds, text)
	}
	return pt.container(n, items, kept, adds, inner)
}

type item struct{ start, end int }

// container takes the items not kept out of an object or array and adds
// adds after the last, in the separator and indentation it already has.
func (pt *patcher) container(n *Node, items []item, kept []bool, adds []string, inner string) error {
	src := pt.src
	last := -1
	for i, k := range kept {
		if k {
			last = i
		}
	}
	if last < 0 {
		// Nothing left of what was there: the container written anew,
		// with whatever is added.
		pt.edits = append(pt.edits, edit{n.Start + 1, n.End - 1, pt.fresh(n, adds, inner)})
		return nil
	}
	// An item before the last one kept goes with its comma: alone on its
	// line, with the line, so the comments about the items around it stay
	// with them; on a line with others, up to the next.
	for i := 0; i < last; i++ {
		if !kept[i] {
			pt.edits = append(pt.edits, pt.cutItem(items[i], items[i+1].start, n.End-1))
		}
	}
	// Items after the last kept one go from the end of it, with the commas
	// between; what is added takes their place.
	end := items[last].end
	cut := end
	if last < len(items)-1 {
		cut = items[len(items)-1].end
		if c := commaAfter(src, cut, n.End-1); c >= 0 {
			cut = c + 1
		}
	}
	var b strings.Builder
	sep := separator(src, items, n, inner)
	for _, a := range adds {
		b.WriteString(",")
		b.WriteString(sep)
		b.WriteString(a)
	}
	if b.Len() > 0 || cut != end {
		pt.edits = append(pt.edits, edit{end, cut, b.String()})
	}
	return nil
}

// cutItem is the text an item before others goes with.
func (pt *patcher) cutItem(it item, next, limit int) edit {
	src := pt.src
	end := it.end
	if c := commaAfter(src, end, limit); c >= 0 {
		end = c + 1
	}
	lineStart := bytes.LastIndexByte(src[:it.start], '\n') + 1
	lineEnd := bytes.IndexByte(src[end:], '\n')
	alone := strings.TrimSpace(string(src[lineStart:it.start])) == "" && lineEnd >= 0 &&
		strings.TrimSpace(string(src[end:end+lineEnd])) == ""
	if alone {
		return edit{lineStart, end + lineEnd + 1, ""}
	}
	return edit{it.start, next, ""}
}

// fresh is a container's inside written again with adds in it.
func (pt *patcher) fresh(n *Node, adds []string, inner string) string {
	if len(adds) == 0 {
		return ""
	}
	if !pt.multiline() {
		return strings.Join(adds, ", ")
	}
	outer := lineIndent(pt.src, n.Start)
	return "\n" + inner + strings.Join(adds, ",\n"+inner) + "\n" + outer
}

// separator is what goes between two items of a container: what the file
// has between its first two, after the comma, or a line break and the
// items' indentation when it has one item on a line of its own.
func separator(src []byte, items []item, n *Node, inner string) string {
	if len(items) >= 2 {
		between := string(src[items[0].end:items[1].start])
		if i := strings.IndexByte(between, ','); i >= 0 {
			between = between[i+1:]
		}
		// A comment between the first two belongs to them, not to every
		// item added: only the space and the line break are taken.
		if strings.Contains(between, "//") || strings.Contains(between, "/*") {
			if strings.Contains(between, "\n") {
				return "\n" + inner
			}
			return " "
		}
		return between
	}
	before := string(src[n.Start+1 : items[0].start])
	if strings.Contains(before, "\n") {
		return "\n" + inner
	}
	return " "
}

func commaAfter(src []byte, from, limit int) int {
	p := &parser{src: src[:limit], pos: from}
	p.skip()
	if p.pos < limit && src[p.pos] == ',' {
		return p.pos
	}
	return -1
}

func keyEnd(src []byte, keyStart int) int {
	p := &parser{src: src, pos: keyStart}
	_ = p.str()
	return p.pos
}

// lineIndent is the whitespace that begins the line pos is on.
func lineIndent(src []byte, pos int) string {
	start := bytes.LastIndexByte(src[:pos], '\n') + 1
	end := start
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	return string(src[start:end])
}

// indentUnit is one level of the file's indentation: the first indented
// line's, or two spaces.
func indentUnit(src []byte) string {
	for _, line := range bytes.Split(src, []byte("\n")) {
		i := 0
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i > 0 && i < len(line) {
			return string(line[:i])
		}
	}
	return "  "
}

// multiline is whether the file lays its values out on lines: a file of one
// line, its final newline aside, is kept on one.
func (pt *patcher) multiline() bool {
	return bytes.IndexByte(bytes.TrimSpace(pt.src), '\n') >= 0
}

// render writes a value laid out as the file would have it, at indent.
func (pt *patcher) render(v any, indent string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if pt.multiline() {
		enc.SetIndent(indent, pt.unit)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Canonical is a value as compact JSON with its keys in the file's order and
// no comments: two texts with the same Canonical say the same thing in the
// same order, which a comparison of decoded maps cannot tell.
func Canonical(src []byte) (string, error) {
	n, err := Parse(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := n.canonical(src, &b); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (n *Node) canonical(src []byte, b *strings.Builder) error {
	switch n.Kind {
	case Object:
		b.WriteByte('{')
		for i, m := range n.Members {
			if i > 0 {
				b.WriteByte(',')
			}
			key, _ := json.Marshal(m.Key)
			b.Write(key)
			b.WriteByte(':')
			if err := m.Value.canonical(src, b); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case Array:
		b.WriteByte('[')
		for i, e := range n.Elems {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := e.canonical(src, b); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case String:
		// The same string written two ways (an escape or not) is the same.
		var s string
		if err := json.Unmarshal(src[n.Start:n.End], &s); err != nil {
			return err
		}
		out, _ := json.Marshal(s)
		b.Write(out)
	default:
		b.Write(src[n.Start:n.End])
	}
	return nil
}
