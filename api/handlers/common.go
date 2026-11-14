package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	dto "ispilolite/api/dto"
)

const maxJSONBodyBytes = 1 << 20

// pathParam extracts the first path segment that follows prefix. It returns an
// empty string when path does not start with prefix or when no segment remains.
// Callers relying on additional trailing segments trim them explicitly.
func pathParam(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}

	remainder := strings.TrimPrefix(path, prefix)
	remainder = strings.Trim(remainder, "/")
	if remainder == "" {
		return ""
	}

	if idx := strings.IndexRune(remainder, '/'); idx >= 0 {
		return remainder[:idx]
	}

	return remainder
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain one JSON object")
	}
	return nil
}

// userIDFromContext returns the authenticated user ID set by AuthMiddleware,
// or an empty string if the request is unauthenticated.
func userIDFromContext(ctx context.Context) string {
	if value := ctx.Value("userID"); value != nil {
		if id, ok := value.(string); ok {
			return id
		}
	}

	return ""
}

func userRoleFromContext(ctx context.Context) string {
	if value, ok := ctx.Value("userRole").(string); ok {
		return value
	}
	return ""
}

func respondWithJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if payload == nil {
		return
	}

	_ = json.NewEncoder(w).Encode(payload)
}

func respondWithError(w http.ResponseWriter, status int, message string) {
	// Never leak internal implementation details in 500 responses.
	if status >= 500 {
		message = safeMessage(nil) // generic message; real error logged by caller
	}
	respondWithJSON(w, status, dto.APIResponse{
		Success: false,
		Message: message,
	})
}

// respondWithErrorRaw sends the message as-is to the client. Use only for
// validation/business errors where the message is safe to expose.
func respondWithErrorRaw(w http.ResponseWriter, status int, message string) {
	respondWithJSON(w, status, dto.APIResponse{
		Success: false,
		Message: message,
	})
}

// respondWithInternalError returns a generic message to the client while
// logging the real error server-side. Never pass raw err.Error() to clients
// in production — it can leak SQL structure, file paths, or internal IDs.
func respondWithInternalError(w http.ResponseWriter, err error) {
	if err != nil {
		log.Printf("internal error: %v", err)
	}
	respondWithError(w, http.StatusInternalServerError, "internal server error")
}

// safeMessage returns a client-safe error message. In production it hides
// implementation details; in development it shows the real message.
func safeMessage(err error) string {
	if err == nil {
		return ""
	}
	if os.Getenv("ENVIRONMENT") == "production" {
		return "internal server error"
	}
	return err.Error()
}
