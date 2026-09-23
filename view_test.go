package editor

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

func testColorscheme() Colorscheme {
	return Colorscheme{
		"default":     tcell.StyleDefault.Foreground(color.White).Background(color.Black),
		"selection":   tcell.StyleDefault.Foreground(color.Black).Background(color.White),
		"cursor-line": tcell.StyleDefault.Foreground(color.DarkCyan).Background(color.Black),
	}
}

// gridScreen is a Screen implementation that does not go through tcell at all:
// it proves that a View renders onto any surface, which is what the demo and
// any embedding application rely on.
type gridScreen struct {
	width, height    int
	cells            [][]rune
	cursorX, cursorY int
	cursorHidden     bool
}

func newGridScreen(width, height int) *gridScreen {
	s := &gridScreen{
		width:        width,
		height:       height,
		cells:        make([][]rune, height),
		cursorHidden: true,
	}
	for y := range s.cells {
		s.cells[y] = make([]rune, width)
		for x := range s.cells[y] {
			s.cells[y][x] = ' '
		}
	}

	return s
}

func (s *gridScreen) SetContent(x, y int, ch rune, comb []rune, style tcell.Style) {
	if x < 0 || x >= s.width || y < 0 || y >= s.height {
		return
	}
	if ch == 0 {
		ch = ' '
	}
	s.cells[y][x] = ch
}

func (s *gridScreen) Get(x, y int) (string, tcell.Style, int) {
	if x < 0 || x >= s.width || y < 0 || y >= s.height {
		return "", tcell.StyleDefault, 0
	}

	return string(s.cells[y][x]), tcell.StyleDefault, 1
}

func (s *gridScreen) Size() (int, int) {
	return s.width, s.height
}

func (s *gridScreen) ShowCursor(x, y int) {
	s.cursorX, s.cursorY, s.cursorHidden = x, y, false
}

func (s *gridScreen) HideCursor() {
	s.cursorHidden = true
}

func (s *gridScreen) line(y int) string {
	return string(s.cells[y])
}

// The demo hosts a View on a tcell screen, so the Screen interface must be
// satisfied by tcell.Screen itself.
var _ Screen = (tcell.Screen)(nil)

func TestViewFocusState(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("", ""))

	if !view.HasFocus() {
		t.Fatal("expected a new view to be focused")
	}

	view.Blur()
	if view.HasFocus() {
		t.Fatal("expected Blur to take the focus away")
	}

	view.Focus()
	if !view.HasFocus() {
		t.Fatal("expected Focus to give the focus back")
	}

	view.SetFocus(false)
	if view.HasFocus() {
		t.Fatal("expected SetFocus(false) to take the focus away")
	}
}

// TestViewDrawWithoutRectFillsSurface covers the documented fallback of Draw:
// a view that was never given a rectangle takes the whole surface.
func TestViewDrawWithoutRectFillsSurface(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("hello world\n", ""))
	view.SetColorscheme(testColorscheme())
	view.Buf.Settings["ruler"] = false

	screen := newTestScreen(t, 16, 4)
	view.Draw(screen)

	if got := screenText(screen, 0, 0, 11); got != "hello world" {
		t.Fatalf("expected the buffer text on the first row, got %q", got)
	}
	if x, y, width, height := view.Rect(); x != 0 || y != 0 || width != 16 || height != 4 {
		t.Fatalf("expected the view to fill the screen, got %d,%d %dx%d", x, y, width, height)
	}
	if got := screenText(screen, 11, 0, 5); got != strings.Repeat(" ", 5) {
		t.Fatalf("expected the rest of the row to be blank, got %q", got)
	}
}

// TestViewDrawOnCustomScreen draws onto a surface that is not a tcell screen.
func TestViewDrawOnCustomScreen(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("package main\n", ""))
	view.SetColorscheme(testColorscheme())
	view.Buf.Settings["ruler"] = false
	view.SetRect(0, 0, 16, 3)
	view.Cursor.GotoLoc(Loc{X: 12, Y: 0})

	screen := newGridScreen(16, 3)
	view.Draw(screen)

	if got, want := screen.line(0), "package main    "; got != want {
		t.Fatalf("expected %q on the first row, got %q", want, got)
	}
	if screen.cursorHidden {
		t.Fatal("expected the cursor to be placed at the end of the line")
	}
	if screen.cursorX != 12 || screen.cursorY != 0 {
		t.Fatalf("expected the cursor at 12,0, got %d,%d", screen.cursorX, screen.cursorY)
	}
}

func TestViewHandleEventInsertsRunes(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("", ""))
	view.SetColorscheme(testColorscheme())
	view.SetRect(0, 0, 20, 4)

	if consumed := view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "h", tcell.ModNone)); !consumed {
		t.Fatal("expected a rune event to be consumed")
	}
	view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "i", tcell.ModNone))
	view.HandleEvent(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "!", tcell.ModNone))

	if got := view.Buf.Line(0); got != "hi" {
		t.Fatalf("expected the first line to be %q, got %q", "hi", got)
	}
	if got := view.Buf.Line(1); got != "!" {
		t.Fatalf("expected the second line to be %q, got %q", "!", got)
	}
}

func TestViewInputCaptureSeesKeysFirst(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("", ""))
	view.SetColorscheme(testColorscheme())
	view.SetRect(0, 0, 20, 4)

	seen := []string{}
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		seen = append(seen, event.Str())
		switch event.Str() {
		case "x":
			// Swallow the key entirely.
			return nil
		case "a":
			// Rewrite it into a different key.
			return tcell.NewEventKey(tcell.KeyRune, "b", tcell.ModNone)
		}
		return event
	})

	view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone))
	view.HandleEvent(tcell.NewEventKey(tcell.KeyRune, "a", tcell.ModNone))

	if len(seen) != 2 {
		t.Fatalf("expected the capture to see both keys, got %v", seen)
	}
	if got := view.Buf.Line(0); got != "b" {
		t.Fatalf("expected the swallowed key to be replaced by the rewritten one, got %q", got)
	}
}

// TestViewHandleEventClassifiesMouseEvents covers the mouse half of
// HandleEvent: the view turns raw tcell events into move, button and wheel
// actions without help from the host.
func TestViewHandleEventClassifiesMouseEvents(t *testing.T) {
	t.Parallel()

	view := NewView(NewBufferFromString("one\ntwo\nthree\nfour\nfive\nsix\n", ""))
	view.SetColorscheme(testColorscheme())
	view.SetRect(0, 0, 20, 4)
	view.Buf.Settings["ruler"] = false

	// Pressing the primary button on the second row moves the cursor there.
	if !view.HandleEvent(tcell.NewEventMouse(2, 1, tcell.ButtonPrimary, tcell.ModNone)) {
		t.Fatal("expected a mouse press inside the view to be consumed")
	}
	if view.Cursor.X != 2 || view.Cursor.Y != 1 {
		t.Fatalf("expected the cursor to follow the click to 2,1, got %d,%d", view.Cursor.X, view.Cursor.Y)
	}

	// Releasing the button leaves the cursor alone.
	view.HandleEvent(tcell.NewEventMouse(2, 1, tcell.ButtonNone, tcell.ModNone))
	if view.Cursor.X != 2 || view.Cursor.Y != 1 {
		t.Fatalf("expected the cursor to stay at 2,1, got %d,%d", view.Cursor.X, view.Cursor.Y)
	}

	// A click outside the view's rectangle is ignored.
	view.HandleEvent(tcell.NewEventMouse(2, 10, tcell.ButtonPrimary, tcell.ModNone))
	if view.Cursor.Y != 1 {
		t.Fatalf("expected a click outside the view to be ignored, cursor moved to row %d", view.Cursor.Y)
	}

	// The wheel scrolls the viewport.
	view.HandleEvent(tcell.NewEventMouse(2, 1, tcell.WheelDown, tcell.ModNone))
	if view.Topline == 0 {
		t.Fatal("expected the wheel to scroll the viewport down")
	}
}
