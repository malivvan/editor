// Package main is a showcase of github.com/malivvan/editor hosted by tcell
// directly: no widget toolkit, no application framework, no layout engine.
//
// The program opens a tcell.Screen itself, paints its own frame and status bar
// with tcell calls, and hands every key and mouse event to a single
// editor.View. All the editor needs from its host is a rendering surface, and
// a tcell.Screen already satisfies it (see editor.Screen) — so the same View
// could just as well be drawn into an in-memory grid.
//
//	go run ./demo [file]
//
// Keys:
//
//	Ctrl-S   save the buffer (built-in sample buffers have no file name)
//	Ctrl-Q   quit
//	F1       toggle line numbers
//	F2       cycle through the embedded colorschemes
//
// Typing a word opens the autocomplete popup; the mouse selects text by
// dragging and moves the cursor on a click.
package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/gdamore/tcell/v3"
	"github.com/malivvan/editor"
	"github.com/malivvan/editor/runtime"
)

// demoText is the buffer shown when no file is given on the command line. It
// exists so the demo has something to highlight and to complete.
const demoText = `// editor: a syntax-highlighting text editor widget for Go.
//
// This is the buffer the demo starts with when no file argument is given.
// Type a few letters of an identifier that appears below (or of any word in
// this file) and the autocomplete popup opens on its own.
package main

import "fmt"

type Point struct {
	X, Y int
}

// Move returns the point translated by dx and dy.
func (p Point) Move(dx, dy int) Point {
	return Point{X: p.X + dx, Y: p.Y + dy}
}

func main() {
	origin := Point{X: 0, Y: 0}
	moved := origin.Move(3, 4)
	fmt.Println("origin", origin, "moved", moved)
}
`

var borderStyle = tcell.StyleDefault.Foreground(tcell.ColorTeal)

// demo ties a View to the tcell screen that displays it.
type demo struct {
	screen tcell.Screen
	view   *editor.View
	buf    *editor.Buffer

	// The embedded colorschemes, by name, and the index of the active one.
	schemes     []string
	schemeIndex int

	// message is shown in the status bar until the next event arrives.
	message string

	quit bool
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	d := new(demo)

	path, sample := "", true
	if len(os.Args) > 1 {
		path, sample = os.Args[1], false
	}

	content := demoText
	if !sample {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content = string(data)
	}

	d.buf = editor.NewBufferFromString(content, path)
	if sample {
		// There is no file name to infer the syntax from, so pick the
		// grammar by hand.
		d.buf.Settings["filetype"] = "go"
	}

	d.view = editor.NewView(d.buf)
	d.view.SetRuntimeFiles(runtime.Files)
	d.schemes = d.schemeNames()
	d.applyScheme("monokai")

	// The host owns the shortcuts it cares about. Because the capture runs
	// before the editor's own handling, returning nil swallows the key —
	// which is exactly what is wanted for a shortcut that must never be
	// inserted into the buffer.
	d.view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlS:
			d.save()
			return nil
		case tcell.KeyCtrlQ:
			d.quit = true
			return nil
		case tcell.KeyF1:
			d.toggleLineNumbers()
			return nil
		case tcell.KeyF2:
			d.nextScheme()
			return nil
		}
		return event
	})

	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()

	// Mouse support is the host's decision: enable it here, and the editor
	// receives the events that follow.
	screen.EnableMouse()
	d.screen = screen

	d.layout()
	d.draw()

	for event := range screen.EventQ() {
		d.message = ""

		switch event.(type) {
		case *tcell.EventKey, *tcell.EventMouse:
			// One call per event: the editor tells keys from mouse events
			// and classifies the latter into actions itself.
			d.view.HandleEvent(event)
			if d.quit {
				return nil
			}
		case *tcell.EventResize:
			screen.Sync()
			d.layout()
		}

		d.draw()
	}

	return nil
}

// layout reserves the frame, the status bar and the bottom border, and gives
// the rest of the screen to the editor.
func (d *demo) layout() {
	width, height := d.screen.Size()
	if width < 4 || height < 5 {
		// Too small for chrome: the editor takes everything.
		d.view.SetRect(0, 0, width, height)
		return
	}
	d.view.SetRect(1, 1, width-2, height-3)
}

func (d *demo) draw() {
	width, height := d.screen.Size()
	if width < 4 || height < 5 {
		d.view.Draw(d.screen)
		d.screen.Show()
		return
	}

	d.frame(width, height)
	d.view.Draw(d.screen)
	d.status(width, height)
	d.screen.Show()
}

// frame paints the chrome the editor knows nothing about: a double line border
// with a title, a separator above the status bar, and key hints at the bottom.
func (d *demo) frame(width, height int) {
	for y := 0; y < height; y++ {
		d.screen.SetContent(0, y, '║', nil, borderStyle)
		d.screen.SetContent(width-1, y, '║', nil, borderStyle)
	}
	for x := 0; x < width; x++ {
		d.screen.SetContent(x, 0, '═', nil, borderStyle)
		d.screen.SetContent(x, height-1, '═', nil, borderStyle)
	}
	d.screen.SetContent(0, 0, '╔', nil, borderStyle)
	d.screen.SetContent(width-1, 0, '╗', nil, borderStyle)
	d.screen.SetContent(0, height-1, '╚', nil, borderStyle)
	d.screen.SetContent(width-1, height-1, '╝', nil, borderStyle)

	for x := 1; x < width-1; x++ {
		d.screen.SetContent(x, height-2, '─', nil, borderStyle)
	}
	d.screen.SetContent(0, height-2, '╟', nil, borderStyle)
	d.screen.SetContent(width-1, height-2, '╢', nil, borderStyle)

	d.put(1, 0, " editor — github.com/malivvan/editor ", borderStyle.Reverse(true), width-2)
	d.put(1, height-1, " Ctrl-S save · Ctrl-Q quit · F1 line numbers · F2 colorscheme · type to autocomplete ", borderStyle, width-2)
}

// status overwrites the separator row with the state of the buffer.
func (d *demo) status(width, height int) {
	name := "untitled (built-in sample)"
	if d.buf.Path != "" {
		name = d.buf.Path
	}

	state := "clean"
	if d.buf.Modified() {
		state = "modified"
	}

	line := fmt.Sprintf("%s · %s · %s · %d:%d · %s · top line %d of %d",
		name, d.buf.FileType(), state,
		d.view.Cursor.Y+1, d.view.Cursor.X+1,
		d.schemes[d.schemeIndex], d.view.Topline+1, d.buf.NumLines)

	if d.message != "" {
		line = d.message
	}

	style := tcell.StyleDefault.Reverse(true)
	for x := 1; x < width-1; x++ {
		d.screen.SetContent(x, height-2, ' ', nil, style)
	}
	d.put(2, height-2, line, style, width-4)
}

// put writes text at x, y and stops after max cells.
func (d *demo) put(x, y int, text string, style tcell.Style, max int) {
	for _, r := range text {
		if max <= 0 {
			return
		}
		d.screen.SetContent(x, y, r, nil, style)
		x++
		max--
	}
}

func (d *demo) toggleLineNumbers() {
	ruler, _ := d.buf.Settings["ruler"].(bool)
	d.buf.Settings["ruler"] = !ruler
}

// schemeNames returns the names of the colorschemes embedded in the runtime
// package, sorted so that F2 cycles through them predictably.
func (d *demo) schemeNames() []string {
	names := []string{}
	for _, file := range runtime.Files.ListRuntimeFiles(editor.RTColorscheme) {
		names = append(names, file.Name())
	}
	sort.Strings(names)

	return names
}

// applyScheme installs the named embedded colorscheme on the view.
func (d *demo) applyScheme(name string) {
	file := runtime.Files.FindFile(editor.RTColorscheme, name)
	if file == nil {
		return
	}
	data, err := file.Data()
	if err != nil {
		return
	}

	d.view.SetColorscheme(editor.ParseColorscheme(string(data)))
	for i, n := range d.schemes {
		if n == name {
			d.schemeIndex = i
			break
		}
	}
}

func (d *demo) nextScheme() {
	if len(d.schemes) == 0 {
		return
	}
	d.applyScheme(d.schemes[(d.schemeIndex+1)%len(d.schemes)])
}

func (d *demo) save() {
	if d.buf.Path == "" {
		d.message = "no file name — this buffer is the built-in sample, use Ctrl-Q to quit"
		return
	}

	if err := os.WriteFile(d.buf.Path, []byte(d.buf.String()), 0o600); err != nil {
		d.message = fmt.Sprintf("save failed: %v", err)
		return
	}

	d.buf.IsModified = false
	d.message = fmt.Sprintf("saved %s (%d bytes)", d.buf.Path, d.buf.Len())
}
