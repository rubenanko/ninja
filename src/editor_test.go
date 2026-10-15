package main

import (
	"strings"
	"testing"
)

func newTestEditor(content string) *Editor {
	e := &Editor{buf: NewBuffer(), indent: "\t", hlDirty: -1}
	if content != "" {
		e.buf = &Buffer{lines: [][]rune{[]rune(content)}}
	}
	return e
}

func bufText(e *Editor) string {
	var sb strings.Builder
	for i, line := range e.buf.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(line))
	}
	return sb.String()
}

func TestBufferInsertDelete(t *testing.T) {
	b := NewBuffer()
	b.insert(0, 0, []rune("hello world"))
	if got := string(b.Line(0)); got != "hello world" {
		t.Fatalf("insert: %q", got)
	}
	b.insert(0, 5, []rune("\nnew"))
	if b.NumLines() != 2 {
		t.Fatalf("lines = %d", b.NumLines())
	}
	if got := string(b.Line(0)); got != "hello" {
		t.Fatalf("line0 = %q", got)
	}
	if got := string(b.Line(1)); got != "new world" {
		t.Fatalf("line1 = %q", got)
	}
	del := b.delete(0, 3, 1, 4)
	if got := string(del); got != "lo\nnew " {
		t.Fatalf("deleted = %q", got)
	}
	if got := string(b.Line(0)); got != "helworld" {
		t.Fatalf("merged = %q", got)
	}
}

func TestUndoRedoTyping(t *testing.T) {
	e := newTestEditor("")
	for _, r := range "hi" {
		e.actTypeChar(r)
	}
	if got := bufText(e); got != "hi" {
		t.Fatalf("typed = %q", got)
	}
	e.actUndo(key{})
	if got := bufText(e); got != "" {
		t.Fatalf("after undo = %q", got)
	}
	e.actRedo(key{})
	if got := bufText(e); got != "hi" {
		t.Fatalf("after redo = %q", got)
	}
}

func TestBackspaceCoalesce(t *testing.T) {
	e := newTestEditor("")
	for _, r := range "hi" {
		e.actTypeChar(r)
	}
	e.actBackspace(key{})
	e.actBackspace(key{})
	if got := bufText(e); got != "" {
		t.Fatalf("after backspaces = %q", got)
	}
	// The two backspaces coalesce into one delete op; the typing coalesced
	// into one insert op, so the whole undo stack is exactly two ops.
	if len(e.undo) != 2 {
		t.Fatalf("undo stack = %d ops, want 2", len(e.undo))
	}
	e.actUndo(key{})
	if got := bufText(e); got != "hi" {
		t.Fatalf("after undo = %q", got)
	}
}

func TestWordMovement(t *testing.T) {
	e := newTestEditor("hello brave new world")
	e.goPos(0, 0, false)
	e.wordRight(false)
	if e.cur.col != 5 {
		t.Fatalf("right1 col = %d", e.cur.col)
	}
	e.wordRight(false)
	if e.cur.col != 11 {
		t.Fatalf("right2 col = %d", e.cur.col)
	}
	e.wordLeft(false)
	if e.cur.col != 6 {
		t.Fatalf("left col = %d", e.cur.col)
	}
	e.wordLeft(false)
	if e.cur.col != 0 {
		t.Fatalf("left2 col = %d", e.cur.col)
	}
}

func TestDeleteWord(t *testing.T) {
	e := newTestEditor("hello world")
	e.goPos(0, 11, false) // end of line
	e.deleteWord()
	if got := bufText(e); got != "hello " {
		t.Fatalf("deleteWord at end = %q", got)
	}
	e2 := newTestEditor("hello world")
	e2.goPos(0, 0, false)
	e2.deleteWord()
	if got := bufText(e2); got != " world" {
		t.Fatalf("deleteWord at start = %q", got)
	}
	e3 := newTestEditor("hello world")
	e3.goPos(0, 6, false) // before the space separating the words
	e3.deleteWordLeft()
	if got := bufText(e3); got != "world" {
		t.Fatalf("deleteWordLeft = %q", got)
	}
	e4 := newTestEditor("hello world")
	e4.goPos(0, 2, false) // middle of the first word
	e4.deleteWord()
	if got := bufText(e4); got != " world" {
		t.Fatalf("deleteWord mid-word = %q", got)
	}
}

func TestAutoIndent(t *testing.T) {
	e := newTestEditor("\tif x {")
	e.goPos(0, len(e.buf.Line(0)), false)
	e.actNewline(key{})
	if got := bufText(e); got != "\tif x {\n\t\t" {
		t.Fatalf("auto indent = %q", got)
	}
	// Smart brace dedent.
	e.actTypeChar('}')
	if got := bufText(e); got != "\tif x {\n\t}" {
		t.Fatalf("smart brace = %q", got)
	}
}

func TestCopyPasteCut(t *testing.T) {
	e := newTestEditor("one two three")
	e.goPos(0, 0, false)
	e.sel = &pos{0, 0}
	e.goPos(0, 7, true) // select "one two"
	e.actCopy(key{})
	if got := string(e.clip); got != "one two" {
		t.Fatalf("clip = %q", got)
	}
	e.goPos(0, 14, false)
	e.actPaste(key{})
	if got := bufText(e); got != "one two threeone two" {
		t.Fatalf("paste = %q", got)
	}
	e2 := newTestEditor("abcdef")
	e2.sel = &pos{0, 1}
	e2.goPos(0, 5, true)
	e2.actCut(key{})
	if got := bufText(e2); got != "af" {
		t.Fatalf("cut = %q", got)
	}
	if got := string(e2.clip); got != "bcde" {
		t.Fatalf("cut clip = %q", got)
	}
}

func TestSelectAll(t *testing.T) {
	e := newTestEditor("line1")
	e.buf.insert(0, 5, []rune("\nline2"))
	e.actSelectAll(key{})
	if !e.hasSelection() {
		t.Fatal("no selection")
	}
	s, en := e.selStartEnd()
	if got := string(e.buf.text(s.row, s.col, en.row, en.col)); got != "line1\nline2" {
		t.Fatalf("select-all text = %q", got)
	}
}

func TestHighlightStates(t *testing.T) {
	e := newTestEditor("")
	e.syntax = langGo
	e.buf = &Buffer{lines: [][]rune{
		[]rune("/* start"),
		[]rune("still comment"),
		[]rune("end */"),
		[]rune("code := 1"),
	}}
	e.hlDirty = 0
	e.recomputeHLStates()
	if e.states[0] || !e.states[1] || !e.states[2] || e.states[3] {
		t.Fatalf("states = %v", e.states)
	}
	colors, _ := e.hlLine(3)
	if colors[0] != colDefault {
		t.Fatalf("expected default color, got %q", colors[0])
	}
}

func TestFirstVisitEndOfLine(t *testing.T) {
	e := &Editor{buf: &Buffer{lines: [][]rune{
		[]rune("hi"),
		[]rune("hello world"),
		[]rune("abc"),
	}}, indent: "\t", hlDirty: -1, visited: []bool{true}, lastCol: []int{0, -1, -1}}
	e.rememberCursor() // cursor at (0,0)

	e.moveDown(false) // first visit to line 1 -> end of line
	if e.cur.row != 1 || e.cur.col != 11 {
		t.Fatalf("first visit line 1: row=%d col=%d, want 1,11", e.cur.row, e.cur.col)
	}
	e.rememberCursor()
	e.moveDown(false) // first visit to line 2 -> end of line
	if e.cur.col != 3 {
		t.Fatalf("first visit line 2: col=%d, want 3", e.cur.col)
	}
	e.rememberCursor()
	// Leave line 2 from column 0, then return to the visited line 1: the
	// cursor must go to line 1's remembered position (11), not stay at col 0.
	e.goPos(2, 0, false)
	e.rememberCursor()
	e.moveUp(false)
	if e.cur.col != 11 {
		t.Fatalf("return to visited line 1: col=%d, want 11", e.cur.col)
	}
	e.moveUp(false) // visited line 0 -> its remembered position (0)
	if e.cur.col != 0 {
		t.Fatalf("visited line 0: col=%d, want 0", e.cur.col)
	}
}

func TestReturnToVisitedLineFallsBackToDesired(t *testing.T) {
	// If a visited line has no remembered column (stale/absent memory), the
	// previous desired column is used.
	e := &Editor{buf: &Buffer{lines: [][]rune{
		[]rune("hi"),
		[]rune("hello world"),
	}}, indent: "\t", hlDirty: -1, visited: []bool{true, true}, lastCol: []int{0, -1}}
	e.goPos(0, 2, false) // desired = 2
	e.moveDown(false)    // line 1 visited but no memory -> desired col 2
	if e.cur.col != 2 {
		t.Fatalf("fallback: col=%d, want 2", e.cur.col)
	}
}

func TestFirstVisitLongLine(t *testing.T) {
	// w=80, gutter 3 + separator -> textW=76; visible cells 0..75.
	long := strings.Repeat("x", 100)
	e := &Editor{buf: &Buffer{lines: [][]rune{[]rune("short"), []rune(long)}},
		indent: "\t", hlDirty: -1, visited: []bool{true}, lastCol: []int{0, -1}, w: 80}
	e.rememberCursor()
	e.moveDown(false) // first visit to the long line
	if e.cur.col != 75 {
		t.Fatalf("long line first visit: col=%d, want 75 (last visible)", e.cur.col)
	}

	// A short line still lands at its end.
	e2 := &Editor{buf: &Buffer{lines: [][]rune{[]rune("abc"), []rune("def")}},
		indent: "\t", hlDirty: -1, visited: []bool{true}, lastCol: []int{0, -1}, w: 80}
	e2.rememberCursor()
	e2.moveDown(false)
	if e2.cur.col != 3 {
		t.Fatalf("short line first visit: col=%d, want 3", e2.cur.col)
	}

	// A line that fits exactly (textW) also avoids scrolling: lands on its
	// last character, not past it.
	exact := strings.Repeat("y", 76)
	e3 := &Editor{buf: &Buffer{lines: [][]rune{[]rune("abc"), []rune(exact)}},
		indent: "\t", hlDirty: -1, visited: []bool{true}, lastCol: []int{0, -1}, w: 80}
	e3.rememberCursor()
	e3.moveDown(false)
	if e3.cur.col != 75 {
		t.Fatalf("exact-fit line first visit: col=%d, want 75", e3.cur.col)
	}
}

func TestBackspaceKeyConventions(t *testing.T) {
	old := eraseByte
	defer func() { eraseByte = old }()

	// xterm convention: VERASE = 0x7f
	eraseByte = 0x7f
	if k := backspaceKey(0x7f); k.mods != modNone {
		t.Fatalf("0x7f should be plain backspace, got mods %v", k.mods)
	}
	if k := backspaceKey(0x08); k.mods != modCtrl {
		t.Fatalf("0x08 should be ctrl+backspace, got mods %v", k.mods)
	}

	// ^H convention: VERASE = 0x08
	eraseByte = 0x08
	if k := backspaceKey(0x08); k.mods != modNone {
		t.Fatalf("0x08 should be plain backspace, got mods %v", k.mods)
	}
	if k := backspaceKey(0x7f); k.mods != modCtrl {
		t.Fatalf("0x7f should be ctrl+backspace, got mods %v", k.mods)
	}
}

func TestLastPosMarker(t *testing.T) {
	// rememberCursor records and grows lazily.
	e := &Editor{buf: &Buffer{lines: [][]rune{[]rune("a b"), []rune("x")}}, lastCol: []int{-1}}
	e.cur = pos{1, 1}
	e.rememberCursor()
	if len(e.lastCol) != 2 || e.lastCol[1] != 1 {
		t.Fatalf("lastCol = %v", e.lastCol)
	}

	// Marker on a space renders as a red '<'.
	e.cur = pos{1, 0}
	e.lastCol = []int{1, -1}
	var sb strings.Builder
	e.renderLine(&sb, 0, 80, pos{0, 0}, pos{0, -1})
	if !strings.Contains(sb.String(), "\x1b[38;5;196m<") {
		t.Fatalf("expected red '<' marker, got %q", sb.String())
	}

	// Marker on a normal character renders that character red.
	e2 := &Editor{buf: &Buffer{lines: [][]rune{[]rune("ab")}}, lastCol: []int{1}}
	e2.cur = pos{5, 0} // not the rendered line
	var sb2 strings.Builder
	e2.renderLine(&sb2, 0, 80, pos{0, 0}, pos{0, -1})
	if !strings.Contains(sb2.String(), "\x1b[38;5;196mb") {
		t.Fatalf("expected red character, got %q", sb2.String())
	}

	// No marker on the line the cursor currently occupies.
	e3 := &Editor{buf: &Buffer{lines: [][]rune{[]rune("ab")}}, lastCol: []int{0}}
	e3.cur = pos{0, 0}
	var sb3 strings.Builder
	e3.renderLine(&sb3, 0, 80, pos{0, 0}, pos{0, -1})
	if strings.Contains(sb3.String(), "\x1b[38;5;196m") {
		t.Fatalf("no marker expected on the cursor line, got %q", sb3.String())
	}
}

func TestPunctJump(t *testing.T) {
	// "foo(bar); baz": punctuation at 3 '(', 7 ')', 8 ';'
	e := newTestEditor("foo(bar); baz")
	e.goPos(0, 0, false)
	e.punctRight(false) // -> '(' at 3
	if e.cur.col != 3 {
		t.Fatalf("punctRight 1: col=%d, want 3", e.cur.col)
	}
	e.punctRight(false) // -> ')' at 7
	if e.cur.col != 7 {
		t.Fatalf("punctRight 2: col=%d, want 7", e.cur.col)
	}
	e.punctRight(false) // -> ';' at 8
	if e.cur.col != 8 {
		t.Fatalf("punctRight 3: col=%d, want 8", e.cur.col)
	}
	e.punctRight(false) // no more punctuation -> stays
	if e.cur.col != 8 {
		t.Fatalf("punctRight 4: col=%d, want 8", e.cur.col)
	}
	e.punctLeft(false) // -> ')' at 7
	if e.cur.col != 7 {
		t.Fatalf("punctLeft 1: col=%d, want 7", e.cur.col)
	}
	e.punctLeft(false) // -> '(' at 3
	if e.cur.col != 3 {
		t.Fatalf("punctLeft 2: col=%d, want 3", e.cur.col)
	}
	e.punctLeft(false) // no punctuation before -> stays
	if e.cur.col != 3 {
		t.Fatalf("punctLeft 3: col=%d, want 3", e.cur.col)
	}
}

func TestPunctJumpSkipsWhitespace(t *testing.T) {
	// "a b, c": space at 1 and 4 are NOT jump targets, comma at 3 is.
	e := newTestEditor("a b, c")
	e.goPos(0, 0, false)
	e.punctRight(false) // skips the space -> comma at 3
	if e.cur.col != 3 {
		t.Fatalf("punctRight skips space: col=%d, want 3", e.cur.col)
	}
	e.punctRight(false) // no more punctuation -> stays
	if e.cur.col != 3 {
		t.Fatalf("punctRight no more: col=%d, want 3", e.cur.col)
	}
	e.punctLeft(false) // no punctuation before -> stays (space is not a target)
	if e.cur.col != 3 {
		t.Fatalf("punctLeft no punct before: col=%d, want 3", e.cur.col)
	}
}

func TestJumpToLastColChar(t *testing.T) {
	e := &Editor{buf: &Buffer{lines: [][]rune{
		[]rune("aa"),
		[]rune("bx"),
		[]rune("cx"),
		[]rune("dd"),
	}}, indent: "\t", hlDirty: -1, visited: []bool{true, true, true, true}, lastCol: []int{0, 1, 1, 0}}
	e.cur = pos{0, 0}
	e.jumpToChar('x') // nearest line with 'x' at its last position
	if e.cur.row != 1 || e.cur.col != 1 {
		t.Fatalf("jump: row=%d col=%d, want 1,1", e.cur.row, e.cur.col)
	}
	e.jumpToChar('x') // already on line 1's spot -> next is line 2
	if e.cur.row != 2 || e.cur.col != 1 {
		t.Fatalf("jump2: row=%d col=%d, want 2,1", e.cur.row, e.cur.col)
	}
	e.jumpToChar('z') // no line -> stays
	if e.cur.row != 2 {
		t.Fatalf("no match: row=%d, want 2", e.cur.row)
	}
}

func TestNinjaMode(t *testing.T) {
	e := &Editor{buf: &Buffer{lines: [][]rune{[]rune("aa"), []rune("bx")}},
		indent: "\t", hlDirty: -1, visited: []bool{true, true}, lastCol: []int{0, 1}}
	e.actNinja(key{})
	if !e.ninja {
		t.Fatal("ninja not active after Ctrl+P")
	}
	e.handleNinja(kRune('x', modNone)) // next character jumps
	if e.ninja {
		t.Fatal("ninja still active after jump")
	}
	if e.cur.row != 1 || e.cur.col != 1 {
		t.Fatalf("ninja jump: row=%d col=%d, want 1,1", e.cur.row, e.cur.col)
	}
	// Esc cancels without moving.
	e2 := &Editor{buf: &Buffer{lines: [][]rune{[]rune("aa"), []rune("bx")}},
		indent: "\t", hlDirty: -1, visited: []bool{true, true}, lastCol: []int{0, 1}}
	e2.actNinja(key{})
	e2.handleNinja(kRune(0x1b, modNone))
	if e2.ninja {
		t.Fatal("ninja still active after Esc")
	}
	if e2.cur.row != 0 || e2.cur.col != 0 {
		t.Fatalf("cursor moved on Esc: %v", e2.cur)
	}
}

func TestSearchMatches(t *testing.T) {
	e := &Editor{buf: &Buffer{lines: [][]rune{[]rune("foo bar"), []rune("baz foo")}}, indent: "\t", hlDirty: -1}
	e.search.query = []rune("foo")
	e.recomputeSearch()
	if len(e.search.matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(e.search.matches))
	}
	m0, m1 := e.search.matches[0], e.search.matches[1]
	if m0.row != 0 || m0.col != 0 || m1.row != 1 || m1.col != 4 {
		t.Fatalf("matches = %+v, %+v", m0, m1)
	}
	if e.search.sel != 0 { // nearest at/after cursor row (0)
		t.Fatalf("sel = %d, want 0", e.search.sel)
	}
	if len(e.search.byRow[1]) != 1 || e.search.byRow[1][0].col != 4 {
		t.Fatalf("byRow[1] = %+v", e.search.byRow[1])
	}
}

func TestSearchNav(t *testing.T) {
	e := &Editor{buf: &Buffer{lines: [][]rune{[]rune("foo bar"), []rune("baz foo")}}, indent: "\t", hlDirty: -1}
	e.search.active = true
	e.search.query = []rune("foo")
	e.recomputeSearch()
	e.handleSearch(kRune('n', modCtrl))
	if e.search.sel != 1 {
		t.Fatalf("after ^N sel=%d, want 1", e.search.sel)
	}
	e.handleSearch(kRune('\n', modNone)) // Enter: jump to selected, exit
	if e.search.active {
		t.Fatal("search still active after Enter")
	}
	if e.cur.row != 1 || e.cur.col != 4 {
		t.Fatalf("cursor = %v, want (1,4)", e.cur)
	}
	// Esc exits without moving.
	e2 := newTestEditor("foo")
	e2.search.active = true
	e2.search.query = []rune("foo")
	e2.recomputeSearch()
	e2.handleSearch(kRune(0x1b, modNone))
	if e2.search.active {
		t.Fatal("search still active after Esc")
	}
	if e2.cur.row != 0 || e2.cur.col != 0 {
		t.Fatalf("cursor moved on Esc: %v", e2.cur)
	}
}

func TestSearchHighlight(t *testing.T) {
	e := newTestEditor("foo bar")
	e.search.active = true
	e.search.query = []rune("foo")
	e.recomputeSearch()
	var sb strings.Builder
	e.renderLine(&sb, 0, 80, pos{0, 0}, pos{0, -1})
	// sel = 0 is the only match, so the selected style (orange) is used.
	if !strings.Contains(sb.String(), "\x1b[48;5;202;38;5;16mfoo") {
		t.Fatalf("no search highlight, got %q", sb.String())
	}
}

func TestDisplayCol(t *testing.T) {
	line := []rune("\tab") // tab, 'a', 'b'
	if got := displayCol(line, 1); got != 8 {
		t.Fatalf("displayCol after tab = %d", got)
	}
	if got := displayCol(line, 3); got != 10 {
		t.Fatalf("displayCol at end = %d", got)
	}
	if got := runeContainingCol(line, 0); got != 0 {
		t.Fatalf("runeContainingCol(0) = %d", got)
	}
	if got := runeContainingCol(line, 5); got != 0 { // inside the tab
		t.Fatalf("runeContainingCol(5) = %d", got)
	}
	if got := runeContainingCol(line, 8); got != 1 { // 'a' starts at col 8
		t.Fatalf("runeContainingCol(8) = %d", got)
	}
	if got := runeContainingCol(line, 20); got != 3 { // beyond end
		t.Fatalf("runeContainingCol(20) = %d", got)
	}
	mid := []rune("a\tb")                    // 'a', tab, 'b'
	if got := displayCol(mid, 3); got != 9 { // a@0, tab 1..7, b@8 -> 9
		t.Fatalf("displayCol mid-line = %d", got)
	}
}
