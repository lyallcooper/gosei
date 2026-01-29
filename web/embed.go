package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var content embed.FS

// TemplatesFS returns the templates filesystem
func TemplatesFS() fs.FS {
	return content
}

// StaticFS returns the static files filesystem
func StaticFS() fs.FS {
	sub, _ := fs.Sub(content, "static")
	return sub
}
