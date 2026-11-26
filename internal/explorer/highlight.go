package explorer

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/vt"
)

// Syntax colouring for the preview, lexical and small on purpose: keywords,
// strings, comments and numbers are what make code readable at a glance,
// and they need no grammar. A real highlighter is a large dependency for a
// panel that shows a file before an editor opens it.

// lang is what the highlighter knows of a language.
type lang struct {
	keywords map[string]bool
	// line starts a comment to the end of the line; blockOpen and
	// blockClose delimit one that spans lines.
	line                  []string
	blockOpen, blockClose string
	// quotes open strings; raw ones span lines (Go's backquote, Python's
	// triple quotes are treated as plain quotes).
	quotes string
	raw    byte
	// markdown colours headings, quotes and code fences instead.
	markdown bool
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

var (
	cFamily = "if else for while do switch case default break continue return goto struct union enum typedef sizeof static const extern void int char float double long short unsigned signed bool true false null NULL include define"
	langs   = map[string]*lang{
		"go": {keywords: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota any error string int int64 uint uint64 byte rune bool float64 make new len cap append"),
			line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`, raw: '`'},
		"rust": {keywords: words("as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while Some None Ok Err Option Result String Vec Box"),
			line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"`},
		"python": {keywords: words("and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield self"),
			line: []string{"#"}, quotes: `"'`},
		"js": {keywords: words("async await break case catch class const continue debugger default delete do else export extends false finally for function if import in instanceof let new null return super switch this throw true try typeof undefined var void while with yield interface type enum implements readonly private public protected"),
			line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`, raw: '`'},
		"shell": {keywords: words("if then else elif fi for while until do done case esac in function return local export set unset shift exit echo printf test"),
			line: []string{"#"}, quotes: `"'`},
		"c": {keywords: words(cFamily + " class public private protected virtual template typename namespace using new delete this nullptr auto"),
			line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`},
		"java": {keywords: words("abstract boolean break byte case catch char class const continue default do double else enum extends final finally float for if implements import instanceof int interface long native new null package private protected public return short static super switch synchronized this throw throws try void volatile while true false var val fun when object data sealed override"),
			line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: `"'`},
		"ruby": {keywords: words("alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield require attr_reader attr_accessor"),
			line: []string{"#"}, quotes: `"'`},
		"lua":  {keywords: words("and break do else elseif end false for function goto if in local nil not or repeat return then true until while"), line: []string{"--"}, quotes: `"'`},
		"sql":  {keywords: words("select from where insert into values update set delete create table drop alter add index join left right inner outer on group by order having limit as and or not null primary key foreign references distinct union SELECT FROM WHERE INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE DROP ALTER JOIN ON GROUP BY ORDER LIMIT AS AND OR NOT NULL"), line: []string{"--"}, quotes: `'"`},
		"conf": {keywords: words("true false null yes no on off"), line: []string{"#"}, quotes: `"'`},
		"json": {keywords: words("true false null"), quotes: `"`},
		"md":   {markdown: true},
	}
	extLang = map[string]string{
		".go": "go", ".rs": "rust", ".py": "python", ".pyi": "python",
		".js": "js", ".mjs": "js", ".cjs": "js", ".jsx": "js", ".ts": "js", ".tsx": "js",
		".sh": "shell", ".bash": "shell", ".zsh": "shell", ".fish": "shell",
		".c": "c", ".h": "c", ".cc": "c", ".cpp": "c", ".hpp": "c", ".cs": "java",
		".java": "java", ".kt": "java", ".kts": "java", ".swift": "java", ".scala": "java",
		".rb": "ruby", ".lua": "lua", ".sql": "sql",
		".toml": "conf", ".yaml": "conf", ".yml": "conf", ".ini": "conf", ".env": "conf", ".conf": "conf",
		".json": "json", ".md": "md", ".markdown": "md",
	}
	nameLang = map[string]string{
		"Makefile": "shell", "Dockerfile": "shell", ".bashrc": "shell", ".zshrc": "shell",
		".gitignore": "conf", "go.mod": "go",
	}
)

// langFor is the language of a file by its name, or nil.
func langFor(path string) *lang {
	base := filepath.Base(path)
	if l, ok := nameLang[base]; ok {
		return langs[l]
	}
	return langs[extLang[strings.ToLower(filepath.Ext(base))]]
}

// TokenKind is what a run of a line is, for the highlighter: the panel
// colours each kind with the terminal's own colours (styleOf), and the
// desktop's files panel with its theme's, from the same tokens.
type TokenKind int

const (
	TokenPlain TokenKind = iota
	TokenKeyword
	TokenString
	TokenComment
	TokenNumber
	TokenHeading
)

// Token is a run of a line of one kind.
type Token struct {
	Text string
	Kind TokenKind
}

// Colours: the terminal's own, so the preview has the theme the rest has.
var (
	hlKeyword = vt.Style{FG: vt.IndexedColor(5), Attrs: vt.AttrBold}
	hlString  = vt.Style{FG: vt.IndexedColor(2)}
	hlComment = vt.Style{FG: vt.IndexedColor(8), Attrs: vt.AttrItalic}
	hlNumber  = vt.Style{FG: vt.IndexedColor(3)}
	hlHeading = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold}
)

// styleOf is the terminal's colour for a kind of token.
func styleOf(k TokenKind) vt.Style {
	switch k {
	case TokenKeyword:
		return hlKeyword
	case TokenString:
		return hlString
	case TokenComment:
		return hlComment
	case TokenNumber:
		return hlNumber
	case TokenHeading:
		return hlHeading
	}
	return styleNormal
}

// span is a run of a line in one style.
type span struct {
	text  string
	style vt.Style
}

// hlState is what carries from one line to the next: being inside a block
// comment or a raw string.
type hlState struct {
	inBlock bool
	inRaw   bool
	inFence bool
}

// highlight colours one line, given the state the line before left.
func (l *lang) highlight(line string, st hlState) ([]span, hlState) {
	toks, st := l.tokens(line, st)
	out := make([]span, len(toks))
	for i, t := range toks {
		out[i] = span{t.Text, styleOf(t.Kind)}
	}
	return out, st
}

// Highlight cuts a file's lines into tokens, by the language its name says:
// a line of tokens per line, plain when the language is not one the
// highlighter knows.
func Highlight(path string, lines []string) [][]Token {
	l := langFor(path)
	var st hlState
	out := make([][]Token, len(lines))
	for i, ln := range lines {
		out[i], st = l.tokens(ln, st)
	}
	return out
}

// tokens cuts one line into tokens, given the state the line before left.
func (l *lang) tokens(line string, st hlState) ([]Token, hlState) {
	if l == nil {
		return []Token{{line, TokenPlain}}, st
	}
	if l.markdown {
		return l.markdownLine(line, st)
	}
	var out []Token
	emit := func(text string, kind TokenKind) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Kind == kind {
			out[n-1].Text += text
			return
		}
		out = append(out, Token{text, kind})
	}
	i := 0
	for i < len(line) {
		rest := line[i:]
		switch {
		case st.inBlock:
			end := strings.Index(rest, l.blockClose)
			if end < 0 {
				emit(rest, TokenComment)
				return out, st
			}
			emit(rest[:end+len(l.blockClose)], TokenComment)
			i += end + len(l.blockClose)
			st.inBlock = false
			continue
		case st.inRaw:
			end := strings.IndexByte(rest, l.raw)
			if end < 0 {
				emit(rest, TokenString)
				return out, st
			}
			emit(rest[:end+1], TokenString)
			i += end + 1
			st.inRaw = false
			continue
		}
		if l.blockOpen != "" && strings.HasPrefix(rest, l.blockOpen) {
			st.inBlock = true
			emit(l.blockOpen, TokenComment)
			i += len(l.blockOpen)
			continue
		}
		lineComment := false
		for _, c := range l.line {
			if strings.HasPrefix(rest, c) {
				lineComment = true
			}
		}
		if lineComment {
			emit(rest, TokenComment)
			return out, st
		}
		c := line[i]
		switch {
		case l.raw != 0 && c == l.raw:
			st.inRaw = true
			emit(string(c), TokenString)
			i++
		case strings.IndexByte(l.quotes, c) >= 0:
			end := i + 1
			for end < len(line) && line[end] != c {
				if line[end] == '\\' {
					end++
				}
				end++
			}
			end = min(end+1, len(line))
			emit(line[i:end], TokenString)
			i = end
		case c >= '0' && c <= '9' && (i == 0 || !isWordByte(line[i-1])):
			end := i
			for end < len(line) && (isWordByte(line[end]) || line[end] == '.') {
				end++
			}
			emit(line[i:end], TokenNumber)
			i = end
		case isWordByte(c):
			end := i
			for end < len(line) && isWordByte(line[end]) {
				end++
			}
			word := line[i:end]
			if l.keywords[word] {
				emit(word, TokenKeyword)
			} else {
				emit(word, TokenPlain)
			}
			i = end
		default:
			_, size := utf8.DecodeRuneInString(rest)
			emit(rest[:size], TokenPlain)
			i += size
		}
	}
	return out, st
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b >= 0x80
}

// markdownLine colours headings, block quotes, list marks and code, which is
// what makes a README readable without rendering it.
func (l *lang) markdownLine(line string, st hlState) ([]Token, hlState) {
	trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
	switch {
	case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
		st.inFence = !st.inFence
		return []Token{{line, TokenComment}}, st
	case st.inFence:
		return []Token{{line, TokenString}}, st
	case strings.HasPrefix(trimmed, "#"):
		return []Token{{line, TokenHeading}}, st
	case strings.HasPrefix(trimmed, ">"):
		return []Token{{line, TokenComment}}, st
	case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ "):
		lead := len(line) - len(trimmed)
		return []Token{{line[:lead+2], TokenKeyword}, {line[lead+2:], TokenPlain}}, st
	}
	return []Token{{line, TokenPlain}}, st
}
