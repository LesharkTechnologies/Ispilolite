package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword compares a plaintext password against a bcrypt hash.
func VerifyPassword(password, hash string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// TokenClaims is the JWT claim set used for access and refresh tokens.
type TokenClaims struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	TokenUse  string `json:"token_use"`
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Expires   int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
	IssuedAt  int64  `json:"iat"`
	ID        string `json:"jti"`
}

// jwtClaims adapts TokenClaims to the golang-jwt/v5 Claims interface via the
// embedded RegisteredClaims type. The library is the maintained successor to
// the archived dgrijalva/jwt-go module (CVE-2020-26160).
type jwtClaims struct {
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	TokenUse string `json:"token_use"`
	jwt.RegisteredClaims
}

// allowedMethods restricts verification to the single algorithm this service
// issues. Rejecting anything else prevents algorithm-confusion attacks.
var allowedMethods = []string{jwt.SigningMethodHS256.Alg()}

// SignToken signs a TokenClaims set into a compact JWT string.
func SignToken(secret []byte, claims TokenClaims) (string, error) {
	c := jwtClaims{
		UserID:   claims.UserID,
		Role:     claims.Role,
		TokenUse: claims.TokenUse,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        claims.ID,
			Issuer:    claims.Issuer,
			Subject:   claims.Subject,
			ExpiresAt: numericDate(claims.Expires),
			NotBefore: numericDate(claims.NotBefore),
			IssuedAt:  numericDate(claims.IssuedAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return token.SignedString(secret)
}

// ParseToken parses and validates a JWT string, returning its TokenClaims.
func ParseToken(secret []byte, tokenString string) (*TokenClaims, error) {
	c := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenString, c,
		func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return secret, nil
		},
		jwt.WithValidMethods(allowedMethods),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return &TokenClaims{
		UserID:    c.UserID,
		Role:      c.Role,
		TokenUse:  c.TokenUse,
		Issuer:    c.Issuer,
		Subject:   c.Subject,
		Expires:   unix(c.ExpiresAt),
		NotBefore: unix(c.NotBefore),
		IssuedAt:  unix(c.IssuedAt),
		ID:        c.ID,
	}, nil
}

// numericDate converts a Unix timestamp into a jwt.NumericDate, leaving a
// zero value unset so the claim is omitted instead of encoding 1970.
func numericDate(unixSeconds int64) *jwt.NumericDate {
	if unixSeconds == 0 {
		return nil
	}
	return jwt.NewNumericDate(time.Unix(unixSeconds, 0))
}

// unix converts a jwt.NumericDate back to a Unix timestamp, treating a nil
// value as zero.
func unix(date *jwt.NumericDate) int64 {
	if date == nil {
		return 0
	}
	return date.Unix()
}
