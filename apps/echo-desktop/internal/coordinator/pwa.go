package coordinator

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed pwa_dist/*
var pwaFS embed.FS

// pwaHandler returns an http.Handler that serves the embedded PWA files.
func pwaHandler() http.Handler {
	sub, err := fs.Sub(pwaFS, "pwa_dist")
	if err != nil {
		panic("failed to create sub filesystem for PWA: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
