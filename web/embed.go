// Package web embeds the built board frontend (web/dist).
package web

import (
	"embed"
	"io/fs"
)

// dist always contains at least .gitkeep, so the Go build works before the UI is built.
//
//go:embed all:dist
var dist embed.FS

// UI returns the built frontend rooted at dist/.
func UI() fs.FS {
	ui, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a constant path inside the embed; this cannot fail
	}
	return ui
}
