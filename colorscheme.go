package editor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"
)

// Colorscheme is a map from string to style -- it represents a colorscheme
type Colorscheme map[string]tcell.Style

// The current default colorscheme
var colorscheme Colorscheme

// GetColor takes in a syntax group and returns the colorscheme's style for that group
func GetColor(color string) tcell.Style {
	return colorscheme.GetColor(color)
}

// GetColor takes in a syntax group and returns the colorscheme's style for that group
func (colorscheme Colorscheme) GetColor(color string) tcell.Style {
	var st tcell.Style
	if color == "" {
		return colorscheme.GetDefault()
	}
	groups := strings.Split(color, ".")
	if len(groups) > 1 {
		curGroup := ""
		for i, g := range groups {
			if i != 0 {
				curGroup += "."
			}
			curGroup += g
			if style, ok := colorscheme[curGroup]; ok {
				st = style
			}
		}
	} else if style, ok := colorscheme[color]; ok {
		st = style
	} else {
		st = StringToStyle(color, colorscheme.GetDefault())
	}

	return st
}

func (colorscheme Colorscheme) GetDefault() tcell.Style {
	return colorscheme["default"]
}

// init picks and initializes the colorscheme when micro starts
func init() {
	colorscheme = make(Colorscheme)
}

// SetDefaultColorscheme sets the current default colorscheme for new Views.
func SetDefaultColorscheme(scheme Colorscheme) {
	colorscheme = scheme
}

// ParseColorscheme parses the text definition for a colorscheme and returns the corresponding object
// Colorschemes are made up of color-link statements linking a color group to a list of colors
// For example, color-link keyword (blue,red) makes all keywords have a blue foreground and
// red background
func ParseColorscheme(text string) Colorscheme {
	lines := strings.Split(text, "\n")

	c := make(Colorscheme)

	cleanLines := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" ||
			strings.TrimSpace(line)[0] == '#' {
			// Ignore this line
			continue
		}
		cleanLines = append(cleanLines, line)
	}

	defaultStyle := tcell.StyleDefault.Foreground(color.White).Background(color.Black)
	c["default"] = defaultStyle
	for _, line := range cleanLines {
		link, style := parseColorLine(line, defaultStyle)
		if link == "default" {
			defaultStyle = style
			break
		}
	}

	for _, line := range cleanLines {
		link, style := parseColorLine(line, defaultStyle)
		if link != "" {
			c[link] = style
		}
	}

	return c
}

func parseColorLine(line string, defaultStyle tcell.Style) (string, tcell.Style) {
	parser := regexp.MustCompile(`color-link\s+(\S*)\s+"(.*)"`)
	matches := parser.FindSubmatch([]byte(line))
	if len(matches) == 3 {
		link := string(matches[1])
		colors := string(matches[2])

		style := StringToStyle(colors, defaultStyle)
		return link, style
	} else {
		fmt.Println("Color-link statement is not valid:", line)
	}
	return "", defaultStyle
}

// StringToStyle returns a style from a string
// The strings must be in the format "extra foregroundcolor,backgroundcolor"
// The 'extra' can be bold, reverse, or underline
func StringToStyle(str string, defaultStyle tcell.Style) tcell.Style {
	var fg, bg string
	spaceSplit := strings.Split(str, " ")
	var split []string
	if len(spaceSplit) > 1 {
		split = strings.Split(spaceSplit[1], ",")
	} else {
		split = strings.Split(str, ",")
	}
	if len(split) > 1 {
		fg, bg = split[0], split[1]
	} else {
		fg = split[0]
	}
	fg = strings.TrimSpace(fg)
	bg = strings.TrimSpace(bg)

	var fgColor, bgColor tcell.Color
	if fg == "" {
		fgColor = defaultStyle.GetForeground()
	} else {
		fgColor = StringToColor(fg)
	}
	if bg == "" {
		bgColor = defaultStyle.GetBackground()
	} else {
		bgColor = StringToColor(bg)
	}

	style := defaultStyle.Foreground(fgColor).Background(bgColor)
	if strings.Contains(str, "bold") {
		style = style.Bold(true)
	}
	if strings.Contains(str, "reverse") {
		style = style.Reverse(true)
	}
	if strings.Contains(str, "underline") {
		style = style.Underline(true)
	}
	return style
}

// StringToColor returns a tcell color from a string representation of a color
// We accept either bright... or light... to mean the brighter version of a color
func StringToColor(str string) tcell.Color {
	switch str {
	case "black":
		return color.Black
	case "red":
		return color.Maroon
	case "green":
		return color.Green
	case "yellow":
		return color.Olive
	case "blue":
		return color.Navy
	case "magenta":
		return color.Purple
	case "cyan":
		return color.Teal
	case "white":
		return color.Silver
	case "brightblack", "lightblack":
		return color.Gray
	case "brightred", "lightred":
		return color.Red
	case "brightgreen", "lightgreen":
		return color.Lime
	case "brightyellow", "lightyellow":
		return color.Yellow
	case "brightblue", "lightblue":
		return color.Blue
	case "brightmagenta", "lightmagenta":
		return color.Fuchsia
	case "brightcyan", "lightcyan":
		return color.Aqua
	case "brightwhite", "lightwhite":
		return color.White
	case "default":
		return color.Default
	default:
		// Check if this is a 256 color
		if num, err := strconv.Atoi(str); err == nil {
			return GetColor256(num)
		}
		// Probably a truecolor hex value
		return tcell.GetColor(str)
	}
}

// GetColor256 returns the tcell color for a number between 0 and 255
func GetColor256(c int) tcell.Color {
	colors := []tcell.Color{color.Black, color.Maroon, color.Green,
		color.Olive, color.Navy, color.Purple,
		color.Teal, color.Silver, color.Gray,
		color.Red, color.Lime, color.Yellow,
		color.Blue, color.Fuchsia, color.Aqua,
		color.White, color.XTerm16, color.XTerm17, color.XTerm18, color.XTerm19, color.XTerm20,
		color.XTerm21, color.XTerm22, color.XTerm23, color.XTerm24, color.XTerm25, color.XTerm26, color.XTerm27, color.XTerm28,
		color.XTerm29, color.XTerm30, color.XTerm31, color.XTerm32, color.XTerm33, color.XTerm34, color.XTerm35, color.XTerm36,
		color.XTerm37, color.XTerm38, color.XTerm39, color.XTerm40, color.XTerm41, color.XTerm42, color.XTerm43, color.XTerm44,
		color.XTerm45, color.XTerm46, color.XTerm47, color.XTerm48, color.XTerm49, color.XTerm50, color.XTerm51, color.XTerm52,
		color.XTerm53, color.XTerm54, color.XTerm55, color.XTerm56, color.XTerm57, color.XTerm58, color.XTerm59, color.XTerm60,
		color.XTerm61, color.XTerm62, color.XTerm63, color.XTerm64, color.XTerm65, color.XTerm66, color.XTerm67, color.XTerm68,
		color.XTerm69, color.XTerm70, color.XTerm71, color.XTerm72, color.XTerm73, color.XTerm74, color.XTerm75, color.XTerm76,
		color.XTerm77, color.XTerm78, color.XTerm79, color.XTerm80, color.XTerm81, color.XTerm82, color.XTerm83, color.XTerm84,
		color.XTerm85, color.XTerm86, color.XTerm87, color.XTerm88, color.XTerm89, color.XTerm90, color.XTerm91, color.XTerm92,
		color.XTerm93, color.XTerm94, color.XTerm95, color.XTerm96, color.XTerm97, color.XTerm98, color.XTerm99, color.XTerm100,
		color.XTerm101, color.XTerm102, color.XTerm103, color.XTerm104, color.XTerm105, color.XTerm106, color.XTerm107, color.XTerm108,
		color.XTerm109, color.XTerm110, color.XTerm111, color.XTerm112, color.XTerm113, color.XTerm114, color.XTerm115, color.XTerm116,
		color.XTerm117, color.XTerm118, color.XTerm119, color.XTerm120, color.XTerm121, color.XTerm122, color.XTerm123, color.XTerm124,
		color.XTerm125, color.XTerm126, color.XTerm127, color.XTerm128, color.XTerm129, color.XTerm130, color.XTerm131, color.XTerm132,
		color.XTerm133, color.XTerm134, color.XTerm135, color.XTerm136, color.XTerm137, color.XTerm138, color.XTerm139, color.XTerm140,
		color.XTerm141, color.XTerm142, color.XTerm143, color.XTerm144, color.XTerm145, color.XTerm146, color.XTerm147, color.XTerm148,
		color.XTerm149, color.XTerm150, color.XTerm151, color.XTerm152, color.XTerm153, color.XTerm154, color.XTerm155, color.XTerm156,
		color.XTerm157, color.XTerm158, color.XTerm159, color.XTerm160, color.XTerm161, color.XTerm162, color.XTerm163, color.XTerm164,
		color.XTerm165, color.XTerm166, color.XTerm167, color.XTerm168, color.XTerm169, color.XTerm170, color.XTerm171, color.XTerm172,
		color.XTerm173, color.XTerm174, color.XTerm175, color.XTerm176, color.XTerm177, color.XTerm178, color.XTerm179, color.XTerm180,
		color.XTerm181, color.XTerm182, color.XTerm183, color.XTerm184, color.XTerm185, color.XTerm186, color.XTerm187, color.XTerm188,
		color.XTerm189, color.XTerm190, color.XTerm191, color.XTerm192, color.XTerm193, color.XTerm194, color.XTerm195, color.XTerm196,
		color.XTerm197, color.XTerm198, color.XTerm199, color.XTerm200, color.XTerm201, color.XTerm202, color.XTerm203, color.XTerm204,
		color.XTerm205, color.XTerm206, color.XTerm207, color.XTerm208, color.XTerm209, color.XTerm210, color.XTerm211, color.XTerm212,
		color.XTerm213, color.XTerm214, color.XTerm215, color.XTerm216, color.XTerm217, color.XTerm218, color.XTerm219, color.XTerm220,
		color.XTerm221, color.XTerm222, color.XTerm223, color.XTerm224, color.XTerm225, color.XTerm226, color.XTerm227, color.XTerm228,
		color.XTerm229, color.XTerm230, color.XTerm231, color.XTerm232, color.XTerm233, color.XTerm234, color.XTerm235, color.XTerm236,
		color.XTerm237, color.XTerm238, color.XTerm239, color.XTerm240, color.XTerm241, color.XTerm242, color.XTerm243, color.XTerm244,
		color.XTerm245, color.XTerm246, color.XTerm247, color.XTerm248, color.XTerm249, color.XTerm250, color.XTerm251, color.XTerm252,
		color.XTerm253, color.XTerm254, color.XTerm255,
	}

	if c >= 0 && c < len(colors) {
		return colors[c]
	}

	return color.Default
}
