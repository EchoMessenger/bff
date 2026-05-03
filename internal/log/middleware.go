package log

import (
	"net"
	"net/http"
	"strings"
	"time"
)

// ResponseWriter wraps http.ResponseWriter to capture status code and bytes written
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytesWritten int
}

// WriteHeader captures the status code
func (rw *ResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures bytes written
func (rw *ResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// LoggingMiddleware logs all incoming requests with structured format
func LoggingMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			// Wrap response writer to capture status code
			wrapped := &ResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Calculate latency
			latency := time.Since(startTime).Milliseconds()

			// Extract user ID from context
			userId := "-"
			if ctx := r.Context().Value("userId"); ctx != nil {
				if str, ok := ctx.(string); ok {
					userId = str
				}
			}

			// Extract client IP
			clientIP := extractClientIP(r)

			// Log the request
			logger.Info("request", map[string]interface{}{
				"method":      r.Method,
				"path":        r.RequestURI,
				"status_code": wrapped.statusCode,
				"latency_ms":  latency,
				"userId":      userId,
				"client_ip":   clientIP,
			})
		})
	}
}

// extractClientIP gets the client IP from X-Forwarded-For or RemoteAddr
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	forwardedFor := r.Header.Get("X-Forwarded-For")
	if forwardedFor != "" {
		// Take the first IP if multiple are present
		ips := strings.Split(forwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
