package editor

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/malivvan/editor/highlight"
)

func runeToByteIndex(n int, txt []byte) int {
	if n == 0 {
		return 0
	}

	count := 0
	i := 0
	for len(txt) > 0 {
		_, size := utf8.DecodeRune(txt)

		txt = txt[size:]
		count += size
		i++

		if i == n {
			break
		}
	}
	return count
}

// A Line contains the data in bytes as well as a highlight state, match
// and a flag for whether the highlighting needs to be updated
type Line struct {
	data []byte

	state       highlight.State
	match       highlight.LineMatch
	rehighlight bool
}

// A LineArray simply stores and array of lines and makes it easy to insert
// and delete in it
type LineArray struct {
	lines    []Line
	initsize uint64
}

const (
	lineEndingUnknown = iota
	lineEndingLF
	lineEndingCRLF
)

// Append efficiently appends lines together
// It allocates an additional 10000 lines if the original estimate
// is incorrect
func Append(slice []Line, data ...Line) []Line {
	l := len(slice)
	if l+len(data) > cap(slice) { // reallocate
		newSlice := make([]Line, (l+len(data))+10000)
		// The copy function is predeclared and works for any slice type.
		copy(newSlice, slice)
		slice = newSlice
	}
	slice = slice[0 : l+len(data)]
	for i, c := range data {
		slice[l+i] = c
	}
	return slice
}

// NewLineArray returns a new line array from an array of bytes and the detected line ending format.
func NewLineArray(size uint64, reader io.Reader) (*LineArray, int) {
	la := new(LineArray)
	fileformat := lineEndingUnknown

	la.lines = make([]Line, 0, 1000)
	la.initsize = size

	br := bufio.NewReader(reader)
	var loaded int

	n := 0
	for {
		data, err := br.ReadBytes('\n')
		if len(data) > 1 && data[len(data)-2] == '\r' {
			data = append(data[:len(data)-2], '\n')
			if fileformat == lineEndingUnknown {
				fileformat = lineEndingCRLF
			}
		} else if len(data) > 0 {
			if fileformat == lineEndingUnknown {
				fileformat = lineEndingLF
			}
		}

		if n >= 1000 && loaded >= 0 {
			totalLinesNum := int(float64(size) * (float64(n) / float64(loaded)))
			newSlice := make([]Line, len(la.lines), totalLinesNum+10000)
			copy(newSlice, la.lines)
			la.lines = newSlice
			loaded = -1
		}

		if loaded >= 0 {
			loaded += len(data)
		}

		if err != nil {
			if err == io.EOF {
				la.lines = Append(la.lines, Line{data[:], nil, nil, false})
				// la.lines = Append(la.lines, Line{data[:len(data)]})
			}
			// Last line was read
			break
		} else {
			// la.lines = Append(la.lines, Line{data[:len(data)-1]})
			la.lines = Append(la.lines, Line{data[:len(data)-1], nil, nil, false})
		}
		n++
	}

	return la, fileformat
}

// Returns the String representation of the LineArray
func (la *LineArray) String() string {
	var sb strings.Builder
	sb.Grow(int(la.initsize + 4096))
	for i, l := range la.lines {
		sb.Write(l.data)
		if i != len(la.lines)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// SaveString returns the string that should be written to disk when
// the line array is saved
// It is the same as string but uses crlf or lf line endings depending
func (la *LineArray) SaveString(useCrlf bool) string {
	return string(la.Bytes(useCrlf))
}

// Bytes returns the string that should be written to disk when
// the line array is saved
func (la *LineArray) Bytes(useCrlf bool) []byte {
	b := new(bytes.Buffer)
	// initsize should provide a good estimate
	b.Grow(int(la.initsize + 4096))
	for i, l := range la.lines {
		b.Write(l.data)
		if i != len(la.lines)-1 {
			if useCrlf {
				b.WriteByte('\r')
			}
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

// NewlineBelow adds a newline below the given line number
func (la *LineArray) NewlineBelow(y int) {
	la.lines = append(la.lines, Line{[]byte{' '}, nil, nil, false})
	copy(la.lines[y+2:], la.lines[y+1:])
	la.lines[y+1] = Line{[]byte{}, la.lines[y].state, nil, false}
}

// inserts a byte array at a given location using a bulk approach.
// Instead of inserting one byte at a time, we split value on '\n' once,
// then splice the prefix/suffix of the target line around the new segments.
func (la *LineArray) insert(pos Loc, value []byte) {
	x := runeToByteIndex(pos.X, la.lines[pos.Y].data)
	y := pos.Y

	// Split the value into segments separated by '\n'.
	segments := bytes.Split(value, []byte{'\n'})

	if len(segments) == 1 {
		// No newlines — splice the segment into the current line in one operation.
		line := la.lines[y].data
		newLine := make([]byte, 0, len(line)+len(segments[0]))
		newLine = append(newLine, line[:x]...)
		newLine = append(newLine, segments[0]...)
		newLine = append(newLine, line[x:]...)
		la.lines[y].data = newLine
		return
	}

	// Multiple segments means we are splitting the current line and
	// inserting new lines in between.
	prefix := la.lines[y].data[:x]
	suffix := make([]byte, len(la.lines[y].data[x:]))
	copy(suffix, la.lines[y].data[x:])

	// The first segment is appended to the prefix of the current line.
	firstLine := make([]byte, 0, len(prefix)+len(segments[0]))
	firstLine = append(firstLine, prefix...)
	firstLine = append(firstLine, segments[0]...)

	// The last segment is prepended to the suffix of the current line.
	lastSeg := segments[len(segments)-1]
	lastLine := make([]byte, 0, len(lastSeg)+len(suffix))
	lastLine = append(lastLine, lastSeg...)
	lastLine = append(lastLine, suffix...)

	// Build the new lines to insert (middle segments become their own lines).
	newCount := len(segments) - 1 // number of new lines to add
	newLines := make([]Line, newCount)
	for i := 0; i < newCount; i++ {
		if i < newCount-1 {
			// Middle segments: segments[1] .. segments[len-2]
			seg := segments[i+1]
			data := make([]byte, len(seg))
			copy(data, seg)
			newLines[i] = Line{data: data, rehighlight: true}
		} else {
			// Last new line gets the suffix appended.
			newLines[i] = Line{data: lastLine, rehighlight: true}
		}
	}

	// Update the current line to be just the first line.
	la.lines[y].data = firstLine
	la.lines[y].state = nil
	la.lines[y].match = nil
	la.lines[y].rehighlight = true

	// Insert the new lines after position y.
	la.lines = append(la.lines, make([]Line, newCount)...)
	copy(la.lines[y+1+newCount:], la.lines[y+1:])
	copy(la.lines[y+1:], newLines)
}

// JoinLines joins the two lines a and b
func (la *LineArray) JoinLines(a, b int) {
	la.insert(Loc{len(la.lines[a].data), a}, la.lines[b].data)
	la.DeleteLine(b)
}

// Split splits a line at a given position
func (la *LineArray) Split(pos Loc) {
	la.NewlineBelow(pos.Y)
	la.insert(Loc{0, pos.Y + 1}, la.lines[pos.Y].data[pos.X:])
	la.lines[pos.Y+1].state = la.lines[pos.Y].state
	la.lines[pos.Y].state = nil
	la.lines[pos.Y].match = nil
	la.lines[pos.Y+1].match = nil
	la.lines[pos.Y].rehighlight = true
	la.DeleteToEnd(Loc{pos.X, pos.Y})
}

// removes from start to end
func (la *LineArray) remove(start, end Loc) []byte {
	sub := la.Substr(start, end)
	startX := runeToByteIndex(start.X, la.lines[start.Y].data)
	endX := runeToByteIndex(end.X, la.lines[end.Y].data)
	if start.Y == end.Y {
		la.lines[start.Y].data = append(la.lines[start.Y].data[:startX], la.lines[start.Y].data[endX:]...)
	} else {
		for i := start.Y + 1; i <= end.Y-1; i++ {
			la.DeleteLine(start.Y + 1)
		}
		la.DeleteToEnd(Loc{startX, start.Y})
		la.DeleteFromStart(Loc{endX - 1, start.Y + 1})
		la.JoinLines(start.Y, start.Y+1)
	}
	return sub
}

// DeleteToEnd deletes from the end of a line to the position
func (la *LineArray) DeleteToEnd(pos Loc) {
	la.lines[pos.Y].data = la.lines[pos.Y].data[:pos.X]
}

// DeleteFromStart deletes from the start of a line to the position
func (la *LineArray) DeleteFromStart(pos Loc) {
	la.lines[pos.Y].data = la.lines[pos.Y].data[pos.X+1:]
}

// DeleteLine deletes the line number
func (la *LineArray) DeleteLine(y int) {
	la.lines = la.lines[:y+copy(la.lines[y:], la.lines[y+1:])]
}

// DeleteByte deletes the byte at a position
func (la *LineArray) DeleteByte(pos Loc) {
	la.lines[pos.Y].data = la.lines[pos.Y].data[:pos.X+copy(la.lines[pos.Y].data[pos.X:], la.lines[pos.Y].data[pos.X+1:])]
}

// Substr returns the string representation between two locations
func (la *LineArray) Substr(start, end Loc) []byte {
	startX := runeToByteIndex(start.X, la.lines[start.Y].data)
	endX := runeToByteIndex(end.X, la.lines[end.Y].data)
	if start.Y == end.Y {
		src := la.lines[start.Y].data[startX:endX]
		dest := make([]byte, len(src))
		copy(dest, src)
		return dest
	}
	str := make([]byte, 0, len(la.lines[start.Y+1].data)*(end.Y-start.Y))
	str = append(str, la.lines[start.Y].data[startX:]...)
	str = append(str, '\n')
	for i := start.Y + 1; i <= end.Y-1; i++ {
		str = append(str, la.lines[i].data...)
		str = append(str, '\n')
	}
	str = append(str, la.lines[end.Y].data[:endX]...)
	return str
}

// State gets the highlight state for the given line number
func (la *LineArray) State(lineN int) highlight.State {
	return la.lines[lineN].state
}

// SetState sets the highlight state at the given line number
func (la *LineArray) SetState(lineN int, s highlight.State) {
	la.lines[lineN].state = s
}

// SetMatch sets the match at the given line number
func (la *LineArray) SetMatch(lineN int, m highlight.LineMatch) {
	la.lines[lineN].match = m
}

// Match retrieves the match for the given line number
func (la *LineArray) Match(lineN int) highlight.LineMatch {
	return la.lines[lineN].match
}
