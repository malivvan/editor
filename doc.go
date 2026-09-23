/*
Package editor is a text editor widget for Go terminal applications.

It provides the parts an editor is made of — a line-array Buffer, locating
Cursors, syntax-highlighted cells, an undo history, key bindings and an
autocomplete popup — behind a View that renders itself onto a Screen and
consumes tcell events. The editing core descends from the femto/micro line of
Go text editors; the README carries the full attribution.

# Hosting a View

A View is not tied to a toolkit. It draws onto the Screen interface, a
five-method subset of tcell.Screen that a tcell screen satisfies as it is, and
it takes tcell events:

	view := editor.NewView(editor.NewBufferFromString(src, "main.go"))
	view.SetRuntimeFiles(runtime.Files) // syntax highlighting, colorschemes
	view.SetRect(1, 1, width-2, height-2)
	view.Draw(screen)

	for event := range screen.EventQ() {
		switch event.(type) {
		case *tcell.EventKey, *tcell.EventMouse:
			view.HandleEvent(event)
		case *tcell.EventResize:
			screen.Sync()
			view.SetRect(1, 1, newWidth-2, newHeight-2)
		}
		view.Draw(screen)
		screen.Show()
	}

Everything the host has to decide is where the view goes (SetRect), when it is
drawn (Draw) and whether it holds the focus (Focus, Blur, SetFocus). The demo
directory contains a complete host built exactly this way.

# Buffers, cursors and views

Buffer stores the text — as a line array — together with the file's path, its
local settings and its syntax highlighter. NewBufferFromString creates one from
a string, NewBuffer from a reader plus an optional cursor position to restore.

Cursor holds a Loc, a rune-based {X, Y} position, its visual X offset and an
optional selection. The editor keeps a list of them alive at once, which is how
multiple cursors work: every cursor in the list replays an action and gets its
own selection highlighted.

View owns the viewport around a buffer: Topline and the horizontal scroll
offset, the colorscheme, the key bindings, the runtime files used for
highlighting, and the autocomplete popup. OpenBuffer swaps the buffer under a
view, ResetSelection and Relocate keep the cursor in sight.

# Sorting out coordinates

Loc is a rune position, not a byte offset: Loc{X: 2, Y: 1} is the third rune of
the second line, and ASCII, combining marks and emoji all count as one.
Visual columns — what a tab or a wide character occupies on screen — are
separate; GetVisualX, StringWidth and CellView are how the editor converts
between the two.

# Highlighting and colorschemes

The highlight subpackage implements micro-compatible syntax grammars: YAML
files with a file-type header and a rule per syntax group. The runtime
subpackage embeds a collection of those grammars along with the colorschemes
that style them, and a view picks up both through SetRuntimeFiles. The grammar
is chosen from the buffer's path, or from the buffer's "filetype" setting when
there is no path to go by.

A Colorscheme maps a syntax group name to a tcell.Style, and ParseColorscheme
reads micro's "color-link" statements, so existing schemes work unchanged.

# Autocomplete

Completion is a two-step affair. An AutocompleteProvider turns the editing
context — the word under the cursor, the buffer, the view — into
CompletionItems, and the view renders them, either in a popup or as inline
ghost text when CompletionItem.InlineText is set. DefaultAutocompleteProvider
suggests words already present in the buffer; SetAutocompleteProvider replaces
it and TriggerAutocomplete opens the popup programmatically.

# Settings

Every buffer carries its own map of settings with the defaults from
DefaultLocalSettings: tabsize, tabstospaces, softwrap, cursorline, ruler,
showwhitespace, colorcolumn, scrollbar, scrollmargin, autoindent, smartpaste,
syntax, filetype and friends. They are read and written through the map
(buf.Settings["ruler"] = false), and a missing or wrongly typed value is
treated as unset rather than fatal.

# Events, keys and the mouse

HandleEvent is the single entry point: it routes key events to the input
capture, then to the autocomplete popup, then to the key bindings, and it
classifies mouse events into MouseActions before handing them to the same
editing code. Hosts that already know which action a mouse event means can call
HandleMouseAction directly, and hosts that need to see keys first install a
function with SetInputCapture.

Key bindings map KeyDescs — a key code, optional modifiers and an optional rune
— to a list of actions, which are plain methods on View. NewKeyBindings parses
the familiar "Ctrl-S" style strings to build a custom set for
SetKeybindings.

# Undo, search and the clipboard

Edits go through an EventHandler that records them as text deltas, so Undo and
Redo are exact. A single Undo keeps unwinding edits that were recorded within
500ms of each other, so a burst of typing is undone in one step, and the history
is trimmed to the "maxundohistory" setting (10 000 by default).
Search and ReplaceAll work on either literal text or regular expressions, with
optional case sensitivity, through Buffer.FindNext.

Copy, cut and paste use github.com/atotto/clipboard, and fall back to an
in-process clipboard when the platform has none (or when there is no X11 or
Wayland session to talk to).

# Thread safety

A View and its Buffer belong to the goroutine that owns the event loop; they
are not safe for concurrent use. Rendering is no exception — Draw and
HandleEvent should both run on that goroutine, which is also where the screen
should be flushed.
*/
package editor
