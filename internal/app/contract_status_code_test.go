package app

import (
	"strings"
	"testing"
)

// TestStatusCodeContract_413And415OnlyOnJSONBodiedOperations is
// Milestone 8 Part 5 section 25: 413 (body too large) and 415 (wrong
// Content-Type) can only ever be produced by httpx.DecodeJSON, which
// only the ten operations in jsonBodiedOperations call at all — every
// other operation's handler never reads a request body, so documenting
// either status on it would claim a status code that operation can
// never actually emit.
//
// This is deliberately a separate test from
// TestRequestBodyContract_OnlyJSONBodiedOperationsDeclareARequestBody:
// that one caught a real bug this same investigation found (POST
// /api/v1/invoices had a spurious 409 for a case the handler actually
// maps to 500) by checking requestBody presence; this one checks the
// two specific status codes that bug's neighbourhood could just as
// easily have gotten wrong in the other direction.
func TestStatusCodeContract_413And415OnlyOnJSONBodiedOperations(t *testing.T) {
	doc := loadSpec(t)

	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			methodUpper := strings.ToUpper(method)
			op := pathItem.Operations()[methodUpper]
			key := methodUpper + " " + normalizePath(path)

			has413 := op.Responses.Value("413") != nil
			has415 := op.Responses.Value("415") != nil
			wantBoth := jsonBodiedOperations[key]

			if wantBoth {
				if !has413 {
					t.Errorf("%s is JSON-bodied but does not document 413", key)
				}
				if !has415 {
					t.Errorf("%s is JSON-bodied but does not document 415", key)
				}
			} else {
				if has413 {
					t.Errorf("%s is not JSON-bodied but documents 413, which its handler can never produce", key)
				}
				if has415 {
					t.Errorf("%s is not JSON-bodied but documents 415, which its handler can never produce", key)
				}
			}
		}
	}
}

// roleGatedOperations is section 26's fixed, explicit list of every
// operation with an actual role restriction or a resource-specific
// self-vs-other authorization check — confirmed by reading
// internal/administration/app.go's RequireRole wiring and
// UserHandler.GetByID's inline check directly (see this package's own
// Part 5 report). Every other protected operation can only ever reject
// with 401 (no valid session), never 403 (a valid session whose role
// isn't permitted) — see auth_middleware.go's unauthorized/forbidden
// split for why those are kept as distinct failure modes in the first
// place.
var roleGatedOperations = map[string]bool{
	"POST /api/v1/users":                  true,
	"GET /api/v1/users":                   true,
	"GET /api/v1/users/{}":                true,
	"PATCH /api/v1/organisation":          true,
	"PATCH /api/v1/organisation/settings": true,
}

// TestStatusCodeContract_403OnlyOnRoleGatedOperations is section 26: 403
// must be documented if and only if the operation is in
// roleGatedOperations — never mechanically added to every protected
// route just because 401 is also documented there.
func TestStatusCodeContract_403OnlyOnRoleGatedOperations(t *testing.T) {
	doc := loadSpec(t)

	for path, pathItem := range doc.Paths.Map() {
		for _, method := range operationMethods(pathItem) {
			methodUpper := strings.ToUpper(method)
			op := pathItem.Operations()[methodUpper]
			key := methodUpper + " " + normalizePath(path)

			// Public operations have no 401 either, and never 403.
			if publicRoutes[key] {
				continue
			}

			has403 := op.Responses.Value("403") != nil
			wantRoleGated := roleGatedOperations[key]

			if wantRoleGated && !has403 {
				t.Errorf("%s has a role restriction but does not document 403", key)
			}
			if !wantRoleGated && has403 {
				t.Errorf("%s documents 403 but has no role restriction or self-vs-other check in the implementation", key)
			}
		}
	}
}
