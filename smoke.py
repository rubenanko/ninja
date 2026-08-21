#!/usr/bin/env python3
"""Smoke-test tinyedit in a pty: type text, use shortcuts, verify the file."""
import fcntl, os, pty, re, select, signal, struct, sys, time

BIN = "./build/tinyedit"

ANSI = re.compile(rb"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]")

def strip_ansi(b):
    return ANSI.sub(b"", b)

def set_winsize(fd, rows=24, cols=80):
    fcntl.ioctl(fd, 0x5414, struct.pack("HHHH", rows, cols, 0, 0))  # TIOCSWINSZ

def run(keys, path, settle=0.25):
    pid, fd = pty.fork()
    if pid == 0:
        os.execv(BIN, [BIN, path])
    set_winsize(fd)
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

# 11. ctrl+backspace (0x08, xterm convention) erases a whole word
p = "/tmp/tinyedit_smoke11.go"
open(p, "w").close()
out = run(b"hello world" + b"\x08" + b"\x13" + b"\x11", p)  # type, ctrl+backspace, ^S, ^Q
check("ctrl+backspace erases a word", open(p).read() == "hello \n", repr(open(p).read()))

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

print("ALL SMOKE TESTS PASSED")
