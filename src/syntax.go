package main

import (
	"path/filepath"
	"strings"
	"unicode"
)

// SGR color codes (256-color palette). Swap these to restyle the editor.
const (
	colDefault   = "39"
	colKeyword   = "38;5;141"         // purple
	colType      = "38;5;81"          // blue
	colBuiltin   = "38;5;117"         // light blue
	colFunc      = "38;5;215"         // orange
	colString    = "38;5;114"         // green
	colComment   = "38;5;245"         // gray
	colNumber    = "38;5;180"         // tan
	colMark      = "38;5;196"         // red: last cursor position on a line
	colSearch    = "48;5;220;38;5;16" // yellow bg, black fg: search occurrence
	colSearchSel = "48;5;202;38;5;16" // orange bg, black fg: selected occurrence
)

// lang holds the keyword tables for one language. Add a language by adding
// a *lang here and a case in syntaxFor.
type lang struct {
	name     string
	keywords map[string]bool
	types    map[string]bool
	builtins map[string]bool
}

func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// line tokenizes one line. inBlock reports whether the line starts inside a
// /* */ comment; the returned bool is the state for the following line.
func (l *lang) line(runes []rune, inBlock bool) ([]string, bool) {
	colors := make([]string, len(runes))
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case inBlock:
			colors[i] = colComment
			if r == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				colors[i+1] = colComment
				i += 2
				inBlock = false
				continue
			}
			i++
		case r == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) {
				colors[i] = colComment
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			colors[i], colors[i+1] = colComment, colComment
			i += 2
			inBlock = true
		case r == '"' || r == '\'' || r == '`':
			quote := r
			colors[i] = colString
			i++
			for i < len(runes) {
				colors[i] = colString
				if quote != '`' && runes[i] == '\\' && i+1 < len(runes) {
					colors[i+1] = colString
					i += 2
					continue
				}
				if runes[i] == quote {
					i++
					break
				}
				i++
			}
		case r >= '0' && r <= '9':
			colors[i] = colNumber
			i++
			for i < len(runes) && isNumberRune(runes[i]) {
				colors[i] = colNumber
				i++
			}
		case isIdentStart(r):
			start := i
			for i < len(runes) && isIdentRune(runes[i]) {
				i++
			}
			word := string(runes[start:i])
			var c string
			switch {
			case l != nil && l.types[word]:
				c = colType
			case l != nil && l.keywords[word]:
				c = colKeyword
			case l != nil && l.builtins[word]:
				c = colBuiltin
			case i < len(runes) && runes[i] == '(':
				c = colFunc
			default:
				c = colDefault
			}
			for j := start; j < i; j++ {
				colors[j] = c
			}
		default:
			colors[i] = colDefault
			i++
		}
	}
	return colors, inBlock
}

func isNumberRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'a' && r <= 'f':
		return true
	case r >= 'A' && r <= 'F':
		return true
	case r == 'x' || r == 'X' || r == 'b' || r == 'B' || r == 'o' || r == 'O':
		return true
	case r == '.' || r == '_' || r == 'e' || r == 'E' || r == 'p' || r == 'P':
		return true
	}
	return false
}

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentRune(r rune) bool  { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func syntaxFor(name string) *lang {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return langGo
	case ".py":
		return langPy
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx":
		return langJS
	case ".c", ".h", ".cc", ".cpp", ".hpp", ".cxx", ".java", ".cs":
		return langC
	case ".rs":
		return langRust
	case ".sh", ".bash", ".zsh", ".ksh":
		return langShell
	}
	return nil
}

var langGo = &lang{
	name: "Go",
	keywords: wordSet(
		"break", "case", "chan", "const", "continue", "default", "defer",
		"else", "fallthrough", "for", "func", "go", "goto", "if", "import",
		"interface", "map", "package", "range", "return", "select", "struct",
		"switch", "type", "var"),
	types: wordSet(
		"any", "bool", "byte", "complex64", "complex128", "error", "float32",
		"float64", "int", "int8", "int16", "int32", "int64", "rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr"),
	builtins: wordSet(
		"append", "cap", "clear", "close", "copy", "delete", "len", "make",
		"max", "min", "new", "panic", "print", "println", "recover",
		"true", "false", "nil", "iota"),
}

var langPy = &lang{
	name: "Python",
	keywords: wordSet(
		"and", "as", "assert", "async", "await", "break", "case", "class",
		"continue", "def", "del", "elif", "else", "except", "finally", "for",
		"from", "global", "if", "import", "in", "is", "lambda", "match",
		"nonlocal", "not", "or", "pass", "raise", "return", "try", "while",
		"with", "yield"),
	types: wordSet(
		"bool", "bytearray", "bytes", "complex", "dict", "float", "frozenset",
		"int", "list", "memoryview", "object", "range", "set", "str", "tuple"),
	builtins: wordSet(
		"abs", "all", "any", "bin", "bool", "breakpoint", "callable", "chr",
		"classmethod", "compile", "delattr", "dir", "divmod", "enumerate",
		"eval", "exec", "filter", "format", "getattr", "globals", "hasattr",
		"hash", "help", "hex", "id", "input", "isinstance", "issubclass",
		"iter", "len", "locals", "map", "max", "min", "next", "oct", "open",
		"ord", "pow", "print", "property", "repr", "reversed", "round",
		"setattr", "slice", "sorted", "staticmethod", "sum", "super", "type",
		"vars", "zip", "None", "True", "False", "NotImplemented", "Ellipsis"),
}

var langJS = &lang{
	name: "JavaScript",
	keywords: wordSet(
		"async", "await", "break", "case", "catch", "class", "const",
		"continue", "debugger", "default", "delete", "do", "else", "export",
		"extends", "finally", "for", "function", "if", "import", "in",
		"instanceof", "let", "new", "of", "return", "static", "super",
		"switch", "this", "throw", "try", "typeof", "var", "void", "while",
		"with", "yield"),
	types: wordSet(
		"Array", "Boolean", "Date", "Error", "Function", "Infinity", "JSON",
		"Map", "Math", "NaN", "Number", "Object", "Promise", "RegExp", "Set",
		"String", "Symbol", "WeakMap", "WeakSet", "undefined", "globalThis"),
	builtins: wordSet(
		"console", "document", "window", "fetch", "alert", "confirm",
		"prompt", "parseInt", "parseFloat", "isNaN", "isFinite",
		"encodeURI", "decodeURI", "eval", "require", "module", "exports",
		"true", "false", "null"),
}

var langC = &lang{
	name: "C-like",
	keywords: wordSet(
		"alignas", "alignof", "and", "and_eq", "asm", "auto", "bitand",
		"bitor", "break", "case", "catch", "class", "compl", "concept",
		"const", "consteval", "constexpr", "constinit", "const_cast",
		"continue", "co_await", "co_return", "co_yield", "decltype",
		"default", "delete", "do", "dynamic_cast", "else", "enum", "explicit",
		"export", "extern", "false", "final", "for", "friend", "goto", "if",
		"inline", "mutable", "namespace", "new", "noexcept", "not", "not_eq",
		"nullptr", "operator", "or", "or_eq", "private", "protected", "public",
		"register", "reinterpret_cast", "requires", "return", "sizeof",
		"static", "static_assert", "static_cast", "struct", "switch",
		"template", "this", "thread_local", "throw", "true", "try", "typedef",
		"typeid", "typename", "union", "unsigned", "using", "virtual", "while",
		"xor", "xor_eq"),
	types: wordSet(
		"bool", "char", "char8_t", "char16_t", "char32_t", "double", "float",
		"int", "int8_t", "int16_t", "int32_t", "int64_t", "long", "short",
		"signed", "size_t", "uint8_t", "uint16_t", "uint32_t", "uint64_t",
		"void", "wchar_t"),
	builtins: wordSet(
		"assert", "NULL", "nullptr", "printf", "scanf", "malloc", "free",
		"sizeof", "new", "delete"),
}

var langRust = &lang{
	name: "Rust",
	keywords: wordSet(
		"as", "async", "await", "break", "const", "continue", "crate", "dyn",
		"else", "enum", "extern", "false", "fn", "for", "if", "impl", "in",
		"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
		"self", "Self", "static", "struct", "super", "trait", "true", "type",
		"unsafe", "use", "where", "while"),
	types: wordSet(
		"bool", "char", "f32", "f64", "i8", "i16", "i32", "i64", "i128",
		"isize", "str", "u8", "u16", "u32", "u64", "u128", "usize",
		"String", "Vec", "Option", "Result", "Box", "HashMap", "HashSet",
		"VecDeque"),
	builtins: wordSet(
		"Some", "None", "Ok", "Err", "println", "print", "eprintln",
		"eprint", "format", "vec", "macro_rules", "panic", "assert",
		"assert_eq"),
}

var langShell = &lang{
	name: "Shell",
	keywords: wordSet(
		"if", "then", "else", "elif", "fi", "case", "esac", "for", "while",
		"until", "do", "done", "function", "in", "select", "time"),
	types: wordSet(),
	builtins: wordSet(
		"echo", "cd", "pwd", "export", "unset", "source", "alias", "declare",
		"local", "read", "printf", "test", "exit", "return", "shift", "set",
		"eval", "exec", "trap", "wait", "jobs", "fg", "bg", "true", "false"),
}
