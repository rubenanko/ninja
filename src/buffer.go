package main

// Buffer is a simple line-based text model. Lines are slices of runes, so
// UTF-8 text is handled without byte-offset headaches.
type Buffer struct {
	lines [][]rune
}

func NewBuffer() *Buffer {
	return &Buffer{lines: [][]rune{{}}}
}

func (b *Buffer) NumLines() int     { return len(b.lines) }
func (b *Buffer) Line(i int) []rune { return b.lines[i] }
func (b *Buffer) LineLen(i int) int { return len(b.lines[i]) }

// insert places text at (row, col). text may contain newlines.
func (b *Buffer) insert(row, col int, text []rune) {
	if len(text) == 0 {
		return
	}
	head := append([]rune{}, b.lines[row][:col]...)
	tail := append([]rune{}, b.lines[row][col:]...)
	segs := splitNewlines(text)
	lines := make([][]rune, 0, len(segs)+1)
	lines = append(lines, append(head, segs[0]...))
	for _, s := range segs[1:] {
		lines = append(lines, s)
	}
	lines[len(lines)-1] = append(lines[len(lines)-1], tail...)
	b.lines = append(b.lines[:row], append(lines, b.lines[row+1:]...)...)
}

// delete removes the range [r1,c1) .. [r2,c2) (c2 exclusive) and returns the
// removed text, including embedded newlines.
func (b *Buffer) delete(r1, c1, r2, c2 int) []rune {
	if r1 < 0 {
		r1 = 0
	}
	if r2 < 0 {
		r2 = 0
	}
	if r1 >= len(b.lines) || r2 >= len(b.lines) {
		return nil
	}
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, r2 = r2, r1
		c1, c2 = c2, c1
	}
	if c1 > len(b.lines[r1]) {
		c1 = len(b.lines[r1])
	}
	if c2 > len(b.lines[r2]) {
		c2 = len(b.lines[r2])
	}
	if r1 == r2 {
		text := append([]rune{}, b.lines[r1][c1:c2]...)
		line := append([]rune{}, b.lines[r1][:c1]...)
		line = append(line, b.lines[r1][c2:]...)
		b.lines[r1] = line
		return text
	}
	var out []rune
	out = append(out, b.lines[r1][c1:]...)
	for i := r1 + 1; i < r2; i++ {
		out = append(out, '\n')
		out = append(out, b.lines[i]...)
	}
	out = append(out, '\n')
	out = append(out, b.lines[r2][:c2]...)
	merged := append([]rune{}, b.lines[r1][:c1]...)
	merged = append(merged, b.lines[r2][c2:]...)
	b.lines[r1] = merged
	b.lines = append(b.lines[:r1+1], b.lines[r2+1:]...)
	return out
}

// text returns the range [r1,c1) .. [r2,c2) without modifying the buffer.
func (b *Buffer) text(r1, c1, r2, c2 int) []rune {
	if r1 < 0 {
		r1 = 0
	}
	if r2 < 0 {
		r2 = 0
	}
	if r1 >= len(b.lines) || r2 >= len(b.lines) {
		return nil
	}
	if r1 > r2 || (r1 == r2 && c1 > c2) {
		r1, r2 = r2, r1
		c1, c2 = c2, c1
	}
	if c1 > len(b.lines[r1]) {
		c1 = len(b.lines[r1])
	}
	if c2 > len(b.lines[r2]) {
		c2 = len(b.lines[r2])
	}
	if r1 == r2 {
		return append([]rune{}, b.lines[r1][c1:c2]...)
	}
	var out []rune
	out = append(out, b.lines[r1][c1:]...)
	for i := r1 + 1; i < r2; i++ {
		out = append(out, '\n')
		out = append(out, b.lines[i]...)
	}
	out = append(out, '\n')
	out = append(out, b.lines[r2][:c2]...)
	return out
}

func splitNewlines(text []rune) [][]rune {
	var out [][]rune
	cur := make([]rune, 0, 8)
	for _, r := range text {
		if r == '\n' {
			out = append(out, cur)
			cur = nil
		} else {
			cur = append(cur, r)
		}
	}
	out = append(out, cur)
	return out
}

func endAfterInsert(row, col int, text []rune) (int, int) {
	r, c := row, col
	for _, ch := range text {
		if ch == '\n' {
			r++
			c = 0
		} else {
			c++
		}
	}
	return r, c
}
