package admin

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateSessionToken_ReturnsNonEmptyToken(t *testing.T) {
	token, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token: %v", err)
	}

	if token == "" {
		t.Fatal("expected a non-empty token")
	}
}

func TestGenerateSessionToken_IsURLSafeBase64(t *testing.T) {
	token, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token: %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("expected token to be valid RawURLEncoding base64, got error: %v", err)
	}

	if len(decoded) != sessionTokenLength {
		t.Errorf("expected %d decoded bytes, got %d", sessionTokenLength, len(decoded))
	}

	if strings.ContainsAny(token, "+/=") {
		t.Errorf("expected a URL-safe token with no '+', '/' or '=' characters, got %q", token)
	}
}

func TestGenerateSessionToken_DifferentCallsProduceDifferentTokens(t *testing.T) {
	first, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token (first): %v", err)
	}

	second, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate session token (second): %v", err)
	}

	if first == second {
		t.Fatal("expected two generated tokens to differ")
	}
}

func TestHashSessionToken_IsDeterministic(t *testing.T) {
	const token = "some-raw-token-value"

	if hashSessionToken(token) != hashSessionToken(token) {
		t.Error("expected hashing the same token twice to produce the same hash")
	}
}

func TestHashSessionToken_DifferentTokensProduceDifferentHashes(t *testing.T) {
	if hashSessionToken("token-a") == hashSessionToken("token-b") {
		t.Error("expected different tokens to hash differently")
	}
}

func TestHashSessionToken_DoesNotContainTheRawToken(t *testing.T) {
	const token = "some-raw-token-value"

	hash := hashSessionToken(token)
	if hash == token {
		t.Fatal("expected the hash to differ from the raw token")
	}

	if strings.Contains(hash, token) {
		t.Fatal("expected the hash not to contain the raw token")
	}
}

func TestHashSessionToken_IsHexEncoded(t *testing.T) {
	hash := hashSessionToken("some-raw-token-value")

	// SHA-256 hex-encoded is always exactly 64 characters.
	if len(hash) != 64 {
		t.Errorf("expected a 64-character hex-encoded SHA-256 hash, got %d characters (%q)", len(hash), hash)
	}

	if strings.ToLower(hash) != hash {
		t.Errorf("expected a lowercase hex string, got %q", hash)
	}
}
