// Package webui embeds the static Next.js export and serves it from the API,
// so one container serves both the UI and /api.
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

// Cache-Control values reproduce the responses the Next.js server sent before
// the export: app routes were pinned by next.config headers, other pages used
// the dynamic-page default, hashed chunks were immutable, and public files were
// revalidated.
const (
	cacheAppHTML   = "no-store, max-age=0"
	cacheOtherHTML = "private, no-cache, no-store, max-age=0, must-revalidate"
	cacheImmutable = "public, max-age=31536000, immutable"
	cachePublic    = "public, max-age=0"
	cacheMetadata  = "public, max-age=0, must-revalidate"
)

// appRoutes are the paths next.config.mjs used to send "no-store, max-age=0".
var appRoutes = map[string]struct{}{
	"/":             {},
	"/transactions": {},
	"/analytics":    {},
	"/reviews":      {},
	"/documents":    {},
	"/household":    {},
	"/settings":     {},
}

// Handler serves the export embedded in the binary.
func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic("webui: embedded assets missing: " + err.Error())
	}
	return NewHandler(assets)
}

// Embedded reports whether this binary contains a built export. A tree without
// a web build still compiles and serves 404s for the UI, so main warns at startup.
func Embedded() bool {
	return hasIndex(embedded)
}

func hasIndex(fsys fs.FS) bool {
	_, err := fs.Stat(fsys, "assets/index.html")
	return err == nil
}

// NewHandler serves a static export from assets. Tests pass an fstest.MapFS.
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
	// The export uses trailingSlash:false; keep the previous 308 from /x/ to /x.
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
	// A path that 404s today may exist after the next deploy; never let a shared
	// cache serve this body for it.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// exists is only a cheap guard: path.Clean already removed traversal and the
// embedded FS is rooted at assets. The root, not this check, is the boundary.
func (h handler) exists(name string) bool {
	if name == "" || strings.Contains(name, "..") {
		return false
	}
	info, err := fs.Stat(h.assets, name)
	return err == nil && info.Mode().IsRegular()
}

func cacheFor(name string) string {
	switch name {
	case "opengraph-image", "icon.svg":
		return cacheMetadata
	}
	if strings.HasPrefix(name, "_next/static/") {
		return cacheImmutable
	}
	if strings.HasSuffix(name, ".html") {
		return htmlCacheFor(routeOf(name))
	}
	// The static export's client router loads RSC payloads as `<route>.txt`;
	// they carry the same class as the route's HTML.
	if strings.HasSuffix(name, ".txt") {
		return htmlCacheFor(routeOf(strings.TrimSuffix(name, ".txt") + ".html"))
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
	if value := mime.TypeByExtension(path.Ext(name)); value != "" {
		return value
	}
	return "application/octet-stream"
}
