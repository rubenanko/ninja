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
	}}, indent: "\t", hlDirty: -1, visited: []bool{true}} // line 0 visited at startup
	e.moveDown(false) // first visit to line 1 -> end of line
	if e.cur.row != 1 || e.cur.col != 11 {
		t.Fatalf("first visit line 1: row=%d col=%d, want 1,11", e.cur.row, e.cur.col)
	}
	e.moveDown(false) // first visit to line 2 -> end of line
	if e.cur.col != 3 {
		t.Fatalf("first visit line 2: col=%d, want 3", e.cur.col)
	}
	e.moveUp(false) // line 1 already visited -> desired column preserved
	if e.cur.col != 3 {
		t.Fatalf("return to visited line: col=%d, want 3", e.cur.col)
	}
	e.goPos(1, 0, false)
	e.moveUp(false) // line 0 visited -> keeps desired col 0
	if e.cur.col != 0 {
		t.Fatalf("visited line 0 keeps desired col: col=%d, want 0", e.cur.col)
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
