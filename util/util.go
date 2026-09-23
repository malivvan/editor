// Package util holds the small helpers the editor shares between its files:
// clamping, converting between byte indices and rune indices, and — in
// unicode.go — everything that has to do with characters built from more than
// one code point.
package util

// Clamp clamps a value between min and max
func Clamp(val, min, max int) int {
	if val < min {
		val = min
	} else if val > max {
		val = max
	}
	return val
}

// SliceEnd returns a byte slice where the index is a rune index
// Slices off the start of the slice
func SliceEnd(slc []byte, index int) []byte {
	len := len(slc)
	i := 0
	totalSize := 0
	for totalSize < len {
		if i >= index {
			return slc[totalSize:]
		}

		_, _, size := DecodeCharacter(slc[totalSize:])
		totalSize += size
		i++
	}

	return slc[totalSize:]
}

// SliceStart returns a byte slice where the index is a rune index
// Slices off the end of the slice
func SliceStart(slc []byte, index int) []byte {
	i := 0
	totalSize := 0
	for totalSize < len(slc) {
		if i >= index {
			return slc[:totalSize]
		}

		_, _, size := DecodeCharacter(slc[totalSize:])
		totalSize += size
		i++
	}

	return slc[:totalSize]
}

// RunePos returns the rune index of a given byte index
// Make sure the byte index is not between code points
func RunePos(b []byte, i int) int {
	return CharacterCount(b[:i])
}
