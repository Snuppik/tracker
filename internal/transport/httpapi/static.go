package httpapi

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"tracker/web"
)

// newStaticHandler отдаёт встроенную статику. Всё неизвестное уходит в index.html.
func newStaticHandler() http.Handler {
	fileServer := http.FileServerFS(web.Files)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		switch {
		case name == "" || name == ".":
			serveEmbedded(w, r, "index.html")
		case name == "login":
			serveEmbedded(w, r, "login.html")
		case name == "sw.js":
			// Service Worker не кэшируем, иначе обновление затянется (docs/04-api.md §8).
			serveEmbedded(w, r, "sw.js")
		case strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "."):
			http.NotFound(w, r)
		default:
			if _, err := fs.Stat(web.Files, name); err == nil {
				if strings.HasPrefix(name, "css/") || strings.HasPrefix(name, "js/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			// SPA-fallback: маршруты вида /board отдают index.html.
			serveEmbedded(w, r, "index.html")
		}
	})
}

func serveEmbedded(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(web.Files, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
