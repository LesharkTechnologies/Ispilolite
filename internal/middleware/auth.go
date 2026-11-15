package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims defines the structure of the JWT claims.
type Claims struct {
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	TokenUse string `json:"token_use"`
	jwt.RegisteredClaims
}

var (
	// jwtKey should be loaded from a secure configuration in a real application.
	jwtKey               []byte
	issuer               string
	revocationChecker    func(string) bool
	accountActiveChecker func(string, string) bool
)

func SetRevocationChecker(checker func(string) bool)            { revocationChecker = checker }
func SetAccountActiveChecker(checker func(string, string) bool) { accountActiveChecker = checker }

// InitAuth initializes the authentication middleware with a secret key and issuer.
func InitAuth(secret, iss string) {
	if secret == "" {
		log.Fatal("JWT secret key is not configured.")
	}
	jwtKey = []byte(secret)
	issuer = iss
}

// GenerateJWT creates a new JWT for a given user ID and role.
func GenerateJWT(userID, role string, expiryHours int) (string, error) {
	expirationTime := time.Now().Add(time.Duration(expiryHours) * time.Hour)
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			Issuer:    issuer,
			Subject:   userID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}

// AuthMiddleware creates a middleware that verifies the JWT and ensures the user has the required role.
// If `requiredRole` is empty, it only validates the token without checking the role.
func AuthMiddleware(requiredRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString, err := extractTokenFromHeader(r)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			claims, err := validateToken(tokenString)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}
			if revocationChecker != nil && revocationChecker(tokenString) {
				http.Error(w, "Unauthorized: token revoked", http.StatusUnauthorized)
				return
			}
			if claims.TokenUse != "access" {
				http.Error(w, "Unauthorized: access token required", http.StatusUnauthorized)
				return
			}
			if accountActiveChecker != nil && !accountActiveChecker(claims.UserID, claims.Role) {
				http.Error(w, "Unauthorized: account is not active", http.StatusUnauthorized)
				return
			}

			// If a specific role is required, check for it.
			if requiredRole != "" && claims.Role != requiredRole {
				http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			// Add user information to the request context.
			ctx := context.WithValue(r.Context(), "userID", claims.UserID)
			ctx = context.WithValue(ctx, "userRole", claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuthMiddleware validates an access token when one is supplied, but
// still allows anonymous requests. It is used for public actions such as
// recording an ad impression while attributing signed-in users to aggregate
// audience insights.
func OptionalAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
				next.ServeHTTP(w, r)
				return
			}
			tokenString, err := extractTokenFromHeader(r)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}
			claims, err := validateToken(tokenString)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}
			if revocationChecker != nil && revocationChecker(tokenString) {
				http.Error(w, "Unauthorized: token revoked", http.StatusUnauthorized)
				return
			}
			if claims.TokenUse != "access" {
				http.Error(w, "Unauthorized: access token required", http.StatusUnauthorized)
				return
			}
			if accountActiveChecker != nil && !accountActiveChecker(claims.UserID, claims.Role) {
				http.Error(w, "Unauthorized: account is not active", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), "userID", claims.UserID)
			ctx = context.WithValue(ctx, "userRole", claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractTokenFromHeader extracts the JWT from the Authorization header.
func extractTokenFromHeader(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("missing authorization header")
	}

	splitToken := strings.Split(authHeader, "Bearer ")
	if len(splitToken) != 2 {
		return "", errors.New("invalid token format")
	}

	return splitToken[1], nil
}

// validateToken parses and validates the JWT string.
func validateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())

	if err != nil {
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) {
			return nil, errors.New("invalid token signature")
		}
		return nil, errors.New("invalid token")
	}

	if !token.Valid {
		return nil, errors.New("token is not valid")
	}

	return claims, nil
}
