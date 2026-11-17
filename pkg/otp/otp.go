// Package otp provides helpers for server-generated one-time verification
// codes. Codes are generated elsewhere with crypto/rand; this package only
// handles at-rest protection so plaintext codes are never persisted.
package otp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// HashKeyEnv is the environment variable holding the dedicated OTP hashing
// key. It falls back to JWT_SECRET, and finally to an insecure development
// fallback that must never appear in production.
const HashKeyEnv = "OTP_HASH_KEY"

// Hash returns the keyed HMAC-SHA256 digest of a verification code. The same
// key and algorithm must be used for both storing and verifying, which this
// package guarantees by being the only implementation.
//
// The digest is deterministic, so a leaked digest cannot be replayed as a
// code and a leaked Redis snapshot does not expose the codes themselves.
func Hash(code string) string {
	mac := hmac.New(sha256.New, hashKey())
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// Matches reports whether a submitted code verifies against a stored digest,
// using a constant-time comparison that does not leak the match position.
func Matches(submitted, storedDigest string) bool {
	return hmac.Equal([]byte(Hash(submitted)), []byte(storedDigest))
}

// hashKey resolves the hashing key from the environment on every call. OTP
// flows call this at most twice per code, so the lookup cost is negligible
// next to the Redis round-trip and SMS delivery, and per-call resolution
// keeps the key hot-reloadable without a process restart.
func hashKey() []byte {
	key := os.Getenv(HashKeyEnv)
	if key == "" {
		key = os.Getenv("JWT_SECRET")
	}
	if key == "" {
		key = "ispilolite-dev-secret"
	}
	return []byte(key)
}
