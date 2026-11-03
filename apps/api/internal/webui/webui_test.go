package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testAssets() fstest.MapFS {
	files := fstest.MapFS{
		"index.html":                     {Data: []byte("home")},
		"404.html":                       {Data: []byte("missing")},
		"opengraph-image":                {Data: []byte("png")},
		"transactions.html":              {Data: []byte("transactions")},
		"login.html":                     {Data: []byte("login")},
		"_next/static/chunks/app.js":     {Data: []byte("js")},
		"fonts/inter.woff2":              {Data: []byte("font")},
		"transactions.txt":               {Data: []byte("rsc")},
		"login.txt":                      {Data: []byte("login rsc")},
		"manifest.webmanifest":           {Data: []byte(`{"name":"Richmod"}`)},
		"build-id.txt":                   {Data: []byte("0123456789ab\n")},
		"sw.js":                          {Data: []byte(`const BUILD = "abc123";`)},
		"icons/icon-192.png":             {Data: []byte("png192")},
		"icons/maskable-512.png":         {Data: []byte("pngmaskable")},
		"icons/apple-touch-icon-180.png": {Data: []byte("pngapple")},
	}
	for _, route := range []string{"actions", "admin", "analytics", "documents", "household", "inbox", "invite", "privacy", "reviews", "settings", "terms", "wealth"} {
		files[route+".html"] = &fstest.MapFile{Data: []byte(route)}
	}
	return files
}

func TestHandlerRoutesAndCacheClasses(t *testing.T) {
	handler := NewHandler(testAssets())
	tests := []struct {
		path      string
		status    int
		cache     string
		mediaType string
		body      string
		location  string
	}{
		{path: "/", status: http.StatusOK, cache: cacheAppHTML, mediaType: "text/html; charset=utf-8", body: "home"},
		{path: "/transactions", status: http.StatusOK, cache: cacheAppHTML, mediaType: "text/html; charset=utf-8", body: "transactions"},
		{path: "/admin", status: http.StatusOK, cache: cacheOtherHTML, mediaType: "text/html; charset=utf-8", body: "admin"},
		{path: "/login", status: http.StatusOK, cache: cacheOtherHTML, mediaType: "text/html; charset=utf-8", body: "login"},
		{path: "/_next/static/chunks/app.js", status: http.StatusOK, cache: cacheImmutable, mediaType: "text/javascript; charset=utf-8", body: "js"},
		{path: "/fonts/inter.woff2", status: http.StatusOK, cache: cachePublic, mediaType: "font/woff2", body: "font"},
		{path: "/transactions.txt", status: http.StatusOK, cache: cacheAppHTML, mediaType: "text/plain; charset=utf-8", body: "rsc"},
		{path: "/login.txt", status: http.StatusOK, cache: cacheOtherHTML, mediaType: "text/plain; charset=utf-8", body: "login rsc"},
		{path: "/opengraph-image", status: http.StatusOK, cache: cacheMetadata, mediaType: "image/png", body: "png"},
		{path: "/manifest.webmanifest", status: http.StatusOK, cache: cacheMetadata, mediaType: "application/manifest+json", body: `{"name":"Richmod"}`},
		{path: "/build-id.txt", status: http.StatusOK, cache: cacheMetadata, mediaType: "text/plain; charset=utf-8", body: "0123456789ab\n"},
		{path: "/sw.js", status: http.StatusOK, cache: cacheServiceWorker, mediaType: "text/javascript; charset=utf-8", body: `const BUILD = "abc123";`},
		{path: "/icons/icon-192.png", status: http.StatusOK, cache: cachePublic, mediaType: "image/png", body: "png192"},
		{path: "/icons/maskable-512.png", status: http.StatusOK, cache: cachePublic, mediaType: "image/png", body: "pngmaskable"},
		{path: "/icons/apple-touch-icon-180.png", status: http.StatusOK, cache: cachePublic, mediaType: "image/png", body: "pngapple"},
		{path: "/transactions/", status: http.StatusPermanentRedirect, location: "/transactions"},
		{path: "/missing", status: http.StatusNotFound, cache: "no-store", mediaType: "text/html; charset=utf-8", body: "missing"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cache {
				t.Errorf("Cache-Control = %q, want %q", got, test.cache)
			}
			if test.location != "" {
				if got := response.Header().Get("Location"); got != test.location {
					t.Errorf("Location = %q, want %q", got, test.location)
				}
				return
			}
			if got := response.Header().Get("Content-Type"); got != test.mediaType {
				t.Errorf("Content-Type = %q, want %q", got, test.mediaType)
			}
			if got := response.Body.String(); got != test.body {
				t.Errorf("body = %q, want %q", got, test.body)
			}
		})
	}
}

func TestHandlerMethodAndHead(t *testing.T) {
	handler := NewHandler(testAssets())

	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", post.Code)
	}

	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/transactions", nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status = %d, body = %q", head.Code, head.Body.String())
	}
	if got := head.Header().Get("Cache-Control"); got != cacheAppHTML {
		t.Fatalf("HEAD Cache-Control = %q, want %q", got, cacheAppHTML)
	}
}

func TestHandlerRejectsTraversal(t *testing.T) {
	handler := NewHandler(testAssets())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/../../etc/passwd", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestHasIndex(t *testing.T) {
	if hasIndex(fstest.MapFS{}) {
		t.Fatal("hasIndex reported an export for an empty assets tree")
	}
	if !hasIndex(fstest.MapFS{"assets/index.html": {Data: []byte("home")}}) {
		t.Fatal("hasIndex missed an embedded index.html")
	}
}
