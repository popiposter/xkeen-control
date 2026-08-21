package webassets

import (
	"embed"
	"io/fs"
	"net/http"
)

// Files is populated by scripts/build-control-plane.sh from the Vite
// production build. Keeping the embed boundary in a tiny package makes it
// impossible for HTTP handlers to read arbitrary router files.
//
//go:embed dist/*
var Files embed.FS

func Handler() http.Handler {
	subtree, err := fs.Sub(Files, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(subtree))
}
