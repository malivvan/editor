package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

func autocompleteTestColorscheme() Colorscheme {
	return Colorscheme{
		"default":     tcell.StyleDefault.Foreground(color.White).Background(color.Black),
		"selection":   tcell.StyleDefault.Foreground(color.Black).Background(color.White),
		"cursor-line": tcell.StyleDefault.Foreground(color.DarkCyan).Background(color.Black),
		"suggestion":  tcell.StyleDefault.Foreground(color.Gray).Background(color.Maroon).Dim(true),
	}
}

func TestAutocompleteDrawsInlineSuggestionWithTransparentBackground(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("hep\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 20, 4)
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return []CompletionItem{{Label: "hello", InsertText: "hello"}}
	})
	view.Cursor.GotoLoc(Loc{X: 2, Y: 0})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}

	screen := newTestScreen(t, 20, 4)
	view.Draw(screen)

	if !view.cursorVisible {
		t.Fatal("expected cursor to be visible for inline suggestion rendering")
	}

	ghost := screenText(screen, view.cursorScreenX, view.cursorScreenY, 3)
	if !strings.HasPrefix(ghost, "llo") {
		t.Fatalf("expected inline suggestion to start with ghost text, got %q", ghost)
	}

	// The inline suggestion should use a blended (faded) foreground color
	// that differs from both the regular text color and the background.
	_, style, _ := screen.Get(view.cursorScreenX, view.cursorScreenY)
	ghostFg := style.GetForeground()
	defFg := autocompleteTestColorscheme()["default"].GetForeground()
	defBg := autocompleteTestColorscheme()["default"].GetBackground()
	if ghostFg == defFg {
		t.Fatalf("expected ghost text foreground to be faded/blended, but it equals the default foreground %v", ghostFg)
	}
	if ghostFg == defBg {
		t.Fatalf("expected ghost text foreground to differ from background, got %v", ghostFg)
	}

	// With a single completion item and visible inline ghost text, the popup
	// should be suppressed to avoid visually disrupting the line below.
	if view.autocomplete.rect.width != 0 || view.autocomplete.rect.height != 0 {
		t.Fatal("expected single-item popup to be hidden when inline ghost text is visible")
	}
}

func TestAutocompleteInlineTextOverrideDoesNotChangeAcceptedInsertText(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("fmt.Pr\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 30, 6)
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return []CompletionItem{{
			Label:      "Println",
			InsertText: "Println",
			InlineText: "intln()",
			Detail:     "fmt function",
		}}
	})
	view.Cursor.GotoLoc(Loc{X: 6, Y: 0})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}

	screen := newTestScreen(t, 30, 6)
	view.Draw(screen)

	ghost := screenText(screen, view.cursorScreenX, view.cursorScreenY, 7)
	if !strings.HasPrefix(ghost, "intln()") {
		t.Fatalf("expected custom inline text to be rendered, got %q", ghost)
	}

	view.HandleEvent(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if got := view.Buf.Line(0); got != "fmt.Println" {
		t.Fatalf("expected accepting autocomplete to use InsertText, got %q", got)
	}
}

func TestAutocompleteTriggerAndAccept(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("hel\nhello\nhelp\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 40, 10)
	view.Cursor.GotoLoc(Loc{X: 3, Y: 0})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}
	if !view.autocomplete.active {
		t.Fatal("autocomplete should be active")
	}

	screen := newTestScreen(t, 40, 10)
	view.Draw(screen)

	popup := view.autocomplete.rect
	if popup.width == 0 || popup.height == 0 {
		t.Fatal("expected autocomplete popup rect to be set")
	}
	if got := screenText(screen, popup.x, popup.y, popup.width); !strings.Contains(got, "help") {
		t.Fatalf("expected first popup row to contain suggestion, got %q", got)
	}
	selected := cellStyle(screen, popup.x, popup.y)
	if bg := selected.GetBackground(); bg != color.White {
		t.Fatalf("expected selected popup row to use selection background, got %v", bg)
	}
	if fg := selected.GetForeground(); fg != color.Black {
		t.Fatalf("expected selected popup row to use selection foreground, got %v", fg)
	}

	view.HandleEvent(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if got := view.Buf.Line(0); got != "help" {
		t.Fatalf("expected accepted completion to replace prefix, got %q", got)
	}
	if view.autocomplete.active {
		t.Fatal("autocomplete should close after accepting a suggestion")
	}

	view.Buf.Undo()
	if got := view.Buf.Line(0); got != "hel" {
		t.Fatalf("expected undo to restore original prefix, got %q", got)
	}
}

func TestAutocompleteRefreshesOnTypingAndCancels(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("he\nhello\nhelp\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 40, 10)
	view.Cursor.GotoLoc(Loc{X: 2, Y: 0})

	view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "l", tcell.ModNone))
	if got := view.Buf.Line(0); got != "hel" {
		t.Fatalf("expected typed character to be inserted, got %q", got)
	}
	if !view.autocomplete.active {
		t.Fatal("expected autocomplete to refresh after typing")
	}
	if got := view.autocomplete.context.Prefix; got != "hel" {
		t.Fatalf("expected autocomplete prefix to refresh, got %q", got)
	}

	view.HandleEvent(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
	if view.autocomplete.active {
		t.Fatal("expected escape to close autocomplete")
	}
}

func TestAutocompleteDoesNotOpenOnCursorMovementAlone(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("help\nhelm\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 40, 10)
	view.Cursor.GotoLoc(Loc{X: 1, Y: 0})

	view.HandleEvent(tcell.NewEventKey(tcell.KeyRight, "", tcell.ModNone))
	if view.autocomplete.active {
		t.Fatal("expected plain cursor movement not to auto-open autocomplete")
	}
	if view.Cursor.Loc != (Loc{X: 2, Y: 0}) {
		t.Fatalf("expected cursor to move right, got %+v", view.Cursor.Loc)
	}
}

func TestAutocompleteUpdatesAndClosesOnCursorMovementWhenOpen(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("help\nhelm\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 40, 10)
	view.Cursor.GotoLoc(Loc{X: 2, Y: 0})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open explicitly")
	}
	if got := view.autocomplete.context.Prefix; got != "he" {
		t.Fatalf("expected initial autocomplete prefix, got %q", got)
	}

	view.HandleEvent(tcell.NewEventKey(tcell.KeyRight, "", tcell.ModNone))
	if !view.autocomplete.active {
		t.Fatal("expected autocomplete to stay open while cursor moves within the word")
	}
	if got := view.autocomplete.context.Prefix; got != "hel" {
		t.Fatalf("expected prefix to refresh after cursor movement, got %q", got)
	}

	view.HandleEvent(tcell.NewEventKey(tcell.KeyHome, "", tcell.ModNone))
	if view.autocomplete.active {
		t.Fatal("expected autocomplete to close when cursor movement leaves the completion prefix")
	}
}

func TestAutocompleteOpensOnDeleteAndBackspace(t *testing.T) {
	t.Parallel()

	t.Run("backspace", func(t *testing.T) {
		view := NewView(NewBufferFromString("abc\nabcd\n", ""))
		view.SetColorscheme(autocompleteTestColorscheme())
		view.SetRect(0, 0, 40, 10)
		view.Cursor.GotoLoc(Loc{X: 3, Y: 0})

		view.HandleEvent(tcell.NewEventKey(tcell.KeyBackspace2, "", tcell.ModNone))

		if got := view.Buf.Line(0); got != "ab" {
			t.Fatalf("expected backspace to delete the previous character, got %q", got)
		}
		if view.Cursor.Loc != (Loc{X: 2, Y: 0}) {
			t.Fatalf("expected cursor to move left after backspace, got %+v", view.Cursor.Loc)
		}
		if !view.autocomplete.active {
			t.Fatal("expected backspace to open autocomplete for the new prefix")
		}
		if got := view.autocomplete.context.Prefix; got != "ab" {
			t.Fatalf("expected autocomplete prefix to refresh after backspace, got %q", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		view := NewView(NewBufferFromString("abcd\nabef\n", ""))
		view.SetColorscheme(autocompleteTestColorscheme())
		view.SetRect(0, 0, 40, 10)
		view.Cursor.GotoLoc(Loc{X: 2, Y: 0})

		view.HandleEvent(tcell.NewEventKey(tcell.KeyDelete, "", tcell.ModNone))

		if got := view.Buf.Line(0); got != "abd" {
			t.Fatalf("expected delete to remove the next character, got %q", got)
		}
		if view.Cursor.Loc != (Loc{X: 2, Y: 0}) {
			t.Fatalf("expected delete to keep the cursor in place, got %+v", view.Cursor.Loc)
		}
		if !view.autocomplete.active {
			t.Fatal("expected delete to open autocomplete for the current prefix")
		}
		if got := view.autocomplete.context.Prefix; got != "ab" {
			t.Fatalf("expected autocomplete prefix to refresh after delete, got %q", got)
		}
	})
}

func TestAutocompletePopupPlacedAboveCursorWhenNeeded(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("one\ntwo\nthree\nabc", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 20, 4)
	view.Cursor.GotoLoc(Loc{X: 3, Y: 3})
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return []CompletionItem{{Label: "alpha"}, {Label: "beta"}, {Label: "gamma"}}
	})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}

	screen := newTestScreen(t, 20, 4)
	view.Draw(screen)

	if !view.cursorVisible {
		t.Fatal("expected cursor to be visible for popup placement")
	}
	if view.autocomplete.rect.y >= view.cursorScreenY {
		t.Fatalf("expected popup to render above cursor when space below is insufficient, rect=%+v cursorY=%d", view.autocomplete.rect, view.cursorScreenY)
	}
}

func TestAutocompleteSelectedRowFallsBackToDefaultStyleWithoutSelectionColor(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("a", ""))
	view.SetColorscheme(Colorscheme{
		"default": tcell.StyleDefault.Foreground(color.Green).Background(color.Navy),
	})
	view.SetRect(0, 0, 20, 6)
	view.Cursor.GotoLoc(Loc{X: 1, Y: 0})
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return []CompletionItem{{Label: "alpha"}, {Label: "beta"}}
	})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}

	screen := newTestScreen(t, 20, 6)
	view.Draw(screen)

	popup := view.autocomplete.rect
	selected := cellStyle(screen, popup.x, popup.y)
	unselected := cellStyle(screen, popup.x, popup.y+1)

	if bg := selected.GetBackground(); bg != color.Navy {
		t.Fatalf("expected selected popup row to fall back to default background, got %v", bg)
	}
	if fg := selected.GetForeground(); fg != color.Green {
		t.Fatalf("expected selected popup row to fall back to default foreground, got %v", fg)
	}
	if attrs := selected.GetAttributes(); attrs&tcell.AttrReverse != 0 {
		t.Fatalf("expected selected popup row fallback style not to be reversed, got attrs=%v", attrs)
	}
	if attrs := unselected.GetAttributes(); attrs&tcell.AttrReverse == 0 {
		t.Fatalf("expected unselected popup rows to keep reversed popup styling, got attrs=%v", attrs)
	}
}

func TestAutocompleteMouseSelectionAcceptsItem(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("a", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 20, 6)
	view.Cursor.GotoLoc(Loc{X: 1, Y: 0})
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return []CompletionItem{{Label: "alpha"}, {Label: "beta"}}
	})

	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open")
	}

	screen := newTestScreen(t, 20, 6)
	view.Draw(screen)

	popup := view.autocomplete.rect
	if popup.width == 0 {
		t.Fatal("expected popup rect to be available after drawing")
	}
	x := popup.x + 1
	y := popup.y + 1

	if consumed := view.HandleMouseAction(MouseLeftDown, tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone)); !consumed {
		t.Fatal("expected mouse down inside popup to be consumed")
	}
	if view.autocomplete.selected != 1 {
		t.Fatalf("expected second completion item to be selected, got %d", view.autocomplete.selected)
	}

	if consumed := view.HandleMouseAction(MouseLeftUp, tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)); !consumed {
		t.Fatal("expected mouse up inside popup to be consumed")
	}
	if got := view.Buf.Line(0); got != "beta" {
		t.Fatalf("expected clicked completion to be applied, got %q", got)
	}
	if view.autocomplete.active {
		t.Fatal("expected autocomplete to close after mouse selection")
	}
}

func TestAutocompleteDoesNotOpenOnMouseCursorChangeAlone(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("zero\nhelp\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 20, 6)
	view.Buf.Settings["ruler"] = false
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return DefaultAutocompleteProvider(ctx)
	})

	if consumed := view.HandleMouseAction(MouseLeftDown, tcell.NewEventMouse(2, 1, tcell.Button1, tcell.ModNone)); !consumed {
		t.Fatal("expected mouse down inside the editor to be consumed")
	}
	if view.Cursor.Loc != (Loc{X: 2, Y: 1}) {
		t.Fatalf("expected mouse click to move cursor, got %+v", view.Cursor.Loc)
	}
	if view.autocomplete.active {
		t.Fatal("expected plain mouse cursor change not to auto-open autocomplete")
	}
}

func TestAutocompleteUpdatesOnMouseCursorChangeWhenOpen(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("zero\nhelp\n", ""))
	view.SetColorscheme(autocompleteTestColorscheme())
	view.SetRect(0, 0, 20, 6)
	view.Buf.Settings["ruler"] = false
	view.SetAutocompleteProvider(func(ctx CompletionContext) []CompletionItem {
		return DefaultAutocompleteProvider(ctx)
	})
	view.Cursor.GotoLoc(Loc{X: 2, Y: 1})
	if !view.TriggerAutocomplete() {
		t.Fatal("expected autocomplete to open explicitly")
	}

	if consumed := view.HandleMouseAction(MouseLeftDown, tcell.NewEventMouse(3, 1, tcell.Button1, tcell.ModNone)); !consumed {
		t.Fatal("expected mouse down inside the editor to be consumed")
	}
	if !view.autocomplete.active {
		t.Fatal("expected active autocomplete to stay open while mouse repositions within the word")
	}
	if got := view.autocomplete.context.Prefix; got != "hel" {
		t.Fatalf("expected autocomplete prefix to refresh after mouse click, got %q", got)
	}

	view.leftMouseDown = true
	if consumed := view.HandleMouseAction(MouseMove, tcell.NewEventMouse(4, 1, tcell.Button1, tcell.ModNone)); !consumed {
		t.Fatal("expected mouse move inside the editor to be consumed")
	}
	if view.autocomplete.active {
		t.Fatal("expected autocomplete to close while a mouse drag creates a selection")
	}
}
