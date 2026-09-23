package editor

import "log"

// settingAs retrieves a setting by key, returning the fallback if the key is
// missing or the stored value is not of type T.  A warning is logged on type
// mismatch so that callers never panic on a bad assertion.
func settingAs[T any](s map[string]interface{}, key string, fallback T) T {
	v, ok := s[key]
	if !ok {
		return fallback
	}
	t, ok := v.(T)
	if !ok {
		log.Printf("warning: setting %q has unexpected type %T, expected %T", key, v, fallback)
		return fallback
	}
	return t
}

// DefaultLocalSettings returns the default local settings
// Note that filetype is a local only option
func DefaultLocalSettings() map[string]interface{} {
	return map[string]interface{}{
		"autoindent":       true,
		"autosave":         false,
		"basename":         false,
		"colorcolumn":      float64(0),
		"cursorline":       true,
		"eofnewline":       false,
		"fastdirty":        true,
		"fileformat":       "unix",
		"filetype":         "Unknown",
		"hidecursoronblur": false,
		"hidehelp":         false,
		"indentchar":       " ",
		"keepautoindent":   false,
		"matchbrace":       false,
		"matchbraceleft":   false,
		"maxundohistory":   float64(10000),
		"rmtrailingws":     false,
		"ruler":            true,
		"savecursor":       false,
		"saveundo":         false,
		"scrollbar":        false,
		"scrollmargin":     float64(3),
		"scrollspeed":      float64(2),
		"showwhitespace":   false,
		"smartpaste":       true,
		"softwrap":         false,
		"splitbottom":      true,
		"splitright":       true,
		"statusline":       true,
		"syntax":           true,
		"tabmovement":      false,
		"tabsize":          float64(4),
		"tabstospaces":     false,
		"useprimary":       true,
	}
}
