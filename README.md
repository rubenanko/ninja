# tinyedit

A tiny TUI code editor written in Go with **zero dependencies** (standard
library only). No ncurses, no termbox, no x/term — just raw `syscall` termios
on Linux and macOS.

## Features

- Syntax highlighting for Go, Python, JavaScript/TypeScript, C/C++/Java/C#,
  Rust and shell (keywords, types, builtins, strings, comments, numbers,
  function calls, block comments that span lines)
- Undo / redo with coalesced typing and backspace (so `Ctrl+Z` undoes a whole
  typed word, not one character)
- Copy / cut / paste with a selection model (shift+arrows, `Ctrl+A` select all)
- Smart cursor:
  - Auto-indent on Enter (keeps leading whitespace, adds a level after
    `{`, `[`, `(`)
  - Smart dedent when typing `}` on an otherwise-empty line
  - `Ctrl+←` / `Ctrl+→` (and `Alt+b/f`) jump between words; `Alt+←` / `Alt+→`
    jump to the next/previous non-alphanumeric, non-space character
    (punctuation and symbols), without leaving the line
  - `Ctrl+Shift+{char}` … ninja jump mode: press `Ctrl+P` (the footer turns
    red), then one character — the cursor jumps to the nearest line whose
    remembered last position character equals it (Esc cancels)
  - `Ctrl+\` deletes a whole word (the word at the cursor, or the next one)
  - `Ctrl+Backspace` deletes the word before the cursor
  - `Ctrl+U` / `Ctrl+K` delete to start / end of line
  - First visit to a line (via up/down arrows) lands at the end of that line;
    if the line is longer than the screen it lands on the last visible
    character instead, so the view never scrolls horizontally; returning to a
    line you've already been on goes back to the position you last had there
  - Each line remembers the last cursor position, shown as a red marker once
    you leave the line (a red `<` marks the spot when it was a space)
- Backspace vs Ctrl+Backspace are auto-detected from the terminal's erase
  byte, so both the `0x7f` (xterm) and `0x08` (^H) conventions work
- String search mode (`Ctrl+/`, also accepted as `Ctrl+Shift+/` and as the
  raw byte `0x0f` that ASCII&0x1f-style terminals send): type to filter,
  `Ctrl+N` / `Ctrl+P` cycle the next/previous occurrence, Enter jumps to the
  selected one, Esc exits. All occurrences are highlighted (orange = selected)
  while searching
- Line numbers, cursor-line highlight, selection highlight, status bar with
  messages and shortcut hints
- Open / save / save-as with an inline filename prompt (`Ctrl+O`, `Ctrl+S`)
- UTF-8 aware (text is stored as runes), handles terminal resizes, alt-screen
  so your shell history is untouched when you quit

## Build & run

```sh
./make.sh            # builds build/tinyedit
./build/tinyedit somefile.go   # or no args for a new empty buffer
```

`make.sh` is a tiny wrapper: it creates `./build` and runs
`go build -o build/tinyedit ./src`. Works on Linux and macOS. Everything is
one package, so `go run ./src` also works.

Tests:

```sh
(cd src && go test ./...)   # unit tests
python3 smoke.py            # end-to-end pty smoke test (needs python3 + build/tinyedit)
```

## Keybindings

| Keys | Action |
| --- | --- |
| `Ctrl+Q` | Quit (press twice to discard unsaved changes) |
| `Ctrl+S` | Save (prompts for a name if none) |
| `Ctrl+O` (or `Alt+O`) | Open a file (prompt). On terminals that encode Ctrl+`/` as byte 0x0f, that byte is Ctrl+`/` now, so use `Alt+O` or CSI-u Ctrl+O |
| `Ctrl+Z` / `Ctrl+Y` | Undo / redo |
| `Ctrl+C` / `Ctrl+X` / `Ctrl+V` | Copy / cut / paste |
| `Ctrl+A` | Select all |
| `Shift+← ↑ ↓ →` | Extend selection |
| `Ctrl+←` / `Ctrl+→` (or `Alt+b/f`) | Jump word by word |
| `Alt+←` / `Alt+→` | Jump to next/previous punctuation/symbol (spaces skipped) |
| `Ctrl+P` | Ninja jump mode: press a character to jump to the nearest line whose last position char matches (footer turns red) |
| `Ctrl+/` (or `Ctrl+Shift+/`) | Search mode (`Ctrl+N`/`Ctrl+P` next/prev, Enter jump, Esc exit) — works for every common encoding of the key |
| `Ctrl+\` | Delete a whole word |
| `Ctrl+Backspace` (or `Alt+Backspace`) | Delete the word before the cursor |
| `Ctrl+U` / `Ctrl+K` | Delete to line start / end |
| `Enter` | Newline with auto-indent |
| `Tab` / `Shift+Tab` | Indent / unindent |
| `Home` / `End` | Line start / end |
| `Ctrl+Home` / `Ctrl+End` | Document start / end |
| `PageUp` / `PageDown` | Page scroll |
| `Ctrl+↑` / `Ctrl+↓` | Scroll without moving the cursor |
| `Ctrl+L` | Center view on cursor |
| `Esc` | Clear selection |

## Layout

```
tinyedit/
  make.sh          build script -> produces build/tinyedit
  build/           build output (created by make.sh)
  src/
    main.go        entry point (opens argv[1] if given)
    editor.go      Editor state, undo/redo, actions, keymap, main loop
    buffer.go      line-based rune buffer (insert / delete / range text)
    keys.go        stdin reader goroutine + escape-sequence key parser
    syntax.go      highlighting lexer + per-language keyword tables
    screen.go      raw mode, rendering, status bar, highlight states
    term_linux.go  termios / window-size syscalls (Linux)
    term_darwin.go termios / window-size syscalls (macOS)
    editor_test.go unit tests for buffer, undo, word ops, indent
  smoke.py         end-to-end pty smoke test (needs python3)
```

## Adding a shortcut (the whole point of the design)

Everything hangs off one table, `keymap` in `editor.go`. Adding a binding is
two steps:

1. Add an action method:

```go
func (e *Editor) actDupLine(_ key) {
    line := e.buf.Line(e.cur.row)
    e.insertRunes(append(append([]rune{}, line...), '\n'))
}
```

2. Add one line to `keymap`:

```go
{kRune('d', modCtrl), "duplicate-line", (*Editor).actDupLine},
```

That's it. The dispatcher in `handle` matches keys by rune/modifier/special
code, so the same table also handles plain keys (a binding with `mods == 0`
shadows the default "insert this character" behavior), arrows, and modified
variants. `keys.go` translates terminal escape sequences into the small
`key` struct, so terminals that disagree on encodings are normalized before
they reach your binding.

To restyle the editor, edit the `col*` constants at the top of `syntax.go`.
To add a language, add a `*lang` table and one `case` in `syntaxFor`.

## Known limitations

- No mouse support.
- Tabs are expanded to 8-column tab stops (matching terminal defaults), so the
  cursor stays aligned on tabbed lines. Wide/CJK characters render, but the
  cursor column can be off for them.
- No find/replace yet — the keymap table makes it easy to add.
- Horizontal scroll uses rune indexes rather than display columns.
