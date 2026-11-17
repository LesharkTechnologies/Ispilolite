package otp

import "testing"

func TestHashIsDeterministicAndDistinct(t *testing.T) {
	t.Setenv(HashKeyEnv, "test-key")

	first := Hash("123456")
	second := Hash("123456")
	if first != second {
		t.Fatalf("hash is not deterministic: %s vs %s", first, second)
	}
	if first == Hash("654321") {
		t.Fatal("different codes produced the same digest")
	}
	if len(first) != 64 {
		t.Fatalf("expected 64-character hex digest, got %d characters", len(first))
	}
}

func TestHashDependsOnKey(t *testing.T) {
	t.Setenv(HashKeyEnv, "key-one")
	withFirstKey := Hash("123456")
	t.Setenv(HashKeyEnv, "key-two")
	withSecondKey := Hash("123456")
	if withFirstKey == withSecondKey {
		t.Fatal("digest did not change when the hashing key changed")
	}
}

func TestMatches(t *testing.T) {
	t.Setenv(HashKeyEnv, "test-key")

	digest := Hash("123456")
	if !Matches("123456", digest) {
		t.Fatal("correct code did not verify")
	}
	if Matches("654321", digest) {
		t.Fatal("wrong code verified")
	}
	if Matches("123456", "not-a-digest") {
		t.Fatal("malformed digest verified")
	}
}

func TestHashFallsBackToJWTSecret(t *testing.T) {
	t.Setenv(HashKeyEnv, "")
	t.Setenv("JWT_SECRET", "jwt-fallback-key")
	if !Matches("123456", Hash("123456")) {
		t.Fatal("JWT_SECRET fallback key did not round-trip")
	}
}
