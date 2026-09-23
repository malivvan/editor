package editor

import (
	"strings"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/vt"
)

// testScreenMu serialises the tests that drive a mock terminal screen. The
// terminfo screen touches tcell's global state, so only one of them may be
// alive at a time, and the tests using it run in parallel with the rest.
var testScreenMu sync.Mutex

// newTestScreen returns a tcell screen backed by a mock terminal of the given
// size. Everything written to it can be read back with Get, which makes it a
// stand-in for a real terminal in tests.
func newTestScreen(t *testing.T, width, height int) tcell.Screen {
	t.Helper()

	testScreenMu.Lock()

	mt := vt.NewMockTerm(vt.MockOptSize{X: vt.Col(width), Y: vt.Row(height)})
	screen, err := tcell.NewTerminfoScreenFromTty(mt)
	if err != nil {
		testScreenMu.Unlock()
		t.Fatalf("failed to create mock terminal screen: %v", err)
	}
	if err := screen.Init(); err != nil {
		testScreenMu.Unlock()
		t.Fatalf("failed to initialize mock terminal screen: %v", err)
	}
	t.Cleanup(func() {
		screen.Fini()
		testScreenMu.Unlock()
	})

	return screen
}

// screenText returns the contents of width cells starting at x, y.
func screenText(screen tcell.Screen, x, y, width int) string {
	var b strings.Builder
	for i := 0; i < width; i++ {
		str, _, _ := screen.Get(x+i, y)
		if str == "" {
			str = " "
		}
		b.WriteString(str)
	}

	return b.String()
}

// cellStyle returns the style of the cell at x, y.
func cellStyle(screen tcell.Screen, x, y int) tcell.Style {
	_, style, _ := screen.Get(x, y)

	return style
}
