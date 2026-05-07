package cors

import "net/http"

const (
	allowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	allowedHeaders = "Authorization, Content-Type, Accept"
)

// Middleware applies CORS headers and terminates preflight requests.
func Middleware(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if _, ok := allowed[origin]; ok {
				setHeaders(w, origin, allowCredentials)
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func setHeaders(w http.ResponseWriter, origin string, allowCredentials bool) {
	headers := w.Header()
	headers.Set("Access-Control-Allow-Origin", origin)
	headers.Set("Access-Control-Allow-Methods", allowedMethods)
	headers.Set("Access-Control-Allow-Headers", allowedHeaders)
	headers.Set("Vary", "Origin")
	if allowCredentials {
		headers.Set("Access-Control-Allow-Credentials", "true")
	}
}
