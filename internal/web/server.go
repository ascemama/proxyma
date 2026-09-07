package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Wrap serves embedded frontend assets for non-API routes when they are present.
func Wrap(api http.Handler) http.Handler {
	return WithAssets(api, assetFS())
}

func WithAssets(api http.Handler, assets fs.FS) http.Handler {
	if assets == nil {
		return api
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		name := assetName(r.URL.Path)
		if exists, isDir := assetExists(assets, name); exists && !isDir {
			files.ServeHTTP(w, r)
			return
		}
		if hasFileExtension(name) {
			http.NotFound(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func isAPIPath(requestPath string) bool {
	return requestPath == "/v1" || strings.HasPrefix(requestPath, "/v1/")
}

func assetName(requestPath string) string {
	clean := path.Clean("/" + requestPath)
	if clean == "/" {
		return "index.html"
	}
	return strings.TrimPrefix(clean, "/")
}

func assetExists(assets fs.FS, name string) (bool, bool) {
	stat, err := fs.Stat(assets, name)
	if err != nil {
		return false, false
	}
	return true, stat.IsDir()
}

func hasFileExtension(name string) bool {
	base := path.Base(name)
	return strings.Contains(base, ".")
}
