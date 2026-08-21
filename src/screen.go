package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// enterRaw switches the terminal to raw mode, enters the alternate screen
// and hides the cursor. The returned func restores everything.
func enterRaw() (func(), error) {
	fd := int(os.Stdin.Fd())
	t, err := getTermios(fd)
	if err != nil {
		return nil, fmt.Errorf("get termios: %w", err)
	}
	orig := *t
	makeRaw(t)
	if err := setTermios(fd, t); err != nil {
		return nil, fmt.Errorf("set termios: %w", err)
	}
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l") // alt screen, hide cursor
	return func() {
		setTermios(fd, &orig)
		os.Stdout.WriteString("\x1b[0m\x1b[?25h\x1b[?1049l")
	}, nil
}

func makeRaw(t *syscall.Termios) {
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
}

func (e *Editor) querySize() {
	w, h, err := termSize(int(os.Stdout.Fd()))
	if err == nil && w > 0 && h > 0 {
		e.w, e.h = w, h
	}
}

// recomputeHLStates rebuilds the block-comment state for every line from the
// first dirty line onward. Only called once per frame when something changed.
func (e *Editor) recomputeHLStates() {
	n := e.buf.NumLines()
	if len(e.states) < n {
		s := make([]bool, n)
		copy(s, e.states)
		e.states = s
	}
	start := e.hlDirty
	if start < 0 {
		start = 0
	}
	in := false
	if start > 0 {
		in = e.states[start-1]
	}
	if e.syntax == nil {
		for i := start; i < n; i++ {
			e.states[i] = false
		}
		return
	}
	for i := start; i < n; i++ {
		e.states[i] = in
		_, in = e.syntax.line(e.buf.Line(i), in)
	}
}

// hlLine returns colors for one line plus the block state for the next line.
func (e *Editor) hlLine(ln int) ([]string, bool) {
	line := e.buf.Line(ln)
	if e.syntax == nil {
		colors := make([]string, len(line))
		for i := range colors {
			colors[i] = colDefault
		}
		return colors, false
	}
	return e.syntax.line(line, e.states[ln])
}

func (e *Editor) render() {
	e.querySize() // cheap ioctl; also picks up terminal resizes
	if e.w <= 0 || e.h <= 0 {
		return
	}
	w, h := e.w, e.h
	vh := h - 1 // rows available for text
	if vh < 1 {
		vh = 1
	}

	gw := 3
	if digits := len(strconv.Itoa(e.buf.NumLines())); digits > gw {
		gw = digits
	}
	textW := w - gw - 1
	if textW < 1 {
		textW = 1
	}

	e.scrollToCursor(textW, vh)

	if e.hlDirty >= 0 {
		e.recomputeHLStates()
		e.hlDirty = -1
	}

	selStart, selEnd := e.selRange()

	var sb strings.Builder
	sb.Grow(w * h * 2)
	sb.WriteString("\x1b[H")
	for row := 0; row < vh; row++ {
		ln := e.top + row
		sb.WriteString("\x1b[K")
		if ln < e.buf.NumLines() {
			sb.WriteString("\x1b[38;5;240m")
			num := strconv.Itoa(ln + 1)
			for i := len(num); i < gw; i++ {
				sb.WriteByte(' ')
			}
			sb.WriteString(num)
			sb.WriteByte(' ')
			e.renderLine(&sb, ln, textW, selStart, selEnd)
			sb.WriteString("\x1b[0m")
		}
		if row < vh-1 {
			sb.WriteString("\r\n")
		}
	}
	sb.WriteString("\r\n")
	e.renderStatus(&sb, w)

	// place the cursor (text area starts at column gw+2: gutter + separator;
	// column accounts for tab expansion)
	curDisp := displayCol(e.buf.Line(e.cur.row), e.cur.col)
	curRow, curCol := e.cur.row-e.top+1, curDisp-e.left+gw+2
	if e.prompt != nil {
		curRow = h
		curCol = len(e.prompt.label) + len(string(e.prompt.input)) + 1
	}
	if curRow < 1 {
		curRow = 1
	}
	if curRow > h {
		curRow = h
	}
	if curCol < 1 {
		curCol = 1
	}
	if curCol > w {
		curCol = w
	}
	fmt.Fprintf(&sb, "\x1b[?25h\x1b[%d;%dH", curRow, curCol) // show + position cursor
	os.Stdout.WriteString(sb.String())
}

func (e *Editor) scrollToCursor(textW, vh int) {
	if e.cur.row < e.top {
		e.top = e.cur.row
	}
	if e.cur.row >= e.top+vh {
		e.top = e.cur.row - vh + 1
	}
	if e.top < 0 {
		e.top = 0
	}
	maxTop := e.buf.NumLines() - vh
	if maxTop < 0 {
		maxTop = 0
	}
	if e.top > maxTop {
		e.top = maxTop
	}
	curDisp := displayCol(e.buf.Line(e.cur.row), e.cur.col)
	if curDisp < e.left {
		e.left = curDisp
	}
	if curDisp >= e.left+textW {
		e.left = curDisp - textW + 1
	}
	if e.left < 0 {
		e.left = 0
	}
}

// tabWidth is the display width of a tab stop.
const tabWidth = 8

// displayCol returns the display column of rune index c in line, expanding
// tabs to tab stops.
func displayCol(line []rune, c int) int {
	col := 0
	for i := 0; i < c && i < len(line); i++ {
		if line[i] == '\t' {
			col += tabWidth - col%tabWidth
		} else {
			col++
		}
	}
	return col
}

// runeContainingCol returns the rune index whose display range contains col.
func runeContainingCol(line []rune, col int) int {
	c := 0
	for i := 0; i < len(line); i++ {
		w := 1
		if line[i] == '\t' {
			w = tabWidth - c%tabWidth
		}
		if c+w > col {
			return i
		}
		c += w
	}
	return len(line)
}

// selRange returns the normalized selection as (start, end) positions.
func (e *Editor) selRange() (pos, pos) {
	if !e.hasSelection() {
		return pos{0, 0}, pos{0, -1}
	}
	return e.selStartEnd()
}

func (e *Editor) renderLine(sb *strings.Builder, ln, textW int, selStart, selEnd pos) {
	line := e.buf.Line(ln)
	colors, _ := e.hlLine(ln)

	// Selection columns on this line.
	selA, selB := 0, -1
	switch {
	case selStart.row < ln && ln < selEnd.row:
		selA, selB = 0, len(line)
	case selStart.row == ln && selEnd.row == ln:
		selA, selB = selStart.col, selEnd.col
	case selStart.row == ln:
		selA, selB = selStart.col, len(line)
	case selEnd.row == ln:
		selA, selB = 0, selEnd.col
	}

	limit := e.left + textW
	i := runeContainingCol(line, e.left)
	col := e.left
	curLine := ln == e.cur.row && e.prompt == nil
	curStyle := ""
	for i < len(line) && col < limit {
		style := colors[i]
		if i >= selA && i < selB {
			style += ";48;5;239"
		} else if curLine {
			style += ";48;5;236"
		}
		if style != curStyle {
			sb.WriteString("\x1b[" + style + "m")
			curStyle = style
		}
		if line[i] == '\t' {
			// Expand the tab into spaces, clipped to the visible window.
			tabStart := displayCol(line, i)
			tabEnd := tabStart + (tabWidth - tabStart%tabWidth)
			for col < tabEnd && col < limit {
				sb.WriteByte(' ')
				col++
			}
		} else {
			sb.WriteRune(line[i])
			col++
		}
		i++
	}
}

func (e *Editor) renderStatus(sb *strings.Builder, w int) {
	left := e.filename
	if left == "" {
		left = "[no file]"
	}
	if e.prompt != nil {
		left = e.prompt.label + string(e.prompt.input)
	} else {
		if e.modified {
			left += " *"
		}
	}
	right := ""
	if e.prompt == nil {
		right = fmt.Sprintf(" %d:%d  ^S save ^Z undo ^Y redo ^X cut ^C copy ^V paste ^Q quit",
			e.cur.row+1, e.cur.col+1)
	}
	if e.message != "" && time.Now().Before(e.msgUntil) {
		left += "  |  " + e.message
	}
	// Trim so the right-aligned hints still fit.
	maxLeft := w - len(right) - 1
	if maxLeft < 1 {
		maxLeft = 1
	}
	if len(left) > maxLeft {
		left = left[:maxLeft]
	}
	sb.WriteString("\x1b[7m") // inverse video
	sb.WriteString(left)
	if pad := w - len(left) - len(right); pad > 0 {
		for i := 0; i < pad; i++ {
			sb.WriteByte(' ')
		}
	}
	sb.WriteString(right)
	sb.WriteString("\x1b[0m\x1b[K")
}
