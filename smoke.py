#!/usr/bin/env python3
"""Smoke-test tinyedit in a pty: type text, use shortcuts, verify the file."""
import fcntl, os, pty, re, select, signal, struct, sys, time

BIN = "./build/tinyedit"

ANSI = re.compile(rb"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]")

def strip_ansi(b):
    return ANSI.sub(b"", b)

def set_winsize(fd, rows=24, cols=80):
    fcntl.ioctl(fd, 0x5414, struct.pack("HHHH", rows, cols, 0, 0))  # TIOCSWINSZ

def run(keys, path, settle=0.25, cols=80):
    pid, fd = pty.fork()
    if pid == 0:
        os.execv(BIN, [BIN, path])
    set_winsize(fd, 24, cols)
    out = b""
    time.sleep(settle)
    os.write(fd, keys)
    time.sleep(settle)
    while True:
        r, _, _ = select.select([fd], [], [], 0.25)
        if not r:
            break
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        out += data
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    os.waitpid(pid, 0)
    return out

import termios

def run_with_erase(keys, path, erase):
    """Like run(), but sets the pty's VERASE byte to `erase` so the editor
    disambiguates plain Backspace vs Ctrl+Backspace the way the user's
    terminal would."""
    master, slave = os.openpty()
    attrs = termios.tcgetattr(slave)
    attrs[6][termios.VERASE] = erase
    termios.tcsetattr(slave, termios.TCSANOW, attrs)
    pid = os.fork()
    if pid == 0:
        os.dup2(slave, 0)
        os.dup2(slave, 1)
        os.dup2(slave, 2)
        os.execv(BIN, [BIN, path])
    set_winsize(master)
    out = b""
    time.sleep(0.25)
    os.write(master, keys)
    time.sleep(0.25)
    while True:
        r, _, _ = select.select([master], [], [], 0.25)
        if not r:
            break
        try:
            data = os.read(master, 65536)
        except OSError:
            break
        if not data:
            break
        out += data
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    os.waitpid(pid, 0)
    os.close(master)
    os.close(slave)
    return out

def check(name, cond, detail=""):
    print(("PASS " if cond else "FAIL ") + name + (("  " + detail) if detail and not cond else ""))
    if not cond:
        sys.exit(1)

# 1. type + save + quit
p = "/tmp/tinyedit_smoke1.go"
open(p, "w").close()
out = run(b"package main" + b"\x13" + b"\x11", p)  # ^S save, ^Q quit
check("typed text appears in terminal output", b"package main" in strip_ansi(out))
check("save writes file", open(p).read() == "package main\n", repr(open(p).read()))

# 2. undo via key, save, quit -> empty file
p = "/tmp/tinyedit_smoke2.go"
open(p, "w").close()
out = run(b"abc" + b"\x1a" + b"\x13" + b"\x11", p)  # ^Z undo, ^S, ^Q
check("undo then save leaves file empty", open(p).read() == "\n", repr(open(p).read()))

# 3. undo + redo, save, quit -> text back
p = "/tmp/tinyedit_smoke3.go"
open(p, "w").close()
out = run(b"abc" + b"\x1a" + b"\x19" + b"\x13" + b"\x11", p)  # ^Z ^Y ^S ^Q
check("redo restores text in file", open(p).read() == "abc\n", repr(open(p).read()))

# 4. newline + auto indent on a brace line, then save
p = "/tmp/tinyedit_smoke4.go"
open(p, "w").close()
out = run(b"func f() {" + b"\r" + b"\x13" + b"\x11", p)
check("auto-indent after '{'", open(p).read() == "func f() {\n\t\n", repr(open(p).read()))

# 5. ctrl+arrow word jump and ctrl+\ word delete on existing text, save
p = "/tmp/tinyedit_smoke5.go"
open(p, "w").write("hello world")
out = run(b"\x1b[1;5C" + b"\x1c" + b"\x13" + b"\x11", p)  # ctrl+right, ctrl+\ (delete word), ^S, ^Q
check("ctrl+\\ deletes the word at the cursor", open(p).read() == " world\n", repr(open(p).read()))

# 6. paste internal clipboard: select-all, copy, go to end, paste
p = "/tmp/tinyedit_smoke6.go"
open(p, "w").close()
# ^A select all (empty -> no-op), type hi, ^A copy, ^X cut, ^V paste -> hi
out = run(b"hi" + b"\x01" + b"\x03" + b"\x18" + b"\x16" + b"\x13" + b"\x11", p)  # ^A ^C ^X ^V ^S ^Q
check("copy/cut/paste round-trips", open(p).read() == "hi\n", repr(open(p).read()))

# 7. open prompt: ctrl+o then type filename, enter
p = "/tmp/tinyedit_smoke7.go"
open(p, "w").write("opened")
run(b"\x0f", p)  # ^O -> prompt should appear; then SIGKILL; just ensure no crash

# 8. argv[1] on a nonexistent file: create it, and Ctrl+S writes there
p = "/tmp/tinyedit_smoke8.go"
if os.path.exists(p):
    os.remove(p)
out = run(b"hi" + b"\x13" + b"\x11", p)  # type, ^S save, ^Q quit
check("argv[1] creates a missing file and Ctrl+S writes to it",
      os.path.exists(p) and open(p).read() == "hi\n",
      repr(open(p).read()) if os.path.exists(p) else "file missing")

# 9. cursor is re-shown (DECTCEM ?25h) right before cursor positioning
p = "/tmp/tinyedit_smoke9.go"
open(p, "w").close()
out = run(b"", p)
check("cursor visibility restored after each render",
      b"\x1b[?25h\x1b[1;" in out, repr(out[-200:]))

# 10. cursor sits one column AFTER the last typed character
p = "/tmp/tinyedit_smoke10.go"
open(p, "w").close()
out = run(b"ab", p)
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("cursor sits after the last character",
      bool(frames) and frames[-1] == (b"1", b"7"),
      frames[-3:] if frames else "none")

# 11-17. backspace vs ctrl+backspace, for both terminal conventions.
# xterm convention (VERASE = 0x7f): 0x7f = backspace, 0x08 = ctrl+backspace
p = "/tmp/tinyedit_smoke11.go"
open(p, "w").close()
out = run_with_erase(b"hello world" + b"\x08" + b"\x13" + b"\x11", p, 0x7f)
check("ctrl+backspace (0x08) erases a word (xterm convention)",
      open(p).read() == "hello \n", repr(open(p).read()))

p = "/tmp/tinyedit_smoke15.go"
open(p, "w").close()
out = run_with_erase(b"ab" + b"\x7f" + b"\x13" + b"\x11", p, 0x7f)
check("backspace (0x7f) erases one char (xterm convention)",
      open(p).read() == "a\n", repr(open(p).read()))

# ^H convention (VERASE = 0x08): 0x08 = backspace, 0x7f = ctrl+backspace
p = "/tmp/tinyedit_smoke16.go"
open(p, "w").close()
out = run_with_erase(b"ab" + b"\x08" + b"\x13" + b"\x11", p, 0x08)
check("backspace (0x08) erases one char (^H convention)",
      open(p).read() == "a\n", repr(open(p).read()))

p = "/tmp/tinyedit_smoke17.go"
open(p, "w").close()
out = run_with_erase(b"hello world" + b"\x7f" + b"\x13" + b"\x11", p, 0x08)
check("ctrl+backspace (0x7f) erases a word (^H convention)",
      open(p).read() == "hello \n", repr(open(p).read()))

# 12. cursor column accounts for tab expansion (\t, a, b -> cursor at display col 10)
p = "/tmp/tinyedit_smoke12.go"
open(p, "w").close()
out = run(b"\tab", p)
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("cursor lands after tab-expanded text",
      bool(frames) and frames[-1] == (b"1", b"15"),
      frames[-3:] if frames else "none")

# 13. tab renders as spaces in the frame (\t = 7 spaces at column 0)
p = "/tmp/tinyedit_smoke13.go"
open(p, "w").close()
out = run(b"a\tb", p)
txt = strip_ansi(out)
check("tab renders as 7 spaces", b"  1 a       b" in txt, repr(txt[:80]))

# 14. first visit to a line (via down arrow) lands at the end of the line
p = "/tmp/tinyedit_smoke14.go"
open(p, "w").write("aaa\nbbb\nccc")
out = run(b"\x1b[B", p)  # down arrow once
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("first visit lands at end of line",
      bool(frames) and frames[-1] == (b"2", b"8"),
      frames[-3:] if frames else "none")

# 18. last cursor position on a line is marked in red (regular character)
p = "/tmp/tinyedit_smoke18.go"
open(p, "w").write("ab\ncd")
out = run(b"\x1b[B" + b"\x1b[D" + b"\x1b[A", p)  # down (line 2 end), left (col 1), up
check("last position on a line shown as red character",
      b"\x1b[38;5;196md" in out, repr(out[-300:]))

# 19. a space at the last cursor position is shown as a red '<'
p = "/tmp/tinyedit_smoke19.go"
open(p, "w").write("a b\nx")
out = run(b"\x1b[C" + b"\x1b[B", p)  # right (onto the space), down
check("space at last position shown as red '<'",
      b"\x1b[38;5;196m<" in out, repr(out[-300:]))

# 20. returning to a visited line goes back to its remembered position
p = "/tmp/tinyedit_smoke20.go"
open(p, "w").write("abc\ndefgh\nij")
out = run(b"\x1b[B" + b"\x1b[B" + b"\x1b[D" + b"\x1b[A", p)  # down, down, left, up
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("return to visited line uses its remembered position",
      bool(frames) and frames[-1] == (b"2", b"10"),
      frames[-3:] if frames else "none")

# 21. long line first visit lands on the last visible character (no h-scroll)
p = "/tmp/tinyedit_smoke21.go"
open(p, "w").write("short\n" + "x" * 40)
out = run(b"\x1b[B", p, cols=30)  # textW = 30-3-1 = 26, last visible cell = 25
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("long line first visit lands on last visible character",
      bool(frames) and frames[-1] == (b"2", b"30"),
      frames[-3:] if frames else "none")

# 22. alt+arrow jumps to the next/previous non-alphanumeric character,
# skipping spaces: "hello world, foo" has a single punctuation (comma at col 11)
p = "/tmp/tinyedit_smoke22.go"
open(p, "w").write("hello world, foo")
out = run(b"\x1b[1;3C" + b"\x1b[1;3C" + b"\x1b[1;3D", p)  # alt+right, alt+right, alt+left
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("alt+arrow jumps over spaces to punctuation",
      bool(frames) and frames[-1] == (b"1", b"16"),  # comma at col 11 -> screen 16
      frames[-3:] if frames else "none")

# 23. search: ctrl+/ (0x1f), type "foo", ^N, Enter -> jumps to second occurrence
p = "/tmp/tinyedit_smoke23.go"
open(p, "w").write("foo bar\nbaz foo")
out = run(b"\x1f" + b"foo" + b"\x0e" + b"\r", p)  # ctrl+/, foo, ^N, Enter
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("search jumps to next occurrence on Enter",
      bool(frames) and frames[-1] == (b"2", b"9"),  # (1,4) -> screen col 9
      frames[-3:] if frames else "none")
check("search occurrences highlighted",
      b"\x1b[48;5;220;38;5;16m" in out, repr(out[-300:]))

# 24. escape exits search mode without moving the editing cursor
p = "/tmp/tinyedit_smoke24.go"
open(p, "w").write("foo bar")
out = run(b"\x1f" + b"foo" + b"\x1b", p)  # ctrl+/, foo, Esc
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("escape exits search without moving cursor",
      bool(frames) and frames[-1] == (b"1", b"5"),
      frames[-3:] if frames else "none")

# 25. ninja jump: ctrl+P then the target character jumps to the nearest
# line whose last position character is that character
p = "/tmp/tinyedit_smoke25.go"
open(p, "w").write("aa\nbx\ncx")
out = run(b"\x1b[B" + b"\x1b[D" + b"\x1b[B" + b"\x1b[D" + b"\x1b[A" + b"\x1b[A" + b"\x10" + b"x", p)
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("ninja jump: ctrl+P then char jumps to nearest line",
      bool(frames) and frames[-1] == (b"2", b"6"),  # (1,1) -> screen col 6
      frames[-3:] if frames else "none")

# 26. search also triggers on ctrl+shift+/ (CSI-u ESC [ 47 ; 6 u) for
# keyboards that need Shift to produce '/'
p = "/tmp/tinyedit_smoke26.go"
open(p, "w").write("foo bar\nbaz foo")
out = run(b"\x1b[47;6u" + b"foo" + b"\r", p)  # ctrl+shift+/, foo, Enter
check("ctrl+shift+/ enters search mode",
      b"search: foo" in out, repr(out[-300:]))
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("search via ctrl+shift+/ jumps on Enter",
      bool(frames) and frames[-1] == (b"1", b"5"),  # first occurrence (0,0)
      frames[-3:] if frames else "none")

# 27. ninja mode footer turns red
p = "/tmp/tinyedit_smoke27.go"
open(p, "w").write("aa\nbx")
pid, fd = pty.fork()
if pid == 0:
    os.execv(BIN, [BIN, p])
set_winsize(fd)
time.sleep(0.25)
os.write(fd, b"\x10")  # ctrl+P -> ninja mode
out = b""
time.sleep(0.25)
while True:
    r, _, _ = select.select([fd], [], [], 0.2)
    if not r:
        break
    try:
        data = os.read(fd, 65536)
    except OSError:
        break
    if not data:
        break
    out += data
try:
    os.kill(pid, signal.SIGKILL)
except ProcessLookupError:
    pass
os.waitpid(pid, 0)
check("ninja mode footer turns red",
      b"\x1b[48;5;196;38;5;15m" in out, repr(out[-300:]))

# 28. ctrl+/ on terminals that encode it as 0x0f (ASCII & 0x1f) now enters search
p = "/tmp/tinyedit_smoke28.go"
open(p, "w").write("foo bar\nbaz foo")
out = run(b"\x0f" + b"foo" + b"\r", p)  # ctrl+/ (0x0f), foo, Enter
check("ctrl+/ (0x0f) enters search mode",
      b"search: foo" in out, repr(out[-300:]))
frames = re.findall(rb"\x1b\[\?25h\x1b\[(\d+);(\d+)H", out)
check("ctrl+/ (0x0f) jumps on Enter",
      bool(frames) and frames[-1] == (b"1", b"5"),
      frames[-3:] if frames else "none")

# 29. alt+o still opens the file prompt (Ctrl+O's byte 0x0f is now Ctrl+/)
p = "/tmp/tinyedit_smoke29.go"
open(p, "w").write("x")
out = run(b"\x1bo", p)  # Alt+O
check("alt+o opens the open prompt",
      b"open: " in out, repr(out[-300:]))

print("ALL SMOKE TESTS PASSED")
