package editor

import (
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
)

const (
	MOUSE_BUTTON_MODE_SINGLE = iota
	MOUSE_BUTTON_MODE_DOUBLE
	MOUSE_BUTTON_MODE_TRIPLE
)

// View is a window onto a Buffer. It owns the viewport (scrolling, soft wrap,
// line numbers), the cursor set, the key bindings, the colorscheme and the
// optional autocomplete popup.
//
// A View draws itself onto a Screen, which is a small subset of tcell.Screen
// — a tcell screen satisfies it as it is — and consumes tcell events. It has
// no opinion on how it is hosted: the same View can be the only thing on a
// tcell screen (see the demo), a pane of a larger application, or the source
// of a buffer that is rendered to an image.
type View struct {
	// A pointer to the buffer's cursor for ease of access
	Cursor *Cursor

	// The topmost line, used for vertical scrolling
	Topline int
	// The leftmost column, used for horizontal scrolling
	leftCol int

	// Specifies whether this view is readonly
	Readonly bool

	// The area the view occupies on the screen.
	x, y          int
	width, height int

	// Whether the view has focus. A blurred view does not place the terminal
	// cursor, see the "hidecursoronblur" setting.
	hasFocus bool

	// inputCapture, when set, receives every key event before the view's own
	// handling and may swallow it by returning nil.
	inputCapture func(event *tcell.EventKey) *tcell.EventKey

	// Mouse bookkeeping used to derive actions from raw tcell events.
	lastMouseX, lastMouseY int
	lastMouseButtons       tcell.ButtonMask

	// How much to offset because of line numbers
	lineNumOffset int

	// The buffer
	Buf *Buffer

	// We need to keep track of insert key press toggle
	isOverwriteMode bool
	lastLoc         Loc

	// lastCutTime stores when the last ctrl+k was issued.
	// It is used for clearing the clipboard to replace it with fresh cut lines.
	lastCutTime time.Time

	// freshClip returns true if the clipboard has never been pasted.
	freshClip bool

	// The cellview used for displaying and syntax highlighting
	cellview *CellView

	// The scrollbar
	scrollbar *ScrollBar

	// The keybindings
	bindings KeyBindings

	// The colorscheme
	colorscheme Colorscheme

	// The runtime files
	runtimeFiles *RuntimeFiles

	leftMouseMode       int
	leftMouseDown       bool
	lastMouseLeftUpTime time.Time

	// What was the last search
	lastSearch string

	// Where should we start the search down from (or up from)
	searchStart Loc

	// Is there currently a search in progress
	searching bool

	autocomplete         autocompleteState
	autocompleteProvider AutocompleteProvider
	cursorScreenX        int
	cursorScreenY        int
	cursorVisible        bool
	lastScreen           Screen
}

// NewView returns a new view with the specified buffer.
//
// The view is focused from the start; call Blur to take the focus away. Its
// rectangle is empty until SetRect is called, which makes Draw fall back to
// filling the whole screen.
func NewView(buf *Buffer) *View {
	v := new(View)

	v.x, v.y, v.width, v.height = 0, 0, 0, 0
	v.hasFocus = true

	v.cellview = new(CellView)

	v.OpenBuffer(buf)

	v.scrollbar = &ScrollBar{
		view: v,
	}

	v.bindings = DefaultKeyBindings
	v.autocompleteProvider = DefaultAutocompleteProvider

	return v
}

// SetRect sets the area the view occupies on the screen.
func (v *View) SetRect(x, y, width, height int) {
	v.x, v.y, v.width, v.height = x, y, width, height
}

// Rect returns the area the view occupies on the screen.
func (v *View) Rect() (x, y, width, height int) {
	return v.x, v.y, v.width, v.height
}

// Focus gives the view focus, which makes it place the terminal cursor.
func (v *View) Focus() {
	v.hasFocus = true
}

// Blur takes the focus away from the view.
func (v *View) Blur() {
	v.hasFocus = false
}

// SetFocus sets the focus state of the view.
func (v *View) SetFocus(focus bool) {
	v.hasFocus = focus
}

// HasFocus returns whether the view currently has focus.
func (v *View) HasFocus() bool {
	return v.hasFocus
}

// SetInputCapture installs a function which receives key events before the
// view handles them. The function can forward the event (or a different one)
// by returning it; returning nil swallows the event.
//
// Providing a nil function removes a previously installed capture.
func (v *View) SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) *View {
	v.inputCapture = capture
	return v
}

// GetInputCapture returns the function installed with SetInputCapture, or nil.
func (v *View) GetInputCapture() func(event *tcell.EventKey) *tcell.EventKey {
	return v.inputCapture
}

// HandleMouseAction handles one already-classified mouse action. It reports
// whether the view consumed the action.
//
// Actions whose position falls outside the view's rectangle are ignored.
func (v *View) HandleMouseAction(action MouseAction, event *tcell.EventMouse) bool {
	x, y, width, height := v.Rect()
	absEventX, absEventY := event.Position()
	eventX := absEventX - x
	eventY := absEventY - y
	if eventX < 0 || eventX >= width || eventY < 0 || eventY >= height {
		return false
	}

	autocompleteBefore := v.autocompleteSnapshot()
	shouldSyncAutocomplete := v.autocomplete.active

	if v.handleAutocompleteMouse(action, event) {
		v.Focus()
		return true
	}
	if v.autocomplete.active && action == MouseLeftDown {
		v.closeAutocomplete()
	}

	switch action {
	case MouseLeftDown:
		v.Focus()
		v.MouseLeftDown(event)
		if shouldSyncAutocomplete {
			v.syncAutocomplete(autocompleteBefore, false)
		}
		return true

	case MouseLeftUp:
		v.MouseLeftUp(event)
		if shouldSyncAutocomplete {
			v.syncAutocomplete(autocompleteBefore, false)
		}
		return true

	case MouseMove:
		v.MouseMove(event)
		if shouldSyncAutocomplete {
			v.syncAutocomplete(autocompleteBefore, false)
		}
		return true

	case MouseScrollUp:
		v.ScrollUp(3)
		return true

	case MouseScrollDown:
		v.ScrollDown(3)
		return true

	}

	return false
}

// HandleMouse derives the logical mouse actions (move, button and wheel
// events) from a raw tcell mouse event and applies them to the view. It
// reports whether the view consumed the event.
func (v *View) HandleMouse(event *tcell.EventMouse) bool {
	x, y := event.Position()
	buttons := event.Buttons()
	changed := buttons ^ v.lastMouseButtons

	consumed := false

	if x != v.lastMouseX || y != v.lastMouseY {
		if v.HandleMouseAction(MouseMove, event) {
			consumed = true
		}
		v.lastMouseX, v.lastMouseY = x, y
	}

	for _, buttonEvent := range []struct {
		button   tcell.ButtonMask
		down, up MouseAction
	}{
		{tcell.ButtonPrimary, MouseLeftDown, MouseLeftUp},
		{tcell.ButtonMiddle, MouseMiddleDown, MouseMiddleUp},
		{tcell.ButtonSecondary, MouseRightDown, MouseRightUp},
	} {
		if changed&buttonEvent.button == 0 {
			continue
		}
		action := buttonEvent.up
		if buttons&buttonEvent.button != 0 {
			action = buttonEvent.down
		}
		if v.HandleMouseAction(action, event) {
			consumed = true
		}
	}

	for _, wheelEvent := range []struct {
		button tcell.ButtonMask
		action MouseAction
	}{
		{tcell.WheelUp, MouseScrollUp},
		{tcell.WheelDown, MouseScrollDown},
		{tcell.WheelLeft, MouseScrollLeft},
		{tcell.WheelRight, MouseScrollRight},
	} {
		if buttons&wheelEvent.button != 0 {
			if v.HandleMouseAction(wheelEvent.action, event) {
				consumed = true
			}
		}
	}

	v.lastMouseButtons = buttons

	return consumed
}

// GetKeyBindings gets the keybindings for this view.
func (v *View) GetKeybindings() KeyBindings {
	return v.bindings
}

// SetKeybindings sets the keybindings for this view.
func (v *View) SetKeybindings(bindings KeyBindings) {
	v.bindings = bindings
}

// SetColorscheme sets the colorscheme for this view.
func (v *View) SetColorscheme(colorscheme Colorscheme) {
	v.colorscheme = colorscheme
	v.Buf.updateRules(v.runtimeFiles)
}

// SetRuntimeFiles sets the runtime files for this view.
func (v *View) SetRuntimeFiles(runtimeFiles *RuntimeFiles) {
	v.runtimeFiles = runtimeFiles
	v.Buf.updateRules(v.runtimeFiles)
}

// True if the view is in overwrite mode, false if in insert mode.
func (v *View) IsOverwriteMode() bool {
	return v.isOverwriteMode
}

func (v *View) paste(clip string) {
	if settingAs(v.Buf.Settings, "smartpaste", true) {
		if v.Cursor.X > 0 && GetLeadingWhitespace(strings.TrimLeft(clip, "\r\n")) == "" {
			leadingWS := GetLeadingWhitespace(v.Buf.Line(v.Cursor.Y))
			clip = strings.Replace(clip, "\n", "\n"+leadingWS, -1)
		}
	}

	if v.Cursor.HasSelection() {
		v.Cursor.DeleteSelection()
		v.Cursor.ResetSelection()
	}

	v.Buf.Insert(v.Cursor.Loc, clip)
	// v.Cursor.Loc = v.Cursor.Loc.Move(Count(clip), v.Buf)
	v.freshClip = false
}

// ScrollUp scrolls the view up n lines (if possible)
func (v *View) ScrollUp(n int) {
	// Try to scroll by n but if it would overflow, scroll by 1
	if v.Topline-n >= 0 {
		v.Topline -= n
	} else if v.Topline > 0 {
		v.Topline--
	}
}

// ScrollDown scrolls the view down n lines (if possible)
func (v *View) ScrollDown(n int) {
	// Try to scroll by n but if it would overflow, scroll by 1
	if v.Topline+n <= v.Buf.NumLines {
		v.Topline += n
	} else if v.Topline < v.Buf.NumLines-1 {
		v.Topline++
	}
}

// OpenBuffer opens a new buffer in this view.
// This resets the topline, event handler and cursor.
func (v *View) OpenBuffer(buf *Buffer) {
	v.Buf = buf
	v.Cursor = &buf.Cursor
	v.Topline = 0
	v.leftCol = 0
	v.Cursor.ResetSelection()
	v.Relocate()
	v.Center()

	// Set isOverwriteMode to false, because we assume we are in the default mode when editor
	// is opened
	v.isOverwriteMode = false
}

// Bottomline returns the line number of the lowest line in the view
// You might think that this is obviously just v.Topline + v.height
// but if softwrap is enabled things get complicated since one buffer
// line can take up multiple lines in the view
func (v *View) Bottomline() int {
	if !settingAs(v.Buf.Settings, "softwrap", false) {
		return v.Topline + v.height
	}

	screenX, screenY := 0, 0
	numLines := 0
	for lineN := v.Topline; lineN < v.Topline+v.height; lineN++ {
		line := v.Buf.Line(lineN)

		colN := 0
		for _, ch := range line {
			if screenX >= v.width-v.lineNumOffset {
				screenX = 0
				screenY++
			}

			if ch == '\t' {
				screenX += int(settingAs(v.Buf.Settings, "tabsize", float64(4))) - 1
			}

			screenX++
			colN++
		}
		screenX = 0
		screenY++
		numLines++

		if screenY >= v.height {
			break
		}
	}
	return numLines + v.Topline
}

// Relocate moves the view window so that the cursor is in view
// This is useful if the user has scrolled far away, and then starts typing
func (v *View) Relocate() bool {
	height := v.Bottomline() - v.Topline
	ret := false
	cy := v.Cursor.Y
	scrollmargin := int(settingAs(v.Buf.Settings, "scrollmargin", float64(3)))
	if cy < v.Topline+scrollmargin && cy > scrollmargin-1 {
		v.Topline = cy - scrollmargin
		ret = true
	} else if cy < v.Topline {
		v.Topline = cy
		ret = true
	}
	if cy > v.Topline+height-1-scrollmargin && cy < v.Buf.NumLines-scrollmargin {
		v.Topline = cy - height + 1 + scrollmargin
		ret = true
	} else if cy >= v.Buf.NumLines-scrollmargin && cy >= height {
		v.Topline = v.Buf.NumLines - height
		ret = true
	}

	if !settingAs(v.Buf.Settings, "softwrap", false) {
		cx := v.Cursor.GetVisualX()
		if cx < v.leftCol {
			v.leftCol = cx
			ret = true
		}
		cursorXOnScreen := cx + v.lineNumOffset
		if cursorXOnScreen > v.leftCol+v.width {
			v.leftCol = cx - v.width + v.lineNumOffset + 1
			ret = true
		}
	}
	return ret
}

// Execute actions executes the supplied actions
func (v *View) ExecuteActions(actions []func(*View) bool) bool {
	relocate := false
	readonlyBindingsList := []string{"Delete", "Insert", "Backspace", "Cut", "Play", "Paste", "Move", "Add", "DuplicateLine", "Macro"}
	for _, action := range actions {
		readonlyBindingsResult := false
		funcName := ShortFuncName(action)
		if v.Readonly == true {
			// check for readonly and if true only let key bindings get called if they do not change the contents.
			for _, readonlyBindings := range readonlyBindingsList {
				if strings.Contains(funcName, readonlyBindings) {
					readonlyBindingsResult = true
				}
			}
		}
		if !readonlyBindingsResult {
			// call the key binding
			relocate = action(v) || relocate
		}
	}

	return relocate
}

// SetCursor sets the view's and buffer's cursor
func (v *View) SetCursor(c *Cursor) bool {
	if c == nil {
		return false
	}
	v.Cursor = c
	v.Buf.curCursor = c.Num

	return true
}

// HandleEvent handles an event passed by the main loop. It reports whether the
// editor consumed the event.
//
// Key events go through the input capture (see SetInputCapture), then the
// autocomplete popup and finally the key bindings. Mouse events are classified
// into actions and offered to HandleMouseAction. Any other event is ignored.
func (v *View) HandleEvent(event tcell.Event) bool {
	switch e := event.(type) {
	case *tcell.EventKey:
		if v.inputCapture != nil {
			if e = v.inputCapture(e); e == nil {
				return true
			}
		}
		v.handleKeyEvent(e)
		return true
	case *tcell.EventMouse:
		return v.HandleMouse(e)
	}

	return false
}

// handleKeyEvent applies a key event to the view. It is what HandleEvent
// calls once the input capture has had its say.
func (v *View) handleKeyEvent(e *tcell.EventKey) {
	// This bool determines whether the view is relocated at the end of the function
	// By default it's true because most events should cause a relocate
	relocate := true

	autocompleteBefore := v.autocompleteSnapshot()
	shouldSyncAutocomplete := v.autocomplete.active || v.shouldRefreshAutocompleteForKey(e)
	if v.handleAutocompleteKey(e) {
		if relocate {
			v.Relocate()
			v.Relocate()
		}
		return
	}

	// Check first if input is a key binding, if it is we 'eat' the input and don't insert a rune
	isBinding := false
	for key, actions := range v.bindings {

		if e.Key() == key.KeyCode {
			if e.Key() == tcell.KeyRune {
				if e.Str() != string(key.R) {
					continue
				}
			}
			if e.Modifiers() == key.Modifiers {
				for _, c := range v.Buf.cursors {
					ok := v.SetCursor(c)
					if !ok {
						break
					}
					relocate = false
					isBinding = true
					relocate = v.ExecuteActions(actions) || relocate
				}
				v.SetCursor(&v.Buf.Cursor)
				v.Buf.MergeCursors()
				break
			}
		}
	}

	if !isBinding && e.Key() == tcell.KeyRune {
		// Check viewtype if readonly don't insert a rune (readonly help and log view etc.)
		if v.Readonly == false {
			for _, c := range v.Buf.cursors {
				v.SetCursor(c)

				// Insert a character
				if v.Cursor.HasSelection() {
					v.Cursor.DeleteSelection()
					v.Cursor.ResetSelection()
				}

				if v.isOverwriteMode {
					next := v.Cursor.Loc
					next.X++
					v.Buf.Replace(v.Cursor.Loc, next, e.Str())
				} else {
					v.Buf.Insert(v.Cursor.Loc, e.Str())
				}
			}
			v.SetCursor(&v.Buf.Cursor)
		}
	}

	if shouldSyncAutocomplete {
		v.syncAutocomplete(autocompleteBefore, false)
	}

	if relocate {
		v.Relocate()
		// We run relocate again because there's a bug with relocating with softwrap
		// when for example you jump to the bottom of the buffer and it tries to
		// calculate where to put the topline so that the bottom line is at the bottom
		// of the terminal and it runs into problems with visual lines vs real lines.
		// This is (hopefully) a temporary solution
		v.Relocate()
	}
}

func (v *View) mainCursor() bool {
	return v.Buf.curCursor == len(v.Buf.cursors)-1
}

func (v *View) getLineNumWidth() int {
	if settingAs(v.Buf.Settings, "ruler", true) {
		// We need to know the string length of the largest line number
		// so we can pad appropriately when displaying line numbers
		return len(strconv.Itoa(v.Buf.NumLines)) + 1
	}
	return 0
}

// displayView draws the view to the screen
func (v *View) displayView(screen Screen) {
	if settingAs(v.Buf.Settings, "softwrap", false) && v.leftCol != 0 {
		v.leftCol = 0
	}
	v.cursorVisible = false

	maxLineNumLength := len(strconv.Itoa(v.Buf.NumLines))
	v.lineNumOffset = v.getLineNumWidth()

	xOffset := v.x + v.lineNumOffset
	yOffset := v.y

	height := v.height
	width := v.width
	left := v.leftCol
	top := v.Topline

	v.cellview.Draw(v.Buf, v.colorscheme, top, height, left, width-v.lineNumOffset)

	screenX := v.x
	realLineN := top - 1
	visualLineN := 0
	var line []*Char
	for visualLineN, line = range v.cellview.lines {
		var firstChar *Char
		if len(line) > 0 {
			firstChar = line[0]
		}

		var softwrapped bool
		if firstChar != nil {
			if firstChar.realLoc.Y == realLineN {
				softwrapped = true
			}
			realLineN = firstChar.realLoc.Y
		} else {
			realLineN++
		}

		defStyle := v.colorscheme.GetDefault()
		colorcolumn := int(settingAs(v.Buf.Settings, "colorcolumn", float64(0)))
		if colorcolumn != 0 && xOffset+colorcolumn-v.leftCol < v.width {
			style := v.colorscheme.GetColor("color-column")
			fg := style.GetForeground()
			st := defStyle.Background(fg)
			screen.SetContent(xOffset+colorcolumn-v.leftCol, yOffset+visualLineN, ' ', nil, st)
		}

		screenX = v.x

		lineNumStyle := defStyle
		if v.Buf.Settings["ruler"] == true {
			// Write the line number
			if style, ok := v.colorscheme["line-number"]; ok {
				lineNumStyle = style
			}
			if style, ok := v.colorscheme["current-line-number"]; ok {
				if realLineN == v.Cursor.Y && !v.Cursor.HasSelection() {
					lineNumStyle = style
				}
			}

			lineNum := strconv.Itoa(realLineN + 1)

			// Write the spaces before the line number if necessary
			for i := 0; i < maxLineNumLength-len(lineNum); i++ {
				screen.SetContent(screenX, yOffset+visualLineN, ' ', nil, lineNumStyle)
				screenX++
			}
			if softwrapped && visualLineN != 0 {
				// Pad without the line number because it was written on the visual line before
				for range lineNum {
					screen.SetContent(screenX, yOffset+visualLineN, ' ', nil, lineNumStyle)
					screenX++
				}
			} else {
				// Write the actual line number
				for _, ch := range lineNum {
					screen.SetContent(screenX, yOffset+visualLineN, ch, nil, lineNumStyle)
					screenX++
				}
			}

			// Write the extra space
			screen.SetContent(screenX, yOffset+visualLineN, ' ', nil, lineNumStyle)
			screenX++
		}

		hasFocus := v.HasFocus()
		showCursors := !settingAs(v.Buf.Settings, "hidecursoronblur", false) || hasFocus

		showWhitespace := settingAs(v.Buf.Settings, "showwhitespace", false)
		tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))

		var lastChar *Char
		cursorSet := false
		for i, char := range line {
			if char != nil {
				lineStyle := char.style

				if colorcolumn != 0 && char.visualLoc.X == colorcolumn {
					style := v.colorscheme.GetColor("color-column")
					fg := style.GetForeground()
					lineStyle = lineStyle.Background(fg)
				}

				charLoc := char.realLoc

				if showCursors {
					for _, c := range v.Buf.cursors {
						v.SetCursor(c)
						if v.Cursor.HasSelection() &&
							(charLoc.GreaterEqual(v.Cursor.CurSelection[0]) && charLoc.LessThan(v.Cursor.CurSelection[1]) ||
								charLoc.LessThan(v.Cursor.CurSelection[0]) && charLoc.GreaterEqual(v.Cursor.CurSelection[1])) {
							// The current character is selected
							lineStyle = defStyle.Reverse(true)

							if style, ok := v.colorscheme["selection"]; ok {
								lineStyle = style
							}
						}
					}
					v.SetCursor(&v.Buf.Cursor)

					if settingAs(v.Buf.Settings, "cursorline", true) &&
						!v.Cursor.HasSelection() && v.Cursor.Y == realLineN {
						style := v.colorscheme.GetColor("cursor-line")
						fg := style.GetForeground()
						lineStyle = lineStyle.Background(fg)
					}
				}

				drawChar := char.drawChar
				if showWhitespace {
					charChar := char.char
					if charChar == '\t' {
						if i%tabsize == 0 {
							drawChar = '\u2192'
						}
					} else {
						if charChar == ' ' {
							drawChar = '\u00B7'
						}
					}
				}
				screen.SetContent(xOffset+char.visualLoc.X, yOffset+char.visualLoc.Y, drawChar, nil, lineStyle)

				if showCursors {
					for i, c := range v.Buf.cursors {
						v.SetCursor(c)
						if !v.Cursor.HasSelection() &&
							v.Cursor.Y == char.realLoc.Y && v.Cursor.X == char.realLoc.X && (!cursorSet || i != 0) {
							if i == 0 {
								v.cursorScreenX = xOffset + char.visualLoc.X
								v.cursorScreenY = yOffset + char.visualLoc.Y
								v.cursorVisible = true
							}
							v.showMultiCursor(screen, xOffset+char.visualLoc.X, yOffset+char.visualLoc.Y, i)
							cursorSet = true
						}
					}
					v.SetCursor(&v.Buf.Cursor)
				}

				lastChar = char
			}
		}

		if showCursors {
			lastX := 0
			var realLoc Loc
			var visualLoc Loc
			var cx, cy int
			if lastChar != nil {
				lastX = xOffset + lastChar.visualLoc.X + lastChar.width
				for i, c := range v.Buf.cursors {
					v.SetCursor(c)
					if !v.Cursor.HasSelection() &&
						v.Cursor.Y == lastChar.realLoc.Y && v.Cursor.X == lastChar.realLoc.X+1 {
						if i == 0 {
							v.cursorScreenX = lastX
							v.cursorScreenY = yOffset + lastChar.visualLoc.Y
							v.cursorVisible = true
						}
						v.showMultiCursor(screen, lastX, yOffset+lastChar.visualLoc.Y, i)
						cx, cy = lastX, yOffset+lastChar.visualLoc.Y
					}
				}
				v.SetCursor(&v.Buf.Cursor)
				realLoc = Loc{lastChar.realLoc.X + 1, realLineN}
				visualLoc = Loc{lastX - xOffset, lastChar.visualLoc.Y}
			} else if len(line) == 0 {
				for i, c := range v.Buf.cursors {
					v.SetCursor(c)
					if !v.Cursor.HasSelection() &&
						v.Cursor.Y == realLineN {
						if i == 0 {
							v.cursorScreenX = xOffset
							v.cursorScreenY = yOffset + visualLineN
							v.cursorVisible = true
						}
						v.showMultiCursor(screen, xOffset, yOffset+visualLineN, i)
						cx, cy = xOffset, yOffset+visualLineN
					}
				}
				v.SetCursor(&v.Buf.Cursor)
				lastX = xOffset
				realLoc = Loc{0, realLineN}
				visualLoc = Loc{0, visualLineN}
			}

			if v.Cursor.HasSelection() &&
				(realLoc.GreaterEqual(v.Cursor.CurSelection[0]) && realLoc.LessThan(v.Cursor.CurSelection[1]) ||
					realLoc.LessThan(v.Cursor.CurSelection[0]) && realLoc.GreaterEqual(v.Cursor.CurSelection[1])) {
				// The current character is selected
				selectStyle := defStyle.Reverse(true)

				if style, ok := v.colorscheme["selection"]; ok {
					selectStyle = style
				}
				screen.SetContent(xOffset+visualLoc.X, yOffset+visualLoc.Y, ' ', nil, selectStyle)
			}

			if settingAs(v.Buf.Settings, "cursorline", true) &&
				!v.Cursor.HasSelection() && v.Cursor.Y == realLineN {
				for i := lastX; i < xOffset+v.width-v.lineNumOffset; i++ {
					style := v.colorscheme.GetColor("cursor-line")
					fg := style.GetForeground()
					style = style.Background(fg)
					if !(!v.Cursor.HasSelection() && i == cx && yOffset+visualLineN == cy) {
						screen.SetContent(i, yOffset+visualLineN, ' ', nil, style)
					}
				}
			}
		}
	}
}

// ShowMultiCursor will display a cursor at a location
// If i == 0 then the terminal cursor will be used
// Otherwise a fake cursor will be drawn at the position
func (v *View) showMultiCursor(screen Screen, x, y, i int) {
	if i == 0 && v.HasFocus() {
		screen.ShowCursor(x, y)
	} else {
		rs, _, _ := screen.Get(x, y)
		var cur rune
		for _, r := range rs {
			cur = r
			break
		}
		defStyle := v.colorscheme.GetDefault()
		screen.SetContent(x, y, cur, nil, defStyle.Reverse(true))
	}
}

// Draw renders the view and the cursor onto the screen.
//
// If SetRect was never called, the view expands to fill the whole screen.
func (v *View) Draw(screen Screen) {
	if v.width <= 0 || v.height <= 0 {
		screenWidth, screenHeight := screen.Size()
		v.x, v.y, v.width, v.height = 0, 0, screenWidth, screenHeight
	}

	// TODO(pdg): just clear from the last line down.
	defStyle := v.colorscheme.GetDefault()
	for y := v.y; y < v.y+v.height; y++ {
		for x := v.x; x < v.x+v.width; x++ {
			screen.SetContent(x, y, ' ', nil, defStyle)
		}
	}

	v.displayView(screen)
	v.lastScreen = screen
	v.drawAutocompleteSuggestion(screen)

	// Don't draw the cursor if it is out of the viewport or if it has a selection
	if v.Cursor.Y-v.Topline < 0 || v.Cursor.Y-v.Topline > v.height-1 || v.Cursor.HasSelection() {
		screen.HideCursor()
	}

	if settingAs(v.Buf.Settings, "scrollbar", false) {
		v.scrollbar.Display(screen)
	}

	v.drawAutocomplete(screen)
}
