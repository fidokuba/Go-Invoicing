package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// sessionTokenLength is the number of random bytes read to generate a raw
// session token. 32 bytes (256 bits) of crypto/rand output is far beyond
// what's practically guessable — the same entropy budget widely used for
// session and API tokens.
const sessionTokenLength = 32

// generateSessionToken returns a new cryptographically random session
// token, base64url-encoded without padding so it's safe to place directly
// into an "Authorization: Bearer <token>" header (or a URL) with no
// further escaping. The value returned here is what's handed to the
// client — it is never persisted; see hashSessionToken.
func generateSessionToken() (string, error) {
	raw := make([]byte, sessionTokenLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashSessionToken returns the SHA-256 hash of a raw session token,
// hex-encoded, for storage and lookup.
//
// SHA-256 — not Argon2id — is deliberate: the input here is a 256-bit
// cryptographically random value, not a low-entropy human-chosen
// password. Argon2id's deliberate slowness defends against brute-forcing
// a guessable secret, which doesn't apply to a value nobody could ever
// guess in the first place; a fast, standard cryptographic hash is the
// appropriate and sufficient choice for this input.
func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
