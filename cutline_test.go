package editor

import (
	"strings"
	"sync"
	"testing"
	"time"
)

var cutlineClipMu sync.Mutex

// TestCutLineStartsFreshAfterTimeout reproduces a correctness bug in CutLine
// where the time-since-last-cut comparison used the wrong type arithmetic:
//
//	time.Since(v.lastCutTime)/time.Second > 10*time.Second
//
// Dividing a time.Duration by time.Second produces another time.Duration whose
// value is the elapsed seconds expressed in nanoseconds (e.g. 15 for 15 s).
// That tiny Duration is then compared against 10*time.Second (10 000 000 000 ns),
// so the condition is false for any elapsed time shorter than ~317 years.
//
// As a consequence, when freshClip is true and more than 10 seconds have passed
// since the last cut, CutLine entered the append branch instead of starting a
// fresh copy.  The fix restructures the condition so the timeout is checked
// inside the freshClip==true branch using the correct expression:
//
//	time.Since(v.lastCutTime) > 10*time.Second
func TestCutLineStartsFreshAfterTimeout(t *testing.T) {
	cutlineClipMu.Lock()
	defer cutlineClipMu.Unlock()

	// Use the internal clipboard so we can inspect it without needing a
	// system clipboard.
	savedInternal := internalClipboard
	savedUse := useInternalClipboard
	defer func() {
		internalClipboard = savedInternal
		useInternalClipboard = savedUse
	}()
	useInternalClipboard = true
	internalClipboard = "old content\n"

	v := NewView(NewBufferFromString("line1\nline2\n", ""))
	v.Cursor.GotoLoc(Loc{X: 0, Y: 0})

	// Simulate being in the middle of a cut sequence that started > 10 s ago.
	v.freshClip = true
	v.lastCutTime = time.Now().Add(-15 * time.Second)

	v.CutLine()

	// After the timeout the clipboard must contain only the freshly cut line,
	// not "old content\n" prepended to it.
	if strings.HasPrefix(internalClipboard, "old content") {
		t.Errorf("CutLine appended to stale clipboard despite 15 s timeout; got %q", internalClipboard)
	}
}

// TestCutLineAppendsWithinTimeout verifies that rapid consecutive CutLine calls
// (within the 10-second window) accumulate lines in the clipboard.
func TestCutLineAppendsWithinTimeout(t *testing.T) {
	cutlineClipMu.Lock()
	defer cutlineClipMu.Unlock()

	savedInternal := internalClipboard
	savedUse := useInternalClipboard
	defer func() {
		internalClipboard = savedInternal
		useInternalClipboard = savedUse
	}()
	useInternalClipboard = true
	internalClipboard = ""

	v := NewView(NewBufferFromString("line1\nline2\nline3\n", ""))
	v.Cursor.GotoLoc(Loc{X: 0, Y: 0})

	// First cut: starts fresh.
	v.CutLine()
	first := internalClipboard

	// Second cut immediately after: should append.
	v.CutLine()
	second := internalClipboard

	if !strings.HasPrefix(second, first) {
		t.Errorf("expected second CutLine to append to first; first=%q second=%q", first, second)
	}
	if second == first {
		t.Errorf("expected second CutLine to add content; first=%q second=%q", first, second)
	}
}

// TestCutLineStartsFreshAfterPaste verifies that after freshClip is reset to
// false (e.g. by a paste), the next CutLine begins a new clipboard entry.
func TestCutLineStartsFreshAfterPaste(t *testing.T) {
	cutlineClipMu.Lock()
	defer cutlineClipMu.Unlock()

	savedInternal := internalClipboard
	savedUse := useInternalClipboard
	defer func() {
		internalClipboard = savedInternal
		useInternalClipboard = savedUse
	}()
	useInternalClipboard = true
	internalClipboard = "old content\n"

	v := NewView(NewBufferFromString("line1\nline2\n", ""))
	v.Cursor.GotoLoc(Loc{X: 0, Y: 0})

	// freshClip == false signals the cut sequence was interrupted (e.g. paste).
	v.freshClip = false
	v.lastCutTime = time.Now().Add(-2 * time.Second)

	v.CutLine()

	if strings.HasPrefix(internalClipboard, "old content") {
		t.Errorf("CutLine should start fresh when freshClip is false; got %q", internalClipboard)
	}
}
