package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/echomessenger/bff/internal/auth"
	"github.com/echomessenger/bff/internal/log"
	"github.com/echomessenger/bff/internal/proxy"
	"github.com/echomessenger/bff/internal/ratelimit"
)

// TestProxyRouting tests that requests are routed to correct services
func TestProxyRouting(t *testing.T) {
	// Create mock backend services
	auditServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "audit"})
	}))
	defer auditServer.Close()

	tasktrackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "tasktracker"})
	}))
	defer tasktrackerServer.Close()

	authauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "restauth"})
	}))
	defer authauthServer.Close()

	// Setup BFF
	logger := log.New("info")
	router := proxy.NewRouter(auditServer.URL, tasktrackerServer.URL, authauthServer.URL)
	proxyHandler := proxy.NewHandler(router, logger)

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "Route to audit service",
			path:     "/bff/v1/audit/events",
			expected: "audit",
		},
		{
			name:     "Route to tasktracker service",
			path:     "/bff/v1/tasktracker/tasks",
			expected: "tasktracker",
		},
		{
			name:     "Route to restauth service",
			path:     "/bff/v1/auth/login",
			expected: "restauth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			proxyHandler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			var result map[string]string
			json.NewDecoder(w.Body).Decode(&result)

			if result["service"] != tt.expected {
				t.Errorf("Expected service %s, got %s", tt.expected, result["service"])
			}
		})
	}
}

// TestHTTPMethods tests that all HTTP methods are forwarded
func TestHTTPMethods(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"method": r.Method})
	}))
	defer mockServer.Close()

	logger := log.New("info")
	router := proxy.NewRouter(mockServer.URL, "", "")
	proxyHandler := proxy.NewHandler(router, logger)

	for _, method := range methods {
		t.Run(fmt.Sprintf("Test %s method", method), func(t *testing.T) {
			body := []byte(`{"test":"data"}`)
			req := httptest.NewRequest(method, "/bff/v1/audit/test", bytes.NewReader(body))
			w := httptest.NewRecorder()

			proxyHandler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			var result map[string]string
			json.NewDecoder(w.Body).Decode(&result)

			if result["method"] != method {
				t.Errorf("Expected method %s, got %s", method, result["method"])
			}
		})
	}
}

// TestRateLimiting tests rate limiting functionality
func TestRateLimiting(t *testing.T) {
	limiter := ratelimit.NewTokenBucketLimiter(5) // 5 requests per minute
	clientIP := "192.168.1.100"

	// Should allow first 5 requests
	for i := 0; i < 5; i++ {
		if !limiter.Check(clientIP) {
			t.Errorf("Expected request %d to be allowed", i+1)
		}
	}

	// Should reject 6th request
	if limiter.Check(clientIP) {
		t.Error("Expected 6th request to be rate limited")
	}

	// After 12+ seconds, all tokens should be refilled (5 tokens per 60 seconds)
	time.Sleep(12100 * time.Millisecond)
	if !limiter.Check(clientIP) {
		t.Error("Expected request after refill to be allowed")
	}
}

// TestRateLimitMiddleware tests rate limit middleware
func TestRateLimitMiddleware(t *testing.T) {
	limiter := ratelimit.NewTokenBucketLimiter(2)

	handler := ratelimit.RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// First two requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, w.Code)
		}
	}

	// Third request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}
}

// TestJWTValidation tests JWT token validation
func TestJWTValidation(t *testing.T) {
	validator, err := auth.NewJWTValidator("http://localhost:8180/realms/echo")
	if err != nil {
		// Keycloak not available - skip this test in local development
		t.Skip("Keycloak not available for JWT validation test")
	}

	tests := []struct {
		name        string
		token       string
		shouldError bool
	}{
		{
			name:        "Empty token",
			token:       "",
			shouldError: true,
		},
		{
			name:        "Invalid token",
			token:       "invalid.token.here",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.ValidateToken(tt.token)

			if tt.shouldError && err == nil {
				t.Error("Expected error, got nil")
			}

			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

// TestAuthMiddleware tests authentication middleware
func TestAuthMiddleware(t *testing.T) {
	validator, err := auth.NewJWTValidator("http://localhost:8180/realms/echo")
	if err != nil {
		// Keycloak not available - use a mock validator instead
		validator = &auth.JWTValidator{
			KeycloakIssuerURI: "http://localhost:8180/realms/echo",
		}
	}

	handler := auth.AuthMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	t.Run("Missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("Invalid bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid.token")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

// TestLoggingMiddleware tests request logging
func TestLoggingMiddleware(t *testing.T) {
	logger := log.New("info")

	handler := log.LoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestHeaderForwarding tests that headers are forwarded
func TestHeaderForwarding(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back custom header
		w.Header().Set("X-Custom-Response", r.Header.Get("X-Custom-Request"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"received_header": r.Header.Get("X-Custom-Request"),
		})
	}))
	defer mockServer.Close()

	logger := log.New("info")
	router := proxy.NewRouter(mockServer.URL, "", "")
	proxyHandler := proxy.NewHandler(router, logger)

	req := httptest.NewRequest("GET", "/bff/v1/audit/test", nil)
	req.Header.Set("X-Custom-Request", "test-value")
	w := httptest.NewRecorder()

	proxyHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if w.Header().Get("X-Custom-Response") != "test-value" {
		t.Error("Expected custom header to be forwarded")
	}
}

// TestRequestBody tests that request body is forwarded
func TestRequestBody(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer mockServer.Close()

	logger := log.New("info")
	router := proxy.NewRouter(mockServer.URL, "", "")
	proxyHandler := proxy.NewHandler(router, logger)

	testBody := []byte(`{"message":"test data"}`)
	req := httptest.NewRequest("POST", "/bff/v1/audit/test", bytes.NewReader(testBody))
	w := httptest.NewRecorder()

	proxyHandler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("test data")) {
		t.Error("Expected request body to be forwarded")
	}
}
