package editor

import (
	"regexp"
	"sync"
	"unicode/utf8"

	"github.com/malivvan/editor/util"
)

// We want "^" and "$" to match only the beginning/end of a line, not the
// beginning/end of the search region if it is in the middle of a line.
// In that case we use padded regexps to require a rune before or after
// the match. (This also affects other empty-string patters like "\\b".)
// The following two flags indicate the padding used.
const (
	padStart = 1 << iota
	padEnd
)

// paddedRegexpCache caches compiled *regexp.Regexp objects keyed by the
// final pattern string. This avoids recompiling the same padded regex on
// every call to findLineParams for boundary lines during a search.
var paddedRegexpCache sync.Map // map[string]*regexp.Regexp

// cachedCompile returns a compiled regexp for the given pattern, using
// the cache to avoid redundant compilation.
func cachedCompile(pattern string) *regexp.Regexp {
	if v, ok := paddedRegexpCache.Load(pattern); ok {
		return v.(*regexp.Regexp)
	}
	r := regexp.MustCompile(pattern)
	paddedRegexpCache.Store(pattern, r)
	return r
}

func findLineParams(b *Buffer, start, end Loc, i int, r *regexp.Regexp) ([]byte, int, int, *regexp.Regexp) {
	l := b.LineBytes(i)
	charpos := 0
	padMode := 0

	if i == end.Y {
		nchars := util.CharacterCount(l)
		end.X = util.Clamp(end.X, 0, nchars)
		if end.X < nchars {
			l = util.SliceStart(l, end.X+1)
			padMode |= padEnd
		}
	}

	if i == start.Y {
		nchars := util.CharacterCount(l)
		start.X = util.Clamp(start.X, 0, nchars)
		if start.X > 0 {
			charpos = start.X - 1
			l = util.SliceEnd(l, charpos)
			padMode |= padStart
		}
	}

	if padMode == padStart {
		r = cachedCompile(".(?:" + r.String() + ")")
	} else if padMode == padEnd {
		r = cachedCompile("(?:" + r.String() + ").")
	} else if padMode == padStart|padEnd {
		r = cachedCompile(".(?:" + r.String() + ").")
	}

	return l, charpos, padMode, r
}

func (b *Buffer) findDown(r *regexp.Regexp, start, end Loc) ([2]Loc, bool) {
	lastcn := util.CharacterCount(b.LineBytes(b.LinesNum() - 1))
	if start.Y > b.LinesNum()-1 {
		start.X = lastcn - 1
	}
	if end.Y > b.LinesNum()-1 {
		end.X = lastcn
	}
	start.Y = util.Clamp(start.Y, 0, b.LinesNum()-1)
	end.Y = util.Clamp(end.Y, 0, b.LinesNum()-1)

	if start.GreaterThan(end) {
		start, end = end, start
	}

	for i := start.Y; i <= end.Y; i++ {
		l, charpos, padMode, rPadded := findLineParams(b, start, end, i, r)

		match := rPadded.FindIndex(l)

		if match != nil {
			if padMode&padStart != 0 {
				_, size := utf8.DecodeRune(l[match[0]:])
				match[0] += size
			}
			if padMode&padEnd != 0 {
				_, size := utf8.DecodeLastRune(l[:match[1]])
				match[1] -= size
			}
			start := Loc{charpos + util.RunePos(l, match[0]), i}
			end := Loc{charpos + util.RunePos(l, match[1]), i}
			return [2]Loc{start, end}, true
		}
	}
	return [2]Loc{}, false
}

func (b *Buffer) findUp(r *regexp.Regexp, start, end Loc) ([2]Loc, bool) {
	lastcn := util.CharacterCount(b.LineBytes(b.LinesNum() - 1))
	if start.Y > b.LinesNum()-1 {
		start.X = lastcn - 1
	}
	if end.Y > b.LinesNum()-1 {
		end.X = lastcn
	}
	start.Y = util.Clamp(start.Y, 0, b.LinesNum()-1)
	end.Y = util.Clamp(end.Y, 0, b.LinesNum()-1)

	if start.GreaterThan(end) {
		start, end = end, start
	}

	for i := end.Y; i >= start.Y; i-- {
		charCount := util.CharacterCount(b.LineBytes(i))
		from := Loc{0, i}.Clamp(start, end)
		to := Loc{charCount, i}.Clamp(start, end)

		allMatches := b.findAll(r, from, to)
		if allMatches != nil {
			match := allMatches[len(allMatches)-1]
			return [2]Loc{match[0], match[1]}, true
		}
	}
	return [2]Loc{}, false
}

func (b *Buffer) findAll(r *regexp.Regexp, start, end Loc) [][2]Loc {
	var matches [][2]Loc
	loc := start
	for {
		match, found := b.findDown(r, loc, end)
		if !found {
			break
		}
		matches = append(matches, match)
		if match[0] != match[1] {
			loc = match[1]
		} else if match[1] != end {
			loc = match[1].Move(1, b)
		} else {
			break
		}
	}
	return matches
}

func createRegex(s string, useRegex bool, caseSensitive bool) (*regexp.Regexp, error) {
	var r *regexp.Regexp
	var err error

	if !useRegex {
		s = regexp.QuoteMeta(s)
	}

	if caseSensitive {
		r, err = regexp.Compile(s)
	} else {
		r, err = regexp.Compile("(?i)" + s)
	}
	return r, err
}

// FindNext finds the next occurrence of a given string in the buffer
// It returns the start and end location of the match (if found) and
// a boolean indicating if it was found
// May also return an error if the search regex is invalid
func (b *Buffer) FindNext(s string, start, end, from Loc, down bool, useRegex bool, caseSensitive bool) ([2]Loc, bool, error) {
	if s == "" {
		return [2]Loc{}, false, nil
	}

	r, err := createRegex(s, useRegex, caseSensitive)
	if err != nil {
		return [2]Loc{}, false, err
	}

	var found bool
	var locationPair [2]Loc
	if down {
		locationPair, found = b.findDown(r, from, end)
	} else {
		locationPair, found = b.findUp(r, from, start)
	}
	return locationPair, found, nil
}

// ReplaceRegex replaces all occurrences of 'search' with 'replace' in the given area
// and returns the number of replacements made and the number of characters
// added or removed on the last line of the range
func (b *Buffer) ReplaceRegex(start, end Loc, search *regexp.Regexp, replace []byte, captureGroups bool) (int, int) {
	if start.GreaterThan(end) {
		start, end = end, start
	}

	charsEnd := util.CharacterCount(b.LineBytes(end.Y))
	found := 0
	var deltas []Delta

	for i := start.Y; i <= end.Y; i++ {
		l := b.LineBytes(i)
		charCount := util.CharacterCount(l)
		if (i == start.Y && start.X > 0) || (i == end.Y && end.X < charCount) {
			// This replacement code works in general, but it creates a separate
			// modification for each match. We only use it for the first and last
			// lines, which may use padded regexps

			from := Loc{0, i}.Clamp(start, end)
			to := Loc{charCount, i}.Clamp(start, end)
			matches := b.findAll(search, from, to)
			found += len(matches)

			for j := len(matches) - 1; j >= 0; j-- {
				// if we counted upwards, the different deltas would interfere
				match := matches[j]
				var newText []byte
				if captureGroups {
					newText = search.ReplaceAll(b.Substr(match[0], match[1]), replace)
				} else {
					newText = replace
				}
				deltas = append(deltas, Delta{newText, match[0], match[1]})
			}
		} else {
			newLine := search.ReplaceAllFunc(l, func(in []byte) []byte {
				found++
				var result []byte
				if captureGroups {
					match := search.FindSubmatchIndex(in)
					result = search.Expand(result, replace, in, match)
				} else {
					result = replace
				}
				return result
			})
			deltas = append(deltas, Delta{newLine, Loc{0, i}, Loc{charCount, i}})
		}
	}

	b.MultipleReplace(deltas)

	return found, util.CharacterCount(b.LineBytes(end.Y)) - charsEnd
}

// Replace all occurrences of str with replaceText in the entire buffer
//
// Returns the number of replacements made and an error if the regex is invalid
func (v *View) ReplaceAll(str string, useRegex bool, caseSensitive bool, replaceText string) (int, error) {
	if str == "" {
		return 0, nil
	}

	regex, err := createRegex(str, useRegex, caseSensitive)
	if err != nil {
		return 0, err
	}

	count, _ := v.Buf.ReplaceRegex(v.Buf.Start(), v.Buf.End(), regex, []byte(replaceText), false)
	return count, nil
}

// Search for a string/regex and select it
//
// Returns true,nil if the string was found, false,nil if not found, and false,error on error.
func (v *View) Search(str string, useRegex bool, caseSensitive bool, searchDown bool) (bool, error) {
	loc := v.Cursor.Loc
	if v.Cursor.HasSelection() && !searchDown {
		loc = v.Cursor.CurSelection[0]
	}

	match, found, err := v.Buf.FindNext(str, v.Buf.Start(), v.Buf.End(), loc, searchDown, useRegex, caseSensitive)
	if err != nil {
		return false, err
	}

	if found {
		v.Cursor.SetSelectionStart(match[0])
		v.Cursor.SetSelectionEnd(match[1])
		v.Cursor.OrigSelection[0] = v.Cursor.CurSelection[0]
		v.Cursor.OrigSelection[1] = v.Cursor.CurSelection[1]
		v.Cursor.GotoLoc(v.Cursor.CurSelection[1])
		return true, nil
	} else {
		return false, nil
	}
}
