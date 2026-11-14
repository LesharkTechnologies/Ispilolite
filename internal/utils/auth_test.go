package utils

import (
	"testing"
	"time"
)

func testClaims(expires time.Time) TokenClaims {
	now := time.Now().UTC()
	return TokenClaims{
		UserID:    "user-1",
		Role:      "customer",
		TokenUse:  "access",
		Issuer:    "ispilolite",
		Subject:   "user-1",
		Expires:   expires.Unix(),
		NotBefore: now.Unix(),
		IssuedAt:  now.Unix(),
		ID:        "jti-1",
	}
}

func TestSignAndParseTokenRoundTrip(t *testing.T) {
	secret := []byte("test-secret")
	want := testClaims(time.Now().Add(time.Hour))

	signed, err := SignToken(secret, want)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	got, err := ParseToken(secret, signed)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if got.UserID != want.UserID || got.Role != want.Role || got.TokenUse != want.TokenUse {
		t.Fatalf("claims mismatch: got %+v want %+v", got, want)
	}
	if got.Issuer != want.Issuer || got.Subject != want.Subject || got.ID != want.ID {
		t.Fatalf("registered claims mismatch: got %+v want %+v", got, want)
	}
	if got.Expires != want.Expires || got.IssuedAt != want.IssuedAt || got.NotBefore != want.NotBefore {
		t.Fatalf("timestamps mismatch: got exp=%d iat=%d nbf=%d", got.Expires, got.IssuedAt, got.NotBefore)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	signed, err := SignToken([]byte("secret-a"), testClaims(time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if _, err := ParseToken([]byte("secret-b"), signed); err == nil {
		t.Fatal("token signed with a different secret was accepted")
	}
}

func TestParseTokenRejectsExpired(t *testing.T) {
	signed, err := SignToken([]byte("test-secret"), testClaims(time.Now().Add(-time.Minute)))
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if _, err := ParseToken([]byte("test-secret"), signed); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestParseTokenRequiresExpiration(t *testing.T) {
	claims := testClaims(time.Now().Add(time.Hour))
	claims.Expires = 0 // omit the exp claim entirely
	signed, err := SignToken([]byte("test-secret"), claims)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if _, err := ParseToken([]byte("test-secret"), signed); err == nil {
		t.Fatal("token without an exp claim was accepted")
	}
}

func TestParseTokenRejectsUnsignedToken(t *testing.T) {
	// {"alg":"none"} . {"sub":"user-1"} . (empty signature)
	const unsigned = "eyJhbGciOiJub25lIn0.eyJzdWIiOiJ1c2VyLTEifQ."
	if _, err := ParseToken([]byte("test-secret"), unsigned); err == nil {
		t.Fatal("alg=none token was accepted")
	}
}
