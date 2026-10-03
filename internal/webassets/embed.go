package webassets

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"strings"
	"time"
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
	static := http.FileServer(http.FS(subtree))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path != "/" && r.URL.Path != "/index.html") || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			static.ServeHTTP(w, r)
			return
		}
		contents, err := fs.ReadFile(subtree, "index.html")
		var random [32]byte
		if err == nil {
			_, err = rand.Read(random[:])
		}
		if err != nil {
			http.Error(w, "UI unavailable", http.StatusInternalServerError)
			return
		}
		nonce := base64.StdEncoding.EncodeToString(random[:])
		// Only the current document's component styles receive authorization.
		// Script restrictions and all other response security directives remain.
		policy := w.Header().Get("Content-Security-Policy")
		style := "style-src 'self' 'nonce-" + nonce + "'"
		if policy == "" {
			policy = style
		} else {
			policy = strings.Replace(policy, "style-src 'self'", style, 1)
		}
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Cache-Control", "no-store")
		meta := []byte(`<meta name="style-nonce" content="` + nonce + `">`)
		contents = bytes.Replace(contents, []byte("<head>"), append([]byte("<head>"), meta...), 1)
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(contents))
	})
}
