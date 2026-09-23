package editor

import (
	"strings"
	"testing"
)

// TestLineArrayString reproduces a compatibility issue in LineArray.String():
// it used += string(l.data) inside a loop, which is O(n²) in total bytes
// because each concatenation allocates and copies the entire accumulated string.
// For a large buffer this causes gigabytes of intermediate allocations.
// The fix replaces the loop with a strings.Builder, making it O(n).
//
// The correctness assertions below also serve as regression tests ensuring
// the refactor does not change the output.
func TestLineArrayString(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"single line no newline", "hello"},
		{"single line with newline", "hello\n"},
		{"two lines", "hello\nworld"},
		{"two lines trailing newline", "hello\nworld\n"},
		{"three lines", "line1\nline2\nline3"},
		{"unicode", "héllo\nwörld\n日本語"},
		{"blank lines", "\n\nfoo\n\nbar\n"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			buf := NewBufferFromString(tc.input, "")

			got := buf.LineArray.String()

			// String() must round-trip the content exactly: the internal
			// LineArray representation preserves trailing newlines via an
			// empty final line, so the output equals the input verbatim.
			want := tc.input
			if got != want {
				t.Errorf("String() = %q, want %q", got, want)
			}
		})
	}
}

// TestLineArrayStringLarge verifies that String() on a large buffer completes
// in a reasonable time and produces the correct output.
// With O(n²) concatenation a 10 000-line buffer allocates roughly 500 MB of
// intermediate strings; the strings.Builder implementation allocates only the
// final result.
func TestLineArrayStringLarge(t *testing.T) {
	const numLines = 10_000
	const lineLen = 80

	line := strings.Repeat("x", lineLen)
	var sb strings.Builder
	for i := 0; i < numLines; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
	}
	// Build without a trailing newline so the round-trip is exact.
	input := sb.String()

	buf := NewBufferFromString(input, "")

	got := buf.LineArray.String()
	if got != input {
		prefix := got
		if len(prefix) > 80 {
			prefix = prefix[:80]
		}
		t.Errorf("String() round-trip failed for large input (first 80 chars): %q", prefix)
	}
}

// TestLineArraySaveString reproduces the same O(n²) issue in SaveString():
// it also concatenated strings in a loop.  The fix uses strings.Builder.
//
// SaveString differs from String() in that it honours the useCrlf flag,
// emitting "\r\n" line endings when true.
func TestLineArraySaveString(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		useCrlf bool
		want    string
	}{
		{"empty lf", "", false, ""},
		{"empty crlf", "", true, ""},
		{"single line lf", "hello\n", false, "hello\n"},
		{"single line crlf", "hello\n", true, "hello\r\n"},
		{"two lines lf", "hello\nworld\n", false, "hello\nworld\n"},
		{"two lines crlf", "hello\nworld\n", true, "hello\r\nworld\r\n"},
		{"three lines lf", "a\nb\nc\n", false, "a\nb\nc\n"},
		{"three lines crlf", "a\nb\nc\n", true, "a\r\nb\r\nc\r\n"},
		{"blank lines lf", "\n\nfoo\n", false, "\n\nfoo\n"},
		{"blank lines crlf", "\n\nfoo\n", true, "\r\n\r\nfoo\r\n"},
		{"no trailing newline lf", "a\nb", false, "a\nb"},
		{"no trailing newline crlf", "a\nb", true, "a\r\nb"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			buf := NewBufferFromString(tc.input, "")
			got := buf.LineArray.SaveString(tc.useCrlf)
			if got != tc.want {
				t.Errorf("SaveString(%v) = %q, want %q", tc.useCrlf, got, tc.want)
			}
		})
	}
}

// TestLineArraySaveStringMatchesBytes verifies that SaveString and Bytes
// produce identical content (as string vs []byte).
func TestLineArraySaveStringMatchesBytes(t *testing.T) {
	inputs := []string{
		"",
		"hello\n",
		"hello\nworld\n",
		"a\nb\nc",
		"unicode: 日本語\n",
	}
	for _, input := range inputs {
		for _, crlf := range []bool{false, true} {
			buf := NewBufferFromString(input, "")
			fromSave := buf.LineArray.SaveString(crlf)
			fromBytes := string(buf.LineArray.Bytes(crlf))
			if fromSave != fromBytes {
				t.Errorf("SaveString(%v) != string(Bytes(%v)) for input %q: %q vs %q",
					crlf, crlf, input, fromSave, fromBytes)
			}
		}
	}
}
