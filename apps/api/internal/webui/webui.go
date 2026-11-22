package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:assets
var embedded embed.FS

const (
	cacheAppHTML       = "no-store, max-age=0"
	cacheOtherHTML     = "private, no-cache, no-store, max-age=0, must-revalidate"
	cacheImmutable     = "public, max-age=31536000, immutable"
	cachePublic        = "public, max-age=0"
	cacheMetadata      = "public, max-age=0, must-revalidate"
	cacheServiceWorker = "no-cache"
)

const hashedAssetDir = "assets/"

var appRoutes = map[string]struct{}{
	"/":             {},
	"/transactions": {},
	"/analytics":    {},
	"/reviews":      {},
	"/documents":    {},
	"/household":    {},
	"/settings":     {},
}

func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic("webui: embedded assets missing: " + err.Error())
	}
	return NewHandler(assets)
}

func Embedded() bool {
	return hasIndex(embedded)
}

func hasIndex(fsys fs.FS) bool {
	_, err := fs.Stat(fsys, "assets/index.html")
	return err == nil
}

func NewHandler(assets fs.FS) http.Handler {
	return handler{assets: assets}
}

type handler struct {
	assets fs.FS
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clean := path.Clean("/" + r.URL.Path)
	if clean == "/" {
		h.serve(w, r, "index.html", cacheAppHTML)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/") && h.exists(strings.TrimPrefix(clean, "/")+".html") {
		target := clean
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
		return
	}
	name := strings.TrimPrefix(clean, "/")
	for _, candidate := range []string{name, name + ".html", name + "/index.html"} {
		if h.exists(candidate) {
			h.serve(w, r, candidate, cacheFor(candidate))
			return
		}
	}
	h.notFound(w, r)
}

func (h handler) serve(w http.ResponseWriter, r *http.Request, name, cache string) {
	data, err := fs.ReadFile(h.assets, name)
	if err != nil {
		h.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("Content-Type", contentType(name))
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (h handler) notFound(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(h.assets, "404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (h handler) exists(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	info, err := fs.Stat(h.assets, name)
	return err == nil && info.Mode().IsRegular()
}

func cacheFor(name string) string {
	switch name {
	case "opengraph-image", "icon.svg", "manifest.webmanifest":
		return cacheMetadata
	case "build-id.txt":
		return cacheMetadata
	case "sw.js":
		return cacheServiceWorker
	}
	if strings.HasPrefix(name, hashedAssetDir) {
		return cacheImmutable
	}
	if strings.HasSuffix(name, ".html") {
		return htmlCacheFor(routeOf(name))
	}
	return cachePublic
}

func htmlCacheFor(route string) string {
	if _, ok := appRoutes[route]; ok {
		return cacheAppHTML
	}
	return cacheOtherHTML
}

func routeOf(name string) string {
	if name == "index.html" {
		return "/"
	}
	return "/" + strings.TrimSuffix(name, ".html")
}

func contentType(name string) string {
	if name == "opengraph-image" {
		return "image/png"
	}
	if name == "manifest.webmanifest" {
		return "application/manifest+json"
	}
	if value := mime.TypeByExtension(path.Ext(name)); value != "" {
		return value
	}
	return "application/octet-stream"
}
