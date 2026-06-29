// Package assets embeds the web/ frontend into the binary (FR-2.1).
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:web
var embedded embed.FS

// FS returns the embedded filesystem rooted at the web/ directory.
// Callers can serve it under /static/ or read index.html from it.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "web")
	if err != nil {
		panic("assets: cannot sub web/ from embedded FS: " + err.Error())
	}
	return sub
}
