// Package webui embeds and serves the production frontend build
// (Milestone 12): the compiled React/TypeScript/Vite application under
// internal/webui/dist, produced by `cd web && npm run build` (see
// web/vite.config.ts's build.outDir, which points directly here so no
// separate copy step is needed).
//
// Exactly like api/openapi.go's embedding of openapi.yaml, this package
// embeds the one artifact the frontend build produces via go:embed —
// there is no second, server-side-templated or separately-hosted copy of
// the frontend anywhere. A minimal placeholder dist/index.html is
// committed so `go build ./...` and `go test ./...` never require
// Node/npm to succeed (go:embed needs *something* on disk at compile
// time) — see that file's own comment, and the project README's frontend
// development section, for the full explanation. Everything else a real
// `npm run build` writes into this directory is git-ignored; only the
// placeholder is tracked.
//
// # Why this package registers no net/http route at all
//
// An earlier version of this package registered a "GET /" pattern
// directly on the application's net/http.ServeMux, and served the SPA
// shell (or a matching static asset) whenever that pattern was reached.
// That approach was reverted after it was proven (empirically, via this
// project's own existing test suite — see internal/httpx's
// FrontendFallback doc comment and internal/app's own comment at its
// call site) to corrupt net/http.ServeMux's automatic 405-vs-404
// distinction for *every other unmatched path in the entire
// application*: once any pattern rooted at "/" exists, ServeMux treats
// it as a path match for every otherwise-unregistered path, turning a
// wrong-method request against a genuinely nonexistent route (e.g. POST
// to a long-removed unversioned bootstrap path) from a plain 404 into a
// 405 — a real, tested, pre-Milestone-12 contract this package must
// never disturb.
//
// Instead, this package registers nothing on the mux at all. Serve is
// called only by internal/httpx.FrontendFallback, and only for the one
// case that's actually safe to override: a GET/HEAD request that
// net/http.ServeMux found *no registered pattern for whatsoever* (see
// that middleware's own doc comment for how it detects this precisely,
// via the request's Pattern field). Every other request — anything that
// matched a real route, any non-404 status, any domain-specific 404 an
// application handler chose to return itself, and any non-GET/HEAD
// method — is never seen by this package at all.
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS is the embedded frontend build, rooted so "index.html" and
// "assets/<hashed-name>" (Vite's default hashed-asset output directory)
// are directly addressable — see Serve.
var FS = mustSub(distFS, "dist")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		// Unreachable in practice: dist/ always exists (the committed
		// placeholder guarantees it), and "dist" is a fixed, valid
		// fs.Sub argument — a failure here could only mean the //go:embed
		// directive itself is broken, which would already fail the build.
		panic(err)
	}

	return sub
}

// frontendRoutePrefixes are this application's actual client-side routes
// (see web/src/App.tsx) — the only paths Serve will ever render the SPA
// shell for. This is deliberately an explicit allowlist, not "any path
// that isn't obviously an API path": this project's own pre-existing
// test suite (internal/app) locks in the guarantee that a handful of
// bare, unprefixed paths left over from before API versioning (e.g.
// GET /organisation, a removed bootstrap route — see
// TestApp_UnversionedRoutes_NoLongerWork) keep returning a genuine 404,
// not the SPA shell, forever. A blanket "serve the app for anything"
// fallback would silently resurrect those as 200s the moment their
// wording happened to not start with /api, /health, or /metrics.
var frontendRoutePrefixes = []string{
	"/login",
	"/register",
	"/terms",
	"/dashboard",
	"/customers",
	"/products",
	"/invoices",
	"/settings",
}

// IsFrontendRoute reports whether path is (or is nested under) one of
// this application's real client-side routes, plus the root path itself.
func IsFrontendRoute(path string) bool {
	if path == "/" {
		return true
	}

	for _, prefix := range frontendRoutePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}

// assetsPrefix is the directory Vite writes every content-hashed,
// long-lived build artifact under (its default "assets/" output
// directory — see web/vite.config.ts). A file living there may be cached
// aggressively and immutably: its filename changes whenever its content
// does, so there is no staleness risk. Everything else this handler
// serves — index.html above all — must NOT be cached this way, since a
// deployment change rewrites it in place and it references the current
// build's hashed asset filenames; see cacheControlFor.
const assetsPrefix = "assets/"

func cacheControlFor(cleanPath string) string {
	if strings.HasPrefix(cleanPath, assetsPrefix) {
		return "public, max-age=31536000, immutable"
	}

	return "no-cache"
}

// isStaticFile reports whether clean names a real, non-directory file in
// the embedded build.
func isStaticFile(clean string) bool {
	f, err := FS.Open(clean)
	if err != nil {
		return false
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return !info.IsDir()
}

var fileServer = http.FileServer(http.FS(FS))

// Serve renders this application's frontend for a request
// internal/httpx.FrontendFallback has already established is a genuine,
// pattern-less 404 for a GET or HEAD method (see that middleware's doc
// comment, and this package's own doc comment, for exactly why that
// precondition matters and how it's guaranteed before this is ever
// called):
//
//   - an exact embedded static asset match (e.g. /assets/index-abc.js)
//     is served as-is, with a long-lived cache header;
//   - a path under IsFrontendRoute serves index.html, so the SPA's own
//     client-side router can take over — this is what makes a refreshed
//     or directly-linked route like /invoices/<id> work;
//   - anything else gets byte-for-byte the same plain-text 404
//     net/http's own default NotFoundHandler would have produced,
//     preserving this project's pre-existing behaviour for every path
//     that is neither a real asset nor a real frontend route (e.g. a
//     removed legacy bootstrap path, or a genuine typo).
func Serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	clean := strings.TrimPrefix(path, "/")

	if isStaticFile(clean) {
		prepareForFileServer(w, cacheControlFor(clean))
		fileServer.ServeHTTP(w, r)
		return
	}

	if IsFrontendRoute(path) {
		prepareForFileServer(w, cacheControlFor("index.html"))
		// Rewrite to "/" so http.FileServer resolves it to index.html,
		// exactly as a static host's own SPA rewrite rule would.
		fallback := r.Clone(r.Context())
		fallback.URL.Path = "/"
		fileServer.ServeHTTP(w, fallback)
		return
	}

	notFoundPlainText(w)
}

// prepareForFileServer clears the header state net/http's own
// NotFoundHandler already wrote before Serve ever runs (Content-Type:
// text/plain — see
// internal/httpx.FrontendFallback's doc comment: this function is only
// ever reached mid-flight through that exact call). http.FileServer's
// underlying ServeContent only sets Content-Type when none is already
// present, so leaving the stale text/plain value in place would silently
// serve real HTML/JS/CSS content mislabelled as plain text — this must
// run before every call to fileServer.ServeHTTP below.
//
// X-Content-Type-Options: nosniff is deliberately kept (Milestone 13
// Part 7): http.FileServer sets each file's real Content-Type, so the
// directive is correct for real assets too — see
// httpx.SecurityHeaders, which sets it on every response.
func prepareForFileServer(w http.ResponseWriter, cacheControl string) {
	h := w.Header()
	h.Del("Content-Type")
	h.Set("Cache-Control", cacheControl)
}

// notFoundPlainText reproduces net/http's own default NotFoundHandler
// output exactly (see net/http.NotFound / net/http.Error) — same status,
// same headers, same body — so a path that is neither a real asset nor a
// recognised frontend route behaves identically to how it always did,
// before this package's interception existed at all.
func notFoundPlainText(w http.ResponseWriter) {
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintln(w, "404 page not found")
}
