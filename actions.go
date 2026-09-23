package editor

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
)

const DOUBLE_CLICK_INTERVAL = 500 * time.Millisecond

func (v *View) deselect(index int) bool {
	if v.Cursor.HasSelection() {
		v.Cursor.Loc = v.Cursor.CurSelection[index]
		v.Cursor.ResetSelection()
		v.Cursor.StoreVisualX()
		return true
	}
	return false
}

// ScrollUpAction scrolls the view up
func (v *View) ScrollUpAction() bool {
	if v.mainCursor() {
		scrollspeed := int(settingAs(v.Buf.Settings, "scrollspeed", float64(2)))
		v.ScrollUp(scrollspeed)
	}
	return false
}

// ScrollDownAction scrolls the view up
func (v *View) ScrollDownAction() bool {
	if v.mainCursor() {
		scrollspeed := int(settingAs(v.Buf.Settings, "scrollspeed", float64(2)))
		v.ScrollDown(scrollspeed)
	}
	return false
}

func (v *View) docLocFromMouse(e *tcell.EventMouse) Loc {
	absoluteMouseX, absoluteMouseY := e.Position()
	rx, ry, _, _ := v.Rect()
	mouseX := absoluteMouseX - rx
	mouseY := absoluteMouseY - ry + v.Topline
	docX := mouseX - v.getLineNumWidth()
	docY := mouseY

	return Loc{X: docX, Y: docY}
}

// MouseLeftDown handles a press of the primary button: it moves the cursor to
// the position the event points at, and turns a second or third press in quick
// succession into a word or line selection. It also arms dragging, which
// MouseMove then extends.
//
// Most hosts do not call this directly; HandleMouse derives the action and
// calls it.
func (v *View) MouseLeftDown(e *tcell.EventMouse) {
	docLoc := v.docLocFromMouse(e)

	fastClick := time.Since(v.lastMouseLeftUpTime) < DOUBLE_CLICK_INTERVAL
	if fastClick {
		switch v.leftMouseMode {
		case MOUSE_BUTTON_MODE_SINGLE:
			v.leftMouseMode = MOUSE_BUTTON_MODE_DOUBLE

		case MOUSE_BUTTON_MODE_DOUBLE:
			v.leftMouseMode = MOUSE_BUTTON_MODE_TRIPLE

		case MOUSE_BUTTON_MODE_TRIPLE:
		}
	} else {
		v.leftMouseMode = MOUSE_BUTTON_MODE_SINGLE
	}

	switch v.leftMouseMode {
	case MOUSE_BUTTON_MODE_SINGLE:
		v.deselect(0)
		v.Cursor.MoveTo(docLoc)
		v.Cursor.SetSelectionStart(docLoc)
		v.Cursor.SetSelectionEnd(docLoc)

	case MOUSE_BUTTON_MODE_DOUBLE:
		v.Cursor.SelectWord()

	case MOUSE_BUTTON_MODE_TRIPLE:
		v.Cursor.SelectLine()
	}
	v.leftMouseDown = true
}

// MouseLeftUp handles the release of the primary button, ending a drag and
// remembering the time so that the next press can be recognized as a double
// click.
func (v *View) MouseLeftUp(e *tcell.EventMouse) {
	v.lastMouseLeftUpTime = time.Now()
	v.leftMouseDown = false
}

// MouseMove handles a pointer movement: while the primary button is held down
// it extends the selection the press started, and it does nothing otherwise.
func (v *View) MouseMove(e *tcell.EventMouse) {
	if !v.leftMouseDown {
		return
	}

	loc := v.docLocFromMouse(e)

	switch v.leftMouseMode {
	case MOUSE_BUTTON_MODE_SINGLE:
		v.Cursor.SetSelectionEnd(loc)
		v.Cursor.MoveTo(loc)

	case MOUSE_BUTTON_MODE_DOUBLE:
		v.Cursor.MoveTo(loc)
		v.Cursor.AddWordToSelection()

	case MOUSE_BUTTON_MODE_TRIPLE:
		v.Cursor.MoveTo(loc)
		v.Cursor.AddLineToSelection()
	}
}

// Center centers the view on the cursor
func (v *View) Center() bool {
	v.Topline = v.Cursor.Y - v.height/2
	if v.Topline+v.height > v.Buf.NumLines {
		v.Topline = v.Buf.NumLines - v.height
	}
	if v.Topline < 0 {
		v.Topline = 0
	}
	return true
}

// CursorUp moves the cursor up
func (v *View) CursorUp() bool {
	v.deselect(0)
	v.Cursor.Up()
	return true
}

// CursorDown moves the cursor down
func (v *View) CursorDown() bool {
	v.deselect(1)
	v.Cursor.Down()
	return true
}

// CursorLeft moves the cursor left
func (v *View) CursorLeft() bool {
	if v.Cursor.HasSelection() {
		v.Cursor.Loc = v.Cursor.CurSelection[0]
		v.Cursor.ResetSelection()
		v.Cursor.StoreVisualX()
	} else {
		tabstospaces := settingAs(v.Buf.Settings, "tabstospaces", false)
		tabmovement := settingAs(v.Buf.Settings, "tabmovement", false)
		if tabstospaces && tabmovement {
			tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))
			line := v.Buf.Line(v.Cursor.Y)
			if v.Cursor.X-tabsize >= 0 && line[v.Cursor.X-tabsize:v.Cursor.X] == Spaces(tabsize) && IsStrWhitespace(line[0:v.Cursor.X-tabsize]) {
				for i := 0; i < tabsize; i++ {
					v.Cursor.Left()
				}
			} else {
				v.Cursor.Left()
			}
		} else {
			v.Cursor.Left()
		}
	}
	return true
}

// CursorRight moves the cursor right
func (v *View) CursorRight() bool {
	if v.Cursor.HasSelection() {
		v.Cursor.Loc = v.Cursor.CurSelection[1]
		v.Cursor.ResetSelection()
		v.Cursor.StoreVisualX()
	} else {
		tabstospaces := settingAs(v.Buf.Settings, "tabstospaces", false)
		tabmovement := settingAs(v.Buf.Settings, "tabmovement", false)
		if tabstospaces && tabmovement {
			tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))
			line := v.Buf.Line(v.Cursor.Y)
			if v.Cursor.X+tabsize < Count(line) && line[v.Cursor.X:v.Cursor.X+tabsize] == Spaces(tabsize) && IsStrWhitespace(line[0:v.Cursor.X]) {
				for i := 0; i < tabsize; i++ {
					v.Cursor.Right()
				}
			} else {
				v.Cursor.Right()
			}
		} else {
			v.Cursor.Right()
		}
	}
	return true
}

// WordRight moves the cursor one word to the right
func (v *View) WordRight() bool {

	v.Cursor.WordRight()

	return true
}

// WordLeft moves the cursor one word to the left
func (v *View) WordLeft() bool {

	v.Cursor.WordLeft()

	return true
}

// SelectUp selects up one line
func (v *View) SelectUp() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.Up()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectDown selects down one line
func (v *View) SelectDown() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.Down()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectLeft selects the character to the left of the cursor
func (v *View) SelectLeft() bool {
	loc := v.Cursor.Loc
	count := v.Buf.End()
	if loc.GreaterThan(count) {
		loc = count
	}
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = loc
	}
	v.Cursor.Left()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectRight selects the character to the right of the cursor
func (v *View) SelectRight() bool {
	loc := v.Cursor.Loc
	count := v.Buf.End()
	if loc.GreaterThan(count) {
		loc = count
	}
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = loc
	}
	v.Cursor.Right()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectWordRight selects the word to the right of the cursor
func (v *View) SelectWordRight() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.WordRight()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectWordLeft selects the word to the left of the cursor
func (v *View) SelectWordLeft() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.WordLeft()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// StartOfLine moves the cursor to the start of the line
func (v *View) StartOfLine() bool {
	v.deselect(0)

	if v.Cursor.X != 0 {
		v.Cursor.Start()
	} else {
		v.Cursor.StartOfText()
	}
	return true
}

// EndOfLine moves the cursor to the end of the line
func (v *View) EndOfLine() bool {
	v.deselect(0)
	v.Cursor.End()
	return true
}

// SelectLine selects the entire current line
func (v *View) SelectLine() bool {
	v.Cursor.SelectLine()
	return true
}

// SelectToStartOfLine selects to the start of the current line
func (v *View) SelectToStartOfLine() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.Start()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectToEndOfLine selects to the end of the current line
func (v *View) SelectToEndOfLine() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.End()
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// ParagraphPrevious moves the cursor to the previous empty line, or beginning of the buffer if there's none
func (v *View) ParagraphPrevious() bool {
	var line int
	for line = v.Cursor.Y; line > 0; line-- {
		if len(v.Buf.lines[line].data) == 0 && line != v.Cursor.Y {
			v.Cursor.X = 0
			v.Cursor.Y = line
			break
		}
	}
	// If no empty line found. move cursor to end of buffer
	if line == 0 {
		v.Cursor.Loc = v.Buf.Start()
	}
	return true
}

// ParagraphNext moves the cursor to the next empty line, or end of the buffer if there's none
func (v *View) ParagraphNext() bool {
	var line int
	for line = v.Cursor.Y; line < len(v.Buf.lines); line++ {
		if len(v.Buf.lines[line].data) == 0 && line != v.Cursor.Y {
			v.Cursor.X = 0
			v.Cursor.Y = line
			break
		}
	}
	// If no empty line found. move cursor to end of buffer
	if line == len(v.Buf.lines) {
		v.Cursor.Loc = v.Buf.End()
	}
	return true
}

// Retab changes all tabs to spaces or all spaces to tabs depending
// on the user's settings. Changes go through the EventHandler so they
// are undoable and cursor positions are updated correctly.
func (v *View) Retab() bool {
	toSpaces := settingAs(v.Buf.Settings, "tabstospaces", false)
	tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))

	for i := 0; i < v.Buf.NumLines; i++ {
		l := v.Buf.Line(i)

		ws := GetLeadingWhitespace(l)
		if ws == "" {
			continue
		}

		var newWs string
		if toSpaces {
			newWs = strings.Replace(ws, "\t", Spaces(tabsize), -1)
		} else {
			newWs = strings.Replace(ws, Spaces(tabsize), "\t", -1)
		}

		if newWs == ws {
			continue
		}

		// Replace the leading whitespace through the EventHandler.
		lineStart := Loc{0, i}
		lineEnd := Loc{Count(ws), i}
		v.Buf.Replace(lineStart, lineEnd, newWs)
	}

	return true
}

// CursorStart moves the cursor to the start of the buffer
func (v *View) CursorStart() bool {
	v.deselect(0)

	v.Cursor.X = 0
	v.Cursor.Y = 0

	return true
}

// CursorEnd moves the cursor to the end of the buffer
func (v *View) CursorEnd() bool {
	v.deselect(0)

	v.Cursor.Loc = v.Buf.End()
	v.Cursor.StoreVisualX()

	return true
}

// SelectToStart selects the text from the cursor to the start of the buffer
func (v *View) SelectToStart() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.CursorStart()
	v.Cursor.SelectTo(v.Buf.Start())
	return true
}

// SelectToEnd selects the text from the cursor to the end of the buffer
func (v *View) SelectToEnd() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.CursorEnd()
	v.Cursor.SelectTo(v.Buf.End())
	return true
}

// InsertSpace inserts a space
func (v *View) InsertSpace() bool {
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	}
	v.Buf.Insert(v.Cursor.Loc, " ")
	// v.Cursor.Right()
	return true
}

// InsertNewline inserts a newline plus possible some whitespace if autoindent is on
func (v *View) InsertNewline() bool {
	// Insert a newline
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	}

	ws := GetLeadingWhitespace(v.Buf.Line(v.Cursor.Y))
	cx := v.Cursor.X
	v.Buf.Insert(v.Cursor.Loc, "\n")
	// v.Cursor.Right()

	if settingAs(v.Buf.Settings, "autoindent", true) {
		if cx < len(ws) {
			ws = ws[0:cx]
		}
		v.Buf.Insert(v.Cursor.Loc, ws)
		// for i := 0; i < len(ws); i++ {
		// 	v.Cursor.Right()
		// }

		// Remove the whitespaces if keepautoindent setting is off
		if IsSpacesOrTabs(v.Buf.Line(v.Cursor.Y-1)) && !settingAs(v.Buf.Settings, "keepautoindent", false) {
			line := v.Buf.Line(v.Cursor.Y - 1)
			v.Buf.Remove(Loc{0, v.Cursor.Y - 1}, Loc{Count(line), v.Cursor.Y - 1})
		}
	}
	v.Cursor.LastVisualX = v.Cursor.GetVisualX()

	return true
}

// Backspace deletes the previous character
func (v *View) Backspace() bool {
	// Delete a character
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	} else if v.Cursor.Loc.GreaterThan(v.Buf.Start()) {
		// We have to do something a bit hacky here because we want to
		// delete the line by first moving left and then deleting backwards
		// but the undo redo would place the cursor in the wrong place
		// So instead we move left, save the position, move back, delete
		// and restore the position

		// If the user is using spaces instead of tabs and they are deleting
		// whitespace at the start of the line, we should delete as if it's a
		// tab (tabSize number of spaces)
		lineStart := sliceEnd(v.Buf.LineBytes(v.Cursor.Y), v.Cursor.X)
		tabSize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))
		if settingAs(v.Buf.Settings, "tabstospaces", false) && IsSpaces(lineStart) && utf8.RuneCount(lineStart) != 0 && utf8.RuneCount(lineStart)%tabSize == 0 {
			loc := v.Cursor.Loc
			v.Buf.Remove(loc.Move(-tabSize, v.Buf), loc)
		} else {
			loc := v.Cursor.Loc
			v.Buf.Remove(loc.Move(-1, v.Buf), loc)
		}
	}
	v.Cursor.LastVisualX = v.Cursor.GetVisualX()

	return true
}

// DeleteWordRight deletes the word to the right of the cursor
func (v *View) DeleteWordRight() bool {
	v.SelectWordRight()
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	}
	return true
}

// DeleteWordLeft deletes the word to the left of the cursor
func (v *View) DeleteWordLeft() bool {
	v.SelectWordLeft()
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	}
	return true
}

// Delete deletes the next character
func (v *View) Delete() bool {
	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	} else {
		loc := v.Cursor.Loc
		if loc.LessThan(v.Buf.End()) {
			v.Buf.Remove(loc, loc.Move(1, v.Buf))
		}
	}
	return true
}

// IndentSelection indents the current selection
func (v *View) IndentSelection() bool {
	if v.Cursor.HasSelection() {
		start := v.Cursor.CurSelection[0]
		end := v.Cursor.CurSelection[1]
		if end.Y < start.Y {
			start, end = end, start
			v.Cursor.SetSelectionStart(start)
			v.Cursor.SetSelectionEnd(end)
		}

		startY := start.Y
		endY := end.Move(-1, v.Buf).Y
		endX := end.Move(-1, v.Buf).X
		tabsize := len(v.Buf.IndentString())
		for y := startY; y <= endY; y++ {
			v.Buf.Insert(Loc{0, y}, v.Buf.IndentString())
			if y == startY && start.X > 0 {
				v.Cursor.SetSelectionStart(start.Move(tabsize, v.Buf))
			}
			if y == endY {
				v.Cursor.SetSelectionEnd(Loc{endX + tabsize + 1, endY})
			}
		}
		v.Cursor.Relocate()

		return true
	}
	return false
}

// OutdentLine moves the current line back one indentation
func (v *View) OutdentLine() bool {
	if v.Cursor.HasSelection() {
		return false
	}

	for x := 0; x < len(v.Buf.IndentString()); x++ {
		if len(GetLeadingWhitespace(v.Buf.Line(v.Cursor.Y))) == 0 {
			break
		}
		v.Buf.Remove(Loc{0, v.Cursor.Y}, Loc{1, v.Cursor.Y})
	}
	v.Cursor.Relocate()
	return true
}

// OutdentSelection takes the current selection and moves it back one indent level
func (v *View) OutdentSelection() bool {
	if v.Cursor.HasSelection() {
		start := v.Cursor.CurSelection[0]
		end := v.Cursor.CurSelection[1]
		if end.Y < start.Y {
			start, end = end, start
			v.Cursor.SetSelectionStart(start)
			v.Cursor.SetSelectionEnd(end)
		}

		startY := start.Y
		endY := end.Move(-1, v.Buf).Y
		for y := startY; y <= endY; y++ {
			for x := 0; x < len(v.Buf.IndentString()); x++ {
				if len(GetLeadingWhitespace(v.Buf.Line(y))) == 0 {
					break
				}
				v.Buf.Remove(Loc{0, y}, Loc{1, y})
			}
		}
		v.Cursor.Relocate()

		return true
	}
	return false
}

// InsertTab inserts a tab or spaces
func (v *View) InsertTab() bool {
	if v.Cursor.HasSelection() {
		return false
	}

	tabBytes := len(v.Buf.IndentString())
	bytesUntilIndent := tabBytes - (v.Cursor.GetVisualX() % tabBytes)
	v.Buf.Insert(v.Cursor.Loc, v.Buf.IndentString()[:bytesUntilIndent])
	// for i := 0; i < bytesUntilIndent; i++ {
	// 	v.Cursor.Right()
	// }

	return true
}

//// Find opens a prompt and searches forward for the input
//func (v *View) Find() bool {
//	if v.mainCursor() {
//		searchStr := ""
//		if v.Cursor.HasSelection() {
//			searchStart = v.Cursor.CurSelection[1]
//			searchStart = v.Cursor.CurSelection[1]
//			searchStr = v.Cursor.GetSelection()
//		} else {
//			searchStart = v.Cursor.Loc
//		}
//		BeginSearch(searchStr)
//
//	}
//	return true
//}
//
//// FindNext searches forwards for the last used search term
//func (v *View) FindNext() bool {
//	if v.Cursor.HasSelection() {
//		searchStart = v.Cursor.CurSelection[1]
//		// lastSearch = v.Cursor.GetSelection()
//	} else {
//		searchStart = v.Cursor.Loc
//	}
//	if lastSearch == "" {
//		return true
//	}
//	Search(lastSearch, v, true)
//	return true
//}
//
//// FindPrevious searches backwards for the last used search term
//func (v *View) FindPrevious() bool {
//	if v.Cursor.HasSelection() {
//		searchStart = v.Cursor.CurSelection[0]
//	} else {
//		searchStart = v.Cursor.Loc
//	}
//	Search(lastSearch, v, false)
//	return true
//}

// Undo undoes the last action
func (v *View) Undo() bool {
	if v.Buf.curCursor == 0 {
		v.Buf.clearCursors()
	}

	v.Buf.Undo()
	return true
}

// Redo redoes the last action
func (v *View) Redo() bool {
	if v.Buf.curCursor == 0 {
		v.Buf.clearCursors()
	}

	v.Buf.Redo()
	return true
}

// Copy the selection to the system clipboard
func (v *View) Copy() bool {
	if v.mainCursor() {
		if v.Cursor.HasSelection() {
			v.Cursor.CopySelection("clipboard")
			v.freshClip = true
		}
	}
	return true
}

// CutLine cuts the current line to the clipboard
func (v *View) CutLine() bool {
	v.Cursor.SelectLine()
	if !v.Cursor.HasSelection() {
		return false
	}
	if v.freshClip && time.Since(v.lastCutTime) <= 10*time.Second {
		// Rapid consecutive cuts: append the new line to the existing clipboard.
		if v.Cursor.HasSelection() {
			if clip, err := clipboardReadAll(); err != nil {
				// do nothing
			} else {
				clipboardWriteAll(clip + v.Cursor.GetSelection())
			}
		}
	} else {
		// First cut in a sequence, or the 10-second window has expired:
		// start a fresh clipboard entry.
		v.Copy()
	}
	v.freshClip = true
	v.lastCutTime = time.Now()
	v.Cursor.DeleteSelection()
	v.Cursor.ResetSelection()

	return true
}

// Cut the selection to the system clipboard
func (v *View) Cut() bool {
	if v.Cursor.HasSelection() {
		v.Cursor.CopySelection("clipboard")
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
		v.freshClip = true

		return true
	} else {
		return v.CutLine()
	}
}

// DuplicateLine duplicates the current line or selection
func (v *View) DuplicateLine() bool {
	if v.Cursor.HasSelection() {
		v.Buf.Insert(v.Cursor.CurSelection[1], v.Cursor.GetSelection())
	} else {
		v.Cursor.End()
		v.Buf.Insert(v.Cursor.Loc, "\n"+v.Buf.Line(v.Cursor.Y))
		// v.Cursor.Right()
	}
	return true
}

// DeleteLine deletes the current line
func (v *View) DeleteLine() bool {
	v.Cursor.SelectLine()
	if !v.Cursor.HasSelection() {
		return false
	}
	v.Cursor.DeleteSelection()
	v.Cursor.ResetSelection()
	return true
}

// MoveLinesUp moves up the current line or selected lines if any
func (v *View) MoveLinesUp() bool {
	if v.Cursor.HasSelection() {
		if v.Cursor.CurSelection[0].Y == 0 {
			return true
		}
		start := v.Cursor.CurSelection[0].Y
		end := v.Cursor.CurSelection[1].Y
		if start > end {
			end, start = start, end
		}

		v.Buf.MoveLinesUp(
			start,
			end,
		)
		v.Cursor.CurSelection[1].Y -= 1
	} else {
		if v.Cursor.Loc.Y == 0 {
			return true
		}
		v.Buf.MoveLinesUp(
			v.Cursor.Loc.Y,
			v.Cursor.Loc.Y+1,
		)
	}
	v.Buf.IsModified = true

	return true
}

// MoveLinesDown moves down the current line or selected lines if any
func (v *View) MoveLinesDown() bool {
	if v.Cursor.HasSelection() {
		if v.Cursor.CurSelection[1].Y >= len(v.Buf.lines) {
			return true
		}
		start := v.Cursor.CurSelection[0].Y
		end := v.Cursor.CurSelection[1].Y
		if start > end {
			end, start = start, end
		}

		v.Buf.MoveLinesDown(
			start,
			end,
		)
	} else {
		if v.Cursor.Loc.Y >= len(v.Buf.lines)-1 {
			return true
		}
		v.Buf.MoveLinesDown(
			v.Cursor.Loc.Y,
			v.Cursor.Loc.Y+1,
		)
	}
	v.Buf.IsModified = true

	return true
}

// Paste whatever is in the system clipboard into the buffer
// Delete and paste if the user has a selection
func (v *View) Paste() bool {
	clip, _ := clipboardReadAll()
	v.paste(clip)
	return true
}

// JumpToMatchingBrace moves the cursor to the matching brace if it is
// currently on a brace
func (v *View) JumpToMatchingBrace() bool {
	for _, bp := range bracePairs {
		r := v.Cursor.RuneUnder(v.Cursor.X)
		if r == bp[0] || r == bp[1] {
			matchingBrace := v.Buf.FindMatchingBrace(bp, v.Cursor.Loc)
			v.Cursor.GotoLoc(matchingBrace)
		}
	}
	return true
}

// SelectAll selects the entire buffer
func (v *View) SelectAll() bool {
	v.Cursor.SetSelectionStart(v.Buf.Start())
	v.Cursor.SetSelectionEnd(v.Buf.End())
	// Put the cursor at the beginning
	v.Cursor.X = 0
	v.Cursor.Y = 0
	return true
}

// Start moves the viewport to the start of the buffer
func (v *View) Start() bool {
	if v.mainCursor() {
		v.Topline = 0
	}
	return false
}

// End moves the viewport to the end of the buffer
func (v *View) End() bool {
	if v.mainCursor() {
		if v.height > v.Buf.NumLines {
			v.Topline = 0
		} else {
			v.Topline = v.Buf.NumLines - v.height
		}

	}
	return false
}

// PageUp scrolls the view up a page
func (v *View) PageUp() bool {
	if v.mainCursor() {
		if v.Topline > v.height {
			v.ScrollUp(v.height)
		} else {
			v.Topline = 0
		}
	}
	return false
}

// PageDown scrolls the view down a page
func (v *View) PageDown() bool {
	if v.mainCursor() {
		if v.Buf.NumLines-(v.Topline+v.height) > v.height {
			v.ScrollDown(v.height)
		} else if v.Buf.NumLines >= v.height {
			v.Topline = v.Buf.NumLines - v.height
		}
	}
	return false
}

// SelectPageUp selects up one page
func (v *View) SelectPageUp() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.UpN(v.height)
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// SelectPageDown selects down one page
func (v *View) SelectPageDown() bool {
	if !v.Cursor.HasSelection() {
		v.Cursor.OrigSelection[0] = v.Cursor.Loc
	}
	v.Cursor.DownN(v.height)
	v.Cursor.SelectTo(v.Cursor.Loc)
	return true
}

// CursorPageUp places the cursor a page up
func (v *View) CursorPageUp() bool {
	v.deselect(0)

	if v.Cursor.HasSelection() {
		v.Cursor.Loc = v.Cursor.CurSelection[0]
		v.Cursor.ResetSelection()
		v.Cursor.StoreVisualX()
	}
	v.Cursor.UpN(v.height)

	return true
}

// CursorPageDown places the cursor a page up
func (v *View) CursorPageDown() bool {
	v.deselect(0)

	if v.Cursor.HasSelection() {
		v.Cursor.Loc = v.Cursor.CurSelection[1]
		v.Cursor.ResetSelection()
		v.Cursor.StoreVisualX()
	}
	v.Cursor.DownN(v.height)

	return true
}

// HalfPageUp scrolls the view up half a page
func (v *View) HalfPageUp() bool {
	if v.mainCursor() {
		if v.Topline > v.height/2 {
			v.ScrollUp(v.height / 2)
		} else {
			v.Topline = 0
		}
	}
	return false
}

// HalfPageDown scrolls the view down half a page
func (v *View) HalfPageDown() bool {
	if v.mainCursor() {
		if v.Buf.NumLines-(v.Topline+v.height) > v.height/2 {
			v.ScrollDown(v.height / 2)
		} else {
			if v.Buf.NumLines >= v.height {
				v.Topline = v.Buf.NumLines - v.height
			}
		}
	}
	return false
}

// ToggleRuler turns line numbers off and on
func (v *View) ToggleRuler() bool {
	if v.mainCursor() {
		if v.Buf.Settings["ruler"] == false {
			v.Buf.Settings["ruler"] = true
		} else {
			v.Buf.Settings["ruler"] = false
		}
	}
	return false
}

// ToggleOverwriteMode lets the user toggle the text overwrite mode
func (v *View) ToggleOverwriteMode() bool {
	if v.mainCursor() {
		v.isOverwriteMode = !v.isOverwriteMode
	}
	return false
}

// Escape leaves current mode
func (v *View) Escape() bool {
	if v.mainCursor() {
		//		// check if user is searching, or the last search is still active
		//		if searching || lastSearch != "" {
		//			ExitSearch(v)
		//			return true
		//		}
	}
	return false
}

// SpawnMultiCursor creates a new multiple cursor at the next occurrence of the current selection or current word
func (v *View) SpawnMultiCursor() bool {
	spawner := v.Buf.cursors[len(v.Buf.cursors)-1]
	// You can only spawn a cursor from the main cursor
	if v.Cursor == spawner {
		if !spawner.HasSelection() {
			spawner.SelectWord()
		} else {
			c := &Cursor{
				buf: v.Buf,
			}

			sel := spawner.GetSelection()
			v.searchStart = spawner.CurSelection[1]
			v.Cursor = c
			v.Search(regexp.QuoteMeta(sel), false, false, true)

			for _, cur := range v.Buf.cursors {
				if c.Loc == cur.Loc {
					return false
				}
			}
			v.Buf.cursors = append(v.Buf.cursors, c)
			v.Buf.UpdateCursors()
			v.Relocate()
			v.Cursor = spawner
		}
	}
	return false
}

// SpawnMultiCursorSelect adds a cursor at the beginning of each line of a selection
func (v *View) SpawnMultiCursorSelect() bool {
	if v.Cursor == &v.Buf.Cursor {
		// Avoid cases where multiple cursors already exist, that would create problems
		if len(v.Buf.cursors) > 1 {
			return false
		}

		var startLine int
		var endLine int

		a, b := v.Cursor.CurSelection[0].Y, v.Cursor.CurSelection[1].Y
		if a > b {
			startLine, endLine = b, a
		} else {
			startLine, endLine = a, b
		}

		if v.Cursor.HasSelection() {
			v.Cursor.ResetSelection()
			v.Cursor.GotoLoc(Loc{0, startLine})

			for i := startLine; i <= endLine; i++ {
				c := &Cursor{
					buf: v.Buf,
				}
				c.GotoLoc(Loc{0, i})
				v.Buf.cursors = append(v.Buf.cursors, c)
			}
			v.Buf.MergeCursors()
			v.Buf.UpdateCursors()
		} else {
			return false
		}
	}
	return false
}

// SkipMultiCursor moves the current multiple cursor to the next available position
func (v *View) SkipMultiCursor() bool {
	cursor := v.Buf.cursors[len(v.Buf.cursors)-1]
	if v.mainCursor() {
		//sel := cursor.GetSelection()

		//searchStart = cursor.CurSelection[1]
		v.Cursor = cursor
		//Search(regexp.QuoteMeta(sel), v, true)
		v.Relocate()
		v.Cursor = cursor

	}
	return false
}

// RemoveMultiCursor removes the latest multiple cursor
func (v *View) RemoveMultiCursor() bool {
	end := len(v.Buf.cursors)
	if end > 1 {
		if v.mainCursor() {
			v.Buf.cursors[end-1] = nil
			v.Buf.cursors = v.Buf.cursors[:end-1]
			v.Buf.UpdateCursors()
			v.Relocate()

			return true
		}
	} else {
		v.RemoveAllMultiCursors()
	}
	return false
}

// RemoveAllMultiCursors removes all cursors except the base cursor
func (v *View) RemoveAllMultiCursors() bool {
	if v.mainCursor() {
		v.Buf.clearCursors()
		v.Relocate()
		return true
	}
	return false
}
