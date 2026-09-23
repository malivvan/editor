package editor

import (
	"github.com/gdamore/tcell/v3"
	"github.com/mattn/go-runewidth"
)

// Screen is the rendering surface the editor draws onto.
//
// It is a deliberately small subset of tcell.Screen, so a tcell screen — or
// the tcell.Screen interface value itself — satisfies it as it is. A host that
// renders somewhere else (an in-memory grid, a network client, an image) can
// implement these five methods and get the same output as a terminal.
type Screen interface {
	// SetContent writes ch with the combining runes comb and the given style
	// to the cell at x, y. Coordinates outside the surface are ignored.
	SetContent(x, y int, ch rune, comb []rune, style tcell.Style)

	// Get returns the contents of the cell at x, y: its string, its style and
	// its width in cells. The editor uses it to read back the character that
	// sits under an additional cursor, and the style of the cell just before
	// the cursor, which the ghost text of the autocomplete blends into.
	Get(x, y int) (str string, style tcell.Style, width int)

	// Size returns the width and height of the surface in cells.
	Size() (width, height int)

	// ShowCursor places the terminal cursor at x, y.
	ShowCursor(x, y int)

	// HideCursor hides the terminal cursor.
	HideCursor()
}

// printStyle writes text to the screen starting at x, y, using the given
// style. It never writes beyond maxWidth cells and returns the number of cells
// it filled. Unlike the markup-aware helpers of a widget toolkit this one
// prints text literally: the editor draws syntax highlighting by choosing a
// style per character, not by embedding tags in strings.
func printStyle(screen Screen, text string, x, y, maxWidth int, style tcell.Style) int {
	if maxWidth <= 0 {
		return 0
	}

	written := 0
	for _, r := range text {
		width := runewidth.RuneWidth(r)
		if width <= 0 {
			width = 1
		}
		if written+width > maxWidth {
			break
		}
		screen.SetContent(x+written, y, r, nil, style)
		written += width
	}

	return written
}
