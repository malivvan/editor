# `editor` - a text editor widget for Go

[![Go Version](https://img.shields.io/github/go-mod/go-version/malivvan/editor)](https://github.com/malivvan/editor)
[![License](https://img.shields.io/github/license/malivvan/editor)](LICENSE)
[![CI](https://github.com/malivvan/editor/actions/workflows/ci.yml/badge.svg)](https://github.com/malivvan/editor/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/malivvan/editor.svg)](https://pkg.go.dev/github.com/malivvan/editor)

> A syntax-highlighting, multi-cursor text editor widget for Go — hosted by
> tcell, or by any surface you draw yourself.

`editor` descends from the femto/micro line of Go text editors and keeps their
core: a line-array buffer, rune-based cursors, micro-compatible syntax grammars
and colorschemes, and an event-based undo history. It has since been detached
from its host widget toolkit into a standalone module: the only UI dependency is
[`github.com/gdamore/tcell/v3`](https://github.com/gdamore/tcell), a `View`
draws onto a five-method `Screen` interface that a tcell screen satisfies as it
is, and everything the editor needs from its host is a rectangle, a draw call
and an event.

---

## Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
  - [A View on a tcell screen](#a-view-on-a-tcell-screen)
  - [Syntax highlighting and colorschemes](#syntax-highlighting-and-colorschemes)
  - [Autocomplete](#autocomplete)
- [Installation](#installation)
- [Core Concepts](#core-concepts)
  - [Buffer](#buffer)
  - [Cursor and Loc](#cursor-and-loc)
  - [View](#view)
  - [Screen](#screen)
  - [Events, key bindings and actions](#events-key-bindings-and-actions)
  - [Mouse](#mouse)
  - [Input capture](#input-capture)
  - [Colorschemes](#colorschemes)
  - [Runtime files](#runtime-files)
  - [Settings](#settings)
  - [Autocomplete](#autocomplete)
  - [Undo and redo](#undo-and-redo)
  - [Search and replace](#search-and-replace)
  - [Clipboard](#clipboard)
- [Default key bindings](#default-key-bindings)
- [Demo](#demo)
- [Development](#development)
- [Documentation](#documentation)
- [License](#license)
- [Acknowledgments](#acknowledgments)

---

## Features

| Area              | Capabilities                                                                                                                   |
|-------------------|--------------------------------------------------------------------------------------------------------------------------------|
| **Buffer**        | Line-array text storage, file path, per-buffer settings, dirty tracking, large-file mode                                        |
| **Editing**       | Insert or overwrite mode, tabs and indentation, indent/outdent, duplicate and move lines, smart paste, auto-indent              |
| **Cursors**       | Multiple independent cursors, each with its own selection; spawn, remove and skip cursors                                       |
| **Undo / redo**   | Exact undo and redo from recorded text deltas, with time-based grouping and a bounded history                                   |
| **Selection**     | Character, word, line, paragraph, page and "to start/end" selections, with multi-cursor highlighting                            |
| **Clipboard**     | Copy, cut, paste, cut-line, with an in-process fallback when the platform has no clipboard                                      |
| **Highlighting**  | micro-compatible YAML syntax grammars, resolved per file type, with regex rules per syntax group                                 |
| **Colorschemes**  | micro `color-link` schemes, parsed into a `tcell.Style` per syntax group                                                        |
| **Autocomplete**  | Provider interface, popup list with details, inline ghost text                                                                  |
| **Search**        | Literal or regular expression search and replace, with optional case sensitivity                                                |
| **Rendering**     | Line numbers, cursor-line highlight, color column, whitespace markers, soft wrap, scrollbar, wide runes and combining characters |
| **Hosting**       | A five-method `Screen` interface, one `HandleEvent` entry point, mouse-action classification, input capture, focus handling      |
| **Embedded data** | Syntax grammars and colorschemes shipped in the `runtime` subpackage                                                            |
| **Demo**          | A complete tcell host written without any framework                                                                             |

---

## Quick Start

### A View on a tcell screen

Nothing stands between the editor and tcell: open a screen, give the view a
rectangle, and forward events.

```go
package main

import (
	"os"

	"github.com/gdamore/tcell/v3"
	"github.com/malivvan/editor"
	"github.com/malivvan/editor/runtime"
)

func main() {
	src, _ := os.ReadFile("main.go")

	buf := editor.NewBufferFromString(string(src), "main.go")
	view := editor.NewView(buf)
	view.SetRuntimeFiles(runtime.Files) // syntax highlighting + colorschemes

	screen, err := tcell.NewScreen()
	if err != nil {
		panic(err)
	}
	if err := screen.Init(); err != nil {
		panic(err)
	}
	defer screen.Fini()
	screen.EnableMouse()

	// Ctrl-Q is the host's business, not the editor's.
	quit := false
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlQ {
			quit = true
			return nil
		}
		return event
	})

	layout := func() {
		width, height := screen.Size()
		view.SetRect(0, 0, width, height)
	}
	layout()

	for event := range screen.EventQ() {
		switch event.(type) {
		case *tcell.EventKey, *tcell.EventMouse:
			view.HandleEvent(event)
		case *tcell.EventResize:
			screen.Sync()
			layout()
		}

		view.Draw(screen)
		screen.Show()

		if quit {
			return
		}
	}
}
```

Calling `SetRect` is optional: a view that was never given a rectangle expands
to fill the screen on the first `Draw`.

### Syntax highlighting and colorschemes

The grammar is picked from the buffer's path. For a scratch buffer that has no
path, name the file type explicitly:

```go
buf := editor.NewBufferFromString(src, "") // no path to infer a grammar from
buf.Settings["filetype"] = "go"

view := editor.NewView(buf)
view.SetRuntimeFiles(runtime.Files)

if file := runtime.Files.FindFile(editor.RTColorscheme, "monokai"); file != nil {
	if data, err := file.Data(); err == nil {
		view.SetColorscheme(editor.ParseColorscheme(string(data)))
	}
}
```

### Autocomplete

A provider turns the editing context into completion items; the view renders
them as a popup, as inline ghost text, or both.

```go
view.SetAutocompleteProvider(func(ctx editor.CompletionContext) []editor.CompletionItem {
	if ctx.Prefix == "" {
		return nil
	}
	return []editor.CompletionItem{
		{Label: "Println", InsertText: "Println", InlineText: `Println("hello")`, Detail: "fmt · func"},
		{Label: "Printf", InsertText: "Printf", Detail: "fmt · func"},
	}
})

if view.TriggerAutocomplete() {
	// The popup is open; the user can keep typing, pick with the mouse, or
	// press Enter/Escape.
}
```

---

## Installation

```sh
go get github.com/malivvan/editor
```

The package requires Go 1.27 or later, and pulls in
[`github.com/gdamore/tcell/v3`](https://github.com/gdamore/tcell),
[`github.com/atotto/clipboard`](https://github.com/atotto/clipboard),
[`github.com/mattn/go-runewidth`](https://github.com/mattn/go-runewidth) and
[`github.com/sergi/go-diff`](https://github.com/sergi/go-diff).

---

## Core Concepts

### Buffer

`Buffer` holds the text of one file (or of one scratch pad) as a line array,
together with its `Path`, its `Settings` map, its syntax highlighter and its
undo history.

```go
buf := editor.NewBufferFromString("hello\n", "greeting.txt") // from a string
buf := editor.NewBuffer(reader, size, "main.go", cursorPos)  // from a reader
```

Reading the text back is line based (`buf.Line(n)`, `buf.Lines(0, buf.LinesNum())`)
or whole-buffer (`buf.String()`, `buf.Len()`), and `buf.Modified()` reports
whether anything was edited since the buffer was opened. Text over
`editor.LargeFileThreshold` (50 000 bytes) switches to a cheaper dirty-tracking
mode automatically.

### Cursor and Loc

A `Loc` is a rune position — `Loc{X: 2, Y: 1}` is the third rune of the second
line — so ASCII, combining marks and emoji all count as a single step. Byte
offsets and visual columns are derived from it:

```go
buf := editor.NewBufferFromString("gööse\n", "")
buf.Line(0)                              // "gööse"
editor.ByteOffset(editor.Loc{X: 3}, buf) // byte offset of the fourth rune
buf.Cursor.GetVisualX()                  // screen column, tabs expanded
```

`Cursor` moves (`Up`, `DownN`, `WordRight`, `MoveTo`), selects (`SelectWord`,
`SelectLine`, `SetSelectionStart`/`SetSelectionEnd`) and reports
(`HasSelection`, `GetSelection`). The editor keeps a slice of cursors on the
buffer; that is what makes multiple cursors work — each cursor replays an action
and gets its own selection drawn.

### View

`View` is the window onto a buffer, and the only type a host deals with:

| Method                                                  | Purpose                                           |
| ------------------------------------------------------- | ------------------------------------------------- |
| `NewView(buf)`                                          | Create a view; it starts focused.                 |
| `SetRect(x, y, w, h)` / `Rect()`                        | Place the view on the screen.                     |
| `Draw(screen)`                                          | Render the buffer, the popup and the cursor.      |
| `HandleEvent(event)`                                    | Feed a `tcell.Event` (key or mouse).              |
| `Focus()` / `Blur()` / `SetFocus(bool)` / `HasFocus()`  | Control whether the terminal cursor is placed.    |
| `SetInputCapture(fn)`                                   | Intercept key events before the editor sees them. |
| `SetKeybindings(b)` / `GetKeybindings()`                | Replace the key bindings.                         |
| `SetColorscheme(c)` / `SetRuntimeFiles(rf)`             | Style the text and resolve grammars.              |
| `SetAutocompleteProvider(p)` / `TriggerAutocomplete()`  | Configure and open completion.                    |
| `OpenBuffer(buf)`                                       | Swap the buffer under the view.                   |
| `ExecuteActions(actions)`                               | Run actions programmatically.                     |
| `ScrollUp(n)` / `ScrollDown(n)` / `Relocate()`          | Drive the viewport.                               |

The view also carries the viewport state a host may want to display: `Topline`
(the first visible line), `Readonly`, and `Cursor`.

### Screen

The rendering surface is deliberately tiny, which is what keeps the editor
portable:

```go
type Screen interface {
	SetContent(x, y int, ch rune, comb []rune, style tcell.Style)
	Get(x, y int) (str string, style tcell.Style, width int)
	Size() (width, height int)
	ShowCursor(x, y int)
	HideCursor()
}
```

A `tcell.Screen` — or the `tcell.Screen` interface value itself — satisfies this
without a wrapper, so `view.Draw(screen)` is all a tcell host needs. Anything
else that can write characters to a grid, read them back and place a cursor can
implement it too; the tests do exactly that to prove the point.

### Events, key bindings and actions

`HandleEvent` is the single entry point. Key events are passed to the input
capture, then to the autocomplete popup, and finally to the key bindings:
a `KeyBindings` map from `KeyDesc` to a list of actions.

```go
// A KeyDesc is a key code, optionally with modifiers and a rune; the action
// names are the keys of editor.BindingActionsMapping.
bindings := editor.NewKeyBindings(map[string]string{
	"AltUp":    "MoveLinesUp,MoveLinesUp", // run two actions in sequence
	"CtrlHome": "CursorStart",
	"Alt-c":    "UnbindKey", // drop a default binding
})
view.SetKeybindings(bindings)
```

Actions are ordinary methods on `View` (`(*View).CursorUp`, `(*View).Undo`,
`(*View).SelectAll`, `(*View).ToggleRuler`, …), so anything a binding can do a
host can also do directly.

### Mouse

Raw mouse events are classified by the view itself — position changes become
`MouseMove`, button changes become `MouseLeftDown`/`MouseLeftUp` and friends,
and wheel events become `MouseScroll*` — and each action is applied in turn:

```go
view.HandleEvent(event)                   // classify and apply
view.HandleMouse(event)                   // same thing, explicit
view.HandleMouseAction(MouseLeftDown, event) // when the host already knows
```

Clicking moves the cursor, dragging selects, double and triple clicks select a
word or a line, the wheel scrolls, and clicking inside the autocomplete popup
picks an item. Events outside the view's rectangle are ignored.

### Input capture

`SetInputCapture` installs a function that sees every key event first and may
swallow it (return `nil`), forward it, or replace it — the hook for shortcuts
that must never reach the buffer:

```go
view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyCtrlS:
		save(buf)
		return nil
	case tcell.KeyCtrlQ:
		quit = true
		return nil
	}
	return event
})
```

### Colorschemes

A `Colorscheme` maps a syntax group (`"keyword"`, `"string"`, `"cursor-line"`,
…) to a `tcell.Style`. `ParseColorscheme` reads micro's format, so existing
schemes work unchanged:

```
color-link default "#F8F8F2,#282828"
color-link keyword "#F92672"
color-link string  "#E6DB74"
```

`Colorscheme.GetColor` walks dotted group names (`"constant.bool"` falls back to
`"constant"`), and `StringToColor` accepts the colour names and hex values the
schemes use.

### Runtime files

`RuntimeFiles` is a registry of loadable resources grouped by kind
(`editor.RTColorscheme`, `editor.RTSyntax`). The `runtime` subpackage embeds a
set of grammars and colorschemes and exposes them pre-built:

```go
view.SetRuntimeFiles(runtime.Files)

for _, file := range runtime.Files.ListRuntimeFiles(editor.RTColorscheme) {
	fmt.Println(file.Name()) // "monokai", "solarized", …
}
```

Add your own with `AddFile` or `AddFilesFromDirectory`.

### Settings

Settings live on the buffer, with the defaults from `DefaultLocalSettings`:
`tabsize`, `tabstospaces`, `softwrap`, `scrollmargin`, `scrollspeed`,
`cursorline`, `ruler`, `colorcolumn`, `showwhitespace`, `indentchar`,
`autoindent`, `keepautoindent`, `smartpaste`, `matchbrace`, `matchbraceleft`,
`tabmovement`, `syntax`, `filetype`, `hidecursoronblur`, `maxundohistory`,
`fastdirty`, `fileformat`.

```go
buf.Settings["ruler"] = false        // no line numbers
buf.Settings["softwrap"] = true
buf.Settings["tabsize"] = float64(2) // numbers are float64
buf.Settings["filetype"] = "go"      // pick a grammar explicitly
```

A missing or wrongly typed value is treated as unset (with a warning in the log)
rather than panicking.

### Autocomplete

Completion is a two-step affair: an `AutocompleteProvider` turns the editing
context into candidates, and the view renders them.

```go
type CompletionContext struct {
	Buffer                     *Buffer
	View                       *View
	Cursor, WordStart, WordEnd Loc
	Prefix, Word, Line         string
}

type CompletionItem struct {
	Label, InsertText, InlineText, Detail string
	ReplaceStart, ReplaceEnd              *Loc
}
```

`InlineText` is what makes ghost text possible — the rest of the word is drawn
faded after the cursor. `DefaultAutocompleteProvider` suggests distinct words
already present in the buffer; pass `nil` to `SetAutocompleteProvider` to switch
completion off. The popup opens while typing, and `TriggerAutocomplete` opens it
programmatically.

### Undo and redo

Every edit is recorded as a `TextEvent` with the deltas needed to reverse it, so
`Undo` and `Redo` restore the exact previous state. A single `Undo` keeps
unwinding edits that were recorded within 500 ms of each other, so a burst of
typing is undone in one step, and the history is trimmed to the buffer's
`maxundohistory` setting (10 000 by default). The stack itself is available as
`editor.Stack` if a host wants to persist it.

### Search and replace

Search and replace work on literal text or regular expressions:

```go
found, err := view.Search("func", false, true, true)     // down, case sensitive
count, err := view.ReplaceAll("foo", false, true, "bar") // literal, case sensitive
```

`Buffer.FindNext` returns the match as a `[2]Loc` range, and
`Buffer.ReplaceRegex` does the same with capture groups.

### Clipboard

Copy, cut and paste use `github.com/atotto/clipboard`. When the platform has no
clipboard at all — or a Unix session has neither `DISPLAY` nor
`WAYLAND_DISPLAY` — the editor keeps an in-process clipboard instead, so the
bindings still work in a bare SSH session.

---

## Default key bindings

```
Up:             CursorUp
Down:           CursorDown
Right:          CursorRight
Left:           CursorLeft
ShiftUp:        SelectUp
ShiftDown:      SelectDown
ShiftLeft:      SelectLeft
ShiftRight:     SelectRight
AltLeft:        WordLeft
AltRight:       WordRight
AltUp:          MoveLinesUp
AltDown:        MoveLinesDown
AltShiftRight:  SelectWordRight
AltShiftLeft:   SelectWordLeft
CtrlLeft:       StartOfLine
CtrlRight:      EndOfLine
CtrlShiftLeft:  SelectToStartOfLine
ShiftHome:      SelectToStartOfLine
CtrlShiftRight: SelectToEndOfLine
ShiftEnd:       SelectToEndOfLine
CtrlUp:         CursorStart
CtrlDown:       CursorEnd
CtrlShiftUp:    SelectToStart
CtrlShiftDown:  SelectToEnd
Alt-{:          ParagraphPrevious
Alt-}:          ParagraphNext
Enter:          InsertNewline
CtrlH:          Backspace
Backspace:      Backspace
OldBackspace:   Backspace
Alt-CtrlH:      DeleteWordLeft
Alt-Backspace:  DeleteWordLeft
Tab:            IndentSelection,InsertTab
Backtab:        OutdentSelection,OutdentLine
CtrlZ:          Undo
CtrlY:          Redo
CtrlC:          Copy
CtrlX:          Cut
CtrlK:          CutLine
CtrlD:          DuplicateLine
CtrlV:          Paste
CtrlA:          SelectAll
Home:           StartOfLine
End:            EndOfLine
CtrlHome:       CursorStart
CtrlEnd:        CursorEnd
PageUp:         CursorPageUp
PageDown:       CursorPageDown
CtrlR:          ToggleRuler
Delete:         Delete
Insert:         ToggleOverwriteMode
Alt-f:          WordRight
Alt-b:          WordLeft
Alt-a:          StartOfLine
Alt-e:          EndOfLine
Esc:            Escape
Alt-n:          SpawnMultiCursor
Alt-m:          SpawnMultiCursorSelect
Alt-p:          RemoveMultiCursor
Alt-c:          RemoveAllMultiCursors
Alt-x:          SkipMultiCursor
```

---

## Demo

The `demo` directory is a complete host built on tcell alone: it opens the
screen, paints its own frame and status bar with plain tcell calls, and hands
every key and mouse event to a single `editor.View`.

```sh
go run ./demo            # edit the built-in sample buffer
go run ./demo main.go    # edit a real file
```

| Keys   | Action                                                 |
| ------ | ------------------------------------------------------ |
| Ctrl-S | Save the buffer (no-op with a message for the sample).  |
| Ctrl-Q | Quit.                                                   |
| F1     | Toggle line numbers.                                    |
| F2     | Cycle through the embedded colorschemes.                |

Typing a word opens the autocomplete popup; clicking moves the cursor, and
dragging selects text.

---

## Development

```sh
make test          # go test ./...
make test-race     # go test -race ./...
make cover         # coverage report
make lint          # go vet + gofmt check
make demo          # compile the demo
```

The CI workflow (`.github/workflows/ci.yml`) runs build, vet, formatting and the
full test suite on every push, with the race detector and coverage on Linux.

---

## Documentation

- Package documentation: <https://pkg.go.dev/github.com/malivvan/editor>
- Syntax grammars and colorschemes: <https://github.com/micro-editor/micro/tree/master/runtime>
- tcell: <https://github.com/gdamore/tcell>

---

## License

MIT — see [LICENSE](LICENSE), and [LICENSE-THIRD-PARTY](LICENSE-THIRD-PARTY) for
the code and data that came from other projects.

---

## Acknowledgments

`editor` is a re-assembly of work done by others, and the code here was
previously shipped as the `editor` package of
[github.com/malivvan/cui](https://github.com/malivvan/cui). Credit belongs to:

- **[github.com/pgavlin/femto](https://github.com/pgavlin/femto)** — Pat Gavlin.
  The original femto text editor: the buffer, view, cursor and key-binding
  design this package still follows, and the origin of much of the editing
  logic.
- **[github.com/sedwards2009/femto](https://github.com/sedwards2009/femto)** —
  Simon Edwards. The maintained fork that this package's editor code was
  actually vendored from (commit `dbf9395`), and where the original editor was
  kept current and carried onto more recent Go terminal libraries.
- **[github.com/micro-editor/micro](https://github.com/micro-editor/micro)** —
  Zachary Yedidia and contributors. The syntax grammar format, the colorschemes
  and much of the runtime data embedded in the `runtime` subpackage come from
  micro's runtime files.

The package is built on libraries that deserve the same credit:
[github.com/gdamore/tcell](https://github.com/gdamore/tcell) (the terminal
library it renders through) and
[github.com/atotto/clipboard](https://github.com/atotto/clipboard),
[github.com/mattn/go-runewidth](https://github.com/mattn/go-runewidth),
[github.com/sergi/go-diff](https://github.com/sergi/go-diff) (the libraries it
depends on).
