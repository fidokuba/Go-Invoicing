package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, per the investigation's recommendation (OWASP's
// baseline for Argon2id). Named constants rather than magic numbers so a
// future tuning change has one obvious place to happen — verifyPassword
// does not depend on these except to hash a fresh comparison password
// with whatever parameters are actually encoded in a stored hash, so
// changing these constants never invalidates already-stored hashes.
const (
	argon2Memory      uint32 = 19456 // KiB (19 MiB)
	argon2Iterations  uint32 = 2
	argon2Parallelism uint8  = 1
	argon2SaltLength  uint32 = 16
	argon2KeyLength   uint32 = 32
)

// ErrMalformedHash is returned when an encoded hash string does not match
// the expected PHC-style Argon2id format at all (wrong number of
// segments, wrong algorithm tag, unparseable numbers, ...).
var ErrMalformedHash = errors.New("malformed password hash")

// ErrUnsupportedHashVariant is returned when an encoded hash identifies an
// Argon2 variant or version this code does not support (e.g. "argon2i",
// or a version newer than this package understands).
var ErrUnsupportedHashVariant = errors.New("unsupported password hash variant")

// ErrPasswordMismatch is returned by verifyPassword when the supplied
// password is well-formed but does not match the encoded hash.
var ErrPasswordMismatch = errors.New("password does not match")

// hashPassword derives an Argon2id key from password using a fresh,
// cryptographically random salt, and encodes the result — algorithm,
// version, parameters, salt and derived key — into a single self
// -describing string suitable for storing directly in the
// users.password_hash column:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 key>
//
// Because every parameter is encoded alongside the hash, verifyPassword
// can always reconstruct exactly how a given hash was produced, even if
// argon2Memory/argon2Iterations/argon2Parallelism above change later.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)

	return encoded, nil
}

// verifyPassword reports whether password matches encodedHash, an Argon2id
// hash produced by hashPassword (now or previously, possibly under
// different parameters). It returns nil on a match, ErrPasswordMismatch
// if the password is wrong, and ErrMalformedHash/ErrUnsupportedHashVariant
// if encodedHash itself isn't a hash this code can parse — never a panic.
//
// The parameters (memory/iterations/parallelism/salt) are parsed from
// encodedHash itself, not assumed to be today's argon2Memory/
// argon2Iterations/argon2Parallelism constants — this is what lets
// already-stored hashes keep verifying correctly after those constants
// are tuned in the future.
//
// verifyPassword is not called by any HTTP endpoint yet — it exists (and
// is tested) ahead of the login endpoint that will use it in a later
// part.
func verifyPassword(password, encodedHash string) error {
	version, memory, iterations, parallelism, salt, key, err := decodeHash(encodedHash)
	if err != nil {
		return err
	}

	if version != argon2.Version {
		return ErrUnsupportedHashVariant
	}

	candidateKey := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(key)))

	// subtle.ConstantTimeCompare requires equal-length slices; a length
	// mismatch alone (from a truncated/corrupted stored hash, say) is
	// itself a mismatch, not something to leak via an early return, so
	// route it through the same constant-time codepath instead.
	if len(candidateKey) != len(key) {
		return ErrPasswordMismatch
	}

	if subtle.ConstantTimeCompare(candidateKey, key) != 1 {
		return ErrPasswordMismatch
	}

	return nil
}

// decodeHash parses a "$argon2id$v=..$m=..,t=..,p=..$<salt>$<key>" string.
// Any deviation from that exact shape — wrong segment count, a non-argon2id
// algorithm tag, unparseable integers, invalid base64 — returns
// ErrMalformedHash rather than panicking.
func decodeHash(encodedHash string) (version int, memory uint32, iterations uint32, parallelism uint8, salt, key []byte, err error) {
	segments := strings.Split(encodedHash, "$")

	// A well-formed hash is "$argon2id$v=19$m=..,t=..,p=..$salt$key",
	// which splits (on the leading "$") into 6 parts: "", "argon2id",
	// "v=19", "m=..,t=..,p=..", salt, key.
	if len(segments) != 6 {
		return 0, 0, 0, 0, nil, nil, ErrMalformedHash
	}

	if segments[1] != "argon2id" {
		return 0, 0, 0, 0, nil, nil, ErrUnsupportedHashVariant
	}

	if _, err := fmt.Sscanf(segments[2], "v=%d", &version); err != nil {
		return 0, 0, 0, 0, nil, nil, ErrMalformedHash
	}

	if _, err := fmt.Sscanf(segments[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return 0, 0, 0, 0, nil, nil, ErrMalformedHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(segments[4])
	if err != nil {
		return 0, 0, 0, 0, nil, nil, ErrMalformedHash
	}

	key, err = base64.RawStdEncoding.DecodeString(segments[5])
	if err != nil {
		return 0, 0, 0, 0, nil, nil, ErrMalformedHash
	}

	return version, memory, iterations, parallelism, salt, key, nil
}
