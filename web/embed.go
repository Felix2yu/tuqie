// Package web embeds the built frontend so one binary serves everything.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the embedded build output rooted at its index.html.
func Dist() (fs.FS, error) { return fs.Sub(dist, "dist") }
