package httpx

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Pagination limits (Milestone 8 Part 3). One fixed pair of constants is
// used by every list endpoint — there is no evidence any resource needs
// a different bound, and inventing per-resource limits would just be
// more surface to keep consistent for no benefit.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Pagination is the wire shape of every list response's "pagination"
// object. Total is the count of rows matching the current filters (not
// the table's overall row count) — see each resource's repository for
// how that count query shares its predicates with the item query.
type Pagination struct {
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

// ListResponse is the one wire shape every list endpoint returns:
// {"items": [...], "pagination": {...}}. A generic type rather than one
// hand-written struct per resource, since the shape itself never varies
// by resource — only the item type does.
type ListResponse[T any] struct {
	Items      []T        `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// NewListResponse builds a ListResponse, defaulting Items to an empty
// (never nil) slice so an empty result serializes as "items": [] rather
// than "items": null.
func NewListResponse[T any](items []T, limit, offset int, total int64) ListResponse[T] {
	if items == nil {
		items = []T{}
	}

	return ListResponse[T]{
		Items:      items,
		Pagination: Pagination{Limit: limit, Offset: offset, Total: total},
	}
}

// ParseLimitOffset parses the "limit" and "offset" query parameters,
// applying DefaultLimit/0 when a parameter is absent and rejecting an
// explicitly-supplied invalid value with 400 rather than silently
// clamping it — an absent value gets a default; a present-but-wrong one
// is the caller's mistake to fix, not this function's to paper over. On
// failure it has already written the error response; the caller only
// needs to return.
func ParseLimitOffset(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	query := r.URL.Query()

	limit = DefaultLimit
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "limit must be an integer")
			return 0, 0, false
		}
		if parsed <= 0 {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "limit must be greater than 0")
			return 0, 0, false
		}
		if parsed > MaxLimit {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "limit must not exceed "+strconv.Itoa(MaxLimit))
			return 0, 0, false
		}
		limit = parsed
	}

	offset = 0
	if raw := query.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "offset must be an integer")
			return 0, 0, false
		}
		if parsed < 0 {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "offset must not be negative")
			return 0, 0, false
		}
		offset = parsed
	}

	return limit, offset, true
}

// ParseSortOrder parses the "sort" and "order" query parameters against a
// fixed, resource-specific allow-list of public field names — never a
// client-supplied SQL identifier. "sort" is matched case-insensitively
// against allowed and returned in its canonical (allow-list) casing;
// "order" must be "asc" or "desc" (case-insensitive), normalized to
// lowercase. Either parameter being absent falls back to
// defaultField/defaultOrder. The caller (a repository) is responsible
// for mapping the returned field name onto an actual SQL column/
// expression via its own explicit switch — this function only validates
// the public contract, it has no notion of SQL at all.
func ParseSortOrder(w http.ResponseWriter, r *http.Request, allowed []string, defaultField, defaultOrder string) (field, order string, ok bool) {
	query := r.URL.Query()

	field = defaultField
	if raw := query.Get("sort"); raw != "" {
		matched := ""
		for _, candidate := range allowed {
			if strings.EqualFold(candidate, raw) {
				matched = candidate
				break
			}
		}
		if matched == "" {
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "invalid sort field")
			return "", "", false
		}
		field = matched
	}

	order = defaultOrder
	if raw := query.Get("order"); raw != "" {
		switch strings.ToLower(raw) {
		case "asc", "desc":
			order = strings.ToLower(raw)
		default:
			WriteError(w, http.StatusBadRequest, CodeValidationFailed, "order must be asc or desc")
			return "", "", false
		}
	}

	return field, order, true
}

// RejectUnknownQueryParams reports whether every parameter present in
// r's query string is in allowed, writing a 400 and returning false on
// the first one that isn't. This is a deliberate default-deny policy
// (Milestone 8 Part 3 section 4): a typo'd filter name silently doing
// nothing is a worse failure mode than a clear 400 telling the caller
// their parameter isn't recognised. It only ever applies to the new list
// endpoints that call it explicitly — no existing route's query-parameter
// handling (including the established, deliberately-inert
// ?organisationId= on single-resource routes) changes at all, since none
// of those routes parse query parameters or call this function.
func RejectUnknownQueryParams(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = struct{}{}
	}

	for key := range r.URL.Query() {
		if _, ok := allowedSet[key]; !ok {
			WriteError(w, http.StatusBadRequest, CodeInvalidRequest, "unknown query parameter: "+key)
			return false
		}
	}

	return true
}

// OptionalQueryParam returns the trimmed value of key if present and
// non-blank, and "" (with present=false) otherwise — a small shared
// convenience so every filter-parsing handler doesn't repeat the same
// three lines for "is this optional string filter actually supplied".
func OptionalQueryParam(query url.Values, key string) (value string, present bool) {
	raw := strings.TrimSpace(query.Get(key))
	if raw == "" {
		return "", false
	}

	return raw, true
}
