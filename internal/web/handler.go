package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// RegisterRoutes adds static file serving to the mux.
// Serves embedded web assets at "/" with SPA fallback to index.html.
func RegisterRoutes(mux *http.ServeMux) {
	// Get the "static" subdirectory from the embedded FS
	staticFS, err := fs.Sub(StaticFS, "static")
	if err != nil {
		panic("web: failed to get static subdirectory: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(staticFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only serve GET/HEAD for static files
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		path := r.URL.Path

		// Try to serve the file directly
		// If it's a known static file (has extension), serve it
		// Otherwise fall back to index.html for SPA routing
		if path == "/" || path == "" || !hasFileExtension(path) {
			// Serve index.html for SPA routes
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}

// hasFileExtension checks if the path looks like it has a file extension
func hasFileExtension(path string) bool {
	// Check for common static file extensions
	extensions := []string{".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot"}
	for _, ext := range extensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
