package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

// AuthMiddleware creates an HTTP middleware that validates JWT tokens
func AuthMiddleware(validator *JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "missing authorization header",
				})
				return
			}

			token, err := validator.ValidateToken(authHeader)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": err.Error(),
				})
				return
			}

			claims, ok := token.Claims.(*CustomClaims)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "unauthorized",
					"message": "invalid claims format",
				})
				return
			}

			// Attach userId to request context
			ctx := context.WithValue(r.Context(), "userId", claims.Sub)
			r = r.WithContext(ctx)

			// Note: Authorization header is preserved for downstream services
			next.ServeHTTP(w, r)
		})
	}
}
