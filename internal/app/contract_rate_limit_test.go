package app

import "testing"

// Milestone 13 Part 4: 429 is documented on exactly the three
// rate-limited operations, with its Retry-After header.
func TestRateLimitContract_429OnlyOnLimitedOperations(t *testing.T) {
	doc := loadSpec(t)

	limited := map[string]bool{
		"POST /api/v1/auth/login":       true,
		"POST /api/v1/register":         true,
		"GET /api/v1/invoices/{id}/pdf": true,
	}

	for path, pathItem := range doc.Paths.Map() {
		for method, op := range pathItem.Operations() {
			key := method + " " + path
			response := op.Responses.Status(429)

			if limited[key] {
				if response == nil {
					t.Errorf("%s: expected a documented 429", key)
					continue
				}
				if _, ok := response.Value.Headers["Retry-After"]; !ok {
					t.Errorf("%s: expected the 429 to document Retry-After", key)
				}
			} else if response != nil {
				t.Errorf("%s is not rate-limited but documents a 429", key)
			}
		}
	}
}
