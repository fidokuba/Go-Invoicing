// Package openapi embeds the maintained OpenAPI 3.1 specification
// (openapi.yaml, in this same directory) directly into the binary via
// go:embed, so the runtime-served document (see internal/app.Handler) can
// never drift from the hand-maintained source file — there is exactly one
// copy of this specification, and this package only ever exposes it
// byte-for-byte.
package openapi

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
