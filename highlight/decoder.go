package highlight

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// yamlUnmarshal
// (https://github.com/go-yaml/yaml/tree/v2.4.0)
// ---------------------------------------------
//
// #### MIT License ####
//
// Copyright (c) 2006-2010 Kirill Simonov
// Copyright (c) 2006-2011 Kirill Simonov
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
// ### Apache License ###
//
// All the remaining project files are covered by the Apache license:
//
// # Copyright (c) 2011-2019 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
func yamlUnmarshal(in []byte, out interface{}, strict bool) (err error) {
	defer yamlHandleErr(&err)
	d := yamlNewDecoder(strict)
	p := yamlNewParser(in)
	defer p.destroy()
	node := p.parse()
	if node != nil {
		v := reflect.ValueOf(out)
		if v.Kind() == reflect.Ptr && !v.IsNil() {
			v = v.Elem()
		}
		d.unmarshal(node, v)
	}
	if len(d.terrors) > 0 {
		return &yamlTypeError{d.terrors}
	}
	return nil
}
func yamlHandleErr(err *error) {
	if v := recover(); v != nil {
		if e, ok := v.(yamlError); ok {
			*err = e.err
		} else {
			panic(v)
		}
	}
}

type yamlError struct {
	err error
}

func yamlFailf(format string, args ...interface{}) {
	panic(yamlError{fmt.Errorf("yaml: "+format, args...)})
}

type yamlTypeError struct {
	Errors []string
}

func (e *yamlTypeError) Error() string {
	return fmt.Sprintf("yaml: unmarshal errors:\n  %s", strings.Join(e.Errors, "\n  "))
}

type yamlMapItem struct {
	Key, Value interface{}
}
type yamlStructInfo struct {
	FieldsMap  map[string]yamlFieldInfo
	FieldsList []yamlFieldInfo
	InlineMap  int
}
type yamlFieldInfo struct {
	Key       string
	Num       int
	OmitEmpty bool
	Flow      bool
	Id        int
	Inline    []int
}

var yamlStructMap = make(map[reflect.Type]*yamlStructInfo)
var yamlFieldMapMutex sync.RWMutex

func yamlGetStructInfo(st reflect.Type) (*yamlStructInfo, error) {
	yamlFieldMapMutex.RLock()
	sinfo, found := yamlStructMap[st]
	yamlFieldMapMutex.RUnlock()
	if found {
		return sinfo, nil
	}
	n := st.NumField()
	fieldsMap := make(map[string]yamlFieldInfo)
	fieldsList := make([]yamlFieldInfo, 0, n)
	inlineMap := -1
	for i := 0; i != n; i++ {
		field := st.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		info := yamlFieldInfo{Num: i}
		tag := field.Tag.Get("yaml")
		if tag == "" && strings.Index(string(field.Tag), ":") < 0 {
			tag = string(field.Tag)
		}
		if tag == "-" {
			continue
		}
		inline := false
		fields := strings.Split(tag, ",")
		if len(fields) > 1 {
			for _, flag := range fields[1:] {
				switch flag {
				case "omitempty":
					info.OmitEmpty = true
				case "flow":
					info.Flow = true
				case "inline":
					inline = true
				default:
					return nil, errors.New(fmt.Sprintf("Unsupported flag %q in tag %q of type %s", flag, tag, st))
				}
			}
			tag = fields[0]
		}
		if inline {
			switch field.Type.Kind() {
			case reflect.Map:
				if inlineMap >= 0 {
					return nil, errors.New("Multiple ,inline maps in struct " + st.String())
				}
				if field.Type.Key() != reflect.TypeOf("") {
					return nil, errors.New("Option ,inline needs a map with string keys in struct " + st.String())
				}
				inlineMap = info.Num
			case reflect.Struct:
				sinfo, err := yamlGetStructInfo(field.Type)
				if err != nil {
					return nil, err
				}
				for _, finfo := range sinfo.FieldsList {
					if _, found := fieldsMap[finfo.Key]; found {
						msg := "Duplicated key '" + finfo.Key + "' in struct " + st.String()
						return nil, errors.New(msg)
					}
					if finfo.Inline == nil {
						finfo.Inline = []int{i, finfo.Num}
					} else {
						finfo.Inline = append([]int{i}, finfo.Inline...)
					}
					finfo.Id = len(fieldsList)
					fieldsMap[finfo.Key] = finfo
					fieldsList = append(fieldsList, finfo)
				}
			default:
				return nil, errors.New("Option ,inline needs a struct value field")
			}
			continue
		}
		if tag != "" {
			info.Key = tag
		} else {
			info.Key = strings.ToLower(field.Name)
		}
		if _, found = fieldsMap[info.Key]; found {
			msg := "Duplicated key '" + info.Key + "' in struct " + st.String()
			return nil, errors.New(msg)
		}
		info.Id = len(fieldsList)
		fieldsList = append(fieldsList, info)
		fieldsMap[info.Key] = info
	}
	sinfo = &yamlStructInfo{
		FieldsMap:  fieldsMap,
		FieldsList: fieldsList,
		InlineMap:  inlineMap,
	}
	yamlFieldMapMutex.Lock()
	yamlStructMap[st] = sinfo
	yamlFieldMapMutex.Unlock()
	return sinfo, nil
}

const (
	yamlDocumentNode = 1 << iota
	yamlMappingNode
	yamlSequenceNode
	yamlScalarNode
	yamlAliasNode
)

type yamlNode struct {
	kind         int
	line, column int
	tag          string
	alias        *yamlNode
	value        string
	implicit     bool
	children     []*yamlNode
	anchors      map[string]*yamlNode
}
type yamlParser struct {
	parser   yaml_parser_t
	event    yaml_event_t
	doc      *yamlNode
	doneInit bool
}

func yamlNewParser(b []byte) *yamlParser {
	p := yamlParser{}
	if !yaml_parser_initialize(&p.parser) {
		panic("failed to initialize YAML emitter")
	}
	if len(b) == 0 {
		b = []byte{'\n'}
	}
	yaml_parser_set_input_string(&p.parser, b)
	return &p
}
func (p *yamlParser) init() {
	if p.doneInit {
		return
	}
	p.expect(yaml_STREAM_START_EVENT)
	p.doneInit = true
}
func (p *yamlParser) destroy() {
	if p.event.typ != yaml_NO_EVENT {
		yaml_event_delete(&p.event)
	}
	yaml_parser_delete(&p.parser)
}
func (p *yamlParser) expect(e yaml_event_type_t) {
	if p.event.typ == yaml_NO_EVENT {
		if !yaml_parser_parse(&p.parser, &p.event) {
			p.fail()
		}
	}
	if p.event.typ == yaml_STREAM_END_EVENT {
		yamlFailf("attempted to go past the end of stream; corrupted value?")
	}
	if p.event.typ != e {
		p.parser.problem = fmt.Sprintf("expected %s event but got %s", e, p.event.typ)
		p.fail()
	}
	yaml_event_delete(&p.event)
	p.event.typ = yaml_NO_EVENT
}
func (p *yamlParser) peek() yaml_event_type_t {
	if p.event.typ != yaml_NO_EVENT {
		return p.event.typ
	}
	if !yaml_parser_parse(&p.parser, &p.event) {
		p.fail()
	}
	return p.event.typ
}
func (p *yamlParser) fail() {
	var where string
	var line int
	if p.parser.problem_mark.line != 0 {
		line = p.parser.problem_mark.line
		if p.parser.error == yaml_SCANNER_ERROR {
			line++
		}
	} else if p.parser.context_mark.line != 0 {
		line = p.parser.context_mark.line
	}
	if line != 0 {
		where = "line " + strconv.Itoa(line) + ": "
	}
	var msg string
	if len(p.parser.problem) > 0 {
		msg = p.parser.problem
	} else {
		msg = "unknown problem parsing YAML content"
	}
	yamlFailf("%s%s", where, msg)
}
func (p *yamlParser) anchor(n *yamlNode, anchor []byte) {
	if anchor != nil {
		p.doc.anchors[string(anchor)] = n
	}
}
func (p *yamlParser) parse() *yamlNode {
	p.init()
	switch p.peek() {
	case yaml_SCALAR_EVENT:
		return p.scalar()
	case yaml_ALIAS_EVENT:
		return p.alias()
	case yaml_MAPPING_START_EVENT:
		return p.mapping()
	case yaml_SEQUENCE_START_EVENT:
		return p.sequence()
	case yaml_DOCUMENT_START_EVENT:
		return p.document()
	case yaml_STREAM_END_EVENT:
		return nil
	default:
		panic("attempted to parse unknown event: " + p.event.typ.String())
	}
}
func (p *yamlParser) node(kind int) *yamlNode {
	return &yamlNode{
		kind:   kind,
		line:   p.event.start_mark.line,
		column: p.event.start_mark.column,
	}
}
func (p *yamlParser) document() *yamlNode {
	n := p.node(yamlDocumentNode)
	n.anchors = make(map[string]*yamlNode)
	p.doc = n
	p.expect(yaml_DOCUMENT_START_EVENT)
	n.children = append(n.children, p.parse())
	p.expect(yaml_DOCUMENT_END_EVENT)
	return n
}
func (p *yamlParser) alias() *yamlNode {
	n := p.node(yamlAliasNode)
	n.value = string(p.event.anchor)
	n.alias = p.doc.anchors[n.value]
	if n.alias == nil {
		yamlFailf("unknown anchor '%s' referenced", n.value)
	}
	p.expect(yaml_ALIAS_EVENT)
	return n
}
func (p *yamlParser) scalar() *yamlNode {
	n := p.node(yamlScalarNode)
	n.value = string(p.event.value)
	n.tag = string(p.event.tag)
	n.implicit = p.event.implicit
	p.anchor(n, p.event.anchor)
	p.expect(yaml_SCALAR_EVENT)
	return n
}
func (p *yamlParser) sequence() *yamlNode {
	n := p.node(yamlSequenceNode)
	p.anchor(n, p.event.anchor)
	p.expect(yaml_SEQUENCE_START_EVENT)
	for p.peek() != yaml_SEQUENCE_END_EVENT {
		n.children = append(n.children, p.parse())
	}
	p.expect(yaml_SEQUENCE_END_EVENT)
	return n
}
func (p *yamlParser) mapping() *yamlNode {
	n := p.node(yamlMappingNode)
	p.anchor(n, p.event.anchor)
	p.expect(yaml_MAPPING_START_EVENT)
	for p.peek() != yaml_MAPPING_END_EVENT {
		n.children = append(n.children, p.parse(), p.parse())
	}
	p.expect(yaml_MAPPING_END_EVENT)
	return n
}

type yamlDecoder struct {
	doc         *yamlNode
	aliases     map[*yamlNode]bool
	mapType     reflect.Type
	terrors     []string
	strict      bool
	decodeCount int
	aliasCount  int
	aliasDepth  int
}

var (
	yamlMapItemType    = reflect.TypeOf(yamlMapItem{})
	yamlDurationType   = reflect.TypeOf(time.Duration(0))
	yamlDefaultMapType = reflect.TypeOf(map[interface{}]interface{}{})
	yamlIfaceType      = yamlDefaultMapType.Elem()
	yamlTimeType       = reflect.TypeOf(time.Time{})
	yamlPtrTimeType    = reflect.TypeOf(&time.Time{})
)

func yamlNewDecoder(strict bool) *yamlDecoder {
	d := &yamlDecoder{mapType: yamlDefaultMapType, strict: strict}
	d.aliases = make(map[*yamlNode]bool)
	return d
}
func (d *yamlDecoder) terror(n *yamlNode, tag string, out reflect.Value) {
	if n.tag != "" {
		tag = n.tag
	}
	value := n.value
	if tag != yaml_SEQ_TAG && tag != yaml_MAP_TAG {
		if len(value) > 10 {
			value = " `" + value[:7] + "...`"
		} else {
			value = " `" + value + "`"
		}
	}
	d.terrors = append(d.terrors, fmt.Sprintf("line %d: cannot unmarshal %s%s into %s", n.line+1, yamlShortTag(tag), value, out.Type()))
}
func (d *yamlDecoder) prepare(n *yamlNode, out reflect.Value) (newout reflect.Value, unmarshaled, good bool) {
	if n.tag == yaml_NULL_TAG || n.kind == yamlScalarNode && n.tag == "" && (n.value == "null" || n.value == "~" || n.value == "" && n.implicit) {
		return out, false, false
	}
	again := true
	for again {
		again = false
		if out.Kind() == reflect.Ptr {
			if out.IsNil() {
				out.Set(reflect.New(out.Type().Elem()))
			}
			out = out.Elem()
			again = true
		}
	}
	return out, false, false
}

const (
	yaml_alias_ratio_range_low  = 400000
	yaml_alias_ratio_range_high = 4000000
	yaml_alias_ratio_range      = float64(yaml_alias_ratio_range_high - yaml_alias_ratio_range_low)
)

func yamlAllowedAliasRatio(decodeCount int) float64 {
	switch {
	case decodeCount <= yaml_alias_ratio_range_low:
		return 0.99
	case decodeCount >= yaml_alias_ratio_range_high:
		return 0.10
	default:
		return 0.99 - 0.89*(float64(decodeCount-yaml_alias_ratio_range_low)/yaml_alias_ratio_range)
	}
}
func (d *yamlDecoder) unmarshal(n *yamlNode, out reflect.Value) (good bool) {
	d.decodeCount++
	if d.aliasDepth > 0 {
		d.aliasCount++
	}
	if d.aliasCount > 100 && d.decodeCount > 1000 && float64(d.aliasCount)/float64(d.decodeCount) > yamlAllowedAliasRatio(d.decodeCount) {
		yamlFailf("document contains excessive aliasing")
	}
	switch n.kind {
	case yamlDocumentNode:
		return d.document(n, out)
	case yamlAliasNode:
		return d.alias(n, out)
	}
	out, unmarshaled, good := d.prepare(n, out)
	if unmarshaled {
		return good
	}
	switch n.kind {
	case yamlScalarNode:
		good = d.scalar(n, out)
	case yamlMappingNode:
		good = d.mapping(n, out)
	case yamlSequenceNode:
		good = d.sequence(n, out)
	default:
		panic("internal error: unknown yamlNode kind: " + strconv.Itoa(n.kind))
	}
	return good
}
func (d *yamlDecoder) document(n *yamlNode, out reflect.Value) (good bool) {
	if len(n.children) == 1 {
		d.doc = n
		d.unmarshal(n.children[0], out)
		return true
	}
	return false
}
func (d *yamlDecoder) alias(n *yamlNode, out reflect.Value) (good bool) {
	if d.aliases[n] {
		yamlFailf("anchor '%s' value contains itself", n.value)
	}
	d.aliases[n] = true
	d.aliasDepth++
	good = d.unmarshal(n.alias, out)
	d.aliasDepth--
	delete(d.aliases, n)
	return good
}

var yamlZeroValue reflect.Value

func (d *yamlDecoder) scalar(n *yamlNode, out reflect.Value) bool {
	var tag string
	var resolved interface{}
	if n.tag == "" && !n.implicit {
		tag = yaml_STR_TAG
		resolved = n.value
	} else {
		tag, resolved = yamlResolve(n.tag, n.value)
		if tag == yaml_BINARY_TAG {
			data, err := base64.StdEncoding.DecodeString(resolved.(string))
			if err != nil {
				yamlFailf("!!binary value contains invalid base64 data")
			}
			resolved = string(data)
		}
	}
	if resolved == nil {
		if out.Kind() == reflect.Map && !out.CanAddr() {
			for _, k := range out.MapKeys() {
				out.SetMapIndex(k, yamlZeroValue)
			}
		} else {
			out.Set(reflect.Zero(out.Type()))
		}
		return true
	}
	if resolvedv := reflect.ValueOf(resolved); out.Type() == resolvedv.Type() {
		out.Set(resolvedv)
		return true
	}
	if out.CanAddr() {
		u, ok := out.Addr().Interface().(encoding.TextUnmarshaler)
		if ok {
			var text []byte
			if tag == yaml_BINARY_TAG {
				text = []byte(resolved.(string))
			} else {
				text = []byte(n.value)
			}
			err := u.UnmarshalText(text)
			if err != nil {
				panic(yamlError{err})
			}
			return true
		}
	}
	switch out.Kind() {
	case reflect.String:
		if tag == yaml_BINARY_TAG {
			out.SetString(resolved.(string))
			return true
		}
		if resolved != nil {
			out.SetString(n.value)
			return true
		}
	case reflect.Interface:
		if resolved == nil {
			out.Set(reflect.Zero(out.Type()))
		} else if tag == yaml_TIMESTAMP_TAG {
			out.Set(reflect.ValueOf(n.value))
		} else {
			out.Set(reflect.ValueOf(resolved))
		}
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch resolved := resolved.(type) {
		case int:
			if !out.OverflowInt(int64(resolved)) {
				out.SetInt(int64(resolved))
				return true
			}
		case int64:
			if !out.OverflowInt(resolved) {
				out.SetInt(resolved)
				return true
			}
		case uint64:
			if resolved <= math.MaxInt64 && !out.OverflowInt(int64(resolved)) {
				out.SetInt(int64(resolved))
				return true
			}
		case float64:
			if resolved <= math.MaxInt64 && !out.OverflowInt(int64(resolved)) {
				out.SetInt(int64(resolved))
				return true
			}
		case string:
			if out.Type() == yamlDurationType {
				d, err := time.ParseDuration(resolved)
				if err == nil {
					out.SetInt(int64(d))
					return true
				}
			}
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch resolved := resolved.(type) {
		case int:
			if resolved >= 0 && !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		case int64:
			if resolved >= 0 && !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		case uint64:
			if !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		case float64:
			if resolved <= math.MaxUint64 && !out.OverflowUint(uint64(resolved)) {
				out.SetUint(uint64(resolved))
				return true
			}
		}
	case reflect.Bool:
		switch resolved := resolved.(type) {
		case bool:
			out.SetBool(resolved)
			return true
		}
	case reflect.Float32, reflect.Float64:
		switch resolved := resolved.(type) {
		case int:
			out.SetFloat(float64(resolved))
			return true
		case int64:
			out.SetFloat(float64(resolved))
			return true
		case uint64:
			out.SetFloat(float64(resolved))
			return true
		case float64:
			out.SetFloat(resolved)
			return true
		}
	case reflect.Struct:
		if resolvedv := reflect.ValueOf(resolved); out.Type() == resolvedv.Type() {
			out.Set(resolvedv)
			return true
		}
	case reflect.Ptr:
		if out.Type().Elem() == reflect.TypeOf(resolved) {
			elem := reflect.New(out.Type().Elem())
			elem.Elem().Set(reflect.ValueOf(resolved))
			out.Set(elem)
			return true
		}
	}
	d.terror(n, tag, out)
	return false
}
func yamlSettableValueOf(i interface{}) reflect.Value {
	v := reflect.ValueOf(i)
	sv := reflect.New(v.Type()).Elem()
	sv.Set(v)
	return sv
}
func (d *yamlDecoder) sequence(n *yamlNode, out reflect.Value) (good bool) {
	l := len(n.children)
	var iface reflect.Value
	switch out.Kind() {
	case reflect.Slice:
		out.Set(reflect.MakeSlice(out.Type(), l, l))
	case reflect.Array:
		if l != out.Len() {
			yamlFailf("invalid array: want %d elements but got %d", out.Len(), l)
		}
	case reflect.Interface:
		iface = out
		out = yamlSettableValueOf(make([]interface{}, l))
	default:
		d.terror(n, yaml_SEQ_TAG, out)
		return false
	}
	et := out.Type().Elem()
	j := 0
	for i := 0; i < l; i++ {
		e := reflect.New(et).Elem()
		if ok := d.unmarshal(n.children[i], e); ok {
			out.Index(j).Set(e)
			j++
		}
	}
	if out.Kind() != reflect.Array {
		out.Set(out.Slice(0, j))
	}
	if iface.IsValid() {
		iface.Set(out)
	}
	return true
}
func (d *yamlDecoder) mapping(n *yamlNode, out reflect.Value) (good bool) {
	switch out.Kind() {
	case reflect.Struct:
		return d.mappingStruct(n, out)
	case reflect.Slice:
		return d.mappingSlice(n, out)
	case reflect.Map:
	case reflect.Interface:
		if d.mapType.Kind() == reflect.Map {
			iface := out
			out = reflect.MakeMap(d.mapType)
			iface.Set(out)
		} else {
			slicev := reflect.New(d.mapType).Elem()
			if !d.mappingSlice(n, slicev) {
				return false
			}
			out.Set(slicev)
			return true
		}
	default:
		d.terror(n, yaml_MAP_TAG, out)
		return false
	}
	outt := out.Type()
	kt := outt.Key()
	et := outt.Elem()
	mapType := d.mapType
	if outt.Key() == yamlIfaceType && outt.Elem() == yamlIfaceType {
		d.mapType = outt
	}
	if out.IsNil() {
		out.Set(reflect.MakeMap(outt))
	}
	l := len(n.children)
	for i := 0; i < l; i += 2 {
		if yamlIsMerge(n.children[i]) {
			d.merge(n.children[i+1], out)
			continue
		}
		k := reflect.New(kt).Elem()
		if d.unmarshal(n.children[i], k) {
			kkind := k.Kind()
			if kkind == reflect.Interface {
				kkind = k.Elem().Kind()
			}
			if kkind == reflect.Map || kkind == reflect.Slice {
				yamlFailf("invalid map key: %#v", k.Interface())
			}
			e := reflect.New(et).Elem()
			if d.unmarshal(n.children[i+1], e) {
				d.setMapIndex(n.children[i+1], out, k, e)
			}
		}
	}
	d.mapType = mapType
	return true
}
func (d *yamlDecoder) setMapIndex(n *yamlNode, out, k, v reflect.Value) {
	if d.strict && out.MapIndex(k) != yamlZeroValue {
		d.terrors = append(d.terrors, fmt.Sprintf("line %d: key %#v already set in map", n.line+1, k.Interface()))
		return
	}
	out.SetMapIndex(k, v)
}
func (d *yamlDecoder) mappingSlice(n *yamlNode, out reflect.Value) (good bool) {
	outt := out.Type()
	if outt.Elem() != yamlMapItemType {
		d.terror(n, yaml_MAP_TAG, out)
		return false
	}
	mapType := d.mapType
	d.mapType = outt
	var slice []yamlMapItem
	var l = len(n.children)
	for i := 0; i < l; i += 2 {
		if yamlIsMerge(n.children[i]) {
			d.merge(n.children[i+1], out)
			continue
		}
		item := yamlMapItem{}
		k := reflect.ValueOf(&item.Key).Elem()
		if d.unmarshal(n.children[i], k) {
			v := reflect.ValueOf(&item.Value).Elem()
			if d.unmarshal(n.children[i+1], v) {
				slice = append(slice, item)
			}
		}
	}
	out.Set(reflect.ValueOf(slice))
	d.mapType = mapType
	return true
}
func (d *yamlDecoder) mappingStruct(n *yamlNode, out reflect.Value) (good bool) {
	sinfo, err := yamlGetStructInfo(out.Type())
	if err != nil {
		panic(err)
	}
	name := yamlSettableValueOf("")
	l := len(n.children)
	var inlineMap reflect.Value
	var elemType reflect.Type
	if sinfo.InlineMap != -1 {
		inlineMap = out.Field(sinfo.InlineMap)
		inlineMap.Set(reflect.New(inlineMap.Type()).Elem())
		elemType = inlineMap.Type().Elem()
	}
	var doneFields []bool
	if d.strict {
		doneFields = make([]bool, len(sinfo.FieldsList))
	}
	for i := 0; i < l; i += 2 {
		ni := n.children[i]
		if yamlIsMerge(ni) {
			d.merge(n.children[i+1], out)
			continue
		}
		if !d.unmarshal(ni, name) {
			continue
		}
		if info, ok := sinfo.FieldsMap[name.String()]; ok {
			if d.strict {
				if doneFields[info.Id] {
					d.terrors = append(d.terrors, fmt.Sprintf("line %d: field %s already set in type %s", ni.line+1, name.String(), out.Type()))
					continue
				}
				doneFields[info.Id] = true
			}
			var field reflect.Value
			if info.Inline == nil {
				field = out.Field(info.Num)
			} else {
				field = out.FieldByIndex(info.Inline)
			}
			d.unmarshal(n.children[i+1], field)
		} else if sinfo.InlineMap != -1 {
			if inlineMap.IsNil() {
				inlineMap.Set(reflect.MakeMap(inlineMap.Type()))
			}
			value := reflect.New(elemType).Elem()
			d.unmarshal(n.children[i+1], value)
			d.setMapIndex(n.children[i+1], inlineMap, name, value)
		} else if d.strict {
			d.terrors = append(d.terrors, fmt.Sprintf("line %d: field %s not found in type %s", ni.line+1, name.String(), out.Type()))
		}
	}
	return true
}
func (d *yamlDecoder) merge(n *yamlNode, out reflect.Value) {
	failWantMap := func() {
		yamlFailf("map merge requires map or sequence of maps as the value")
	}
	switch n.kind {
	case yamlMappingNode:
		d.unmarshal(n, out)
	case yamlAliasNode:
		if n.alias != nil && n.alias.kind != yamlMappingNode {
			failWantMap()
		}
		d.unmarshal(n, out)
	case yamlSequenceNode:
		for i := len(n.children) - 1; i >= 0; i-- {
			ni := n.children[i]
			if ni.kind == yamlAliasNode {
				if ni.alias != nil && ni.alias.kind != yamlMappingNode {
					failWantMap()
				}
			} else if ni.kind != yamlMappingNode {
				failWantMap()
			}
			d.unmarshal(ni, out)
		}
	default:
		failWantMap()
	}
}
func yamlIsMerge(n *yamlNode) bool {
	return n.kind == yamlScalarNode && n.value == "<<" && (n.implicit == true || n.tag == yaml_MERGE_TAG)
}

type yamlResolveMapItem struct {
	value interface{}
	tag   string
}

var yamlResolveTable = make([]byte, 256)
var yamlResolveMap = make(map[string]yamlResolveMapItem)

func init() {
	t := yamlResolveTable
	t[int('+')] = 'S'
	t[int('-')] = 'S'
	for _, c := range "0123456789" {
		t[int(c)] = 'D'
	}
	for _, c := range "yYnNtTfFoO~" {
		t[int(c)] = 'M'
	}
	t[int('.')] = '.'
	var resolveMapList = []struct {
		v   interface{}
		tag string
		l   []string
	}{
		{true, yaml_BOOL_TAG, []string{"y", "Y", "yes", "Yes", "YES"}},
		{true, yaml_BOOL_TAG, []string{"true", "True", "TRUE"}},
		{true, yaml_BOOL_TAG, []string{"on", "On", "ON"}},
		{false, yaml_BOOL_TAG, []string{"n", "N", "no", "No", "NO"}},
		{false, yaml_BOOL_TAG, []string{"false", "False", "FALSE"}},
		{false, yaml_BOOL_TAG, []string{"off", "Off", "OFF"}},
		{nil, yaml_NULL_TAG, []string{"", "~", "null", "Null", "NULL"}},
		{math.NaN(), yaml_FLOAT_TAG, []string{".nan", ".NaN", ".NAN"}},
		{math.Inf(+1), yaml_FLOAT_TAG, []string{".inf", ".Inf", ".INF"}},
		{math.Inf(+1), yaml_FLOAT_TAG, []string{"+.inf", "+.Inf", "+.INF"}},
		{math.Inf(-1), yaml_FLOAT_TAG, []string{"-.inf", "-.Inf", "-.INF"}},
		{"<<", yaml_MERGE_TAG, []string{"<<"}},
	}
	m := yamlResolveMap
	for _, item := range resolveMapList {
		for _, s := range item.l {
			m[s] = yamlResolveMapItem{item.v, item.tag}
		}
	}
}

const yamlLongTagPrefix = "tag:yaml.org,2002:"

func yamlShortTag(tag string) string {
	if strings.HasPrefix(tag, yamlLongTagPrefix) {
		return "!!" + tag[len(yamlLongTagPrefix):]
	}
	return tag
}
func yamlResolvableTag(tag string) bool {
	switch tag {
	case "", yaml_STR_TAG, yaml_BOOL_TAG, yaml_INT_TAG, yaml_FLOAT_TAG, yaml_NULL_TAG, yaml_TIMESTAMP_TAG:
		return true
	}
	return false
}

var yamlStyleFloat = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)

func yamlResolve(tag string, in string) (rtag string, out interface{}) {
	if !yamlResolvableTag(tag) {
		return tag, in
	}
	defer func() {
		switch tag {
		case "", rtag, yaml_STR_TAG, yaml_BINARY_TAG:
			return
		case yaml_FLOAT_TAG:
			if rtag == yaml_INT_TAG {
				switch v := out.(type) {
				case int64:
					rtag = yaml_FLOAT_TAG
					out = float64(v)
					return
				case int:
					rtag = yaml_FLOAT_TAG
					out = float64(v)
					return
				}
			}
		}
		yamlFailf("cannot decode %s `%s` as a %s", yamlShortTag(rtag), in, yamlShortTag(tag))
	}()
	hint := byte('N')
	if in != "" {
		hint = yamlResolveTable[in[0]]
	}
	if hint != 0 && tag != yaml_STR_TAG && tag != yaml_BINARY_TAG {
		if item, ok := yamlResolveMap[in]; ok {
			return item.tag, item.value
		}
		switch hint {
		case 'M':
		case '.':
			floatv, err := strconv.ParseFloat(in, 64)
			if err == nil {
				return yaml_FLOAT_TAG, floatv
			}
		case 'D', 'S':
			if tag == "" || tag == yaml_TIMESTAMP_TAG {
				t, ok := yamlParseTimestamp(in)
				if ok {
					return yaml_TIMESTAMP_TAG, t
				}
			}
			plain := strings.Replace(in, "_", "", -1)
			intv, err := strconv.ParseInt(plain, 0, 64)
			if err == nil {
				if intv == int64(int(intv)) {
					return yaml_INT_TAG, int(intv)
				} else {
					return yaml_INT_TAG, intv
				}
			}
			uintv, err := strconv.ParseUint(plain, 0, 64)
			if err == nil {
				return yaml_INT_TAG, uintv
			}
			if yamlStyleFloat.MatchString(plain) {
				floatv, err := strconv.ParseFloat(plain, 64)
				if err == nil {
					return yaml_FLOAT_TAG, floatv
				}
			}
			if strings.HasPrefix(plain, "0b") {
				intv, err := strconv.ParseInt(plain[2:], 2, 64)
				if err == nil {
					if intv == int64(int(intv)) {
						return yaml_INT_TAG, int(intv)
					} else {
						return yaml_INT_TAG, intv
					}
				}
				uintv, err := strconv.ParseUint(plain[2:], 2, 64)
				if err == nil {
					return yaml_INT_TAG, uintv
				}
			} else if strings.HasPrefix(plain, "-0b") {
				intv, err := strconv.ParseInt("-"+plain[3:], 2, 64)
				if err == nil {
					if true || intv == int64(int(intv)) {
						return yaml_INT_TAG, int(intv)
					} else {
						return yaml_INT_TAG, intv
					}
				}
			}
		default:
			panic("yamlResolveTable item not yet handled: " + string(rune(hint)) + " (with " + in + ")")
		}
	}
	return yaml_STR_TAG, in
}

var yamlAllowedTimestampFormats = []string{
	"2006-1-2T15:4:5.999999999Z07:00",
	"2006-1-2t15:4:5.999999999Z07:00",
	"2006-1-2 15:4:5.999999999",
	"2006-1-2",
}

func yamlParseTimestamp(s string) (time.Time, bool) {
	i := 0
	for ; i < len(s); i++ {
		if c := s[i]; c < '0' || c > '9' {
			break
		}
	}
	if i != 4 || i == len(s) || s[i] != '-' {
		return time.Time{}, false
	}
	for _, format := range yamlAllowedTimestampFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type yaml_version_directive_t struct {
	major int8
	minor int8
}
type yaml_tag_directive_t struct {
	handle []byte
	prefix []byte
}
type yaml_encoding_t int

const (
	yaml_ANY_ENCODING yaml_encoding_t = iota
	yaml_UTF8_ENCODING
	yaml_UTF16LE_ENCODING
	yaml_UTF16BE_ENCODING
)

type yaml_break_t int

const (
	yaml_ANY_BREAK yaml_break_t = iota
	yaml_CR_BREAK
	yaml_LN_BREAK
	yaml_CRLN_BREAK
)

type yaml_error_type_t int

const (
	yaml_NO_ERROR yaml_error_type_t = iota
	yaml_MEMORY_ERROR
	yaml_READER_ERROR
	yaml_SCANNER_ERROR
	yaml_PARSER_ERROR
	yaml_COMPOSER_ERROR
	yaml_WRITER_ERROR
	yaml_EMITTER_ERROR
)

type yaml_mark_t struct {
	index  int
	line   int
	column int
}
type yaml_style_t int8
type yaml_scalar_style_t yaml_style_t

const (
	yaml_ANY_SCALAR_STYLE yaml_scalar_style_t = iota
	yaml_PLAIN_SCALAR_STYLE
	yaml_SINGLE_QUOTED_SCALAR_STYLE
	yaml_DOUBLE_QUOTED_SCALAR_STYLE
	yaml_LITERAL_SCALAR_STYLE
	yaml_FOLDED_SCALAR_STYLE
)

type yaml_sequence_style_t yaml_style_t

const (
	yaml_ANY_SEQUENCE_STYLE yaml_sequence_style_t = iota
	yaml_BLOCK_SEQUENCE_STYLE
	yaml_FLOW_SEQUENCE_STYLE
)

type yaml_mapping_style_t yaml_style_t

const (
	yaml_ANY_MAPPING_STYLE yaml_mapping_style_t = iota
	yaml_BLOCK_MAPPING_STYLE
	yaml_FLOW_MAPPING_STYLE
)

type yaml_token_type_t int

const (
	yaml_NO_TOKEN yaml_token_type_t = iota
	yaml_STREAM_START_TOKEN
	yaml_STREAM_END_TOKEN
	yaml_VERSION_DIRECTIVE_TOKEN
	yaml_TAG_DIRECTIVE_TOKEN
	yaml_DOCUMENT_START_TOKEN
	yaml_DOCUMENT_END_TOKEN
	yaml_BLOCK_SEQUENCE_START_TOKEN
	yaml_BLOCK_MAPPING_START_TOKEN
	yaml_BLOCK_END_TOKEN
	yaml_FLOW_SEQUENCE_START_TOKEN
	yaml_FLOW_SEQUENCE_END_TOKEN
	yaml_FLOW_MAPPING_START_TOKEN
	yaml_FLOW_MAPPING_END_TOKEN
	yaml_BLOCK_ENTRY_TOKEN
	yaml_FLOW_ENTRY_TOKEN
	yaml_KEY_TOKEN
	yaml_VALUE_TOKEN
	yaml_ALIAS_TOKEN
	yaml_ANCHOR_TOKEN
	yaml_TAG_TOKEN
	yaml_SCALAR_TOKEN
)

func (tt yaml_token_type_t) String() string {
	switch tt {
	case yaml_NO_TOKEN:
		return "yaml_NO_TOKEN"
	case yaml_STREAM_START_TOKEN:
		return "yaml_STREAM_START_TOKEN"
	case yaml_STREAM_END_TOKEN:
		return "yaml_STREAM_END_TOKEN"
	case yaml_VERSION_DIRECTIVE_TOKEN:
		return "yaml_VERSION_DIRECTIVE_TOKEN"
	case yaml_TAG_DIRECTIVE_TOKEN:
		return "yaml_TAG_DIRECTIVE_TOKEN"
	case yaml_DOCUMENT_START_TOKEN:
		return "yaml_DOCUMENT_START_TOKEN"
	case yaml_DOCUMENT_END_TOKEN:
		return "yaml_DOCUMENT_END_TOKEN"
	case yaml_BLOCK_SEQUENCE_START_TOKEN:
		return "yaml_BLOCK_SEQUENCE_START_TOKEN"
	case yaml_BLOCK_MAPPING_START_TOKEN:
		return "yaml_BLOCK_MAPPING_START_TOKEN"
	case yaml_BLOCK_END_TOKEN:
		return "yaml_BLOCK_END_TOKEN"
	case yaml_FLOW_SEQUENCE_START_TOKEN:
		return "yaml_FLOW_SEQUENCE_START_TOKEN"
	case yaml_FLOW_SEQUENCE_END_TOKEN:
		return "yaml_FLOW_SEQUENCE_END_TOKEN"
	case yaml_FLOW_MAPPING_START_TOKEN:
		return "yaml_FLOW_MAPPING_START_TOKEN"
	case yaml_FLOW_MAPPING_END_TOKEN:
		return "yaml_FLOW_MAPPING_END_TOKEN"
	case yaml_BLOCK_ENTRY_TOKEN:
		return "yaml_BLOCK_ENTRY_TOKEN"
	case yaml_FLOW_ENTRY_TOKEN:
		return "yaml_FLOW_ENTRY_TOKEN"
	case yaml_KEY_TOKEN:
		return "yaml_KEY_TOKEN"
	case yaml_VALUE_TOKEN:
		return "yaml_VALUE_TOKEN"
	case yaml_ALIAS_TOKEN:
		return "yaml_ALIAS_TOKEN"
	case yaml_ANCHOR_TOKEN:
		return "yaml_ANCHOR_TOKEN"
	case yaml_TAG_TOKEN:
		return "yaml_TAG_TOKEN"
	case yaml_SCALAR_TOKEN:
		return "yaml_SCALAR_TOKEN"
	}
	return "<unknown token>"
}

type yaml_token_t struct {
	typ                  yaml_token_type_t
	start_mark, end_mark yaml_mark_t
	encoding             yaml_encoding_t
	value                []byte
	suffix               []byte
	prefix               []byte
	style                yaml_scalar_style_t
	major, minor         int8
}
type yaml_event_type_t int8

const (
	yaml_NO_EVENT yaml_event_type_t = iota
	yaml_STREAM_START_EVENT
	yaml_STREAM_END_EVENT
	yaml_DOCUMENT_START_EVENT
	yaml_DOCUMENT_END_EVENT
	yaml_ALIAS_EVENT
	yaml_SCALAR_EVENT
	yaml_SEQUENCE_START_EVENT
	yaml_SEQUENCE_END_EVENT
	yaml_MAPPING_START_EVENT
	yaml_MAPPING_END_EVENT
)

var yamlEventStrings = []string{
	yaml_NO_EVENT:             "none",
	yaml_STREAM_START_EVENT:   "stream start",
	yaml_STREAM_END_EVENT:     "stream end",
	yaml_DOCUMENT_START_EVENT: "document start",
	yaml_DOCUMENT_END_EVENT:   "document end",
	yaml_ALIAS_EVENT:          "alias",
	yaml_SCALAR_EVENT:         "scalar",
	yaml_SEQUENCE_START_EVENT: "sequence start",
	yaml_SEQUENCE_END_EVENT:   "sequence end",
	yaml_MAPPING_START_EVENT:  "mapping start",
	yaml_MAPPING_END_EVENT:    "mapping end",
}

func (e yaml_event_type_t) String() string {
	if e < 0 || int(e) >= len(yamlEventStrings) {
		return fmt.Sprintf("unknown event %d", e)
	}
	return yamlEventStrings[e]
}

type yaml_event_t struct {
	typ                  yaml_event_type_t
	start_mark, end_mark yaml_mark_t
	encoding             yaml_encoding_t
	version_directive    *yaml_version_directive_t
	tag_directives       []yaml_tag_directive_t
	anchor               []byte
	tag                  []byte
	value                []byte
	implicit             bool
	quoted_implicit      bool
	style                yaml_style_t
}

func (e *yaml_event_t) scalar_style() yaml_scalar_style_t     { return yaml_scalar_style_t(e.style) }
func (e *yaml_event_t) sequence_style() yaml_sequence_style_t { return yaml_sequence_style_t(e.style) }
func (e *yaml_event_t) mapping_style() yaml_mapping_style_t   { return yaml_mapping_style_t(e.style) }

const (
	yaml_NULL_TAG             = "tag:yaml.org,2002:null"
	yaml_BOOL_TAG             = "tag:yaml.org,2002:bool"
	yaml_STR_TAG              = "tag:yaml.org,2002:str"
	yaml_INT_TAG              = "tag:yaml.org,2002:int"
	yaml_FLOAT_TAG            = "tag:yaml.org,2002:float"
	yaml_TIMESTAMP_TAG        = "tag:yaml.org,2002:timestamp"
	yaml_SEQ_TAG              = "tag:yaml.org,2002:seq"
	yaml_MAP_TAG              = "tag:yaml.org,2002:map"
	yaml_BINARY_TAG           = "tag:yaml.org,2002:binary"
	yaml_MERGE_TAG            = "tag:yaml.org,2002:merge"
	yaml_DEFAULT_SCALAR_TAG   = yaml_STR_TAG
	yaml_DEFAULT_SEQUENCE_TAG = yaml_SEQ_TAG
	yaml_DEFAULT_MAPPING_TAG  = yaml_MAP_TAG
)

type yaml_node_type_t int

const (
	yaml_NO_NODE yaml_node_type_t = iota
	yaml_SCALAR_NODE
	yaml_SEQUENCE_NODE
	yaml_MAPPING_NODE
)

type yaml_node_item_t int
type yaml_node_pair_t struct {
	key   int
	value int
}
type yaml_node_t struct {
	typ    yaml_node_type_t
	tag    []byte
	scalar struct {
		value  []byte
		length int
		style  yaml_scalar_style_t
	}
	sequence struct {
		items_data []yaml_node_item_t
		style      yaml_sequence_style_t
	}
	mapping struct {
		pairs_data  []yaml_node_pair_t
		pairs_start *yaml_node_pair_t
		pairs_end   *yaml_node_pair_t
		pairs_top   *yaml_node_pair_t
		style       yaml_mapping_style_t
	}
	start_mark yaml_mark_t
	end_mark   yaml_mark_t
}
type yaml_document_t struct {
	nodes                []yaml_node_t
	version_directive    *yaml_version_directive_t
	tag_directives_data  []yaml_tag_directive_t
	tag_directives_start int
	tag_directives_end   int
	start_implicit       int
	end_implicit         int
	start_mark, end_mark yaml_mark_t
}
type yaml_read_handler_t func(parser *yaml_parser_t, buffer []byte) (n int, err error)
type yaml_simple_key_t struct {
	possible     bool
	required     bool
	token_number int
	mark         yaml_mark_t
}
type yaml_parser_state_t int

const (
	yaml_PARSE_STREAM_START_STATE yaml_parser_state_t = iota
	yaml_PARSE_IMPLICIT_DOCUMENT_START_STATE
	yaml_PARSE_DOCUMENT_START_STATE
	yaml_PARSE_DOCUMENT_CONTENT_STATE
	yaml_PARSE_DOCUMENT_END_STATE
	yaml_PARSE_BLOCK_NODE_STATE
	yaml_PARSE_BLOCK_NODE_OR_INDENTLESS_SEQUENCE_STATE
	yaml_PARSE_FLOW_NODE_STATE
	yaml_PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE
	yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE
	yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE
	yaml_PARSE_BLOCK_MAPPING_FIRST_KEY_STATE
	yaml_PARSE_BLOCK_MAPPING_KEY_STATE
	yaml_PARSE_BLOCK_MAPPING_VALUE_STATE
	yaml_PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE
	yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE
	yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE
	yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE
	yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE
	yaml_PARSE_FLOW_MAPPING_FIRST_KEY_STATE
	yaml_PARSE_FLOW_MAPPING_KEY_STATE
	yaml_PARSE_FLOW_MAPPING_VALUE_STATE
	yaml_PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE
	yaml_PARSE_END_STATE
)

func (ps yaml_parser_state_t) String() string {
	switch ps {
	case yaml_PARSE_STREAM_START_STATE:
		return "yaml_PARSE_STREAM_START_STATE"
	case yaml_PARSE_IMPLICIT_DOCUMENT_START_STATE:
		return "yaml_PARSE_IMPLICIT_DOCUMENT_START_STATE"
	case yaml_PARSE_DOCUMENT_START_STATE:
		return "yaml_PARSE_DOCUMENT_START_STATE"
	case yaml_PARSE_DOCUMENT_CONTENT_STATE:
		return "yaml_PARSE_DOCUMENT_CONTENT_STATE"
	case yaml_PARSE_DOCUMENT_END_STATE:
		return "yaml_PARSE_DOCUMENT_END_STATE"
	case yaml_PARSE_BLOCK_NODE_STATE:
		return "yaml_PARSE_BLOCK_NODE_STATE"
	case yaml_PARSE_BLOCK_NODE_OR_INDENTLESS_SEQUENCE_STATE:
		return "yaml_PARSE_BLOCK_NODE_OR_INDENTLESS_SEQUENCE_STATE"
	case yaml_PARSE_FLOW_NODE_STATE:
		return "yaml_PARSE_FLOW_NODE_STATE"
	case yaml_PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE:
		return "yaml_PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE"
	case yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE:
		return "yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE"
	case yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE:
		return "yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE"
	case yaml_PARSE_BLOCK_MAPPING_FIRST_KEY_STATE:
		return "yaml_PARSE_BLOCK_MAPPING_FIRST_KEY_STATE"
	case yaml_PARSE_BLOCK_MAPPING_KEY_STATE:
		return "yaml_PARSE_BLOCK_MAPPING_KEY_STATE"
	case yaml_PARSE_BLOCK_MAPPING_VALUE_STATE:
		return "yaml_PARSE_BLOCK_MAPPING_VALUE_STATE"
	case yaml_PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE:
		return "yaml_PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE"
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE:
		return "yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE"
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE:
		return "yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE"
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE:
		return "yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE"
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE:
		return "yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE"
	case yaml_PARSE_FLOW_MAPPING_FIRST_KEY_STATE:
		return "yaml_PARSE_FLOW_MAPPING_FIRST_KEY_STATE"
	case yaml_PARSE_FLOW_MAPPING_KEY_STATE:
		return "yaml_PARSE_FLOW_MAPPING_KEY_STATE"
	case yaml_PARSE_FLOW_MAPPING_VALUE_STATE:
		return "yaml_PARSE_FLOW_MAPPING_VALUE_STATE"
	case yaml_PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE:
		return "yaml_PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE"
	case yaml_PARSE_END_STATE:
		return "yaml_PARSE_END_STATE"
	}
	return "<unknown yamlParser state>"
}

type yaml_alias_data_t struct {
	anchor []byte
	index  int
	mark   yaml_mark_t
}
type yaml_parser_t struct {
	error                 yaml_error_type_t
	problem               string
	problem_offset        int
	problem_value         int
	problem_mark          yaml_mark_t
	context               string
	context_mark          yaml_mark_t
	read_handler          yaml_read_handler_t
	input_reader          io.Reader
	input                 []byte
	input_pos             int
	eof                   bool
	buffer                []byte
	buffer_pos            int
	unread                int
	raw_buffer            []byte
	raw_buffer_pos        int
	encoding              yaml_encoding_t
	offset                int
	mark                  yaml_mark_t
	stream_start_produced bool
	stream_end_produced   bool
	flow_level            int
	tokens                []yaml_token_t
	tokens_head           int
	tokens_parsed         int
	token_available       bool
	indent                int
	indents               []int
	simple_key_allowed    bool
	simple_keys           []yaml_simple_key_t
	simple_keys_by_tok    map[int]int
	state                 yaml_parser_state_t
	states                []yaml_parser_state_t
	marks                 []yaml_mark_t
	tag_directives        []yaml_tag_directive_t
	aliases               []yaml_alias_data_t
	document              *yaml_document_t
}

func yaml_insert_token(parser *yaml_parser_t, pos int, token *yaml_token_t) {
	if parser.tokens_head > 0 && len(parser.tokens) == cap(parser.tokens) {
		if parser.tokens_head != len(parser.tokens) {
			copy(parser.tokens, parser.tokens[parser.tokens_head:])
		}
		parser.tokens = parser.tokens[:len(parser.tokens)-parser.tokens_head]
		parser.tokens_head = 0
	}
	parser.tokens = append(parser.tokens, *token)
	if pos < 0 {
		return
	}
	copy(parser.tokens[parser.tokens_head+pos+1:], parser.tokens[parser.tokens_head+pos:])
	parser.tokens[parser.tokens_head+pos] = *token
}
func yaml_parser_initialize(parser *yaml_parser_t) bool {
	*parser = yaml_parser_t{
		raw_buffer: make([]byte, 0, 512),
		buffer:     make([]byte, 0, 1536),
	}
	return true
}
func yaml_parser_delete(parser *yaml_parser_t) {
	*parser = yaml_parser_t{}
}
func yaml_string_read_handler(parser *yaml_parser_t, buffer []byte) (n int, err error) {
	if parser.input_pos == len(parser.input) {
		return 0, io.EOF
	}
	n = copy(buffer, parser.input[parser.input_pos:])
	parser.input_pos += n
	return n, nil
}
func yaml_parser_set_input_string(parser *yaml_parser_t, input []byte) {
	if parser.read_handler != nil {
		panic("must set the input source only once")
	}
	parser.read_handler = yaml_string_read_handler
	parser.input = input
	parser.input_pos = 0
}
func yaml_event_delete(event *yaml_event_t) {
	*event = yaml_event_t{}
}
func yaml_peek_token(parser *yaml_parser_t) *yaml_token_t {
	if parser.token_available || yaml_parser_fetch_more_tokens(parser) {
		return &parser.tokens[parser.tokens_head]
	}
	return nil
}
func yaml_skip_token(parser *yaml_parser_t) {
	parser.token_available = false
	parser.tokens_parsed++
	parser.stream_end_produced = parser.tokens[parser.tokens_head].typ == yaml_STREAM_END_TOKEN
	parser.tokens_head++
}
func yaml_parser_parse(parser *yaml_parser_t, event *yaml_event_t) bool {
	*event = yaml_event_t{}
	if parser.stream_end_produced || parser.error != yaml_NO_ERROR || parser.state == yaml_PARSE_END_STATE {
		return true
	}
	return yaml_parser_state_machine(parser, event)
}
func yaml_parser_set_parser_error(parser *yaml_parser_t, problem string, problem_mark yaml_mark_t) bool {
	parser.error = yaml_PARSER_ERROR
	parser.problem = problem
	parser.problem_mark = problem_mark
	return false
}
func yaml_parser_set_parser_error_context(parser *yaml_parser_t, context string, context_mark yaml_mark_t, problem string, problem_mark yaml_mark_t) bool {
	parser.error = yaml_PARSER_ERROR
	parser.context = context
	parser.context_mark = context_mark
	parser.problem = problem
	parser.problem_mark = problem_mark
	return false
}
func yaml_parser_state_machine(parser *yaml_parser_t, event *yaml_event_t) bool {
	switch parser.state {
	case yaml_PARSE_STREAM_START_STATE:
		return yaml_parser_parse_stream_start(parser, event)
	case yaml_PARSE_IMPLICIT_DOCUMENT_START_STATE:
		return yaml_parser_parse_document_start(parser, event, true)
	case yaml_PARSE_DOCUMENT_START_STATE:
		return yaml_parser_parse_document_start(parser, event, false)
	case yaml_PARSE_DOCUMENT_CONTENT_STATE:
		return yaml_parser_parse_document_content(parser, event)
	case yaml_PARSE_DOCUMENT_END_STATE:
		return yaml_parser_parse_document_end(parser, event)
	case yaml_PARSE_BLOCK_NODE_STATE:
		return yaml_parser_parse_node(parser, event, true, false)
	case yaml_PARSE_BLOCK_NODE_OR_INDENTLESS_SEQUENCE_STATE:
		return yaml_parser_parse_node(parser, event, true, true)
	case yaml_PARSE_FLOW_NODE_STATE:
		return yaml_parser_parse_node(parser, event, false, false)
	case yaml_PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE:
		return yaml_parser_parse_block_sequence_entry(parser, event, true)
	case yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE:
		return yaml_parser_parse_block_sequence_entry(parser, event, false)
	case yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE:
		return yaml_parser_parse_indentless_sequence_entry(parser, event)
	case yaml_PARSE_BLOCK_MAPPING_FIRST_KEY_STATE:
		return yaml_parser_parse_block_mapping_key(parser, event, true)
	case yaml_PARSE_BLOCK_MAPPING_KEY_STATE:
		return yaml_parser_parse_block_mapping_key(parser, event, false)
	case yaml_PARSE_BLOCK_MAPPING_VALUE_STATE:
		return yaml_parser_parse_block_mapping_value(parser, event)
	case yaml_PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE:
		return yaml_parser_parse_flow_sequence_entry(parser, event, true)
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE:
		return yaml_parser_parse_flow_sequence_entry(parser, event, false)
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE:
		return yaml_parser_parse_flow_sequence_entry_mapping_key(parser, event)
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE:
		return yaml_parser_parse_flow_sequence_entry_mapping_value(parser, event)
	case yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE:
		return yaml_parser_parse_flow_sequence_entry_mapping_end(parser, event)
	case yaml_PARSE_FLOW_MAPPING_FIRST_KEY_STATE:
		return yaml_parser_parse_flow_mapping_key(parser, event, true)
	case yaml_PARSE_FLOW_MAPPING_KEY_STATE:
		return yaml_parser_parse_flow_mapping_key(parser, event, false)
	case yaml_PARSE_FLOW_MAPPING_VALUE_STATE:
		return yaml_parser_parse_flow_mapping_value(parser, event, false)
	case yaml_PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE:
		return yaml_parser_parse_flow_mapping_value(parser, event, true)
	default:
		panic("invalid yamlParser state")
	}
}
func yaml_parser_parse_stream_start(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ != yaml_STREAM_START_TOKEN {
		return yaml_parser_set_parser_error(parser, "did not find expected <stream-start>", token.start_mark)
	}
	parser.state = yaml_PARSE_IMPLICIT_DOCUMENT_START_STATE
	*event = yaml_event_t{
		typ:        yaml_STREAM_START_EVENT,
		start_mark: token.start_mark,
		end_mark:   token.end_mark,
		encoding:   token.encoding,
	}
	yaml_skip_token(parser)
	return true
}
func yaml_parser_parse_document_start(parser *yaml_parser_t, event *yaml_event_t, implicit bool) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if !implicit {
		for token.typ == yaml_DOCUMENT_END_TOKEN {
			yaml_skip_token(parser)
			token = yaml_peek_token(parser)
			if token == nil {
				return false
			}
		}
	}
	if implicit && token.typ != yaml_VERSION_DIRECTIVE_TOKEN &&
		token.typ != yaml_TAG_DIRECTIVE_TOKEN &&
		token.typ != yaml_DOCUMENT_START_TOKEN &&
		token.typ != yaml_STREAM_END_TOKEN {
		if !yaml_parser_process_directives(parser, nil, nil) {
			return false
		}
		parser.states = append(parser.states, yaml_PARSE_DOCUMENT_END_STATE)
		parser.state = yaml_PARSE_BLOCK_NODE_STATE
		*event = yaml_event_t{
			typ:        yaml_DOCUMENT_START_EVENT,
			start_mark: token.start_mark,
			end_mark:   token.end_mark,
		}
	} else if token.typ != yaml_STREAM_END_TOKEN {
		var version_directive *yaml_version_directive_t
		var tag_directives []yaml_tag_directive_t
		start_mark := token.start_mark
		if !yaml_parser_process_directives(parser, &version_directive, &tag_directives) {
			return false
		}
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_DOCUMENT_START_TOKEN {
			yaml_parser_set_parser_error(parser,
				"did not find expected <document start>", token.start_mark)
			return false
		}
		parser.states = append(parser.states, yaml_PARSE_DOCUMENT_END_STATE)
		parser.state = yaml_PARSE_DOCUMENT_CONTENT_STATE
		end_mark := token.end_mark
		*event = yaml_event_t{
			typ:               yaml_DOCUMENT_START_EVENT,
			start_mark:        start_mark,
			end_mark:          end_mark,
			version_directive: version_directive,
			tag_directives:    tag_directives,
			implicit:          false,
		}
		yaml_skip_token(parser)
	} else {
		parser.state = yaml_PARSE_END_STATE
		*event = yaml_event_t{
			typ:        yaml_STREAM_END_EVENT,
			start_mark: token.start_mark,
			end_mark:   token.end_mark,
		}
		yaml_skip_token(parser)
	}
	return true
}
func yaml_parser_parse_document_content(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_VERSION_DIRECTIVE_TOKEN ||
		token.typ == yaml_TAG_DIRECTIVE_TOKEN ||
		token.typ == yaml_DOCUMENT_START_TOKEN ||
		token.typ == yaml_DOCUMENT_END_TOKEN ||
		token.typ == yaml_STREAM_END_TOKEN {
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		return yaml_parser_process_empty_scalar(parser, event,
			token.start_mark)
	}
	return yaml_parser_parse_node(parser, event, true, false)
}
func yaml_parser_parse_document_end(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	start_mark := token.start_mark
	end_mark := token.start_mark
	implicit := true
	if token.typ == yaml_DOCUMENT_END_TOKEN {
		end_mark = token.end_mark
		yaml_skip_token(parser)
		implicit = false
	}
	parser.tag_directives = parser.tag_directives[:0]
	parser.state = yaml_PARSE_DOCUMENT_START_STATE
	*event = yaml_event_t{
		typ:        yaml_DOCUMENT_END_EVENT,
		start_mark: start_mark,
		end_mark:   end_mark,
		implicit:   implicit,
	}
	return true
}
func yaml_parser_parse_node(parser *yaml_parser_t, event *yaml_event_t, block, indentless_sequence bool) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_ALIAS_TOKEN {
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		*event = yaml_event_t{
			typ:        yaml_ALIAS_EVENT,
			start_mark: token.start_mark,
			end_mark:   token.end_mark,
			anchor:     token.value,
		}
		yaml_skip_token(parser)
		return true
	}
	start_mark := token.start_mark
	end_mark := token.start_mark
	var tag_token bool
	var tag_handle, tag_suffix, anchor []byte
	var tag_mark yaml_mark_t
	if token.typ == yaml_ANCHOR_TOKEN {
		anchor = token.value
		start_mark = token.start_mark
		end_mark = token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ == yaml_TAG_TOKEN {
			tag_token = true
			tag_handle = token.value
			tag_suffix = token.suffix
			tag_mark = token.start_mark
			end_mark = token.end_mark
			yaml_skip_token(parser)
			token = yaml_peek_token(parser)
			if token == nil {
				return false
			}
		}
	} else if token.typ == yaml_TAG_TOKEN {
		tag_token = true
		tag_handle = token.value
		tag_suffix = token.suffix
		start_mark = token.start_mark
		tag_mark = token.start_mark
		end_mark = token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ == yaml_ANCHOR_TOKEN {
			anchor = token.value
			end_mark = token.end_mark
			yaml_skip_token(parser)
			token = yaml_peek_token(parser)
			if token == nil {
				return false
			}
		}
	}
	var tag []byte
	if tag_token {
		if len(tag_handle) == 0 {
			tag = tag_suffix
			tag_suffix = nil
		} else {
			for i := range parser.tag_directives {
				if bytes.Equal(parser.tag_directives[i].handle, tag_handle) {
					tag = append([]byte(nil), parser.tag_directives[i].prefix...)
					tag = append(tag, tag_suffix...)
					break
				}
			}
			if len(tag) == 0 {
				yaml_parser_set_parser_error_context(parser,
					"while parsing a yamlNode", start_mark,
					"found undefined tag handle", tag_mark)
				return false
			}
		}
	}
	implicit := len(tag) == 0
	if indentless_sequence && token.typ == yaml_BLOCK_ENTRY_TOKEN {
		end_mark = token.end_mark
		parser.state = yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE
		*event = yaml_event_t{
			typ:        yaml_SEQUENCE_START_EVENT,
			start_mark: start_mark,
			end_mark:   end_mark,
			anchor:     anchor,
			tag:        tag,
			implicit:   implicit,
			style:      yaml_style_t(yaml_BLOCK_SEQUENCE_STYLE),
		}
		return true
	}
	if token.typ == yaml_SCALAR_TOKEN {
		var plain_implicit, quoted_implicit bool
		end_mark = token.end_mark
		if (len(tag) == 0 && token.style == yaml_PLAIN_SCALAR_STYLE) || (len(tag) == 1 && tag[0] == '!') {
			plain_implicit = true
		} else if len(tag) == 0 {
			quoted_implicit = true
		}
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		*event = yaml_event_t{
			typ:             yaml_SCALAR_EVENT,
			start_mark:      start_mark,
			end_mark:        end_mark,
			anchor:          anchor,
			tag:             tag,
			value:           token.value,
			implicit:        plain_implicit,
			quoted_implicit: quoted_implicit,
			style:           yaml_style_t(token.style),
		}
		yaml_skip_token(parser)
		return true
	}
	if token.typ == yaml_FLOW_SEQUENCE_START_TOKEN {
		end_mark = token.end_mark
		parser.state = yaml_PARSE_FLOW_SEQUENCE_FIRST_ENTRY_STATE
		*event = yaml_event_t{
			typ:        yaml_SEQUENCE_START_EVENT,
			start_mark: start_mark,
			end_mark:   end_mark,
			anchor:     anchor,
			tag:        tag,
			implicit:   implicit,
			style:      yaml_style_t(yaml_FLOW_SEQUENCE_STYLE),
		}
		return true
	}
	if token.typ == yaml_FLOW_MAPPING_START_TOKEN {
		end_mark = token.end_mark
		parser.state = yaml_PARSE_FLOW_MAPPING_FIRST_KEY_STATE
		*event = yaml_event_t{
			typ:        yaml_MAPPING_START_EVENT,
			start_mark: start_mark,
			end_mark:   end_mark,
			anchor:     anchor,
			tag:        tag,
			implicit:   implicit,
			style:      yaml_style_t(yaml_FLOW_MAPPING_STYLE),
		}
		return true
	}
	if block && token.typ == yaml_BLOCK_SEQUENCE_START_TOKEN {
		end_mark = token.end_mark
		parser.state = yaml_PARSE_BLOCK_SEQUENCE_FIRST_ENTRY_STATE
		*event = yaml_event_t{
			typ:        yaml_SEQUENCE_START_EVENT,
			start_mark: start_mark,
			end_mark:   end_mark,
			anchor:     anchor,
			tag:        tag,
			implicit:   implicit,
			style:      yaml_style_t(yaml_BLOCK_SEQUENCE_STYLE),
		}
		return true
	}
	if block && token.typ == yaml_BLOCK_MAPPING_START_TOKEN {
		end_mark = token.end_mark
		parser.state = yaml_PARSE_BLOCK_MAPPING_FIRST_KEY_STATE
		*event = yaml_event_t{
			typ:        yaml_MAPPING_START_EVENT,
			start_mark: start_mark,
			end_mark:   end_mark,
			anchor:     anchor,
			tag:        tag,
			implicit:   implicit,
			style:      yaml_style_t(yaml_BLOCK_MAPPING_STYLE),
		}
		return true
	}
	if len(anchor) > 0 || len(tag) > 0 {
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		*event = yaml_event_t{
			typ:             yaml_SCALAR_EVENT,
			start_mark:      start_mark,
			end_mark:        end_mark,
			anchor:          anchor,
			tag:             tag,
			implicit:        implicit,
			quoted_implicit: false,
			style:           yaml_style_t(yaml_PLAIN_SCALAR_STYLE),
		}
		return true
	}
	context := "while parsing a flow yamlNode"
	if block {
		context = "while parsing a block yamlNode"
	}
	yaml_parser_set_parser_error_context(parser, context, start_mark,
		"did not find expected yamlNode content", token.start_mark)
	return false
}
func yaml_parser_parse_block_sequence_entry(parser *yaml_parser_t, event *yaml_event_t, first bool) bool {
	if first {
		token := yaml_peek_token(parser)
		parser.marks = append(parser.marks, token.start_mark)
		yaml_skip_token(parser)
	}
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_BLOCK_ENTRY_TOKEN {
		mark := token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_BLOCK_ENTRY_TOKEN && token.typ != yaml_BLOCK_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE)
			return yaml_parser_parse_node(parser, event, true, false)
		} else {
			parser.state = yaml_PARSE_BLOCK_SEQUENCE_ENTRY_STATE
			return yaml_parser_process_empty_scalar(parser, event, mark)
		}
	}
	if token.typ == yaml_BLOCK_END_TOKEN {
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		parser.marks = parser.marks[:len(parser.marks)-1]
		*event = yaml_event_t{
			typ:        yaml_SEQUENCE_END_EVENT,
			start_mark: token.start_mark,
			end_mark:   token.end_mark,
		}
		yaml_skip_token(parser)
		return true
	}
	context_mark := parser.marks[len(parser.marks)-1]
	parser.marks = parser.marks[:len(parser.marks)-1]
	return yaml_parser_set_parser_error_context(parser,
		"while parsing a block collection", context_mark,
		"did not find expected '-' indicator", token.start_mark)
}
func yaml_parser_parse_indentless_sequence_entry(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_BLOCK_ENTRY_TOKEN {
		mark := token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_BLOCK_ENTRY_TOKEN &&
			token.typ != yaml_KEY_TOKEN &&
			token.typ != yaml_VALUE_TOKEN &&
			token.typ != yaml_BLOCK_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE)
			return yaml_parser_parse_node(parser, event, true, false)
		}
		parser.state = yaml_PARSE_INDENTLESS_SEQUENCE_ENTRY_STATE
		return yaml_parser_process_empty_scalar(parser, event, mark)
	}
	parser.state = parser.states[len(parser.states)-1]
	parser.states = parser.states[:len(parser.states)-1]
	*event = yaml_event_t{
		typ:        yaml_SEQUENCE_END_EVENT,
		start_mark: token.start_mark,
		end_mark:   token.start_mark,
	}
	return true
}
func yaml_parser_parse_block_mapping_key(parser *yaml_parser_t, event *yaml_event_t, first bool) bool {
	if first {
		token := yaml_peek_token(parser)
		parser.marks = append(parser.marks, token.start_mark)
		yaml_skip_token(parser)
	}
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_KEY_TOKEN {
		mark := token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_KEY_TOKEN &&
			token.typ != yaml_VALUE_TOKEN &&
			token.typ != yaml_BLOCK_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_BLOCK_MAPPING_VALUE_STATE)
			return yaml_parser_parse_node(parser, event, true, true)
		} else {
			parser.state = yaml_PARSE_BLOCK_MAPPING_VALUE_STATE
			return yaml_parser_process_empty_scalar(parser, event, mark)
		}
	} else if token.typ == yaml_BLOCK_END_TOKEN {
		parser.state = parser.states[len(parser.states)-1]
		parser.states = parser.states[:len(parser.states)-1]
		parser.marks = parser.marks[:len(parser.marks)-1]
		*event = yaml_event_t{
			typ:        yaml_MAPPING_END_EVENT,
			start_mark: token.start_mark,
			end_mark:   token.end_mark,
		}
		yaml_skip_token(parser)
		return true
	}
	context_mark := parser.marks[len(parser.marks)-1]
	parser.marks = parser.marks[:len(parser.marks)-1]
	return yaml_parser_set_parser_error_context(parser,
		"while parsing a block mapping", context_mark,
		"did not find expected key", token.start_mark)
}
func yaml_parser_parse_block_mapping_value(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_VALUE_TOKEN {
		mark := token.end_mark
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_KEY_TOKEN &&
			token.typ != yaml_VALUE_TOKEN &&
			token.typ != yaml_BLOCK_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_BLOCK_MAPPING_KEY_STATE)
			return yaml_parser_parse_node(parser, event, true, true)
		}
		parser.state = yaml_PARSE_BLOCK_MAPPING_KEY_STATE
		return yaml_parser_process_empty_scalar(parser, event, mark)
	}
	parser.state = yaml_PARSE_BLOCK_MAPPING_KEY_STATE
	return yaml_parser_process_empty_scalar(parser, event, token.start_mark)
}
func yaml_parser_parse_flow_sequence_entry(parser *yaml_parser_t, event *yaml_event_t, first bool) bool {
	if first {
		token := yaml_peek_token(parser)
		parser.marks = append(parser.marks, token.start_mark)
		yaml_skip_token(parser)
	}
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ != yaml_FLOW_SEQUENCE_END_TOKEN {
		if !first {
			if token.typ == yaml_FLOW_ENTRY_TOKEN {
				yaml_skip_token(parser)
				token = yaml_peek_token(parser)
				if token == nil {
					return false
				}
			} else {
				context_mark := parser.marks[len(parser.marks)-1]
				parser.marks = parser.marks[:len(parser.marks)-1]
				return yaml_parser_set_parser_error_context(parser,
					"while parsing a flow sequence", context_mark,
					"did not find expected ',' or ']'", token.start_mark)
			}
		}
		if token.typ == yaml_KEY_TOKEN {
			parser.state = yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_KEY_STATE
			*event = yaml_event_t{
				typ:        yaml_MAPPING_START_EVENT,
				start_mark: token.start_mark,
				end_mark:   token.end_mark,
				implicit:   true,
				style:      yaml_style_t(yaml_FLOW_MAPPING_STYLE),
			}
			yaml_skip_token(parser)
			return true
		} else if token.typ != yaml_FLOW_SEQUENCE_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE)
			return yaml_parser_parse_node(parser, event, false, false)
		}
	}
	parser.state = parser.states[len(parser.states)-1]
	parser.states = parser.states[:len(parser.states)-1]
	parser.marks = parser.marks[:len(parser.marks)-1]
	*event = yaml_event_t{
		typ:        yaml_SEQUENCE_END_EVENT,
		start_mark: token.start_mark,
		end_mark:   token.end_mark,
	}
	yaml_skip_token(parser)
	return true
}
func yaml_parser_parse_flow_sequence_entry_mapping_key(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ != yaml_VALUE_TOKEN &&
		token.typ != yaml_FLOW_ENTRY_TOKEN &&
		token.typ != yaml_FLOW_SEQUENCE_END_TOKEN {
		parser.states = append(parser.states, yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE)
		return yaml_parser_parse_node(parser, event, false, false)
	}
	mark := token.end_mark
	yaml_skip_token(parser)
	parser.state = yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_VALUE_STATE
	return yaml_parser_process_empty_scalar(parser, event, mark)
}
func yaml_parser_parse_flow_sequence_entry_mapping_value(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ == yaml_VALUE_TOKEN {
		yaml_skip_token(parser)
		token := yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_FLOW_ENTRY_TOKEN && token.typ != yaml_FLOW_SEQUENCE_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE)
			return yaml_parser_parse_node(parser, event, false, false)
		}
	}
	parser.state = yaml_PARSE_FLOW_SEQUENCE_ENTRY_MAPPING_END_STATE
	return yaml_parser_process_empty_scalar(parser, event, token.start_mark)
}
func yaml_parser_parse_flow_sequence_entry_mapping_end(parser *yaml_parser_t, event *yaml_event_t) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	parser.state = yaml_PARSE_FLOW_SEQUENCE_ENTRY_STATE
	*event = yaml_event_t{
		typ:        yaml_MAPPING_END_EVENT,
		start_mark: token.start_mark,
		end_mark:   token.start_mark,
	}
	return true
}
func yaml_parser_parse_flow_mapping_key(parser *yaml_parser_t, event *yaml_event_t, first bool) bool {
	if first {
		token := yaml_peek_token(parser)
		parser.marks = append(parser.marks, token.start_mark)
		yaml_skip_token(parser)
	}
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if token.typ != yaml_FLOW_MAPPING_END_TOKEN {
		if !first {
			if token.typ == yaml_FLOW_ENTRY_TOKEN {
				yaml_skip_token(parser)
				token = yaml_peek_token(parser)
				if token == nil {
					return false
				}
			} else {
				context_mark := parser.marks[len(parser.marks)-1]
				parser.marks = parser.marks[:len(parser.marks)-1]
				return yaml_parser_set_parser_error_context(parser,
					"while parsing a flow mapping", context_mark,
					"did not find expected ',' or '}'", token.start_mark)
			}
		}
		if token.typ == yaml_KEY_TOKEN {
			yaml_skip_token(parser)
			token = yaml_peek_token(parser)
			if token == nil {
				return false
			}
			if token.typ != yaml_VALUE_TOKEN &&
				token.typ != yaml_FLOW_ENTRY_TOKEN &&
				token.typ != yaml_FLOW_MAPPING_END_TOKEN {
				parser.states = append(parser.states, yaml_PARSE_FLOW_MAPPING_VALUE_STATE)
				return yaml_parser_parse_node(parser, event, false, false)
			} else {
				parser.state = yaml_PARSE_FLOW_MAPPING_VALUE_STATE
				return yaml_parser_process_empty_scalar(parser, event, token.start_mark)
			}
		} else if token.typ != yaml_FLOW_MAPPING_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_FLOW_MAPPING_EMPTY_VALUE_STATE)
			return yaml_parser_parse_node(parser, event, false, false)
		}
	}
	parser.state = parser.states[len(parser.states)-1]
	parser.states = parser.states[:len(parser.states)-1]
	parser.marks = parser.marks[:len(parser.marks)-1]
	*event = yaml_event_t{
		typ:        yaml_MAPPING_END_EVENT,
		start_mark: token.start_mark,
		end_mark:   token.end_mark,
	}
	yaml_skip_token(parser)
	return true
}
func yaml_parser_parse_flow_mapping_value(parser *yaml_parser_t, event *yaml_event_t, empty bool) bool {
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	if empty {
		parser.state = yaml_PARSE_FLOW_MAPPING_KEY_STATE
		return yaml_parser_process_empty_scalar(parser, event, token.start_mark)
	}
	if token.typ == yaml_VALUE_TOKEN {
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
		if token.typ != yaml_FLOW_ENTRY_TOKEN && token.typ != yaml_FLOW_MAPPING_END_TOKEN {
			parser.states = append(parser.states, yaml_PARSE_FLOW_MAPPING_KEY_STATE)
			return yaml_parser_parse_node(parser, event, false, false)
		}
	}
	parser.state = yaml_PARSE_FLOW_MAPPING_KEY_STATE
	return yaml_parser_process_empty_scalar(parser, event, token.start_mark)
}
func yaml_parser_process_empty_scalar(parser *yaml_parser_t, event *yaml_event_t, mark yaml_mark_t) bool {
	*event = yaml_event_t{
		typ:        yaml_SCALAR_EVENT,
		start_mark: mark,
		end_mark:   mark,
		value:      nil,
		implicit:   true,
		style:      yaml_style_t(yaml_PLAIN_SCALAR_STYLE),
	}
	return true
}

var yaml_default_tag_directives = []yaml_tag_directive_t{
	{[]byte("!"), []byte("!")},
	{[]byte("!!"), []byte("tag:yaml.org,2002:")},
}

func yaml_parser_process_directives(parser *yaml_parser_t,
	version_directive_ref **yaml_version_directive_t,
	tag_directives_ref *[]yaml_tag_directive_t) bool {
	var version_directive *yaml_version_directive_t
	var tag_directives []yaml_tag_directive_t
	token := yaml_peek_token(parser)
	if token == nil {
		return false
	}
	for token.typ == yaml_VERSION_DIRECTIVE_TOKEN || token.typ == yaml_TAG_DIRECTIVE_TOKEN {
		if token.typ == yaml_VERSION_DIRECTIVE_TOKEN {
			if version_directive != nil {
				yaml_parser_set_parser_error(parser,
					"found duplicate %YAML directive", token.start_mark)
				return false
			}
			if token.major != 1 || token.minor != 1 {
				yaml_parser_set_parser_error(parser,
					"found incompatible YAML document", token.start_mark)
				return false
			}
			version_directive = &yaml_version_directive_t{
				major: token.major,
				minor: token.minor,
			}
		} else if token.typ == yaml_TAG_DIRECTIVE_TOKEN {
			value := yaml_tag_directive_t{
				handle: token.value,
				prefix: token.prefix,
			}
			if !yaml_parser_append_tag_directive(parser, value, false, token.start_mark) {
				return false
			}
			tag_directives = append(tag_directives, value)
		}
		yaml_skip_token(parser)
		token = yaml_peek_token(parser)
		if token == nil {
			return false
		}
	}
	for i := range yaml_default_tag_directives {
		if !yaml_parser_append_tag_directive(parser, yaml_default_tag_directives[i], true, token.start_mark) {
			return false
		}
	}
	if version_directive_ref != nil {
		*version_directive_ref = version_directive
	}
	if tag_directives_ref != nil {
		*tag_directives_ref = tag_directives
	}
	return true
}
func yaml_parser_append_tag_directive(parser *yaml_parser_t, value yaml_tag_directive_t, allow_duplicates bool, mark yaml_mark_t) bool {
	for i := range parser.tag_directives {
		if bytes.Equal(value.handle, parser.tag_directives[i].handle) {
			if allow_duplicates {
				return true
			}
			return yaml_parser_set_parser_error(parser, "found duplicate %TAG directive", mark)
		}
	}
	value_copy := yaml_tag_directive_t{
		handle: make([]byte, len(value.handle)),
		prefix: make([]byte, len(value.prefix)),
	}
	copy(value_copy.handle, value.handle)
	copy(value_copy.prefix, value.prefix)
	parser.tag_directives = append(parser.tag_directives, value_copy)
	return true
}
func yaml_skip(parser *yaml_parser_t) {
	parser.mark.index++
	parser.mark.column++
	parser.unread--
	parser.buffer_pos += yaml_width(parser.buffer[parser.buffer_pos])
}
func yaml_skip_line(parser *yaml_parser_t) {
	if yaml_is_crlf(parser.buffer, parser.buffer_pos) {
		parser.mark.index += 2
		parser.mark.column = 0
		parser.mark.line++
		parser.unread -= 2
		parser.buffer_pos += 2
	} else if yaml_is_break(parser.buffer, parser.buffer_pos) {
		parser.mark.index++
		parser.mark.column = 0
		parser.mark.line++
		parser.unread--
		parser.buffer_pos += yaml_width(parser.buffer[parser.buffer_pos])
	}
}
func yaml_read(parser *yaml_parser_t, s []byte) []byte {
	w := yaml_width(parser.buffer[parser.buffer_pos])
	if w == 0 {
		panic("invalid character sequence")
	}
	if len(s) == 0 {
		s = make([]byte, 0, 32)
	}
	if w == 1 && len(s)+w <= cap(s) {
		s = s[:len(s)+1]
		s[len(s)-1] = parser.buffer[parser.buffer_pos]
		parser.buffer_pos++
	} else {
		s = append(s, parser.buffer[parser.buffer_pos:parser.buffer_pos+w]...)
		parser.buffer_pos += w
	}
	parser.mark.index++
	parser.mark.column++
	parser.unread--
	return s
}
func read_line(parser *yaml_parser_t, s []byte) []byte {
	buf := parser.buffer
	pos := parser.buffer_pos
	switch {
	case buf[pos] == '\r' && buf[pos+1] == '\n':
		s = append(s, '\n')
		parser.buffer_pos += 2
		parser.mark.index++
		parser.unread--
	case buf[pos] == '\r' || buf[pos] == '\n':
		s = append(s, '\n')
		parser.buffer_pos += 1
	case buf[pos] == '\xC2' && buf[pos+1] == '\x85':
		s = append(s, '\n')
		parser.buffer_pos += 2
	case buf[pos] == '\xE2' && buf[pos+1] == '\x80' && (buf[pos+2] == '\xA8' || buf[pos+2] == '\xA9'):
		s = append(s, buf[parser.buffer_pos:pos+3]...)
		parser.buffer_pos += 3
	default:
		return s
	}
	parser.mark.index++
	parser.mark.column = 0
	parser.mark.line++
	parser.unread--
	return s
}
func yaml_parser_set_reader_error(parser *yaml_parser_t, problem string, offset int, value int) bool {
	parser.error = yaml_READER_ERROR
	parser.problem = problem
	parser.problem_offset = offset
	parser.problem_value = value
	return false
}

const (
	yaml_bom_UTF8    = "\xef\xbb\xbf"
	yaml_bom_UTF16LE = "\xff\xfe"
	yaml_bom_UTF16BE = "\xfe\xff"
)

func yaml_parser_determine_encoding(parser *yaml_parser_t) bool {
	for !parser.eof && len(parser.raw_buffer)-parser.raw_buffer_pos < 3 {
		if !yaml_parser_update_raw_buffer(parser) {
			return false
		}
	}
	buf := parser.raw_buffer
	pos := parser.raw_buffer_pos
	avail := len(buf) - pos
	if avail >= 2 && buf[pos] == yaml_bom_UTF16LE[0] && buf[pos+1] == yaml_bom_UTF16LE[1] {
		parser.encoding = yaml_UTF16LE_ENCODING
		parser.raw_buffer_pos += 2
		parser.offset += 2
	} else if avail >= 2 && buf[pos] == yaml_bom_UTF16BE[0] && buf[pos+1] == yaml_bom_UTF16BE[1] {
		parser.encoding = yaml_UTF16BE_ENCODING
		parser.raw_buffer_pos += 2
		parser.offset += 2
	} else if avail >= 3 && buf[pos] == yaml_bom_UTF8[0] && buf[pos+1] == yaml_bom_UTF8[1] && buf[pos+2] == yaml_bom_UTF8[2] {
		parser.encoding = yaml_UTF8_ENCODING
		parser.raw_buffer_pos += 3
		parser.offset += 3
	} else {
		parser.encoding = yaml_UTF8_ENCODING
	}
	return true
}
func yaml_parser_update_raw_buffer(parser *yaml_parser_t) bool {
	size_read := 0
	if parser.raw_buffer_pos == 0 && len(parser.raw_buffer) == cap(parser.raw_buffer) {
		return true
	}
	if parser.eof {
		return true
	}
	if parser.raw_buffer_pos > 0 && parser.raw_buffer_pos < len(parser.raw_buffer) {
		copy(parser.raw_buffer, parser.raw_buffer[parser.raw_buffer_pos:])
	}
	parser.raw_buffer = parser.raw_buffer[:len(parser.raw_buffer)-parser.raw_buffer_pos]
	parser.raw_buffer_pos = 0
	size_read, err := parser.read_handler(parser, parser.raw_buffer[len(parser.raw_buffer):cap(parser.raw_buffer)])
	parser.raw_buffer = parser.raw_buffer[:len(parser.raw_buffer)+size_read]
	if err == io.EOF {
		parser.eof = true
	} else if err != nil {
		return yaml_parser_set_reader_error(parser, "input error: "+err.Error(), parser.offset, -1)
	}
	return true
}
func yaml_parser_update_buffer(parser *yaml_parser_t, length int) bool {
	if parser.read_handler == nil {
		panic("read handler must be set")
	}
	if parser.eof && parser.raw_buffer_pos == len(parser.raw_buffer) {
	}
	if parser.unread >= length {
		return true
	}
	if parser.encoding == yaml_ANY_ENCODING {
		if !yaml_parser_determine_encoding(parser) {
			return false
		}
	}
	buffer_len := len(parser.buffer)
	if parser.buffer_pos > 0 && parser.buffer_pos < buffer_len {
		copy(parser.buffer, parser.buffer[parser.buffer_pos:])
		buffer_len -= parser.buffer_pos
		parser.buffer_pos = 0
	} else if parser.buffer_pos == buffer_len {
		buffer_len = 0
		parser.buffer_pos = 0
	}
	parser.buffer = parser.buffer[:cap(parser.buffer)]
	first := true
	for parser.unread < length {
		if !first || parser.raw_buffer_pos == len(parser.raw_buffer) {
			if !yaml_parser_update_raw_buffer(parser) {
				parser.buffer = parser.buffer[:buffer_len]
				return false
			}
		}
		first = false
	inner:
		for parser.raw_buffer_pos != len(parser.raw_buffer) {
			var value rune
			var width int
			raw_unread := len(parser.raw_buffer) - parser.raw_buffer_pos
			switch parser.encoding {
			case yaml_UTF8_ENCODING:
				octet := parser.raw_buffer[parser.raw_buffer_pos]
				switch {
				case octet&0x80 == 0x00:
					width = 1
				case octet&0xE0 == 0xC0:
					width = 2
				case octet&0xF0 == 0xE0:
					width = 3
				case octet&0xF8 == 0xF0:
					width = 4
				default:
					return yaml_parser_set_reader_error(parser,
						"invalid leading UTF-8 octet",
						parser.offset, int(octet))
				}
				if width > raw_unread {
					if parser.eof {
						return yaml_parser_set_reader_error(parser,
							"incomplete UTF-8 octet sequence",
							parser.offset, -1)
					}
					break inner
				}
				switch {
				case octet&0x80 == 0x00:
					value = rune(octet & 0x7F)
				case octet&0xE0 == 0xC0:
					value = rune(octet & 0x1F)
				case octet&0xF0 == 0xE0:
					value = rune(octet & 0x0F)
				case octet&0xF8 == 0xF0:
					value = rune(octet & 0x07)
				default:
					value = 0
				}
				for k := 1; k < width; k++ {
					octet = parser.raw_buffer[parser.raw_buffer_pos+k]
					if (octet & 0xC0) != 0x80 {
						return yaml_parser_set_reader_error(parser,
							"invalid trailing UTF-8 octet",
							parser.offset+k, int(octet))
					}
					value = (value << 6) + rune(octet&0x3F)
				}
				switch {
				case width == 1:
				case width == 2 && value >= 0x80:
				case width == 3 && value >= 0x800:
				case width == 4 && value >= 0x10000:
				default:
					return yaml_parser_set_reader_error(parser,
						"invalid length of a UTF-8 sequence",
						parser.offset, -1)
				}
				if value >= 0xD800 && value <= 0xDFFF || value > 0x10FFFF {
					return yaml_parser_set_reader_error(parser,
						"invalid Unicode character",
						parser.offset, int(value))
				}
			case yaml_UTF16LE_ENCODING, yaml_UTF16BE_ENCODING:
				var low, high int
				if parser.encoding == yaml_UTF16LE_ENCODING {
					low, high = 0, 1
				} else {
					low, high = 1, 0
				}
				if raw_unread < 2 {
					if parser.eof {
						return yaml_parser_set_reader_error(parser,
							"incomplete UTF-16 character",
							parser.offset, -1)
					}
					break inner
				}
				value = rune(parser.raw_buffer[parser.raw_buffer_pos+low]) +
					(rune(parser.raw_buffer[parser.raw_buffer_pos+high]) << 8)
				if value&0xFC00 == 0xDC00 {
					return yaml_parser_set_reader_error(parser,
						"unexpected low surrogate area",
						parser.offset, int(value))
				}
				if value&0xFC00 == 0xD800 {
					width = 4
					if raw_unread < 4 {
						if parser.eof {
							return yaml_parser_set_reader_error(parser,
								"incomplete UTF-16 surrogate pair",
								parser.offset, -1)
						}
						break inner
					}
					value2 := rune(parser.raw_buffer[parser.raw_buffer_pos+low+2]) +
						(rune(parser.raw_buffer[parser.raw_buffer_pos+high+2]) << 8)
					if value2&0xFC00 != 0xDC00 {
						return yaml_parser_set_reader_error(parser,
							"expected low surrogate area",
							parser.offset+2, int(value2))
					}
					value = 0x10000 + ((value & 0x3FF) << 10) + (value2 & 0x3FF)
				} else {
					width = 2
				}
			default:
				panic("impossible")
			}
			switch {
			case value == 0x09:
			case value == 0x0A:
			case value == 0x0D:
			case value >= 0x20 && value <= 0x7E:
			case value == 0x85:
			case value >= 0xA0 && value <= 0xD7FF:
			case value >= 0xE000 && value <= 0xFFFD:
			case value >= 0x10000 && value <= 0x10FFFF:
			default:
				return yaml_parser_set_reader_error(parser,
					"control characters are not allowed",
					parser.offset, int(value))
			}
			parser.raw_buffer_pos += width
			parser.offset += width
			if value <= 0x7F {
				parser.buffer[buffer_len+0] = byte(value)
				buffer_len += 1
			} else if value <= 0x7FF {
				parser.buffer[buffer_len+0] = byte(0xC0 + (value >> 6))
				parser.buffer[buffer_len+1] = byte(0x80 + (value & 0x3F))
				buffer_len += 2
			} else if value <= 0xFFFF {
				parser.buffer[buffer_len+0] = byte(0xE0 + (value >> 12))
				parser.buffer[buffer_len+1] = byte(0x80 + ((value >> 6) & 0x3F))
				parser.buffer[buffer_len+2] = byte(0x80 + (value & 0x3F))
				buffer_len += 3
			} else {
				parser.buffer[buffer_len+0] = byte(0xF0 + (value >> 18))
				parser.buffer[buffer_len+1] = byte(0x80 + ((value >> 12) & 0x3F))
				parser.buffer[buffer_len+2] = byte(0x80 + ((value >> 6) & 0x3F))
				parser.buffer[buffer_len+3] = byte(0x80 + (value & 0x3F))
				buffer_len += 4
			}
			parser.unread++
		}
		if parser.eof {
			parser.buffer[buffer_len] = 0
			buffer_len++
			parser.unread++
			break
		}
	}
	for buffer_len < length {
		parser.buffer[buffer_len] = 0
		buffer_len++
	}
	parser.buffer = parser.buffer[:buffer_len]
	return true
}
func yaml_parser_set_scanner_error(parser *yaml_parser_t, context string, context_mark yaml_mark_t, problem string) bool {
	parser.error = yaml_SCANNER_ERROR
	parser.context = context
	parser.context_mark = context_mark
	parser.problem = problem
	parser.problem_mark = parser.mark
	return false
}
func yaml_parser_set_scanner_tag_error(parser *yaml_parser_t, directive bool, context_mark yaml_mark_t, problem string) bool {
	context := "while parsing a tag"
	if directive {
		context = "while parsing a %TAG directive"
	}
	return yaml_parser_set_scanner_error(parser, context, context_mark, problem)
}
func yaml_parser_fetch_more_tokens(parser *yaml_parser_t) bool {
	for {
		if parser.tokens_head != len(parser.tokens) {
			head_tok_idx, ok := parser.simple_keys_by_tok[parser.tokens_parsed]
			if !ok {
				break
			} else if valid, ok := yaml_simple_key_is_valid(parser, &parser.simple_keys[head_tok_idx]); !ok {
				return false
			} else if !valid {
				break
			}
		}
		if !yaml_parser_fetch_next_token(parser) {
			return false
		}
	}
	parser.token_available = true
	return true
}
func yaml_parser_fetch_next_token(parser *yaml_parser_t) bool {
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	if !parser.stream_start_produced {
		return yaml_parser_fetch_stream_start(parser)
	}
	if !yaml_parser_scan_to_next_token(parser) {
		return false
	}
	if !yaml_parser_unroll_indent(parser, parser.mark.column) {
		return false
	}
	if parser.unread < 4 && !yaml_parser_update_buffer(parser, 4) {
		return false
	}
	if yaml_is_z(parser.buffer, parser.buffer_pos) {
		return yaml_parser_fetch_stream_end(parser)
	}
	if parser.mark.column == 0 && parser.buffer[parser.buffer_pos] == '%' {
		return yaml_parser_fetch_directive(parser)
	}
	buf := parser.buffer
	pos := parser.buffer_pos
	if parser.mark.column == 0 && buf[pos] == '-' && buf[pos+1] == '-' && buf[pos+2] == '-' && yaml_is_blankz(buf, pos+3) {
		return yaml_parser_fetch_document_indicator(parser, yaml_DOCUMENT_START_TOKEN)
	}
	if parser.mark.column == 0 && buf[pos] == '.' && buf[pos+1] == '.' && buf[pos+2] == '.' && yaml_is_blankz(buf, pos+3) {
		return yaml_parser_fetch_document_indicator(parser, yaml_DOCUMENT_END_TOKEN)
	}
	if buf[pos] == '[' {
		return yaml_parser_fetch_flow_collection_start(parser, yaml_FLOW_SEQUENCE_START_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == '{' {
		return yaml_parser_fetch_flow_collection_start(parser, yaml_FLOW_MAPPING_START_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == ']' {
		return yaml_parser_fetch_flow_collection_end(parser,
			yaml_FLOW_SEQUENCE_END_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == '}' {
		return yaml_parser_fetch_flow_collection_end(parser,
			yaml_FLOW_MAPPING_END_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == ',' {
		return yaml_parser_fetch_flow_entry(parser)
	}
	if parser.buffer[parser.buffer_pos] == '-' && yaml_is_blankz(parser.buffer, parser.buffer_pos+1) {
		return yaml_parser_fetch_block_entry(parser)
	}
	if parser.buffer[parser.buffer_pos] == '?' && (parser.flow_level > 0 || yaml_is_blankz(parser.buffer, parser.buffer_pos+1)) {
		return yaml_parser_fetch_key(parser)
	}
	if parser.buffer[parser.buffer_pos] == ':' && (parser.flow_level > 0 || yaml_is_blankz(parser.buffer, parser.buffer_pos+1)) {
		return yaml_parser_fetch_value(parser)
	}
	if parser.buffer[parser.buffer_pos] == '*' {
		return yaml_parser_fetch_anchor(parser, yaml_ALIAS_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == '&' {
		return yaml_parser_fetch_anchor(parser, yaml_ANCHOR_TOKEN)
	}
	if parser.buffer[parser.buffer_pos] == '!' {
		return yaml_parser_fetch_tag(parser)
	}
	if parser.buffer[parser.buffer_pos] == '|' && parser.flow_level == 0 {
		return yaml_parser_fetch_block_scalar(parser, true)
	}
	if parser.buffer[parser.buffer_pos] == '>' && parser.flow_level == 0 {
		return yaml_parser_fetch_block_scalar(parser, false)
	}
	if parser.buffer[parser.buffer_pos] == '\'' {
		return yaml_parser_fetch_flow_scalar(parser, true)
	}
	if parser.buffer[parser.buffer_pos] == '"' {
		return yaml_parser_fetch_flow_scalar(parser, false)
	}
	if !(yaml_is_blankz(parser.buffer, parser.buffer_pos) || parser.buffer[parser.buffer_pos] == '-' ||
		parser.buffer[parser.buffer_pos] == '?' || parser.buffer[parser.buffer_pos] == ':' ||
		parser.buffer[parser.buffer_pos] == ',' || parser.buffer[parser.buffer_pos] == '[' ||
		parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '{' ||
		parser.buffer[parser.buffer_pos] == '}' || parser.buffer[parser.buffer_pos] == '#' ||
		parser.buffer[parser.buffer_pos] == '&' || parser.buffer[parser.buffer_pos] == '*' ||
		parser.buffer[parser.buffer_pos] == '!' || parser.buffer[parser.buffer_pos] == '|' ||
		parser.buffer[parser.buffer_pos] == '>' || parser.buffer[parser.buffer_pos] == '\'' ||
		parser.buffer[parser.buffer_pos] == '"' || parser.buffer[parser.buffer_pos] == '%' ||
		parser.buffer[parser.buffer_pos] == '@' || parser.buffer[parser.buffer_pos] == '`') ||
		(parser.buffer[parser.buffer_pos] == '-' && !yaml_is_blank(parser.buffer, parser.buffer_pos+1)) ||
		(parser.flow_level == 0 &&
			(parser.buffer[parser.buffer_pos] == '?' || parser.buffer[parser.buffer_pos] == ':') &&
			!yaml_is_blankz(parser.buffer, parser.buffer_pos+1)) {
		return yaml_parser_fetch_plain_scalar(parser)
	}
	return yaml_parser_set_scanner_error(parser,
		"while scanning for the next token", parser.mark,
		"found character that cannot start any token")
}
func yaml_simple_key_is_valid(parser *yaml_parser_t, simple_key *yaml_simple_key_t) (valid, ok bool) {
	if !simple_key.possible {
		return false, true
	}
	if simple_key.mark.line < parser.mark.line || simple_key.mark.index+1024 < parser.mark.index {
		if simple_key.required {
			return false, yaml_parser_set_scanner_error(parser,
				"while scanning a simple key", simple_key.mark,
				"could not find expected ':'")
		}
		simple_key.possible = false
		return false, true
	}
	return true, true
}
func yaml_parser_save_simple_key(parser *yaml_parser_t) bool {
	required := parser.flow_level == 0 && parser.indent == parser.mark.column
	if parser.simple_key_allowed {
		simple_key := yaml_simple_key_t{
			possible:     true,
			required:     required,
			token_number: parser.tokens_parsed + (len(parser.tokens) - parser.tokens_head),
			mark:         parser.mark,
		}
		if !yaml_parser_remove_simple_key(parser) {
			return false
		}
		parser.simple_keys[len(parser.simple_keys)-1] = simple_key
		parser.simple_keys_by_tok[simple_key.token_number] = len(parser.simple_keys) - 1
	}
	return true
}
func yaml_parser_remove_simple_key(parser *yaml_parser_t) bool {
	i := len(parser.simple_keys) - 1
	if parser.simple_keys[i].possible {
		if parser.simple_keys[i].required {
			return yaml_parser_set_scanner_error(parser,
				"while scanning a simple key", parser.simple_keys[i].mark,
				"could not find expected ':'")
		}
		parser.simple_keys[i].possible = false
		delete(parser.simple_keys_by_tok, parser.simple_keys[i].token_number)
	}
	return true
}

const yaml_max_flow_level = 10000

func yaml_parser_increase_flow_level(parser *yaml_parser_t) bool {
	parser.simple_keys = append(parser.simple_keys, yaml_simple_key_t{
		possible:     false,
		required:     false,
		token_number: parser.tokens_parsed + (len(parser.tokens) - parser.tokens_head),
		mark:         parser.mark,
	})
	parser.flow_level++
	if parser.flow_level > yaml_max_flow_level {
		return yaml_parser_set_scanner_error(parser,
			"while increasing flow level", parser.simple_keys[len(parser.simple_keys)-1].mark,
			fmt.Sprintf("exceeded max depth of %d", yaml_max_flow_level))
	}
	return true
}
func yaml_parser_decrease_flow_level(parser *yaml_parser_t) bool {
	if parser.flow_level > 0 {
		parser.flow_level--
		last := len(parser.simple_keys) - 1
		delete(parser.simple_keys_by_tok, parser.simple_keys[last].token_number)
		parser.simple_keys = parser.simple_keys[:last]
	}
	return true
}

const max_indents = 10000

func yaml_parser_roll_indent(parser *yaml_parser_t, column, number int, typ yaml_token_type_t, mark yaml_mark_t) bool {
	if parser.flow_level > 0 {
		return true
	}
	if parser.indent < column {
		parser.indents = append(parser.indents, parser.indent)
		parser.indent = column
		if len(parser.indents) > max_indents {
			return yaml_parser_set_scanner_error(parser,
				"while increasing indent level", parser.simple_keys[len(parser.simple_keys)-1].mark,
				fmt.Sprintf("exceeded max depth of %d", max_indents))
		}
		token := yaml_token_t{
			typ:        typ,
			start_mark: mark,
			end_mark:   mark,
		}
		if number > -1 {
			number -= parser.tokens_parsed
		}
		yaml_insert_token(parser, number, &token)
	}
	return true
}
func yaml_parser_unroll_indent(parser *yaml_parser_t, column int) bool {
	if parser.flow_level > 0 {
		return true
	}
	for parser.indent > column {
		token := yaml_token_t{
			typ:        yaml_BLOCK_END_TOKEN,
			start_mark: parser.mark,
			end_mark:   parser.mark,
		}
		yaml_insert_token(parser, -1, &token)
		parser.indent = parser.indents[len(parser.indents)-1]
		parser.indents = parser.indents[:len(parser.indents)-1]
	}
	return true
}
func yaml_parser_fetch_stream_start(parser *yaml_parser_t) bool {
	parser.indent = -1
	parser.simple_keys = append(parser.simple_keys, yaml_simple_key_t{})
	parser.simple_keys_by_tok = make(map[int]int)
	parser.simple_key_allowed = true
	parser.stream_start_produced = true
	token := yaml_token_t{
		typ:        yaml_STREAM_START_TOKEN,
		start_mark: parser.mark,
		end_mark:   parser.mark,
		encoding:   parser.encoding,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_stream_end(parser *yaml_parser_t) bool {
	if parser.mark.column != 0 {
		parser.mark.column = 0
		parser.mark.line++
	}
	if !yaml_parser_unroll_indent(parser, -1) {
		return false
	}
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	token := yaml_token_t{
		typ:        yaml_STREAM_END_TOKEN,
		start_mark: parser.mark,
		end_mark:   parser.mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_directive(parser *yaml_parser_t) bool {
	if !yaml_parser_unroll_indent(parser, -1) {
		return false
	}
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	token := yaml_token_t{}
	if !yaml_parser_scan_directive(parser, &token) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_document_indicator(parser *yaml_parser_t, typ yaml_token_type_t) bool {
	if !yaml_parser_unroll_indent(parser, -1) {
		return false
	}
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	start_mark := parser.mark
	yaml_skip(parser)
	yaml_skip(parser)
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        typ,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_flow_collection_start(parser *yaml_parser_t, typ yaml_token_type_t) bool {
	if !yaml_parser_save_simple_key(parser) {
		return false
	}
	if !yaml_parser_increase_flow_level(parser) {
		return false
	}
	parser.simple_key_allowed = true
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        typ,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_flow_collection_end(parser *yaml_parser_t, typ yaml_token_type_t) bool {
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	if !yaml_parser_decrease_flow_level(parser) {
		return false
	}
	parser.simple_key_allowed = false
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        typ,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_flow_entry(parser *yaml_parser_t) bool {
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = true
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        yaml_FLOW_ENTRY_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_block_entry(parser *yaml_parser_t) bool {
	if parser.flow_level == 0 {
		if !parser.simple_key_allowed {
			return yaml_parser_set_scanner_error(parser, "", parser.mark,
				"block sequence entries are not allowed in this context")
		}
		if !yaml_parser_roll_indent(parser, parser.mark.column, -1, yaml_BLOCK_SEQUENCE_START_TOKEN, parser.mark) {
			return false
		}
	} else {
	}
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = true
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        yaml_BLOCK_ENTRY_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_key(parser *yaml_parser_t) bool {
	if parser.flow_level == 0 {
		if !parser.simple_key_allowed {
			return yaml_parser_set_scanner_error(parser, "", parser.mark,
				"mapping keys are not allowed in this context")
		}
		if !yaml_parser_roll_indent(parser, parser.mark.column, -1, yaml_BLOCK_MAPPING_START_TOKEN, parser.mark) {
			return false
		}
	}
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = parser.flow_level == 0
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        yaml_KEY_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_value(parser *yaml_parser_t) bool {
	simple_key := &parser.simple_keys[len(parser.simple_keys)-1]
	if valid, ok := yaml_simple_key_is_valid(parser, simple_key); !ok {
		return false
	} else if valid {
		token := yaml_token_t{
			typ:        yaml_KEY_TOKEN,
			start_mark: simple_key.mark,
			end_mark:   simple_key.mark,
		}
		yaml_insert_token(parser, simple_key.token_number-parser.tokens_parsed, &token)
		if !yaml_parser_roll_indent(parser, simple_key.mark.column,
			simple_key.token_number,
			yaml_BLOCK_MAPPING_START_TOKEN, simple_key.mark) {
			return false
		}
		simple_key.possible = false
		delete(parser.simple_keys_by_tok, simple_key.token_number)
		parser.simple_key_allowed = false
	} else {
		if parser.flow_level == 0 {
			if !parser.simple_key_allowed {
				return yaml_parser_set_scanner_error(parser, "", parser.mark,
					"mapping values are not allowed in this context")
			}
			if !yaml_parser_roll_indent(parser, parser.mark.column, -1, yaml_BLOCK_MAPPING_START_TOKEN, parser.mark) {
				return false
			}
		}
		parser.simple_key_allowed = parser.flow_level == 0
	}
	start_mark := parser.mark
	yaml_skip(parser)
	end_mark := parser.mark
	token := yaml_token_t{
		typ:        yaml_VALUE_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_anchor(parser *yaml_parser_t, typ yaml_token_type_t) bool {
	if !yaml_parser_save_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	var token yaml_token_t
	if !yaml_parser_scan_anchor(parser, &token, typ) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_tag(parser *yaml_parser_t) bool {
	if !yaml_parser_save_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	var token yaml_token_t
	if !yaml_parser_scan_tag(parser, &token) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_block_scalar(parser *yaml_parser_t, literal bool) bool {
	if !yaml_parser_remove_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = true
	var token yaml_token_t
	if !yaml_parser_scan_block_scalar(parser, &token, literal) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_flow_scalar(parser *yaml_parser_t, single bool) bool {
	if !yaml_parser_save_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	var token yaml_token_t
	if !yaml_parser_scan_flow_scalar(parser, &token, single) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_fetch_plain_scalar(parser *yaml_parser_t) bool {
	if !yaml_parser_save_simple_key(parser) {
		return false
	}
	parser.simple_key_allowed = false
	var token yaml_token_t
	if !yaml_parser_scan_plain_scalar(parser, &token) {
		return false
	}
	yaml_insert_token(parser, -1, &token)
	return true
}
func yaml_parser_scan_to_next_token(parser *yaml_parser_t) bool {
	for {
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		if parser.mark.column == 0 && yaml_is_bom(parser.buffer, parser.buffer_pos) {
			yaml_skip(parser)
		}
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		for parser.buffer[parser.buffer_pos] == ' ' || ((parser.flow_level > 0 || !parser.simple_key_allowed) && parser.buffer[parser.buffer_pos] == '\t') {
			yaml_skip(parser)
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
		if parser.buffer[parser.buffer_pos] == '#' {
			for !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
				yaml_skip(parser)
				if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
					return false
				}
			}
		}
		if yaml_is_break(parser.buffer, parser.buffer_pos) {
			if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
				return false
			}
			yaml_skip_line(parser)
			if parser.flow_level == 0 {
				parser.simple_key_allowed = true
			}
		} else {
			break
		}
	}
	return true
}
func yaml_parser_scan_directive(parser *yaml_parser_t, token *yaml_token_t) bool {
	start_mark := parser.mark
	yaml_skip(parser)
	var name []byte
	if !yaml_parser_scan_directive_name(parser, start_mark, &name) {
		return false
	}
	if bytes.Equal(name, []byte("YAML")) {
		var major, minor int8
		if !yaml_parser_scan_version_directive_value(parser, start_mark, &major, &minor) {
			return false
		}
		end_mark := parser.mark
		*token = yaml_token_t{
			typ:        yaml_VERSION_DIRECTIVE_TOKEN,
			start_mark: start_mark,
			end_mark:   end_mark,
			major:      major,
			minor:      minor,
		}
	} else if bytes.Equal(name, []byte("TAG")) {
		var handle, prefix []byte
		if !yaml_parser_scan_tag_directive_value(parser, start_mark, &handle, &prefix) {
			return false
		}
		end_mark := parser.mark
		*token = yaml_token_t{
			typ:        yaml_TAG_DIRECTIVE_TOKEN,
			start_mark: start_mark,
			end_mark:   end_mark,
			value:      handle,
			prefix:     prefix,
		}
	} else {
		yaml_parser_set_scanner_error(parser, "while scanning a directive",
			start_mark, "found unknown directive name")
		return false
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if parser.buffer[parser.buffer_pos] == '#' {
		for !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
			yaml_skip(parser)
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
	}
	if !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a directive",
			start_mark, "did not find expected comment or line break")
		return false
	}
	if yaml_is_break(parser.buffer, parser.buffer_pos) {
		if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
			return false
		}
		yaml_skip_line(parser)
	}
	return true
}
func yaml_parser_scan_directive_name(parser *yaml_parser_t, start_mark yaml_mark_t, name *[]byte) bool {
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	var s []byte
	for yaml_is_alpha(parser.buffer, parser.buffer_pos) {
		s = yaml_read(parser, s)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if len(s) == 0 {
		yaml_parser_set_scanner_error(parser, "while scanning a directive",
			start_mark, "could not find expected directive name")
		return false
	}
	if !yaml_is_blankz(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a directive",
			start_mark, "found unexpected non-alphabetical character")
		return false
	}
	*name = s
	return true
}
func yaml_parser_scan_version_directive_value(parser *yaml_parser_t, start_mark yaml_mark_t, major, minor *int8) bool {
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if !yaml_parser_scan_version_directive_number(parser, start_mark, major) {
		return false
	}
	if parser.buffer[parser.buffer_pos] != '.' {
		return yaml_parser_set_scanner_error(parser, "while scanning a %YAML directive",
			start_mark, "did not find expected digit or '.' character")
	}
	yaml_skip(parser)
	if !yaml_parser_scan_version_directive_number(parser, start_mark, minor) {
		return false
	}
	return true
}

const max_number_length = 2

func yaml_parser_scan_version_directive_number(parser *yaml_parser_t, start_mark yaml_mark_t, number *int8) bool {
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	var value, length int8
	for yaml_is_digit(parser.buffer, parser.buffer_pos) {
		length++
		if length > max_number_length {
			return yaml_parser_set_scanner_error(parser, "while scanning a %YAML directive",
				start_mark, "found extremely long version number")
		}
		value = value*10 + int8(yaml_as_digit(parser.buffer, parser.buffer_pos))
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if length == 0 {
		return yaml_parser_set_scanner_error(parser, "while scanning a %YAML directive",
			start_mark, "did not find expected version number")
	}
	*number = value
	return true
}
func yaml_parser_scan_tag_directive_value(parser *yaml_parser_t, start_mark yaml_mark_t, handle, prefix *[]byte) bool {
	var handle_value, prefix_value []byte
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if !yaml_parser_scan_tag_handle(parser, true, start_mark, &handle_value) {
		return false
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	if !yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a %TAG directive",
			start_mark, "did not find expected whitespace")
		return false
	}
	for yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if !yaml_parser_scan_tag_uri(parser, true, nil, start_mark, &prefix_value) {
		return false
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	if !yaml_is_blankz(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a %TAG directive",
			start_mark, "did not find expected whitespace or line break")
		return false
	}
	*handle = handle_value
	*prefix = prefix_value
	return true
}
func yaml_parser_scan_anchor(parser *yaml_parser_t, token *yaml_token_t, typ yaml_token_type_t) bool {
	var s []byte
	start_mark := parser.mark
	yaml_skip(parser)
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_alpha(parser.buffer, parser.buffer_pos) {
		s = yaml_read(parser, s)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	end_mark := parser.mark
	/*
	 * Check if length of the anchor is greater than 0 and it is followed by
	 * a whitespace character or one of the indicators:
	 *
	 *      '?', ':', ',', ']', '}', '%', '@', '`'.
	 */
	if len(s) == 0 ||
		!(yaml_is_blankz(parser.buffer, parser.buffer_pos) || parser.buffer[parser.buffer_pos] == '?' ||
			parser.buffer[parser.buffer_pos] == ':' || parser.buffer[parser.buffer_pos] == ',' ||
			parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '}' ||
			parser.buffer[parser.buffer_pos] == '%' || parser.buffer[parser.buffer_pos] == '@' ||
			parser.buffer[parser.buffer_pos] == '`') {
		context := "while scanning an alias"
		if typ == yaml_ANCHOR_TOKEN {
			context = "while scanning an anchor"
		}
		yaml_parser_set_scanner_error(parser, context, start_mark,
			"did not find expected alphabetic or numeric character")
		return false
	}
	*token = yaml_token_t{
		typ:        typ,
		start_mark: start_mark,
		end_mark:   end_mark,
		value:      s,
	}
	return true
}

/*
 * Scan a TAG token.
 */
func yaml_parser_scan_tag(parser *yaml_parser_t, token *yaml_token_t) bool {
	var handle, suffix []byte
	start_mark := parser.mark
	if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
		return false
	}
	if parser.buffer[parser.buffer_pos+1] == '<' {
		yaml_skip(parser)
		yaml_skip(parser)
		if !yaml_parser_scan_tag_uri(parser, false, nil, start_mark, &suffix) {
			return false
		}
		if parser.buffer[parser.buffer_pos] != '>' {
			yaml_parser_set_scanner_error(parser, "while scanning a tag",
				start_mark, "did not find the expected '>'")
			return false
		}
		yaml_skip(parser)
	} else {
		if !yaml_parser_scan_tag_handle(parser, false, start_mark, &handle) {
			return false
		}
		if handle[0] == '!' && len(handle) > 1 && handle[len(handle)-1] == '!' {
			if !yaml_parser_scan_tag_uri(parser, false, nil, start_mark, &suffix) {
				return false
			}
		} else {
			if !yaml_parser_scan_tag_uri(parser, false, handle, start_mark, &suffix) {
				return false
			}
			handle = []byte{'!'}
			if len(suffix) == 0 {
				handle, suffix = suffix, handle
			}
		}
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	if !yaml_is_blankz(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a tag",
			start_mark, "did not find expected whitespace or line break")
		return false
	}
	end_mark := parser.mark
	*token = yaml_token_t{
		typ:        yaml_TAG_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
		value:      handle,
		suffix:     suffix,
	}
	return true
}
func yaml_parser_scan_tag_handle(parser *yaml_parser_t, directive bool, start_mark yaml_mark_t, handle *[]byte) bool {
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	if parser.buffer[parser.buffer_pos] != '!' {
		yaml_parser_set_scanner_tag_error(parser, directive,
			start_mark, "did not find expected '!'")
		return false
	}
	var s []byte
	s = yaml_read(parser, s)
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_alpha(parser.buffer, parser.buffer_pos) {
		s = yaml_read(parser, s)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if parser.buffer[parser.buffer_pos] == '!' {
		s = yaml_read(parser, s)
	} else {
		if directive && string(s) != "!" {
			yaml_parser_set_scanner_tag_error(parser, directive,
				start_mark, "did not find expected '!'")
			return false
		}
	}
	*handle = s
	return true
}
func yaml_parser_scan_tag_uri(parser *yaml_parser_t, directive bool, head []byte, start_mark yaml_mark_t, uri *[]byte) bool {
	var s []byte
	hasTag := len(head) > 0
	if len(head) > 1 {
		s = append(s, head[1:]...)
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_alpha(parser.buffer, parser.buffer_pos) || parser.buffer[parser.buffer_pos] == ';' ||
		parser.buffer[parser.buffer_pos] == '/' || parser.buffer[parser.buffer_pos] == '?' ||
		parser.buffer[parser.buffer_pos] == ':' || parser.buffer[parser.buffer_pos] == '@' ||
		parser.buffer[parser.buffer_pos] == '&' || parser.buffer[parser.buffer_pos] == '=' ||
		parser.buffer[parser.buffer_pos] == '+' || parser.buffer[parser.buffer_pos] == '$' ||
		parser.buffer[parser.buffer_pos] == ',' || parser.buffer[parser.buffer_pos] == '.' ||
		parser.buffer[parser.buffer_pos] == '!' || parser.buffer[parser.buffer_pos] == '~' ||
		parser.buffer[parser.buffer_pos] == '*' || parser.buffer[parser.buffer_pos] == '\'' ||
		parser.buffer[parser.buffer_pos] == '(' || parser.buffer[parser.buffer_pos] == ')' ||
		parser.buffer[parser.buffer_pos] == '[' || parser.buffer[parser.buffer_pos] == ']' ||
		parser.buffer[parser.buffer_pos] == '%' {
		if parser.buffer[parser.buffer_pos] == '%' {
			if !yaml_parser_scan_uri_escapes(parser, directive, start_mark, &s) {
				return false
			}
		} else {
			s = yaml_read(parser, s)
		}
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		hasTag = true
	}
	if !hasTag {
		yaml_parser_set_scanner_tag_error(parser, directive,
			start_mark, "did not find expected tag URI")
		return false
	}
	*uri = s
	return true
}
func yaml_parser_scan_uri_escapes(parser *yaml_parser_t, directive bool, start_mark yaml_mark_t, s *[]byte) bool {
	w := 1024
	for w > 0 {
		if parser.unread < 3 && !yaml_parser_update_buffer(parser, 3) {
			return false
		}
		if !(parser.buffer[parser.buffer_pos] == '%' &&
			yaml_is_hex(parser.buffer, parser.buffer_pos+1) &&
			yaml_is_hex(parser.buffer, parser.buffer_pos+2)) {
			return yaml_parser_set_scanner_tag_error(parser, directive,
				start_mark, "did not find URI escaped octet")
		}
		octet := byte((yaml_as_hex(parser.buffer, parser.buffer_pos+1) << 4) + yaml_as_hex(parser.buffer, parser.buffer_pos+2))
		if w == 1024 {
			w = yaml_width(octet)
			if w == 0 {
				return yaml_parser_set_scanner_tag_error(parser, directive,
					start_mark, "found an incorrect leading UTF-8 octet")
			}
		} else {
			if octet&0xC0 != 0x80 {
				return yaml_parser_set_scanner_tag_error(parser, directive,
					start_mark, "found an incorrect trailing UTF-8 octet")
			}
		}
		*s = append(*s, octet)
		yaml_skip(parser)
		yaml_skip(parser)
		yaml_skip(parser)
		w--
	}
	return true
}
func yaml_parser_scan_block_scalar(parser *yaml_parser_t, token *yaml_token_t, literal bool) bool {
	start_mark := parser.mark
	yaml_skip(parser)
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	var chomping, increment int
	if parser.buffer[parser.buffer_pos] == '+' || parser.buffer[parser.buffer_pos] == '-' {
		if parser.buffer[parser.buffer_pos] == '+' {
			chomping = +1
		} else {
			chomping = -1
		}
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		if yaml_is_digit(parser.buffer, parser.buffer_pos) {
			if parser.buffer[parser.buffer_pos] == '0' {
				yaml_parser_set_scanner_error(parser, "while scanning a block scalar",
					start_mark, "found an indentation indicator equal to 0")
				return false
			}
			increment = yaml_as_digit(parser.buffer, parser.buffer_pos)
			yaml_skip(parser)
		}
	} else if yaml_is_digit(parser.buffer, parser.buffer_pos) {
		if parser.buffer[parser.buffer_pos] == '0' {
			yaml_parser_set_scanner_error(parser, "while scanning a block scalar",
				start_mark, "found an indentation indicator equal to 0")
			return false
		}
		increment = yaml_as_digit(parser.buffer, parser.buffer_pos)
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		if parser.buffer[parser.buffer_pos] == '+' || parser.buffer[parser.buffer_pos] == '-' {
			if parser.buffer[parser.buffer_pos] == '+' {
				chomping = +1
			} else {
				chomping = -1
			}
			yaml_skip(parser)
		}
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	for yaml_is_blank(parser.buffer, parser.buffer_pos) {
		yaml_skip(parser)
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
	}
	if parser.buffer[parser.buffer_pos] == '#' {
		for !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
			yaml_skip(parser)
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
	}
	if !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
		yaml_parser_set_scanner_error(parser, "while scanning a block scalar",
			start_mark, "did not find expected comment or line break")
		return false
	}
	if yaml_is_break(parser.buffer, parser.buffer_pos) {
		if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
			return false
		}
		yaml_skip_line(parser)
	}
	end_mark := parser.mark
	var indent int
	if increment > 0 {
		if parser.indent >= 0 {
			indent = parser.indent + increment
		} else {
			indent = increment
		}
	}
	var s, leading_break, trailing_breaks []byte
	if !yaml_parser_scan_block_scalar_breaks(parser, &indent, &trailing_breaks, start_mark, &end_mark) {
		return false
	}
	if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
		return false
	}
	var leading_blank, trailing_blank bool
	for parser.mark.column == indent && !yaml_is_z(parser.buffer, parser.buffer_pos) {
		trailing_blank = yaml_is_blank(parser.buffer, parser.buffer_pos)
		if !literal && !leading_blank && !trailing_blank && len(leading_break) > 0 && leading_break[0] == '\n' {
			if len(trailing_breaks) == 0 {
				s = append(s, ' ')
			}
		} else {
			s = append(s, leading_break...)
		}
		leading_break = leading_break[:0]
		s = append(s, trailing_breaks...)
		trailing_breaks = trailing_breaks[:0]
		leading_blank = yaml_is_blank(parser.buffer, parser.buffer_pos)
		for !yaml_is_breakz(parser.buffer, parser.buffer_pos) {
			s = yaml_read(parser, s)
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
		if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
			return false
		}
		leading_break = read_line(parser, leading_break)
		if !yaml_parser_scan_block_scalar_breaks(parser, &indent, &trailing_breaks, start_mark, &end_mark) {
			return false
		}
	}
	if chomping != -1 {
		s = append(s, leading_break...)
	}
	if chomping == 1 {
		s = append(s, trailing_breaks...)
	}
	*token = yaml_token_t{
		typ:        yaml_SCALAR_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
		value:      s,
		style:      yaml_LITERAL_SCALAR_STYLE,
	}
	if !literal {
		token.style = yaml_FOLDED_SCALAR_STYLE
	}
	return true
}
func yaml_parser_scan_block_scalar_breaks(parser *yaml_parser_t, indent *int, breaks *[]byte, start_mark yaml_mark_t, end_mark *yaml_mark_t) bool {
	*end_mark = parser.mark
	max_indent := 0
	for {
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		for (*indent == 0 || parser.mark.column < *indent) && yaml_is_space(parser.buffer, parser.buffer_pos) {
			yaml_skip(parser)
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
		if parser.mark.column > max_indent {
			max_indent = parser.mark.column
		}
		if (*indent == 0 || parser.mark.column < *indent) && yaml_is_tab(parser.buffer, parser.buffer_pos) {
			return yaml_parser_set_scanner_error(parser, "while scanning a block scalar",
				start_mark, "found a tab character where an indentation space is expected")
		}
		if !yaml_is_break(parser.buffer, parser.buffer_pos) {
			break
		}
		if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
			return false
		}
		*breaks = read_line(parser, *breaks)
		*end_mark = parser.mark
	}
	if *indent == 0 {
		*indent = max_indent
		if *indent < parser.indent+1 {
			*indent = parser.indent + 1
		}
		if *indent < 1 {
			*indent = 1
		}
	}
	return true
}
func yaml_parser_scan_flow_scalar(parser *yaml_parser_t, token *yaml_token_t, single bool) bool {
	start_mark := parser.mark
	yaml_skip(parser)
	var s, leading_break, trailing_breaks, whitespaces []byte
	for {
		if parser.unread < 4 && !yaml_parser_update_buffer(parser, 4) {
			return false
		}
		if parser.mark.column == 0 &&
			((parser.buffer[parser.buffer_pos+0] == '-' &&
				parser.buffer[parser.buffer_pos+1] == '-' &&
				parser.buffer[parser.buffer_pos+2] == '-') ||
				(parser.buffer[parser.buffer_pos+0] == '.' &&
					parser.buffer[parser.buffer_pos+1] == '.' &&
					parser.buffer[parser.buffer_pos+2] == '.')) &&
			yaml_is_blankz(parser.buffer, parser.buffer_pos+3) {
			yaml_parser_set_scanner_error(parser, "while scanning a quoted scalar",
				start_mark, "found unexpected document indicator")
			return false
		}
		if yaml_is_z(parser.buffer, parser.buffer_pos) {
			yaml_parser_set_scanner_error(parser, "while scanning a quoted scalar",
				start_mark, "found unexpected end of stream")
			return false
		}
		leading_blanks := false
		for !yaml_is_blankz(parser.buffer, parser.buffer_pos) {
			if single && parser.buffer[parser.buffer_pos] == '\'' && parser.buffer[parser.buffer_pos+1] == '\'' {
				s = append(s, '\'')
				yaml_skip(parser)
				yaml_skip(parser)
			} else if single && parser.buffer[parser.buffer_pos] == '\'' {
				break
			} else if !single && parser.buffer[parser.buffer_pos] == '"' {
				break
			} else if !single && parser.buffer[parser.buffer_pos] == '\\' && yaml_is_break(parser.buffer, parser.buffer_pos+1) {
				if parser.unread < 3 && !yaml_parser_update_buffer(parser, 3) {
					return false
				}
				yaml_skip(parser)
				yaml_skip_line(parser)
				leading_blanks = true
				break
			} else if !single && parser.buffer[parser.buffer_pos] == '\\' {
				code_length := 0
				switch parser.buffer[parser.buffer_pos+1] {
				case '0':
					s = append(s, 0)
				case 'a':
					s = append(s, '\x07')
				case 'b':
					s = append(s, '\x08')
				case 't', '\t':
					s = append(s, '\x09')
				case 'n':
					s = append(s, '\x0A')
				case 'v':
					s = append(s, '\x0B')
				case 'f':
					s = append(s, '\x0C')
				case 'r':
					s = append(s, '\x0D')
				case 'e':
					s = append(s, '\x1B')
				case ' ':
					s = append(s, '\x20')
				case '"':
					s = append(s, '"')
				case '\'':
					s = append(s, '\'')
				case '\\':
					s = append(s, '\\')
				case 'N':
					s = append(s, '\xC2')
					s = append(s, '\x85')
				case '_':
					s = append(s, '\xC2')
					s = append(s, '\xA0')
				case 'L':
					s = append(s, '\xE2')
					s = append(s, '\x80')
					s = append(s, '\xA8')
				case 'P':
					s = append(s, '\xE2')
					s = append(s, '\x80')
					s = append(s, '\xA9')
				case 'x':
					code_length = 2
				case 'u':
					code_length = 4
				case 'U':
					code_length = 8
				default:
					yaml_parser_set_scanner_error(parser, "while parsing a quoted scalar",
						start_mark, "found unknown escape character")
					return false
				}
				yaml_skip(parser)
				yaml_skip(parser)
				if code_length > 0 {
					var value int
					if parser.unread < code_length && !yaml_parser_update_buffer(parser, code_length) {
						return false
					}
					for k := 0; k < code_length; k++ {
						if !yaml_is_hex(parser.buffer, parser.buffer_pos+k) {
							yaml_parser_set_scanner_error(parser, "while parsing a quoted scalar",
								start_mark, "did not find expected hexdecimal number")
							return false
						}
						value = (value << 4) + yaml_as_hex(parser.buffer, parser.buffer_pos+k)
					}
					if (value >= 0xD800 && value <= 0xDFFF) || value > 0x10FFFF {
						yaml_parser_set_scanner_error(parser, "while parsing a quoted scalar",
							start_mark, "found invalid Unicode character escape code")
						return false
					}
					if value <= 0x7F {
						s = append(s, byte(value))
					} else if value <= 0x7FF {
						s = append(s, byte(0xC0+(value>>6)))
						s = append(s, byte(0x80+(value&0x3F)))
					} else if value <= 0xFFFF {
						s = append(s, byte(0xE0+(value>>12)))
						s = append(s, byte(0x80+((value>>6)&0x3F)))
						s = append(s, byte(0x80+(value&0x3F)))
					} else {
						s = append(s, byte(0xF0+(value>>18)))
						s = append(s, byte(0x80+((value>>12)&0x3F)))
						s = append(s, byte(0x80+((value>>6)&0x3F)))
						s = append(s, byte(0x80+(value&0x3F)))
					}
					for k := 0; k < code_length; k++ {
						yaml_skip(parser)
					}
				}
			} else {
				s = yaml_read(parser, s)
			}
			if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
				return false
			}
		}
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		if single {
			if parser.buffer[parser.buffer_pos] == '\'' {
				break
			}
		} else {
			if parser.buffer[parser.buffer_pos] == '"' {
				break
			}
		}
		for yaml_is_blank(parser.buffer, parser.buffer_pos) || yaml_is_break(parser.buffer, parser.buffer_pos) {
			if yaml_is_blank(parser.buffer, parser.buffer_pos) {
				if !leading_blanks {
					whitespaces = yaml_read(parser, whitespaces)
				} else {
					yaml_skip(parser)
				}
			} else {
				if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
					return false
				}
				if !leading_blanks {
					whitespaces = whitespaces[:0]
					leading_break = read_line(parser, leading_break)
					leading_blanks = true
				} else {
					trailing_breaks = read_line(parser, trailing_breaks)
				}
			}
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
		if leading_blanks {
			if len(leading_break) > 0 && leading_break[0] == '\n' {
				if len(trailing_breaks) == 0 {
					s = append(s, ' ')
				} else {
					s = append(s, trailing_breaks...)
				}
			} else {
				s = append(s, leading_break...)
				s = append(s, trailing_breaks...)
			}
			trailing_breaks = trailing_breaks[:0]
			leading_break = leading_break[:0]
		} else {
			s = append(s, whitespaces...)
			whitespaces = whitespaces[:0]
		}
	}
	yaml_skip(parser)
	end_mark := parser.mark
	*token = yaml_token_t{
		typ:        yaml_SCALAR_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
		value:      s,
		style:      yaml_SINGLE_QUOTED_SCALAR_STYLE,
	}
	if !single {
		token.style = yaml_DOUBLE_QUOTED_SCALAR_STYLE
	}
	return true
}
func yaml_parser_scan_plain_scalar(parser *yaml_parser_t, token *yaml_token_t) bool {
	var s, leading_break, trailing_breaks, whitespaces []byte
	var leading_blanks bool
	var indent = parser.indent + 1
	start_mark := parser.mark
	end_mark := parser.mark
	for {
		if parser.unread < 4 && !yaml_parser_update_buffer(parser, 4) {
			return false
		}
		if parser.mark.column == 0 &&
			((parser.buffer[parser.buffer_pos+0] == '-' &&
				parser.buffer[parser.buffer_pos+1] == '-' &&
				parser.buffer[parser.buffer_pos+2] == '-') ||
				(parser.buffer[parser.buffer_pos+0] == '.' &&
					parser.buffer[parser.buffer_pos+1] == '.' &&
					parser.buffer[parser.buffer_pos+2] == '.')) &&
			yaml_is_blankz(parser.buffer, parser.buffer_pos+3) {
			break
		}
		if parser.buffer[parser.buffer_pos] == '#' {
			break
		}
		for !yaml_is_blankz(parser.buffer, parser.buffer_pos) {
			if (parser.buffer[parser.buffer_pos] == ':' && yaml_is_blankz(parser.buffer, parser.buffer_pos+1)) ||
				(parser.flow_level > 0 &&
					(parser.buffer[parser.buffer_pos] == ',' ||
						parser.buffer[parser.buffer_pos] == '?' || parser.buffer[parser.buffer_pos] == '[' ||
						parser.buffer[parser.buffer_pos] == ']' || parser.buffer[parser.buffer_pos] == '{' ||
						parser.buffer[parser.buffer_pos] == '}')) {
				break
			}
			if leading_blanks || len(whitespaces) > 0 {
				if leading_blanks {
					if leading_break[0] == '\n' {
						if len(trailing_breaks) == 0 {
							s = append(s, ' ')
						} else {
							s = append(s, trailing_breaks...)
						}
					} else {
						s = append(s, leading_break...)
						s = append(s, trailing_breaks...)
					}
					trailing_breaks = trailing_breaks[:0]
					leading_break = leading_break[:0]
					leading_blanks = false
				} else {
					s = append(s, whitespaces...)
					whitespaces = whitespaces[:0]
				}
			}
			s = yaml_read(parser, s)
			end_mark = parser.mark
			if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
				return false
			}
		}
		if !(yaml_is_blank(parser.buffer, parser.buffer_pos) || yaml_is_break(parser.buffer, parser.buffer_pos)) {
			break
		}
		if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
			return false
		}
		for yaml_is_blank(parser.buffer, parser.buffer_pos) || yaml_is_break(parser.buffer, parser.buffer_pos) {
			if yaml_is_blank(parser.buffer, parser.buffer_pos) {
				if leading_blanks && parser.mark.column < indent && yaml_is_tab(parser.buffer, parser.buffer_pos) {
					yaml_parser_set_scanner_error(parser, "while scanning a plain scalar",
						start_mark, "found a tab character that violates indentation")
					return false
				}
				if !leading_blanks {
					whitespaces = yaml_read(parser, whitespaces)
				} else {
					yaml_skip(parser)
				}
			} else {
				if parser.unread < 2 && !yaml_parser_update_buffer(parser, 2) {
					return false
				}
				if !leading_blanks {
					whitespaces = whitespaces[:0]
					leading_break = read_line(parser, leading_break)
					leading_blanks = true
				} else {
					trailing_breaks = read_line(parser, trailing_breaks)
				}
			}
			if parser.unread < 1 && !yaml_parser_update_buffer(parser, 1) {
				return false
			}
		}
		if parser.flow_level == 0 && parser.mark.column < indent {
			break
		}
	}
	*token = yaml_token_t{
		typ:        yaml_SCALAR_TOKEN,
		start_mark: start_mark,
		end_mark:   end_mark,
		value:      s,
		style:      yaml_PLAIN_SCALAR_STYLE,
	}
	if leading_blanks {
		parser.simple_key_allowed = true
	}
	return true
}
func yaml_is_alpha(b []byte, i int) bool {
	return b[i] >= '0' && b[i] <= '9' || b[i] >= 'A' && b[i] <= 'Z' || b[i] >= 'a' && b[i] <= 'z' || b[i] == '_' || b[i] == '-'
}
func yaml_is_digit(b []byte, i int) bool {
	return b[i] >= '0' && b[i] <= '9'
}
func yaml_as_digit(b []byte, i int) int {
	return int(b[i]) - '0'
}
func yaml_is_hex(b []byte, i int) bool {
	return b[i] >= '0' && b[i] <= '9' || b[i] >= 'A' && b[i] <= 'F' || b[i] >= 'a' && b[i] <= 'f'
}
func yaml_as_hex(b []byte, i int) int {
	bi := b[i]
	if bi >= 'A' && bi <= 'F' {
		return int(bi) - 'A' + 10
	}
	if bi >= 'a' && bi <= 'f' {
		return int(bi) - 'a' + 10
	}
	return int(bi) - '0'
}
func yaml_is_z(b []byte, i int) bool {
	return b[i] == 0x00
}
func yaml_is_bom(b []byte, i int) bool {
	return b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF
}
func yaml_is_space(b []byte, i int) bool {
	return b[i] == ' '
}
func yaml_is_tab(b []byte, i int) bool {
	return b[i] == '\t'
}
func yaml_is_blank(b []byte, i int) bool {
	return b[i] == ' ' || b[i] == '\t'
}
func yaml_is_break(b []byte, i int) bool {
	return b[i] == '\r' ||
		b[i] == '\n' ||
		b[i] == 0xC2 && b[i+1] == 0x85 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA8 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA9
}
func yaml_is_crlf(b []byte, i int) bool {
	return b[i] == '\r' && b[i+1] == '\n'
}
func yaml_is_breakz(b []byte, i int) bool {
	return b[i] == '\r' ||
		b[i] == '\n' ||
		b[i] == 0xC2 && b[i+1] == 0x85 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA8 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA9 ||
		b[i] == 0
}
func yaml_is_blankz(b []byte, i int) bool {
	return b[i] == ' ' || b[i] == '\t' ||
		b[i] == '\r' ||
		b[i] == '\n' ||
		b[i] == 0xC2 && b[i+1] == 0x85 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA8 ||
		b[i] == 0xE2 && b[i+1] == 0x80 && b[i+2] == 0xA9 ||
		b[i] == 0
}
func yaml_width(b byte) int {
	if b&0x80 == 0x00 {
		return 1
	}
	if b&0xE0 == 0xC0 {
		return 2
	}
	if b&0xF0 == 0xE0 {
		return 3
	}
	if b&0xF8 == 0xF0 {
		return 4
	}
	return 0
}
