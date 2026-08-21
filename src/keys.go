package main

import (
	"os"
	"time"
)

// Modifier flags.
type mods int

const (
	modNone  mods = 0
	modShift mods = 1 << iota
	modCtrl
	modAlt
)

// Special key codes (not representable as a rune).
type code int

const (
	codeNone code = iota
	codeBackspace
	codeDelete
	codeInsert
	codeArrowUp
	codeArrowDown
	codeArrowLeft
	codeArrowRight
	codeHome
	codeEnd
	codePageUp
	codePageDown
)

// key is one normalized keypress.
type key struct {
	r    rune // rune value (0 when code != codeNone)
	code code // special code (codeNone when r is meaningful)
	mods mods
}

func kRune(r rune, m mods) key { return key{r: r, mods: m} }
func kCode(c code, m mods) key { return key{code: c, mods: m} }

// keyParser reads bytes from stdin on a goroutine and decodes them into keys.
type keyParser struct {
	ch chan byte
}

func newKeyParser() *keyParser {
	p := &keyParser{ch: make(chan byte, 256)}
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			for i := 0; i < n; i++ {
				p.ch <- buf[i]
			}
			if err != nil {
				close(p.ch)
				return
			}
		}
	}()
	return p
}

func (p *keyParser) readByte() (byte, bool) {
	b, ok := <-p.ch
	return b, ok
}

func (p *keyParser) readByteTimeout(d time.Duration) (byte, bool) {
	select {
	case b, ok := <-p.ch:
		return b, ok
	case <-time.After(d):
		return 0, false
	}
}

// next returns the next key, or ok=false when stdin is closed.
func (p *keyParser) next() (key, bool) {
	b, ok := p.readByte()
	if !ok {
		return key{}, false
	}
	if b != 0x1b {
		return p.parseLead(b), true
	}
	// ESC: could be a lone escape or the start of an escape sequence.
	nb, ok := p.readByteTimeout(30 * time.Millisecond)
	if !ok {
		return kRune(0x1b, modNone), true // lone Esc
	}
	switch nb {
	case '[':
		return p.parseCSI(), true
	case 'O':
		return p.parseSS3(), true
	case 0x1b:
		return kRune(0x1b, modAlt), true
	default:
		k := p.parseLead(nb)
		k.mods |= modAlt
		return k, true
	}
}

func (p *keyParser) parseLead(b byte) key {
	switch {
	case b == 0x0d || b == 0x0a:
		return kRune('\n', modNone) // Enter
	case b == 0x09:
		return kRune('\t', modNone) // Tab
	case b == 0x7f:
		return kCode(codeBackspace, modNone) // Backspace
	case b == 0x08:
		return kCode(codeBackspace, modCtrl) // Ctrl+Backspace (xterm convention)
	case b == 0x1c:
		return kRune('\\', modCtrl) // Ctrl+\
	case b >= 1 && b <= 26:
		return kRune(rune('a'-1+b), modCtrl)
	case b >= 0x20 && b < 0x7f:
		return kRune(rune(b), modNone)
	case b >= 0xc0:
		return p.decodeUTF8(b)
	default:
		return kRune(rune(b), modNone)
	}
}

func (p *keyParser) decodeUTF8(b byte) key {
	n := 2
	r := rune(b & 0x1f)
	switch {
	case b >= 0xf0:
		n, r = 4, rune(b&0x07)
	case b >= 0xe0:
		n, r = 3, rune(b&0x0f)
	}
	for i := 1; i < n; i++ {
		cb, ok := p.readByteTimeout(50 * time.Millisecond)
		if !ok || cb&0xc0 != 0x80 {
			return kRune(0xfffd, modNone) // malformed; replacement char
		}
		r = r<<6 | rune(cb&0x3f)
	}
	return kRune(r, modNone)
}

// parseCSI handles "ESC [ ..." sequences, e.g. arrows, home/end, delete,
// and modified variants like "ESC [ 1 ; 5 C" (Ctrl+Right).
func (p *keyParser) parseCSI() key {
	params := []int{}
	num := -1
	var final byte
	for {
		b, ok := p.readByte()
		if !ok {
			return kRune(0x1b, modNone)
		}
		switch {
		case b >= '0' && b <= '9':
			if num == -1 {
				num = 0
			}
			num = num*10 + int(b-'0')
		case b == ';':
			if num == -1 {
				num = 0
			}
			params = append(params, num)
			num = -1
		case b >= 0x40 && b <= 0x7e:
			final = b
		}
		if final != 0 {
			break
		}
	}
	if num >= 0 {
		params = append(params, num)
	}
	if len(params) == 0 {
		params = append(params, 0)
	}
	m := modNone
	if len(params) > 1 {
		switch params[1] {
		case 2:
			m = modShift
		case 3:
			m = modAlt
		case 4:
			m = modAlt | modShift
		case 5:
			m = modCtrl
		case 6:
			m = modCtrl | modShift
		case 7:
			m = modCtrl | modAlt
		case 8:
			m = modCtrl | modAlt | modShift
		}
	}
	switch final {
	case 'A':
		return kCode(codeArrowUp, m)
	case 'B':
		return kCode(codeArrowDown, m)
	case 'C':
		return kCode(codeArrowRight, m)
	case 'D':
		return kCode(codeArrowLeft, m)
	case 'H':
		return kCode(codeHome, m)
	case 'F':
		return kCode(codeEnd, m)
	case 'Z':
		return kRune('\t', modShift)
	case '~':
		switch params[0] {
		case 1, 7:
			return kCode(codeHome, m)
		case 4, 8:
			return kCode(codeEnd, m)
		case 2:
			return kCode(codeInsert, m)
		case 3:
			return kCode(codeDelete, m)
		case 5:
			return kCode(codePageUp, m)
		case 6:
			return kCode(codePageDown, m)
		}
	case 'u':
		// CSI-u (kitty protocol): ESC [ <code> ; <mods> u
		if params[0] == 8 || params[0] == 127 {
			return kCode(codeBackspace, m)
		}
	}
	return kRune(0x1b, modNone)
}

// parseSS3 handles "ESC O ..." sequences (application cursor keys).
func (p *keyParser) parseSS3() key {
	b, ok := p.readByte()
	if !ok {
		return kRune(0x1b, modNone)
	}
	switch b {
	case 'A':
		return kCode(codeArrowUp, modNone)
	case 'B':
		return kCode(codeArrowDown, modNone)
	case 'C':
		return kCode(codeArrowRight, modNone)
	case 'D':
		return kCode(codeArrowLeft, modNone)
	case 'H':
		return kCode(codeHome, modNone)
	case 'F':
		return kCode(codeEnd, modNone)
	}
	return kRune(0x1b, modNone)
}
