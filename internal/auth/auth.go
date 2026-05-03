package auth

import (
	"net/http"
	"strings"
)

// GetUserIDFromRequest extracts the user ID from request context
func GetUserIDFromRequest(r *http.Request) string {
	userId := r.Context().Value("userId")
	if userId != nil {
		if str, ok := userId.(string); ok {
			return str
		}
	}
	return "-"
}

// GetBearerToken extracts Bearer token from Authorization header
func GetBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}

	return parts[1]
}
