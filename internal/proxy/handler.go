package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/echomessenger/bff/internal/log"
)

// Handler handles HTTP proxy requests
type Handler struct {
	router *Router
	logger *log.Logger
}

// NewHandler creates a new proxy handler
func NewHandler(router *Router, logger *log.Logger) *Handler {
	return &Handler{
		router: router,
		logger: logger,
	}
}

// ServeHTTP implements http.Handler interface
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Handle(w, r)
}

// Handle handles the actual proxy request
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	// Find target service
	targetURL, upstreamPath := h.router.FindRoute(r.URL.Path)
	if targetURL == "" {
		h.logger.Warn("route not found", map[string]interface{}{
			"path": r.URL.Path,
		})
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":"not found"}`)
		return
	}

	// Build upstream URL
	upstreamURL, err := BuildUpstreamURL(targetURL, upstreamPath)
	if err != nil {
		h.logger.Error("failed to build upstream URL", map[string]interface{}{
			"error": err.Error(),
			"path":  r.URL.Path,
		})
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"internal server error"}`)
		return
	}

	// Add query parameters
	if r.URL.RawQuery != "" {
		upstreamURL = upstreamURL + "?" + r.URL.RawQuery
	}

	// Create upstream request
	req, err := http.NewRequest(r.Method, upstreamURL, r.Body)
	if err != nil {
		h.logger.Error("failed to create upstream request", map[string]interface{}{
			"error": err.Error(),
			"path":  r.URL.Path,
		})
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"internal server error"}`)
		return
	}

	// Copy headers from original request
	copyRequestHeaders(req.Header, r.Header)

	// Forward the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		h.logger.Error("upstream request failed", map[string]interface{}{
			"error": err.Error(),
			"path":  r.URL.Path,
			"url":   upstreamURL,
		})
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"bad gateway"}`)
		return
	}
	defer resp.Body.Close()

	// BFF owns browser-facing CORS for proxied routes, so upstream CORS headers
	// must not leak through and combine into invalid multi-value responses.
	copyResponseHeaders(w.Header(), resp.Header)

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.Error("failed to copy response body", map[string]interface{}{
			"error": err.Error(),
		})
	}
}

// copyRequestHeaders copies headers from source to destination.
// Skips hop-by-hop headers.
func copyRequestHeaders(dst, src http.Header) {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for key, values := range src {
		if isHopByHopHeader(key, hopByHopHeaders) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// copyResponseHeaders copies headers from source to destination.
// Skips hop-by-hop headers and upstream CORS headers because BFF is the single
// browser-facing CORS terminator for proxied routes.
func copyResponseHeaders(dst, src http.Header) {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}
	corsHeaders := map[string]struct{}{
		"access-control-allow-origin":      {},
		"access-control-allow-methods":     {},
		"access-control-allow-headers":     {},
		"access-control-allow-credentials": {},
		"access-control-expose-headers":    {},
		"access-control-max-age":           {},
	}

	for key, values := range src {
		if isHopByHopHeader(key, hopByHopHeaders) {
			continue
		}
		if _, skip := corsHeaders[strings.ToLower(key)]; skip {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// isHopByHopHeader checks if a header is hop-by-hop
func isHopByHopHeader(key string, hopByHop []string) bool {
	key = strings.ToLower(key)
	for _, hop := range hopByHop {
		if strings.ToLower(hop) == key {
			return true
		}
	}
	return false
}
