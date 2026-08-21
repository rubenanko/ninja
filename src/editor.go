package main

import (
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type pos struct{ row, col int }

type opKind int

const (
	opInsert opKind = iota
	opDelete
)

// op is one undoable edit operation.
type op struct {
	kind opKind
	row  int
	col  int
	text []rune
}

type prompt struct {
	label string
	input []rune
	done  func(string)
}

type Editor struct {
	buf       *Buffer
	cur       pos
	desired   int // preferred column when moving up/down
	sel       *pos
	top, left int // scroll offset
	w, h      int // terminal size
	filename  string
	modified  bool
	clip      []rune
	undo      []op
	redo      []op
	keys      *keyParser
	prompt    *prompt
	message   string
	msgUntil  time.Time
	indent    string // indentation unit inserted by Tab
	syntax    *lang
	states    []bool // block-comment state at the start of each line
	hlDirty   int    // first line whose highlight state is stale, -1 = clean
	visited   []bool // lines the cursor has already been on
	quit      bool
	quitArm   bool
}

func NewEditor() *Editor {
	return &Editor{
		buf:     NewBuffer(),
		indent:  "\t",
		keys:    newKeyParser(),
		hlDirty: -1,
		visited: []bool{true}, // cursor starts on line 0
	}
}

// ---------------------------------------------------------------------------
// File I/O
// ---------------------------------------------------------------------------

func (e *Editor) openFile(name string) {
	if name == "" {
		return
	}
	data, err := os.ReadFile(name)
	if err != nil {
		if !os.IsNotExist(err) {
			e.setMessage("cannot open " + name + ": " + err.Error())
			return
		}
		// Create the file so it becomes the save target (Ctrl+S) right away.
		f, cerr := os.Create(name)
		if cerr != nil {
			e.setMessage("cannot create " + name + ": " + cerr.Error())
			return
		}
		f.Close()
	}
	e.loadContent(data)
	e.filename = name
	e.syntax = syntaxFor(name)
	e.setMessage("opened " + name)
}

func (e *Editor) loadContent(data []byte) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	lines := make([][]rune, len(parts))
	for i, p := range parts {
		lines[i] = []rune(p)
	}
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	e.buf = &Buffer{lines: lines}
	e.undo, e.redo = nil, nil
	e.sel, e.top, e.left = nil, 0, 0
	e.cur, e.desired = pos{0, 0}, 0
	e.visited = []bool{true} // cursor starts on line 0
	e.modified = false
	e.hlDirty = 0
}

func (e *Editor) save() {
	if e.filename == "" {
		e.startPrompt("save as: ", func(n string) { e.saveAs(n) })
		return
	}
	e.saveAs(e.filename)
}

func (e *Editor) saveAs(name string) {
	if name == "" {
		e.setMessage("no filename given")
		return
	}
	var sb strings.Builder
	for i, line := range e.buf.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(line))
	}
	sb.WriteByte('\n')
	if err := os.WriteFile(name, []byte(sb.String()), 0o644); err != nil {
		e.setMessage("save failed: " + err.Error())
		return
	}
	e.filename = name
	e.modified = false
	e.quitArm = false
	e.setMessage("saved " + name)
}

// ---------------------------------------------------------------------------
// Undo / redo
// ---------------------------------------------------------------------------

func (e *Editor) pushUndo(o op) {
	e.redo = nil
	if n := len(e.undo); n > 0 {
		last := &e.undo[n-1]
		// Coalesce contiguous typing into one undo step.
		if o.kind == opInsert && last.kind == opInsert && last.row == o.row && last.col+len(last.text) == o.col {
			last.text = append(last.text, o.text...)
			return
		}
		// Coalesce a run of backspaces into one undo step.
		if o.kind == opDelete && last.kind == opDelete && last.row == o.row && last.col == o.col+len(o.text) {
			last.text = append(o.text, last.text...)
			last.col = o.col
			return
		}
	}
	e.undo = append(e.undo, o)
}

func (e *Editor) actUndo(_ key) {
	if len(e.undo) == 0 {
		e.setMessage("nothing to undo")
		return
	}
	o := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	switch o.kind {
	case opInsert:
		er, ec := endAfterInsert(o.row, o.col, o.text)
		e.buf.delete(o.row, o.col, er, ec)
		e.cur, e.desired = pos{o.row, o.col}, o.col
	case opDelete:
		e.buf.insert(o.row, o.col, o.text)
		er, ec := endAfterInsert(o.row, o.col, o.text)
		e.cur, e.desired = pos{er, ec}, ec
	}
	e.redo = append(e.redo, o)
	e.sel = nil
	e.hlDirty = o.row
	e.modified = true
}

func (e *Editor) actRedo(_ key) {
	if len(e.redo) == 0 {
		e.setMessage("nothing to redo")
		return
	}
	o := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	switch o.kind {
	case opInsert:
		e.buf.insert(o.row, o.col, o.text)
		er, ec := endAfterInsert(o.row, o.col, o.text)
		e.cur, e.desired = pos{er, ec}, ec
	case opDelete:
		er, ec := endAfterInsert(o.row, o.col, o.text)
		e.buf.delete(o.row, o.col, er, ec)
		e.cur, e.desired = pos{o.row, o.col}, o.col
	}
	e.undo = append(e.undo, o)
	e.sel = nil
	e.hlDirty = o.row
	e.modified = true
}

// ---------------------------------------------------------------------------
// Core editing primitives
// ---------------------------------------------------------------------------

func (e *Editor) markDirty(row int) {
	if e.hlDirty < 0 || row < e.hlDirty {
		e.hlDirty = row
	}
	e.modified = true
	e.quitArm = false
}

func (e *Editor) insertRunes(text []rune) {
	if len(text) == 0 {
		return
	}
	e.deleteSelection()
	r, c := e.cur.row, e.cur.col
	e.buf.insert(r, c, text)
	e.pushUndo(op{opInsert, r, c, text})
	er, ec := endAfterInsert(r, c, text)
	e.cur, e.desired = pos{er, ec}, ec
	e.markDirty(r)
}

func (e *Editor) deleteRange(r1, c1, r2, c2 int) {
	text := e.buf.delete(r1, c1, r2, c2)
	if text == nil {
		return
	}
	e.pushUndo(op{opDelete, r1, c1, text})
	e.cur, e.desired = pos{r1, c1}, c1
	e.markDirty(r1)
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

func (e *Editor) hasSelection() bool {
	return e.sel != nil && *e.sel != e.cur
}

func (e *Editor) selStartEnd() (pos, pos) {
	if e.sel == nil {
		return e.cur, e.cur
	}
	if e.sel.row < e.cur.row || (e.sel.row == e.cur.row && e.sel.col < e.cur.col) {
		return *e.sel, e.cur
	}
	return e.cur, *e.sel
}

func (e *Editor) deleteSelection() bool {
	if !e.hasSelection() {
		return false
	}
	s, en := e.selStartEnd()
	e.sel = nil
	e.deleteRange(s.row, s.col, en.row, en.col)
	return true
}

// ---------------------------------------------------------------------------
// Cursor movement
// ---------------------------------------------------------------------------

func (e *Editor) goPos(r, c int, extend bool) {
	if r < 0 {
		r = 0
	}
	if r >= e.buf.NumLines() {
		r = e.buf.NumLines() - 1
	}
	if c < 0 {
		c = 0
	}
	if l := e.buf.LineLen(r); c > l {
		c = l
	}
	if extend {
		if e.sel == nil {
			a := e.cur
			e.sel = &a
		}
	} else {
		e.sel = nil
	}
	e.cur, e.desired = pos{r, c}, c
}

// visitLine marks line r as visited and reports whether it already was.
// The array is grown lazily, so line insertions/deletions can leave stale
// flags; that is harmless (worst case one extra end-of-line landing).
func (e *Editor) visitLine(r int) bool {
	if r >= len(e.visited) {
		g := make([]bool, e.buf.NumLines())
		copy(g, e.visited)
		e.visited = g
	}
	was := e.visited[r]
	e.visited[r] = true
	return was
}

func (e *Editor) moveUp(extend bool) {
	r, c := e.cur.row, e.cur.col
	if r > 0 {
		r--
		c = e.desired
		if l := e.buf.LineLen(r); c > l {
			c = l
		}
		if !e.visitLine(r) {
			c = e.buf.LineLen(r) // first time on this line: land at its end
		}
	} else {
		c = 0
	}
	e.goPos(r, c, extend)
}

func (e *Editor) moveDown(extend bool) {
	r, c := e.cur.row, e.cur.col
	if r+1 < e.buf.NumLines() {
		r++
		c = e.desired
		if l := e.buf.LineLen(r); c > l {
			c = l
		}
		if !e.visitLine(r) {
			c = e.buf.LineLen(r) // first time on this line: land at its end
		}
	}
	e.goPos(r, c, extend)
}

func (e *Editor) moveLeft(extend bool) {
	r, c := e.cur.row, e.cur.col
	if c > 0 {
		c--
	} else if r > 0 {
		r--
		c = e.buf.LineLen(r)
	}
	e.goPos(r, c, extend)
}

func (e *Editor) moveRight(extend bool) {
	r, c := e.cur.row, e.cur.col
	if c < e.buf.LineLen(r) {
		c++
	} else if r+1 < e.buf.NumLines() {
		r++
		c = 0
	}
	e.goPos(r, c, extend)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func (e *Editor) wordLeft(extend bool) {
	r, c := e.cur.row, e.cur.col
	line := e.buf.Line(r)
	for {
		if c > 0 {
			if isWordRune(line[c-1]) {
				for c > 0 && isWordRune(line[c-1]) {
					c--
				}
				break
			}
			for c > 0 && !isWordRune(line[c-1]) {
				c--
			}
			continue
		}
		if r == 0 {
			break
		}
		r--
		line = e.buf.Line(r)
		c = len(line)
	}
	e.goPos(r, c, extend)
}

func (e *Editor) wordRight(extend bool) {
	r, c := e.cur.row, e.cur.col
	for {
		line := e.buf.Line(r)
		if c < len(line) {
			if isWordRune(line[c]) {
				for c < len(line) && isWordRune(line[c]) {
					c++
				}
				break
			}
			for c < len(line) && !isWordRune(line[c]) {
				c++
			}
			continue
		}
		if r+1 >= e.buf.NumLines() {
			break
		}
		r++
		c = 0
	}
	e.goPos(r, c, extend)
}

// ---------------------------------------------------------------------------
// Editing actions
// ---------------------------------------------------------------------------

func (e *Editor) actTypeChar(r rune) {
	if r == '}' && e.sel == nil {
		// Smart dedent: closing a brace on an otherwise-empty line backs up
		// one indentation level before inserting.
		line := e.buf.Line(e.cur.row)
		prefix := line[:e.cur.col]
		u := []rune(e.indent)
		if len(prefix) >= len(u) && allSpace(prefix) && equalRunes(prefix[len(prefix)-len(u):], u) {
			e.deleteRange(e.cur.row, e.cur.col-len(u), e.cur.row, e.cur.col)
		}
	}
	e.insertRunes([]rune{r})
}

func (e *Editor) actNewline(_ key) {
	e.deleteSelection()
	line := e.buf.Line(e.cur.row)
	ind := leadingIndent(line)
	trimmed := strings.TrimRight(string(line), " \t")
	if len(trimmed) > 0 {
		switch trimmed[len(trimmed)-1] {
		case '{', '[', '(':
			ind += e.indent
		}
	}
	e.insertRunes([]rune("\n" + ind))
}

func (e *Editor) actTab(_ key) {
	if e.hasSelection() {
		e.deleteSelection()
	}
	e.insertRunes([]rune(e.indent))
}

func (e *Editor) actUnindent(_ key) {
	line := e.buf.Line(e.cur.row)
	u := []rune(e.indent)
	if len(line) >= len(u) && equalRunes(line[:len(u)], u) {
		e.deleteRange(e.cur.row, 0, e.cur.row, len(u))
	} else if len(line) > 0 && line[0] == '\t' {
		e.deleteRange(e.cur.row, 0, e.cur.row, 1)
	}
}

func (e *Editor) actBackspace(_ key) {
	if e.deleteSelection() {
		return
	}
	r, c := e.cur.row, e.cur.col
	if c > 0 {
		e.deleteRange(r, c-1, r, c)
	} else if r > 0 {
		e.deleteRange(r-1, e.buf.LineLen(r-1), r, 0)
	}
}

func (e *Editor) actDelete(_ key) {
	if e.deleteSelection() {
		return
	}
	r, c := e.cur.row, e.cur.col
	if c < e.buf.LineLen(r) {
		e.deleteRange(r, c, r, c+1)
	} else if r+1 < e.buf.NumLines() {
		e.deleteRange(r, c, r+1, 0)
	}
}

// deleteWordLeft removes the word before the cursor (plus any whitespace
// between it and the cursor). If only punctuation precedes the cursor, the
// punctuation run is removed instead.
func (e *Editor) deleteWordLeft() {
	r, c := e.cur.row, e.cur.col
	line := e.buf.Line(r)
	if c == 0 {
		if r > 0 {
			e.deleteRange(r-1, e.buf.LineLen(r-1), r, 0)
		}
		return
	}
	start := c
	prev := line[c-1]
	switch {
	case isWordRune(prev):
		for c > 0 && isWordRune(line[c-1]) {
			c--
		}
	case prev == ' ' || prev == '\t':
		for c > 0 && (line[c-1] == ' ' || line[c-1] == '\t') {
			c--
		}
		for c > 0 && isWordRune(line[c-1]) {
			c--
		}
	default:
		for c > 0 && !isWordRune(line[c-1]) && line[c-1] != ' ' && line[c-1] != '\t' {
			c--
		}
	}
	if c == start {
		return
	}
	e.deleteRange(r, c, r, start)
}

// deleteWord removes one whole word: the word the cursor is on or ending at
// the cursor; otherwise the word in front of the cursor.
func (e *Editor) deleteWord() {
	r, c := e.cur.row, e.cur.col
	line := e.buf.Line(r)
	insideWord := c > 0 && isWordRune(line[c-1])
	if !insideWord && c < len(line) {
		insideWord = isWordRune(line[c])
	}
	if insideWord {
		s := c
		for s > 0 && isWordRune(line[s-1]) {
			s--
		}
		en := c
		for en < len(line) && isWordRune(line[en]) {
			en++
		}
		e.deleteRange(r, s, r, en)
		return
	}
	// On whitespace/punctuation: delete the next word (and the space before it).
	if c < len(line) {
		for c < len(line) && !isWordRune(line[c]) {
			c++
		}
		for c < len(line) && isWordRune(line[c]) {
			c++
		}
		e.deleteRange(r, e.cur.col, r, c)
		return
	}
	if r+1 < e.buf.NumLines() {
		e.deleteRange(r, c, r+1, 0)
	}
}

func (e *Editor) actDeleteWordLeft(_ key) { e.deleteWordLeft() }
func (e *Editor) actDeleteWord(_ key)     { e.deleteWord() }

func (e *Editor) actDeleteToBOL(_ key) {
	r, c := e.cur.row, e.cur.col
	if c > 0 {
		e.deleteRange(r, 0, r, c)
	}
}

func (e *Editor) actDeleteToEOL(_ key) {
	r, c := e.cur.row, e.cur.col
	if c < e.buf.LineLen(r) {
		e.deleteRange(r, c, r, e.buf.LineLen(r))
	} else if r+1 < e.buf.NumLines() {
		e.deleteRange(r, c, r+1, 0)
	}
}

// ---------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------

func (e *Editor) actCopy(_ key) {
	if !e.hasSelection() {
		e.setMessage("no selection to copy")
		return
	}
	s, en := e.selStartEnd()
	e.clip = e.buf.text(s.row, s.col, en.row, en.col)
	e.setMessage("copied " + strconv.Itoa(len(e.clip)) + " chars")
}

func (e *Editor) actCut(k key) {
	if !e.hasSelection() {
		e.setMessage("no selection to cut")
		return
	}
	e.actCopy(k)
	e.deleteSelection()
}

func (e *Editor) actPaste(_ key) {
	if len(e.clip) == 0 {
		e.setMessage("clipboard is empty")
		return
	}
	e.insertRunes(e.clip)
}

func (e *Editor) actSelectAll(_ key) {
	n := e.buf.NumLines()
	if n == 0 {
		return
	}
	a := pos{0, 0}
	e.sel = &a
	e.cur = pos{n - 1, e.buf.LineLen(n - 1)}
	e.desired = e.cur.col
}

// ---------------------------------------------------------------------------
// Movement actions (keymap targets)
// ---------------------------------------------------------------------------

func (e *Editor) actArrowUp(k key) {
	if k.mods&modCtrl != 0 {
		e.scrollView(-1)
		return
	}
	e.moveUp(k.mods&modShift != 0)
}

func (e *Editor) actArrowDown(k key) {
	if k.mods&modCtrl != 0 {
		e.scrollView(1)
		return
	}
	e.moveDown(k.mods&modShift != 0)
}

func (e *Editor) actArrowLeft(k key) { e.moveLeft(k.mods&modShift != 0) }
func (e *Editor) actArrowRight(k key) {
	e.moveRight(k.mods&modShift != 0)
}

func (e *Editor) actWordLeft(k key)  { e.wordLeft(k.mods&modShift != 0) }
func (e *Editor) actWordRight(k key) { e.wordRight(k.mods&modShift != 0) }

func (e *Editor) actHome(k key) {
	if k.mods&modCtrl != 0 {
		e.goPos(0, 0, false)
		return
	}
	e.goPos(e.cur.row, 0, k.mods&modShift != 0)
}

func (e *Editor) actEnd(k key) {
	if k.mods&modCtrl != 0 {
		e.goPos(e.buf.NumLines()-1, e.buf.LineLen(e.buf.NumLines()-1), false)
		return
	}
	e.goPos(e.cur.row, e.buf.LineLen(e.cur.row), k.mods&modShift != 0)
}

func (e *Editor) actPageUp(_ key) {
	step := e.h - 2
	if step < 1 {
		step = 1
	}
	if e.cur.row > 0 {
		e.goPos(e.cur.row-step, e.cur.col, false)
	}
}

func (e *Editor) actPageDown(_ key) {
	step := e.h - 2
	if step < 1 {
		step = 1
	}
	e.goPos(e.cur.row+step, e.cur.col, false)
}

func (e *Editor) scrollView(d int) {
	e.top += d
	if e.top < 0 {
		e.top = 0
	}
	maxTop := e.buf.NumLines() - (e.h - 1)
	if maxTop < 0 {
		maxTop = 0
	}
	if e.top > maxTop {
		e.top = maxTop
	}
}

func (e *Editor) actRecenter(_ key) {
	vh := e.h - 1
	if vh < 1 {
		vh = 1
	}
	e.top = e.cur.row - vh/2
	if e.top < 0 {
		e.top = 0
	}
	e.left = displayCol(e.buf.Line(e.cur.row), e.cur.col) - (e.w / 3)
	if e.left < 0 {
		e.left = 0
	}
}

// ---------------------------------------------------------------------------
// Misc actions
// ---------------------------------------------------------------------------

func (e *Editor) actCancel(_ key) { e.sel = nil }

func (e *Editor) actQuit(_ key) {
	if !e.modified || e.quitArm {
		e.quit = true
		return
	}
	e.quitArm = true
	e.setMessage("unsaved changes - press ^Q again to quit")
}

func (e *Editor) actOpen(_ key) {
	e.startPrompt("open: ", func(n string) { e.openFile(n) })
}

func (e *Editor) actSave(_ key) { e.save() }

// ---------------------------------------------------------------------------
// Key dispatch
// ---------------------------------------------------------------------------

type binding struct {
	k     key
	label string
	fn    func(*Editor, key)
}

// keymap is THE place to add shortcuts: add a binding here and an action
// method above. A binding matching a plain rune (mods == 0) shadows the
// default "insert this character" behavior.
var keymap = []binding{
	// file
	{kRune('q', modCtrl), "quit", (*Editor).actQuit},
	{kRune('s', modCtrl), "save", (*Editor).actSave},
	{kRune('o', modCtrl), "open", (*Editor).actOpen},
	// edit
	{kRune('z', modCtrl), "undo", (*Editor).actUndo},
	{kRune('y', modCtrl), "redo", (*Editor).actRedo},
	{kRune('c', modCtrl), "copy", (*Editor).actCopy},
	{kRune('x', modCtrl), "cut", (*Editor).actCut},
	{kRune('v', modCtrl), "paste", (*Editor).actPaste},
	{kRune('a', modCtrl), "select-all", (*Editor).actSelectAll},
	{kRune('\\', modCtrl), "delete-word", (*Editor).actDeleteWord},
	{kRune('u', modCtrl), "delete-to-bol", (*Editor).actDeleteToBOL},
	{kRune('k', modCtrl), "delete-to-eol", (*Editor).actDeleteToEOL},
	{kRune('l', modCtrl), "recenter", (*Editor).actRecenter},
	{kRune(0x1b, modNone), "cancel", (*Editor).actCancel},
	// insert
	{kRune('\n', modNone), "newline", (*Editor).actNewline},
	{kRune('\t', modNone), "tab", (*Editor).actTab},
	{kRune('\t', modShift), "unindent", (*Editor).actUnindent},
	// deletion
	{kCode(codeBackspace, modNone), "backspace", (*Editor).actBackspace},
	{kCode(codeBackspace, modCtrl), "delete-word-left", (*Editor).actDeleteWordLeft},
	{kCode(codeBackspace, modAlt), "delete-word-left", (*Editor).actDeleteWordLeft},
	{kCode(codeDelete, modNone), "delete", (*Editor).actDelete},
	// movement
	{kCode(codeArrowUp, modNone), "up", (*Editor).actArrowUp},
	{kCode(codeArrowDown, modNone), "down", (*Editor).actArrowDown},
	{kCode(codeArrowLeft, modNone), "left", (*Editor).actArrowLeft},
	{kCode(codeArrowRight, modNone), "right", (*Editor).actArrowRight},
	{kCode(codeArrowUp, modShift), "select-up", (*Editor).actArrowUp},
	{kCode(codeArrowDown, modShift), "select-down", (*Editor).actArrowDown},
	{kCode(codeArrowLeft, modShift), "select-left", (*Editor).actArrowLeft},
	{kCode(codeArrowRight, modShift), "select-right", (*Editor).actArrowRight},
	{kCode(codeArrowUp, modCtrl), "scroll-up", (*Editor).actArrowUp},
	{kCode(codeArrowDown, modCtrl), "scroll-down", (*Editor).actArrowDown},
	{kCode(codeArrowLeft, modCtrl), "word-left", (*Editor).actWordLeft},
	{kCode(codeArrowRight, modCtrl), "word-right", (*Editor).actWordRight},
	{kCode(codeArrowLeft, modCtrl|modShift), "select-word-left", (*Editor).actWordLeft},
	{kCode(codeArrowRight, modCtrl|modShift), "select-word-right", (*Editor).actWordRight},
	{kCode(codeArrowLeft, modAlt), "word-left", (*Editor).actWordLeft},
	{kCode(codeArrowRight, modAlt), "word-right", (*Editor).actWordRight},
	{kRune('b', modAlt), "word-left", (*Editor).actWordLeft},
	{kRune('f', modAlt), "word-right", (*Editor).actWordRight},
	{kCode(codeHome, modNone), "home", (*Editor).actHome},
	{kCode(codeEnd, modNone), "end", (*Editor).actEnd},
	{kCode(codeHome, modShift), "select-home", (*Editor).actHome},
	{kCode(codeEnd, modShift), "select-end", (*Editor).actEnd},
	{kCode(codeHome, modCtrl), "doc-start", (*Editor).actHome},
	{kCode(codeEnd, modCtrl), "doc-end", (*Editor).actEnd},
	{kCode(codePageUp, modNone), "page-up", (*Editor).actPageUp},
	{kCode(codePageDown, modNone), "page-down", (*Editor).actPageDown},
}

func (e *Editor) handle(k key) {
	for i := range keymap {
		if keymap[i].k == k {
			keymap[i].fn(e, k)
			return
		}
	}
	if k.mods == modNone && k.r != 0 {
		e.actTypeChar(k.r)
	}
}

// ---------------------------------------------------------------------------
// Prompt (filename entry on the status line)
// ---------------------------------------------------------------------------

func (e *Editor) startPrompt(label string, done func(string)) {
	e.prompt = &prompt{label: label, done: done}
}

func (e *Editor) handlePrompt(k key) {
	p := e.prompt
	if p == nil {
		return
	}
	switch {
	case k == kRune('\n', modNone):
		e.prompt = nil
		p.done(string(p.input))
	case k == kRune(0x1b, modNone):
		e.prompt = nil
	case k.code == codeBackspace:
		if len(p.input) > 0 {
			p.input = p.input[:len(p.input)-1]
		}
	case k.mods == modCtrl && k.r == 'u':
		p.input = nil
	case k.mods == modNone && k.r != 0:
		p.input = append(p.input, k.r)
	}
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

func (e *Editor) run() error {
	restore, err := enterRaw()
	if err != nil {
		return err
	}
	defer restore()
	e.querySize()
	for !e.quit {
		e.render()
		k, ok := e.keys.next()
		if !ok {
			break
		}
		if e.prompt != nil {
			e.handlePrompt(k)
		} else {
			e.handle(k)
		}
	}
	return nil
}

func (e *Editor) setMessage(msg string) {
	e.message = msg
	e.msgUntil = time.Now().Add(4 * time.Second)
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func leadingIndent(line []rune) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return string(line[:i])
}

func allSpace(rs []rune) bool {
	for _, r := range rs {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
