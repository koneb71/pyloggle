package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"pyloggle/web"
)

// spaHandler serves the built React app: hashed assets with long cache headers, index.html for
// every client-side route. When dist holds no build, a short explanatory page is shown instead.
type spaHandler struct {
	fs    fs.FS
	index []byte
	files http.Handler
}

func newSPAHandler() *spaHandler {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return &spaHandler{}
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return &spaHandler{fs: sub}
	}
	return &spaHandler{fs: sub, index: index, files: http.FileServer(http.FS(sub))}
}

func (s *spaHandler) built() bool { return len(s.index) > 0 }

func (s *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.built() {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notBuiltPage))
		return
	}
	// /ui/assets/... and other real files
	rel := strings.TrimPrefix(r.URL.Path, "/ui/")
	rel = strings.TrimPrefix(rel, "/")
	if rel != "" && rel != "index.html" {
		if f, err := s.fs.Open(rel); err == nil {
			_ = f.Close()
			if strings.HasPrefix(rel, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + rel
			s.files.ServeHTTP(w, r2)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(s.index)
}

// isAssetPath reports whether a path looks like a static file request rather than a route.
func isAssetPath(p string) bool {
	return path.Ext(p) != ""
}

const notBuiltPage = `<!doctype html><html><head><meta charset="utf-8"><title>Pyloggle</title>
<style>body{font:15px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;color:#222}code{background:#eee;padding:.1rem .3rem;border-radius:3px}</style></head>
<body><h1>UI not built</h1><p>This binary was built without the web UI. Build it with:</p>
<pre><code>cd web &amp;&amp; npm ci &amp;&amp; npm run build
go build ./cmd/ingestd</code></pre>
<p>The JSON API is available under <code>/ui/api/</code> and <code>/v1/q/</code> meanwhile.</p></body></html>`
