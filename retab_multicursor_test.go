package editor

import (
	"strings"
	"testing"
)

// --- editor#9: Retab() writes directly to LineArray.lines[].data, completely
// bypassing the EventHandler.Insert/Remove mechanism. This means the change
// cannot be undone (Ctrl+Z does nothing) and multi-cursor positions are not
// updated.
func TestRetab_IsUndoable(t *testing.T) {
	// Create a buffer with tab-indented content.
	buf := NewBufferFromString("\thello\n\tworld\n", "")
	buf.Settings["tabstospaces"] = true
	buf.Settings["tabsize"] = float64(4)
	v := NewView(buf)

	// Record the original content.
	original := buf.String()

	// Run Retab — should convert tabs to spaces.
	v.Retab()
	retabbed := buf.String()

	if retabbed == original {
		t.Fatal("Retab did not modify the buffer")
	}
	if strings.Contains(retabbed, "\t") {
		t.Errorf("Retab did not convert tabs to spaces: %q", retabbed)
	}

	// Undo should restore the original content.
	buf.Undo()
	afterUndo := buf.String()

	if afterUndo != original {
		t.Errorf("Undo after Retab did not restore original content.\ngot:  %q\nwant: %q", afterUndo, original)
	}
}

// Additional regression test: Retab with spaces-to-tabs should also be undoable.
func TestRetab_SpacesToTabs_IsUndoable(t *testing.T) {
	buf := NewBufferFromString("    hello\n    world\n", "")
	buf.Settings["tabstospaces"] = false
	buf.Settings["tabsize"] = float64(4)
	v := NewView(buf)

	original := buf.String()
	v.Retab()
	retabbed := buf.String()

	if retabbed == original {
		t.Fatal("Retab did not modify the buffer")
	}

	buf.Undo()
	afterUndo := buf.String()

	if afterUndo != original {
		t.Errorf("Undo after Retab did not restore original content.\ngot:  %q\nwant: %q", afterUndo, original)
	}
}

// --- editor#10: The move closure in EventHandler.Insert() only adjusts Y for
// cursors on lines after a multi-line insert. It does not adjust X for cursors
// on the SAME line as the insert start but after the insert column. When the
// last inserted line has a different length than the prefix before the insert
// point, these cursors land at wrong X positions.
func TestInsert_MultiLine_CursorOnSameLine(t *testing.T) {
	// Buffer: "abcdef" with two cursors:
	//   cursor 0 at (0,0) — main cursor out of the way
	//   cursor 1 at (5,0) — after the insertion point, on the same line
	buf := NewBufferFromString("abcdef", "")

	c0 := &buf.Cursor
	c0.Loc = Loc{0, 0}
	c0.Num = 0

	c1 := &Cursor{buf: buf, Loc: Loc{5, 0}, Num: 1}
	buf.cursors = []*Cursor{c0, c1}

	// Insert "X\nYYY" at (2,0). The inserted text has 1 char on the first
	// line and 3 chars on the second, so end.X (3) != start.X (2).
	//
	// Before: "abcdef"
	// After:  "abX\nYYYcdef"
	//   line 0: "abX"
	//   line 1: "YYYcdef"
	//
	// Cursor 1 pointed to 'f' at {5,0}. After the insert, 'f' is at {6,1}
	// because (5-2)+3 = 6 on the new line 1.
	buf.Insert(Loc{2, 0}, "X\nYYY")

	if c1.Loc.Y != 1 {
		t.Errorf("cursor 1 Y: got %d, want 1", c1.Loc.Y)
	}
	if c1.Loc.X != 6 {
		t.Errorf("cursor 1 X: got %d, want 6", c1.Loc.X)
	}
}

// The same issue exists in Remove: removing a multi-line range should
// correctly adjust X for cursors on the end line of the removal.
func TestRemove_MultiLine_CursorOnEndLine(t *testing.T) {
	// Buffer: "abX\nYYYcdef" with cursor at {6,1} pointing to 'f'.
	buf := NewBufferFromString("abX\nYYYcdef", "")

	c0 := &buf.Cursor
	c0.Loc = Loc{0, 0}
	c0.Num = 0

	c1 := &Cursor{buf: buf, Loc: Loc{6, 1}, Num: 1}
	buf.cursors = []*Cursor{c0, c1}

	// Remove from {2,0} to {3,1} — removes "X\nYYY", merging lines:
	// Result: "abcdef"
	// Cursor 1 was at {6,1} ('f'). After removal, 'f' should be at {5,0}:
	//   X = (6 - 3) + 2 = 5, Y = 1 - 1 = 0
	buf.Remove(Loc{2, 0}, Loc{3, 1})

	if c1.Loc.Y != 0 {
		t.Errorf("cursor 1 Y: got %d, want 0", c1.Loc.Y)
	}
	if c1.Loc.X != 5 {
		t.Errorf("cursor 1 X: got %d, want 5", c1.Loc.X)
	}
}

// Regression: single-line inserts should still work correctly.
func TestInsert_SingleLine_CursorAdjustment(t *testing.T) {
	buf := NewBufferFromString("abcdef", "")

	c0 := &buf.Cursor
	c0.Loc = Loc{2, 0}
	c0.Num = 0

	c1 := &Cursor{buf: buf, Loc: Loc{5, 0}, Num: 1}
	buf.cursors = []*Cursor{c0, c1}

	// Insert "XX" at (2,0) — same line, no newlines.
	// "abcdef" -> "abXXcdef"
	// Cursor 1 was at 5, should move to 7.
	buf.Insert(Loc{2, 0}, "XX")

	if c1.Loc.Y != 0 {
		t.Errorf("cursor 1 Y: got %d, want 0", c1.Loc.Y)
	}
	if c1.Loc.X != 7 {
		t.Errorf("cursor 1 X: got %d, want 7", c1.Loc.X)
	}
}

// Regression: cursors on lines AFTER a multi-line insert should have Y adjusted.
func TestInsert_MultiLine_CursorOnLaterLine(t *testing.T) {
	buf := NewBufferFromString("line0\nline1\nline2", "")

	c0 := &buf.Cursor
	c0.Loc = Loc{0, 0}
	c0.Num = 0

	c1 := &Cursor{buf: buf, Loc: Loc{3, 2}, Num: 1}
	buf.cursors = []*Cursor{c0, c1}

	// Insert a newline at (5,0) — adds one line.
	// "line0" -> "line0\n"  (line 0 stays, new empty line 1)
	// Original line 1 ("line1") becomes line 2, original line 2 becomes line 3.
	// Cursor 1 was at {3,2}, should be at {3,3}.
	buf.Insert(Loc{5, 0}, "\n")

	if c1.Loc.Y != 3 {
		t.Errorf("cursor 1 Y: got %d, want 3", c1.Loc.Y)
	}
	if c1.Loc.X != 3 {
		t.Errorf("cursor 1 X: got %d, want 3", c1.Loc.X)
	}
}
