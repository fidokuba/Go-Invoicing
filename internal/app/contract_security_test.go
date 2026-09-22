package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// publicRoutes is Milestone 8 Part 5 section 22's fixed, explicit list of
// every route that must NOT require authentication — taken verbatim from
// the task's own enumeration, not derived. Everything this application
// registers that is not in this set is expected to be protected.
//
// This is deliberately a small, explicit table rather than a generic
// "authorization metadata" framework: automating this fully would mean
// either duplicating app.go's authMiddleware.RequireAuth wiring in a
// second, parallel description (exactly the kind of second
// hand-maintained list Part 5 exists to avoid), or reflecting into the
// wrapped handler closures to detect whether RequireAuth is present,
// which net/http.HandlerFunc gives no way to do. A short, explicit,
// task-sourced list is the honest middle ground the task itself
// sanctions ("perform a focused explicit audit test instead").
var publicRoutes = map[string]bool{
	"POST /api/v1/register":    true,
	"POST /api/v1/auth/login":  true,
	"GET /health":              true,
	"GET /health/db":           true,
	"GET /api/v1/openapi.yaml": true,
}

// pathParamRE matches a ServeMux path-parameter segment like "{id}".
var pathParamRE = regexp.MustCompile(`\{[^{}]+\}`)

// concretePath substitutes every path-parameter segment in pattern with
// a syntactically-plausible placeholder, so a request can actually be
// built for it. The placeholder's exact value doesn't matter for this
// test — every handler here checks authentication before it ever parses
// a path parameter (see the doc comment on
// TestSecurity_RuntimeAuthenticationMatchesPublicRouteList for why that
// ordering is what makes this test valid without a database at all).
func concretePath(pattern string) string {
	return pathParamRE.ReplaceAllString(pattern, "11111111-1111-1111-1111-111111111111")
}

// TestSecurity_RuntimeAuthenticationMatchesPublicRouteList sends an
// unauthenticated request (no Authorization header, no body) to every
// route this application registers and asserts it gets 401 if and only
// if the route is NOT in publicRoutes.
//
// This works without a database because of the actual order every
// protected route's middleware runs in: authMiddleware.RequireAuth (see
// internal/administration/auth_middleware.go) calls parseBearerToken
// FIRST, and returns 401 immediately on a missing/malformed header
// without ever calling sessionRepository.GetByTokenHash — so a protected
// route rejects an unauthenticated request before touching the database
// at all, and a nil *pgxpool.Pool is safe here for exactly the same
// reason RoutePatterns() is (see App.New's own doc comment).
func TestSecurity_RuntimeAuthenticationMatchesPublicRouteList(t *testing.T) {
	application := New(nil, testLogger, nil)
	handler := application.Handler()

	for _, r := range application.RoutePatterns() {
		key := r.Method + " " + r.Path
		wantPublic := publicRoutes[key]

		t.Run(key, func(t *testing.T) {
			request := httptest.NewRequest(r.Method, concretePath(r.Path), nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			gotUnauthorized := recorder.Code == http.StatusUnauthorized
			if wantPublic && gotUnauthorized {
				t.Errorf("%s is documented as public but an unauthenticated request got 401", key)
			}
			if !wantPublic && !gotUnauthorized {
				t.Errorf("%s is expected to be protected but an unauthenticated request got %d, not 401", key, recorder.Code)
			}
		})
	}

	// Sanity check: every entry in publicRoutes actually corresponds to a
	// registered route, so a typo'd/stale entry (e.g. after a route is
	// renamed) can't silently stop being exercised by the loop above.
	registered := make(map[string]bool)
	for _, r := range application.RoutePatterns() {
		registered[r.Method+" "+r.Path] = true
	}
	for key := range publicRoutes {
		if !registered[key] {
			t.Errorf("publicRoutes lists %q but no such route is registered", key)
		}
	}
}

// TestSecurity_SpecDeclarationsMatchPublicRouteList is the OpenAPI-side
// half: it walks every operation in the maintained spec and asserts its
// `security` declaration agrees with the same publicRoutes list — an
// operation in publicRoutes must declare `security: []` (explicitly
// overriding the document's global bearerAuth requirement); every other
// operation must NOT declare an empty security override (it either
// inherits the global requirement by declaring nothing, or repeats
// bearerAuth explicitly — either is fine, an accidental `security: []`
// on a route that should be protected is what this catches).
func TestSecurity_SpecDeclarationsMatchPublicRouteList(t *testing.T) {
	doc := loadSpec(t)

	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			methodUpper := strings.ToUpper(method)
			op := pathItem.Operations()[methodUpper]
			key := methodUpper + " " + path

			isEmptySecurity := op.Security != nil && len(*op.Security) == 0
			wantPublic := publicRoutes[key]

			if wantPublic && !isEmptySecurity {
				t.Errorf("%s is documented as public in this test's list but its OpenAPI operation does not declare `security: []`", key)
			}
			if !wantPublic && isEmptySecurity {
				t.Errorf("%s declares `security: []` (public) in the OpenAPI spec but is expected to be protected", key)
			}
		}
	}
}
