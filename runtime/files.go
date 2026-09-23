// Package runtime embeds the syntax definitions and colorschemes the editor
// ships with, and hands them to a view as a ready-made set of runtime files:
//
//	view := editor.NewView(buf)
//	view.SetRuntimeFiles(runtime.Files)
//
// The data is micro's: the grammars under files/syntax and the colorschemes
// under files/colorschemes. It is loaded through the same editor.NewRuntimeFiles
// that a host can point at its own directory or http.FileSystem, so additional
// files can simply be added on top:
//
//	runtime.Files.AddFile(editor.RTColorscheme, myScheme)
//
// The directories under files/ are embedded as they are, which includes the
// small maintenance tools (files/syntax/*.go) that keep the grammars and their
// converters in shape.
package runtime

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/malivvan/editor"
)

//go:embed files/colorschemes files/syntax
var embedded embed.FS

var sub, _ = fs.Sub(embedded, "files")

var Files = editor.NewRuntimeFiles(http.FS(sub))
