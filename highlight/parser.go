package highlight

import (
	"fmt"
	"regexp"
)

// Header describes how a syntax definition recognizes its file type: the name
// of the type it claims, plus the patterns that identify it. Files are matched
// through FileNameRegex (typically an extension or a well-known name), and
// contents through HeaderRegex — tried against the whole first line — or
// SignatureRegex, which may match any part of it.
type Header struct {
	FileType       string
	FileNameRegex  *regexp.Regexp
	HeaderRegex    *regexp.Regexp
	SignatureRegex *regexp.Regexp
}

// Match will check if the header matches the filename or the first line of the file
func (header *Header) Match(filename string, firstLine []byte) bool {
	if filename != "" && header.FileNameRegex != nil && header.FileNameRegex.MatchString(filename) {
		return true
	}
	if firstLine != nil && header.SignatureRegex != nil && header.SignatureRegex.Match(firstLine) {
		return true
	}
	return false
}

// A Group represents a syntax group
type Group uint8

// Groups contains all of the groups that are defined
// You can access them in the map via their string name
var Groups map[string]Group
var numGroups Group

// String returns the group name attached to the specific group
func (g Group) String() string {
	for k, v := range Groups {
		if v == g {
			return k
		}
	}
	return ""
}

// A Def is a full syntax definition for a language
// It has a filetype, information about how to detect the filetype based
// on filename or header (the first line of the file)
// Then it has the rules which define how to highlight the file
type Def struct {
	*Header

	rules *rules
}

// File is a syntax definition that has been read but not yet compiled: its
// file type plus the raw YAML it came from. Pairs of files and headers are
// what ParseDef turns into the rules a Highlighter runs.
type File struct {
	FileType string

	yamlSrc map[interface{}]interface{}
}

// A Pattern is one simple syntax rule
// It has a group that the rule belongs to, as well as
// the regular expression to match the pattern
type pattern struct {
	group Group
	regex *regexp.Regexp
}

// rules defines which patterns and regions can be used to highlight
// a filetype
type rules struct {
	regions  []*region
	patterns []*pattern
	includes []string
}

// A region is a highlighted region (such as a multiline comment, or a string)
// It belongs to a group, and has start and end regular expressions
// A region also has rules of its own that only apply when matching inside the
// region and also rules from the above region do not match inside this region
// Note that a region may contain more regions
type region struct {
	group      Group
	limitGroup Group
	parent     *region
	start      *regexp.Regexp
	end        *regexp.Regexp
	skip       *regexp.Regexp
	rules      *rules
}

func init() {
	Groups = make(map[string]Group)
}

// ParseHeader reads the header of a syntax definition — the "filetype" and
// "detect" keys — without compiling the rules. It is what a program calls to
// find out which grammar claims a file, before deciding to use it.
func ParseHeader(data []byte) (*Header, error) {
	var hdrYaml = struct {
		FileType string `yaml:"filetype"`
		Detect   struct {
			FNameRegexStr     string `yaml:"filename"`
			HeaderRegexStr    string `yaml:"header"`
			SignatureRegexStr string `yaml:"signature"`
		} `yaml:"detect"`
	}{}
	err := yamlUnmarshal(data, &hdrYaml, false)
	if err != nil {
		return nil, err
	}

	header := new(Header)
	header.FileType = hdrYaml.FileType

	if hdrYaml.Detect.FNameRegexStr != "" {
		header.FileNameRegex, err = regexp.Compile(hdrYaml.Detect.FNameRegexStr)
	}
	if err == nil && hdrYaml.Detect.HeaderRegexStr != "" {
		header.HeaderRegex, err = regexp.Compile(hdrYaml.Detect.HeaderRegexStr)
	}
	if err == nil && hdrYaml.Detect.SignatureRegexStr != "" {
		header.SignatureRegex, err = regexp.Compile(hdrYaml.Detect.SignatureRegexStr)
	}

	if err != nil {
		return nil, err
	}

	return header, nil
}

// ParseFile parses a syntax definition into its file type and its raw rules.
// The result, together with a header from ParseHeader, is the input of ParseDef.
func ParseFile(input []byte) (f *File, err error) {
	// This is just so if we have an error, we can exit cleanly and return the parse error to the user
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("pkg: %v", r)
			}
		}
	}()

	var rules map[interface{}]interface{}
	if err = yamlUnmarshal(input, &rules, false); err != nil {
		return nil, err
	}

	f = new(File)
	f.yamlSrc = rules

	for k, v := range rules {
		if k == "filetype" {
			filetype := v.(string)

			f.FileType = filetype
			break
		}
	}

	return f, err
}

// ParseDef parses an input syntax file into a highlight Def
func ParseDef(f *File, header *Header) (s *Def, err error) {
	// This is just so if we have an error, we can exit cleanly and return the parse error to the user
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("pkg: %v", r)
			}
		}
	}()

	rules := f.yamlSrc

	s = new(Def)
	s.Header = header

	for k, v := range rules {
		if k == "rules" {
			inputRules := v.([]interface{})

			rules, err := parseRules(inputRules, nil)
			if err != nil {
				return nil, err
			}

			s.rules = rules
		}
	}

	return s, err
}

// ResolveIncludes will sort out the rules for including other filetypes
// You should call this after parsing all the Defs
func ResolveIncludes(def *Def, files []*File) {
	resolveIncludesInDef(files, def)
}

func resolveIncludesInDef(files []*File, d *Def) {
	for _, lang := range d.rules.includes {
		for _, searchFile := range files {
			if lang == searchFile.FileType {
				searchDef, _ := ParseDef(searchFile, nil)
				d.rules.patterns = append(d.rules.patterns, searchDef.rules.patterns...)
				d.rules.regions = append(d.rules.regions, searchDef.rules.regions...)
			}
		}
	}
	for _, r := range d.rules.regions {
		resolveIncludesInRegion(files, r)
		r.parent = nil
	}
}

func resolveIncludesInRegion(files []*File, region *region) {
	for _, lang := range region.rules.includes {
		for _, searchFile := range files {
			if lang == searchFile.FileType {
				searchDef, _ := ParseDef(searchFile, nil)
				region.rules.patterns = append(region.rules.patterns, searchDef.rules.patterns...)
				region.rules.regions = append(region.rules.regions, searchDef.rules.regions...)
			}
		}
	}
	for _, r := range region.rules.regions {
		resolveIncludesInRegion(files, r)
		r.parent = region
	}
}

func parseRules(input []interface{}, curRegion *region) (ru *rules, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("pkg: %v", r)
			}
		}
	}()
	ru = new(rules)

	for _, v := range input {
		rule := v.(map[interface{}]interface{})
		for k, val := range rule {
			group := k

			switch object := val.(type) {
			case string:
				if object == "" {
					return nil, fmt.Errorf("Empty rule %s", k)
				}

				if k == "include" {
					ru.includes = append(ru.includes, object)
				} else {
					// Pattern
					r, err := regexp.Compile(object)
					if err != nil {
						return nil, err
					}

					groupStr := group.(string)
					if _, ok := Groups[groupStr]; !ok {
						numGroups++
						Groups[groupStr] = numGroups
					}
					groupNum := Groups[groupStr]
					ru.patterns = append(ru.patterns, &pattern{groupNum, r})
				}
			case map[interface{}]interface{}:
				// region
				region, err := parseRegion(group.(string), object, curRegion)
				if err != nil {
					return nil, err
				}
				ru.regions = append(ru.regions, region)
			default:
				return nil, fmt.Errorf("Bad type %T", object)
			}
		}
	}

	return ru, nil
}

func parseRegion(group string, regionInfo map[interface{}]interface{}, prevRegion *region) (r *region, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("pkg: %v", r)
			}
		}
	}()

	r = new(region)
	if _, ok := Groups[group]; !ok {
		numGroups++
		Groups[group] = numGroups
	}
	groupNum := Groups[group]
	r.group = groupNum
	r.parent = prevRegion

	// start is mandatory
	if start, ok := regionInfo["start"]; ok {
		start := start.(string)
		if start == "" {
			return nil, fmt.Errorf("Empty start in %s", group)
		}

		r.start, err = regexp.Compile(start)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("Missing start in %s", group)
	}

	// end is mandatory
	if end, ok := regionInfo["end"]; ok {
		end := end.(string)
		if end == "" {
			return nil, fmt.Errorf("Empty end in %s", group)
		}

		r.end, err = regexp.Compile(end)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("Missing end in %s", group)
	}

	// skip is optional
	if skip, ok := regionInfo["skip"]; ok {
		skip := skip.(string)
		if skip == "" {
			return nil, fmt.Errorf("Empty skip in %s", group)
		}

		r.skip, err = regexp.Compile(skip)
		if err != nil {
			return nil, err
		}
	}

	// limit-color is optional
	if groupStr, ok := regionInfo["limit-group"]; ok {
		groupStr := groupStr.(string)
		if groupStr == "" {
			return nil, fmt.Errorf("Empty limit-group in %s", group)
		}

		if _, ok := Groups[groupStr]; !ok {
			numGroups++
			Groups[groupStr] = numGroups
		}
		groupNum := Groups[groupStr]
		r.limitGroup = groupNum

		if err != nil {
			return nil, err
		}
	} else {
		r.limitGroup = r.group
	}

	// rules are optional
	if rules, ok := regionInfo["rules"]; ok {
		r.rules, err = parseRules(rules.([]interface{}), r)
		if err != nil {
			return nil, err
		}
	}

	if r.rules == nil {
		// allow empty rules
		r.rules = &rules{}
	}

	return r, nil
}
