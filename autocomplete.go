package editor

import (
	"sort"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/mattn/go-runewidth"
)

const (
	defaultAutocompleteMaxItems    = 32
	defaultAutocompleteVisibleRows = 8
)

// CompletionItem represents one autocomplete candidate.
type CompletionItem struct {
	Label        string
	InsertText   string
	InlineText   string
	Detail       string
	ReplaceStart *Loc
	ReplaceEnd   *Loc
}

// CompletionContext describes the editor state for autocomplete providers.
type CompletionContext struct {
	Buffer    *Buffer
	View      *View
	Cursor    Loc
	WordStart Loc
	WordEnd   Loc
	Prefix    string
	Word      string
	Line      string
}

// AutocompleteProvider returns completion candidates for the current context.
type AutocompleteProvider func(ctx CompletionContext) []CompletionItem

type autocompleteRect struct {
	x, y, width, height int
}

type autocompleteState struct {
	active   bool
	items    []CompletionItem
	selected int
	offset   int
	context  CompletionContext
	rect     autocompleteRect
}

type autocompleteSyncState struct {
	usable    bool
	cursor    Loc
	selection [2]Loc
	line      string
}

// isAutocompleteWordChar extends IsWordChar to include '-' so that
// hyphenated identifiers like "text-align" are treated as single words
// in autocomplete context.  Cursor-movement word boundaries are not
// affected.
func isAutocompleteWordChar(str string) bool {
	if IsWordChar(str) {
		return true
	}
	if len(str) == 1 && str[0] == '-' {
		return true
	}
	return false
}

// DefaultAutocompleteProvider suggests distinct words from the current buffer.
func DefaultAutocompleteProvider(ctx CompletionContext) []CompletionItem {
	prefix := ctx.Prefix
	if prefix == "" {
		return nil
	}

	seen := make(map[string]struct{})
	items := make([]CompletionItem, 0, defaultAutocompleteMaxItems)
	prefixLower := strings.ToLower(prefix)

	appendWord := func(word string) {
		if word == "" || word == prefix {
			return
		}
		if _, ok := seen[word]; ok {
			return
		}
		if !strings.HasPrefix(word, prefix) && !strings.HasPrefix(strings.ToLower(word), prefixLower) {
			return
		}
		seen[word] = struct{}{}
		items = append(items, CompletionItem{Label: word, InsertText: word})
	}

	for lineN := 0; lineN < ctx.Buffer.NumLines && len(items) < defaultAutocompleteMaxItems; lineN++ {
		line := []rune(ctx.Buffer.Line(lineN))
		for i := 0; i < len(line) && len(items) < defaultAutocompleteMaxItems; {
			if !isAutocompleteWordChar(string(line[i])) {
				i++
				continue
			}

			start := i
			for i < len(line) && isAutocompleteWordChar(string(line[i])) {
				i++
			}
			appendWord(string(line[start:i]))
		}
	}

	sort.Slice(items, func(i, j int) bool {
		li := strings.ToLower(items[i].Label)
		lj := strings.ToLower(items[j].Label)
		if len(li) == len(lj) {
			return li < lj
		}
		return len(li) < len(lj)
	})

	return items
}

// SetAutocompleteProvider configures how this view produces completion items.
// Passing nil disables autocomplete.
func (v *View) SetAutocompleteProvider(provider AutocompleteProvider) {
	v.autocompleteProvider = provider
	if provider == nil {
		v.closeAutocomplete()
	}
}

// TriggerAutocomplete opens or refreshes the autocomplete popup at the current cursor.
func (v *View) TriggerAutocomplete() bool {
	return v.refreshAutocomplete(true)
}

func (v *View) closeAutocomplete() {
	v.autocomplete.active = false
	v.autocomplete.items = nil
	v.autocomplete.selected = 0
	v.autocomplete.offset = 0
	v.autocomplete.rect = autocompleteRect{}
	v.autocomplete.context = CompletionContext{}
}

func (v *View) shouldUseAutocomplete() bool {
	return v.autocompleteProvider != nil && !v.Readonly && len(v.Buf.cursors) == 1 && !v.Cursor.HasSelection()
}

func (v *View) autocompleteContext() (CompletionContext, bool) {
	if !v.shouldUseAutocomplete() || v.Cursor.Y < 0 || v.Cursor.Y >= v.Buf.NumLines {
		return CompletionContext{}, false
	}

	lineRunes := []rune(v.Buf.Line(v.Cursor.Y))
	cursorX := v.Cursor.X
	if cursorX < 0 {
		cursorX = 0
	}
	if cursorX > len(lineRunes) {
		cursorX = len(lineRunes)
	}

	start := cursorX
	for start > 0 && isAutocompleteWordChar(string(lineRunes[start-1])) {
		start--
	}

	end := cursorX
	for end < len(lineRunes) && isAutocompleteWordChar(string(lineRunes[end])) {
		end++
	}

	return CompletionContext{
		Buffer:    v.Buf,
		View:      v,
		Cursor:    v.Cursor.Loc,
		WordStart: Loc{X: start, Y: v.Cursor.Y},
		WordEnd:   Loc{X: end, Y: v.Cursor.Y},
		Prefix:    string(lineRunes[start:cursorX]),
		Word:      string(lineRunes[start:end]),
		Line:      string(lineRunes),
	}, true
}

func (v *View) refreshAutocomplete(force bool) bool {
	ctx, ok := v.autocompleteContext()
	if !ok {
		v.closeAutocomplete()
		return false
	}
	if !force && ctx.Prefix == "" {
		v.closeAutocomplete()
		return false
	}

	items := v.autocompleteProvider(ctx)
	if len(items) == 0 {
		v.closeAutocomplete()
		return false
	}

	previousLabel := ""
	if v.autocomplete.active && v.autocomplete.selected >= 0 && v.autocomplete.selected < len(v.autocomplete.items) {
		previousLabel = v.autocomplete.items[v.autocomplete.selected].Label
	}

	normalized := make([]CompletionItem, 0, len(items))
	for _, item := range items {
		if item.Label == "" {
			if item.InsertText == "" {
				continue
			}
			item.Label = item.InsertText
		}
		if item.InsertText == "" {
			item.InsertText = item.Label
		}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		v.closeAutocomplete()
		return false
	}

	v.autocomplete.active = true
	v.autocomplete.items = normalized
	v.autocomplete.context = ctx
	v.autocomplete.selected = 0
	for i, item := range normalized {
		if item.Label == previousLabel {
			v.autocomplete.selected = i
			break
		}
	}
	v.clampAutocompleteSelection()
	return true
}

func (v *View) autocompleteSnapshot() autocompleteSyncState {
	state := autocompleteSyncState{
		cursor:    v.Cursor.Loc,
		selection: v.Cursor.CurSelection,
	}
	if !v.shouldUseAutocomplete() || v.Cursor.Y < 0 || v.Cursor.Y >= v.Buf.NumLines {
		return state
	}
	state.usable = true
	state.line = v.Buf.Line(v.Cursor.Y)
	return state
}

func (v *View) syncAutocomplete(previous autocompleteSyncState, force bool) bool {
	current := v.autocompleteSnapshot()
	if !current.usable {
		wasActive := v.autocomplete.active
		v.closeAutocomplete()
		return wasActive
	}
	if !force && current == previous {
		return false
	}
	return v.refreshAutocomplete(force)
}

func (v *View) autocompleteVisibleRows() int {
	rows := defaultAutocompleteVisibleRows
	if v.height-2 < rows {
		rows = v.height - 2
	}
	if rows < 1 {
		rows = 1
	}
	if len(v.autocomplete.items) < rows {
		rows = len(v.autocomplete.items)
	}
	return rows
}

func (v *View) clampAutocompleteSelection() {
	if len(v.autocomplete.items) == 0 {
		v.autocomplete.selected = 0
		v.autocomplete.offset = 0
		return
	}
	if v.autocomplete.selected < 0 {
		v.autocomplete.selected = 0
	}
	if v.autocomplete.selected >= len(v.autocomplete.items) {
		v.autocomplete.selected = len(v.autocomplete.items) - 1
	}

	visibleRows := v.autocompleteVisibleRows()
	if v.autocomplete.offset > v.autocomplete.selected {
		v.autocomplete.offset = v.autocomplete.selected
	}
	if v.autocomplete.selected >= v.autocomplete.offset+visibleRows {
		v.autocomplete.offset = v.autocomplete.selected - visibleRows + 1
	}
	maxOffset := len(v.autocomplete.items) - visibleRows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.autocomplete.offset > maxOffset {
		v.autocomplete.offset = maxOffset
	}
	if v.autocomplete.offset < 0 {
		v.autocomplete.offset = 0
	}
}

func (v *View) moveAutocompleteSelection(delta int) bool {
	if !v.autocomplete.active || len(v.autocomplete.items) == 0 {
		return false
	}
	v.autocomplete.selected += delta
	v.clampAutocompleteSelection()
	return true
}

func (v *View) selectedAutocompleteItem() (CompletionItem, bool) {
	if !v.autocomplete.active || len(v.autocomplete.items) == 0 {
		return CompletionItem{}, false
	}
	if v.autocomplete.selected < 0 || v.autocomplete.selected >= len(v.autocomplete.items) {
		return CompletionItem{}, false
	}
	return v.autocomplete.items[v.autocomplete.selected], true
}

func (v *View) autocompleteInlineText() (string, bool) {
	if !v.autocomplete.active || !v.cursorVisible || !v.shouldUseAutocomplete() {
		return "", false
	}

	item, ok := v.selectedAutocompleteItem()
	if !ok {
		return "", false
	}
	if item.InlineText != "" {
		return item.InlineText, true
	}

	start := v.autocomplete.context.WordStart
	end := v.autocomplete.context.WordEnd
	if item.ReplaceStart != nil {
		start = *item.ReplaceStart
	}
	if item.ReplaceEnd != nil {
		end = *item.ReplaceEnd
	}
	if start.GreaterThan(end) {
		start, end = end, start
	}
	if start.Y != v.Cursor.Y || end.Y != v.Cursor.Y || v.Cursor.X < start.X {
		return "", false
	}

	line := []rune(v.Buf.Line(v.Cursor.Y))
	if start.X < 0 {
		start.X = 0
	}
	if end.X < 0 {
		end.X = 0
	}
	if start.X > len(line) {
		start.X = len(line)
	}
	if end.X > len(line) {
		end.X = len(line)
	}
	cursorX := v.Cursor.X
	if cursorX > len(line) {
		cursorX = len(line)
	}
	if cursorX < start.X || end.X < cursorX {
		return "", false
	}
	if string(line[start.X:end.X]) == item.InsertText {
		return "", false
	}

	prefixLen := cursorX - start.X
	insertRunes := []rune(item.InsertText)
	if prefixLen < 0 || prefixLen > len(insertRunes) {
		return "", false
	}
	if string(insertRunes[:prefixLen]) != string(line[start.X:cursorX]) {
		return "", false
	}

	inlineText := string(insertRunes[prefixLen:])
	if inlineText == "" {
		return "", false
	}
	return inlineText, true
}

// blendColor mixes fg toward bg by the given fraction (0.0 = pure fg, 1.0 = pure bg).
// This produces a "transparent" version of fg as if seen through a semi-transparent layer.
func blendColor(fg, bg tcell.Color, frac float64) tcell.Color {
	fr, fg2, fb := fg.TrueColor().RGB()
	br, bg2, bb := bg.TrueColor().RGB()
	mix := func(a, b int32) int32 {
		return a + int32(float64(b-a)*frac)
	}
	return tcell.NewRGBColor(mix(fr, br), mix(fg2, bg2), mix(fb, bb))
}

func (v *View) autocompleteSuggestionStyle(existing tcell.Style) tcell.Style {
	// Determine the foreground from the cell just before the cursor, which
	// carries the syntax-highlighting color of the token being completed.
	fg := v.colorscheme.GetDefault().GetForeground()
	if v.cursorVisible && v.cursorScreenX > 0 && v.lastScreen != nil {
		_, prevStyle, _ := v.lastScreen.Get(v.cursorScreenX-1, v.cursorScreenY)
		if prevFg := prevStyle.GetForeground(); prevFg != existing.GetBackground() {
			fg = prevFg
		}
	}

	// Blend the foreground 50 % toward the background to create a visibly
	// faded "transparent" color, instead of relying on the Dim attribute
	// which many terminals render identically to normal text.
	bg := existing.GetBackground()
	faded := blendColor(fg, bg, 0.5)
	return existing.Foreground(faded)
}

func (v *View) drawAutocompleteSuggestion(screen Screen) {
	inlineText, ok := v.autocompleteInlineText()
	if !ok {
		return
	}

	innerX, innerY, innerWidth, innerHeight := v.Rect()
	if innerWidth <= 0 || innerHeight <= 0 {
		return
	}

	x := v.cursorScreenX
	y := v.cursorScreenY
	if x < innerX || x >= innerX+innerWidth || y < innerY || y >= innerY+innerHeight {
		return
	}

	tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))
	visualX := v.Cursor.GetVisualX()
	indentRunes := []rune(settingAs(v.Buf.Settings, "indentchar", " "))
	ghostTabRune := ' '
	if len(indentRunes) > 0 && !IsStrWhitespace(string(indentRunes[0])) {
		ghostTabRune = indentRunes[0]
	}

	for _, r := range inlineText {
		if r == '\n' || y >= innerY+innerHeight || x >= innerX+innerWidth {
			break
		}

		cellRune := r
		cellWidth := runewidth.RuneWidth(r)
		if r == '\t' {
			cellRune = ghostTabRune
			cellWidth = tabsize - (visualX % tabsize)
		}
		if cellWidth < 1 {
			cellWidth = 1
		}

		for offset := 0; offset < cellWidth && x+offset < innerX+innerWidth; offset++ {
			drawRune := cellRune
			if offset > 0 {
				drawRune = ' '
			}
			_, existingStyle, _ := screen.Get(x+offset, y)
			screen.SetContent(x+offset, y, drawRune, nil, v.autocompleteSuggestionStyle(existingStyle))
		}

		x += cellWidth
		visualX += cellWidth
	}
}

func (v *View) pageAutocompleteSelection(delta int) bool {
	if !v.autocomplete.active {
		return false
	}
	visibleRows := v.autocompleteVisibleRows()
	if visibleRows == 0 {
		return false
	}
	v.autocomplete.selected += visibleRows * delta
	v.clampAutocompleteSelection()
	return true
}

func (v *View) acceptAutocomplete() bool {
	if !v.autocomplete.active || len(v.autocomplete.items) == 0 {
		return false
	}

	item := v.autocomplete.items[v.autocomplete.selected]
	start := v.autocomplete.context.WordStart
	end := v.autocomplete.context.WordEnd
	if item.ReplaceStart != nil {
		start = *item.ReplaceStart
	}
	if item.ReplaceEnd != nil {
		end = *item.ReplaceEnd
	}
	if start.GreaterThan(end) {
		start, end = end, start
	}

	v.Buf.MultipleReplace([]Delta{{
		Text:  []byte(item.InsertText),
		Start: start,
		End:   end,
	}})
	v.Cursor.GotoLoc(start.Move(Count(item.InsertText), v.Buf))
	v.Cursor.ResetSelection()
	v.closeAutocomplete()
	return true
}

func (v *View) handleAutocompleteKey(event *tcell.EventKey) bool {
	if !v.autocomplete.active {
		return false
	}

	switch event.Key() {
	case tcell.KeyUp:
		return v.moveAutocompleteSelection(-1)
	case tcell.KeyDown:
		return v.moveAutocompleteSelection(1)
	case tcell.KeyPgUp:
		return v.pageAutocompleteSelection(-1)
	case tcell.KeyPgDn:
		return v.pageAutocompleteSelection(1)
	case tcell.KeyEnter, tcell.KeyTab:
		return v.acceptAutocomplete()
	case tcell.KeyEscape:
		v.closeAutocomplete()
		return true
	}

	return false
}

func (v *View) shouldRefreshAutocompleteForKey(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyRune:
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete:
		return true
	}
	return false
}

func (v *View) shouldCloseAutocompleteForKey(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyRune, tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyDelete:
		return false
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyEnter, tcell.KeyTab, tcell.KeyEscape:
		return false
	}
	return true
}

func (v *View) autocompletePopupRect() (autocompleteRect, bool) {
	if !v.autocomplete.active || !v.cursorVisible || len(v.autocomplete.items) == 0 {
		return autocompleteRect{}, false
	}

	innerX, innerY, innerWidth, innerHeight := v.Rect()
	if innerWidth < 2 || innerHeight < 1 {
		return autocompleteRect{}, false
	}

	visibleRows := v.autocompleteVisibleRows()
	if visibleRows == 0 {
		return autocompleteRect{}, false
	}

	contentWidth := 0
	for _, item := range v.autocomplete.items {
		text := item.Label
		if item.Detail != "" {
			text += " — " + item.Detail
		}
		if w := StringWidth(text, int(settingAs(v.Buf.Settings, "tabsize", float64(4)))); w > contentWidth {
			contentWidth = w
		}
	}

	width := contentWidth + 2
	if width > innerWidth {
		width = innerWidth
	}
	if width < 2 {
		width = 2
	}

	height := visibleRows
	if height > innerHeight {
		height = innerHeight
	}

	tabsize := int(settingAs(v.Buf.Settings, "tabsize", float64(4)))

	x := v.cursorScreenX
	// Anchor the popup at the word start rather than the cursor position
	// so the dropdown aligns with the beginning of the word being completed.
	if v.autocomplete.context.Prefix != "" {
		x -= StringWidth(v.autocomplete.context.Prefix, tabsize)
	}
	if x+width > innerX+innerWidth {
		x = innerX + innerWidth - width
	}
	if x < innerX {
		x = innerX
	}

	y := v.cursorScreenY + 1
	if y+height > innerY+innerHeight {
		y = v.cursorScreenY - height
	}
	if y < innerY {
		y = innerY
	}
	if y+height > innerY+innerHeight {
		y = innerY + innerHeight - height
	}

	return autocompleteRect{x: x, y: y, width: width, height: height}, true
}

func (v *View) drawAutocomplete(screen Screen) {
	// When there is only one completion candidate and the inline ghost text
	// is already visible, skip the popup entirely.  The ghost text provides
	// the visual cue; the popup would only overlay the line below and create
	// confusing visual artifacts (e.g. a padding space that looks like a
	// character insertion in the underlying content).
	if len(v.autocomplete.items) == 1 {
		if _, ok := v.autocompleteInlineText(); ok {
			v.autocomplete.rect = autocompleteRect{}
			return
		}
	}

	popup, ok := v.autocompletePopupRect()
	if !ok {
		v.autocomplete.rect = autocompleteRect{}
		return
	}
	v.autocomplete.rect = popup

	popupStyle := v.colorscheme.GetDefault().Reverse(true)
	selectedStyle := v.colorscheme.GetDefault()
	if sel, ok := v.colorscheme["selection"]; ok {
		selectedStyle = sel
	}

	for y := popup.y; y < popup.y+popup.height; y++ {
		for x := popup.x; x < popup.x+popup.width; x++ {
			screen.SetContent(x, y, ' ', nil, popupStyle)
		}
	}

	rows := popup.height
	for row := 0; row < rows; row++ {
		itemIndex := v.autocomplete.offset + row
		if itemIndex >= len(v.autocomplete.items) {
			break
		}

		item := v.autocomplete.items[itemIndex]
		lineStyle := popupStyle
		if itemIndex == v.autocomplete.selected {
			lineStyle = selectedStyle
		}
		lineY := popup.y + row
		for x := popup.x; x < popup.x+popup.width; x++ {
			screen.SetContent(x, lineY, ' ', nil, lineStyle)
		}

		text := item.Label
		if item.Detail != "" {
			text += " — " + item.Detail
		}
		printStyle(screen, text, popup.x+1, lineY, popup.width-1, lineStyle)
	}
}

func (v *View) autocompleteItemAt(x, y int) (int, bool) {
	popup := v.autocomplete.rect
	if !v.autocomplete.active || popup.width == 0 || popup.height == 0 {
		return 0, false
	}
	if x < popup.x || x >= popup.x+popup.width || y < popup.y || y >= popup.y+popup.height {
		return 0, false
	}
	index := v.autocomplete.offset + (y - popup.y)
	if index < 0 || index >= len(v.autocomplete.items) {
		return 0, false
	}
	return index, true
}

func (v *View) handleAutocompleteMouse(action MouseAction, event *tcell.EventMouse) bool {
	if !v.autocomplete.active {
		return false
	}
	x, y := event.Position()
	index, ok := v.autocompleteItemAt(x, y)
	if !ok {
		return false
	}

	switch action {
	case MouseLeftDown:
		v.autocomplete.selected = index
		v.clampAutocompleteSelection()
		return true
	case MouseLeftUp:
		v.autocomplete.selected = index
		v.clampAutocompleteSelection()
		return v.acceptAutocomplete()
	case MouseScrollUp:
		return v.moveAutocompleteSelection(-1)
	case MouseScrollDown:
		return v.moveAutocompleteSelection(1)
	}

	return false
}
