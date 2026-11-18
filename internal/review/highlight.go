package review

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Span is a run of text with one highlight class: "" for plain text, or
// keyword, string, comment, number, type, func, punct.
type Span struct {
	Text  string `json:"t"`
	Class string `json:"c,omitempty"`
}

// language describes just enough syntax to color source code without a
// parser: comment markers, string quotes, and keyword sets.
type language struct {
	line       []string // line comment markers
	blockOpen  string
	blockClose string
	quotes     string // characters that open strings
	raw        string // quote that spans lines (Go and JS backticks)
	keywords   map[string]bool
	types      map[string]bool
	hashLine   bool // # starts a comment
	upperTypes bool // Capitalized words are types
}

func words(list string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(list) {
		out[word] = true
	}
	return out
}

var (
	cStyle = language{line: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: "\"'"}

	languages = map[string]language{}
)

func init() {
	golang := cStyle
	golang.raw = "`"
	golang.keywords = words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false iota")
	golang.types = words("bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable")
	golang.upperTypes = true

	js := cStyle
	js.raw = "`"
	js.keywords = words("abstract as async await break case catch class const continue debugger declare default delete do else enum export extends false finally for from function get if implements import in instanceof interface keyof let namespace new null of private protected public readonly return satisfies set static super switch this throw true try type typeof undefined var void while with yield")
	js.types = words("any bigint boolean never number object string symbol unknown void Array Promise Record Partial Readonly Map Set")
	js.upperTypes = true

	rust := cStyle
	rust.quotes = "\""
	rust.keywords = words("as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while")
	rust.types = words("bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box")
	rust.upperTypes = true

	c := cStyle
	c.keywords = words("auto break case catch class const constexpr continue default delete do else enum explicit extern false for friend goto if inline namespace new noexcept nullptr operator private protected public return sizeof static struct switch template this throw true try typedef typename union using virtual void volatile while #include #define #ifdef #ifndef #endif #if #else #pragma")
	c.types = words("bool char double float int long short signed unsigned size_t int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t std string vector")

	java := cStyle
	java.keywords = words("abstract assert break case catch class const continue default do else enum extends final finally for if implements import instanceof interface native new package private protected public return static super switch synchronized this throw throws try void volatile while true false null var val fun when object data sealed override open internal suspend in is as typealias companion init let func struct protocol extension guard defer self Self nil import")
	java.types = words("boolean byte char double float int long short String Int Long Double Float Boolean Unit Any List Map Set Array")
	java.upperTypes = true

	python := language{quotes: "\"'", hashLine: true}
	python.keywords = words("and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield match case self")
	python.types = words("int float str bool list dict set tuple bytes object type")
	python.upperTypes = true

	ruby := language{quotes: "\"'", hashLine: true}
	ruby.keywords = words("alias and begin break case class def defined? do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield require attr_reader attr_accessor")
	ruby.upperTypes = true

	shell := language{quotes: "\"'", hashLine: true}
	shell.keywords = words("if then else elif fi case esac for while until do done in function return local export readonly set unset shift exit echo printf source alias cd test true false")

	yaml := language{quotes: "\"'", hashLine: true}
	yaml.keywords = words("true false null yes no on off")

	json := language{quotes: "\""}
	json.keywords = words("true false null")

	sql := language{line: []string{"--"}, blockOpen: "/*", blockClose: "*/", quotes: "'\""}
	sql.keywords = words("select from where and or not insert into values update set delete create table index view drop alter add primary key foreign references join left right inner outer on group by order having limit offset as distinct union all null is in like between case when then else end returning with SELECT FROM WHERE AND OR NOT INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE INDEX VIEW DROP ALTER ADD PRIMARY KEY FOREIGN REFERENCES JOIN LEFT RIGHT INNER OUTER ON GROUP BY ORDER HAVING LIMIT OFFSET AS DISTINCT UNION ALL NULL IS IN LIKE BETWEEN CASE WHEN THEN ELSE END RETURNING WITH")
	sql.types = words("int integer bigint text varchar char boolean timestamp date numeric serial uuid jsonb INT INTEGER BIGINT TEXT VARCHAR CHAR BOOLEAN TIMESTAMP DATE NUMERIC SERIAL UUID JSONB")

	css := language{blockOpen: "/*", blockClose: "*/", quotes: "\"'"}
	css.keywords = words("@media @import @keyframes @font-face @supports !important")

	php := cStyle
	php.hashLine = true
	php.keywords = words("abstract and array as break case catch class clone const continue declare default do echo else elseif empty enddeclare endfor endforeach endif endswitch endwhile extends final finally fn for foreach function global goto if implements include include_once instanceof insteadof interface isset list match namespace new or print private protected public readonly require require_once return static switch throw trait try unset use var while yield true false null self parent")
	php.upperTypes = true

	lua := language{line: []string{"--"}, quotes: "\"'"}
	lua.keywords = words("and break do else elseif end false for function goto if in local nil not or repeat return then true until while")

	for _, ext := range []string{"go"} {
		languages[ext] = golang
	}
	for _, ext := range []string{"js", "jsx", "mjs", "cjs", "ts", "tsx", "mts", "cts", "vue", "svelte"} {
		languages[ext] = js
	}
	languages["rs"] = rust
	for _, ext := range []string{"c", "h", "cc", "cpp", "cxx", "hpp", "hh", "m", "mm", "cs", "zig"} {
		languages[ext] = c
	}
	for _, ext := range []string{"java", "kt", "kts", "scala", "swift", "dart", "groovy", "gradle"} {
		languages[ext] = java
	}
	languages["py"] = python
	languages["rb"] = ruby
	for _, ext := range []string{"sh", "bash", "zsh", "fish", "env", "dockerfile", "makefile", "mk"} {
		languages[ext] = shell
	}
	for _, ext := range []string{"yml", "yaml", "toml", "ini", "conf", "cfg", "properties"} {
		languages[ext] = yaml
	}
	languages["json"] = json
	languages["jsonc"] = cStyleJSON(json)
	languages["sql"] = sql
	for _, ext := range []string{"css", "scss", "sass", "less"} {
		languages[ext] = css
	}
	languages["php"] = php
	languages["lua"] = lua
}

func cStyleJSON(json language) language {
	json.line, json.blockOpen, json.blockClose = []string{"//"}, "/*", "*/"
	return json
}

// languageFor picks a language by file name, or false for plain text.
func languageFor(name string) (language, bool) {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	switch base {
	case "dockerfile", "makefile", "gnumakefile", ".bashrc", ".zshrc", ".profile", ".env":
		base = strings.TrimPrefix(base, ".") + "." + strings.TrimPrefix(base, ".")
	}
	ext := base
	if index := strings.LastIndexByte(base, '.'); index >= 0 {
		ext = base[index+1:]
	}
	switch ext {
	case "bashrc", "zshrc", "profile":
		ext = "sh"
	}
	lang, ok := languages[ext]
	return lang, ok
}

// HighlightLines colors lines of the file name, carrying block comments and
// multi-line raw strings across lines. Unknown types come back as plain
// spans, one per line.
func HighlightLines(name string, lines []string) [][]Span {
	out := make([][]Span, len(lines))
	lang, ok := languageFor(name)
	if !ok {
		for index, line := range lines {
			out[index] = []Span{{Text: line}}
		}
		return out
	}
	var state lexState
	for index, line := range lines {
		out[index] = lang.highlight(line, &state)
	}
	return out
}

// lexState carries constructs that span lines.
type lexState struct {
	inBlock bool // inside a block comment
	inRaw   bool // inside a raw string
}

func (lang language) highlight(line string, state *lexState) []Span {
	var spans []Span
	add := func(text, class string) {
		if text == "" {
			return
		}
		if n := len(spans); n > 0 && spans[n-1].Class == class {
			spans[n-1].Text += text
			return
		}
		spans = append(spans, Span{Text: text, Class: class})
	}
	i := 0
	for i < len(line) {
		rest := line[i:]
		switch {
		case state.inBlock:
			end := strings.Index(rest, lang.blockClose)
			if end < 0 {
				add(rest, "comment")
				return spans
			}
			add(rest[:end+len(lang.blockClose)], "comment")
			i += end + len(lang.blockClose)
			state.inBlock = false
		case state.inRaw:
			end := strings.Index(rest, lang.raw)
			if end < 0 {
				add(rest, "string")
				return spans
			}
			add(rest[:end+1], "string")
			i += end + 1
			state.inRaw = false
		case lang.blockOpen != "" && strings.HasPrefix(rest, lang.blockOpen):
			add(lang.blockOpen, "comment")
			i += len(lang.blockOpen)
			state.inBlock = true
		case lang.lineComment(rest, line, i):
			add(rest, "comment")
			return spans
		case lang.raw != "" && strings.HasPrefix(rest, lang.raw):
			add(lang.raw, "string")
			i += len(lang.raw)
			state.inRaw = true
		case strings.IndexByte(lang.quotes, rest[0]) >= 0:
			n := quoted(rest)
			add(rest[:n], "string")
			i += n
		case isDigit(rest[0]) || (rest[0] == '.' && len(rest) > 1 && isDigit(rest[1])):
			n := number(rest)
			add(rest[:n], "number")
			i += n
		case isWordStart(rest):
			n := word(rest)
			text := rest[:n]
			add(text, lang.classify(text, rest[n:]))
			i += n
		default:
			r, size := utf8.DecodeRuneInString(rest)
			class := ""
			if strings.ContainsRune("{}()[];,.:=+-*/%<>!&|^~?@", r) {
				class = "punct"
			}
			add(rest[:size], class)
			i += size
		}
	}
	return spans
}

func (lang language) lineComment(rest, line string, at int) bool {
	for _, marker := range lang.line {
		if strings.HasPrefix(rest, marker) {
			return true
		}
	}
	if lang.hashLine && rest[0] == '#' {
		// Only a # at the start or after whitespace opens a comment, so
		// shell "$#" and "${#x}" stay code.
		return at == 0 || line[at-1] == ' ' || line[at-1] == '\t'
	}
	return false
}

func (lang language) classify(text, after string) string {
	switch {
	case lang.keywords[text]:
		return "keyword"
	case lang.types[text]:
		return "type"
	case strings.HasPrefix(strings.TrimLeft(after, " "), "("):
		return "func"
	case lang.upperTypes && len(text) > 1 && unicode.IsUpper(rune(text[0])):
		return "type"
	}
	return ""
}

// quoted returns the length of the string literal at the start of s,
// honoring backslash escapes. An unterminated literal runs to the end.
func quoted(s string) int {
	quote := s[0]
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}
	return len(s)
}

func number(s string) int {
	i := 0
	for i < len(s) && (isDigit(s[i]) || isLetter(s[i]) || s[i] == '.' || s[i] == '_') {
		i++
	}
	return i
}

func isWordStart(s string) bool {
	if s[0] == '@' || s[0] == '#' || s[0] == '$' {
		return len(s) > 1 && (isLetter(s[1]) || s[1] == '_')
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || unicode.IsLetter(r)
}

func word(s string) int {
	i := 0
	if s[0] == '@' || s[0] == '#' || s[0] == '$' {
		i = 1
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		i += size
	}
	return max(i, 1)
}

func isDigit(b byte) bool  { return b >= '0' && b <= '9' }
func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
