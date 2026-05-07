package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/echomessenger/bff/internal/auth"
	"github.com/echomessenger/bff/internal/cors"
	"github.com/echomessenger/bff/internal/log"
	"github.com/echomessenger/bff/internal/proxy"
	"github.com/echomessenger/bff/internal/ratelimit"
	"github.com/golang-jwt/jwt/v5"
)

// TestProxyRouting tests that requests are routed to correct services
func TestProxyRouting(t *testing.T) {
	// Create mock backend services
	auditServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "audit", "path": r.URL.Path})
	}))
	defer auditServer.Close()

	tasktrackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "tasktracker", "path": r.URL.Path})
	}))
	defer tasktrackerServer.Close()

	// Setup BFF
	logger := log.New("info")
	router := proxy.NewRouter(auditServer.URL, tasktrackerServer.URL)
	proxyHandler := proxy.NewHandler(router, logger)

	tests := []struct {
		name         string
		path         string
		expected     string
		expectedPath string
	}{
		{
			name:         "Route to audit service",
			path:         "/bff/v1/audit/events",
			expected:     "audit",
			expectedPath: "/api/v1/audit/events",
		},
		{
			name:         "Route to audit analytics",
			path:         "/bff/v1/analytics/summary",
			expected:     "audit",
			expectedPath: "/api/v1/analytics/summary",
		},
		{
			name:         "Route to audit incidents",
			path:         "/bff/v1/incidents",
			expected:     "audit",
			expectedPath: "/api/v1/incidents",
		},
		{
			name:         "Route to tasktracker service",
			path:         "/bff/v1/tasktracker/tasks",
			expected:     "tasktracker",
			expectedPath: "/tasks",
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

			if result["path"] != tt.expectedPath {
				t.Errorf("Expected upstream path %s, got %s", tt.expectedPath, result["path"])
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
	router := proxy.NewRouter(mockServer.URL, "")
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
	router := proxy.NewRouter(mockServer.URL, "")
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
	router := proxy.NewRouter(mockServer.URL, "")
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

func TestCORSPreflightBypassesAuth(t *testing.T) {
	logger := log.New("info")
	upstreamCalls := 0

	tasktrackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer tasktrackerServer.Close()

	handler := buildBFFHandler(t, logger, tasktrackerServer.URL, []string{"http://192.168.56.1:8080"}, false)

	req := httptest.NewRequest(http.MethodOptions, "/bff/v1/tasktracker/v1/tasks/", nil)
	req.Header.Set("Origin", "http://192.168.56.1:8080")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Accept")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", w.Code)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.56.1:8080" {
		t.Fatalf("Expected allow origin header, got %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Fatalf("Unexpected allow methods header: %q", got)
	}

	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, Accept" {
		t.Fatalf("Unexpected allow headers header: %q", got)
	}

	if upstreamCalls != 0 {
		t.Fatalf("Expected preflight not to reach upstream, got %d calls", upstreamCalls)
	}
}

func TestCORSAllowsAuthenticatedGET(t *testing.T) {
	logger := log.New("info")
	receivedAuth := ""

	tasktrackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"service": "tasktracker"})
	}))
	defer tasktrackerServer.Close()

	handler := buildBFFHandler(t, logger, tasktrackerServer.URL, []string{"http://192.168.56.1:8080"}, false)
	token := newSignedBearerToken(t)

	req := httptest.NewRequest(http.MethodGet, "/bff/v1/tasktracker/v1/tasks/", nil)
	req.Header.Set("Origin", "http://192.168.56.1:8080")
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://192.168.56.1:8080" {
		t.Fatalf("Expected allow origin header, got %q", got)
	}

	if receivedAuth != token {
		t.Fatalf("Expected Authorization header to be forwarded, got %q", receivedAuth)
	}
}

func TestCORSDisallowedOriginDoesNotGetAllowOriginHeader(t *testing.T) {
	logger := log.New("info")
	handler := buildBFFHandler(t, logger, "", []string{"http://192.168.56.1:8080"}, false)

	req := httptest.NewRequest(http.MethodOptions, "/bff/v1/tasktracker/v1/tasks/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", w.Code)
	}

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Expected no allow origin header, got %q", got)
	}
}

func TestCORSAllowCredentials(t *testing.T) {
	logger := log.New("info")
	handler := buildBFFHandler(t, logger, "", []string{"http://192.168.56.1:8080"}, true)

	req := httptest.NewRequest(http.MethodOptions, "/bff/v1/tasktracker/v1/tasks/", nil)
	req.Header.Set("Origin", "http://192.168.56.1:8080")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Expected allow credentials header, got %q", got)
	}
}

func buildBFFHandler(t *testing.T, logger *log.Logger, tasktrackerURL string, allowedOrigins []string, allowCredentials bool) http.Handler {
	t.Helper()

	validator := newTestJWTValidator(t)
	limiter := ratelimit.NewTokenBucketLimiter(100)
	router := proxy.NewRouter("", tasktrackerURL)
	proxyHandler := proxy.NewHandler(router, logger)

	handler := http.Handler(proxyHandler)
	handler = auth.AuthMiddleware(validator)(handler)
	handler = ratelimit.RateLimitMiddleware(limiter)(handler)
	handler = log.LoggingMiddleware(logger)(handler)
	handler = cors.Middleware(allowedOrigins, allowCredentials)(handler)

	return handler
}

func newTestJWTValidator(t *testing.T) *auth.JWTValidator {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	var issuerURL string
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/protocol/openid-connect/certs" {
			http.NotFound(w, r)
			return
		}

		eBytes := big.NewInt(int64(key.PublicKey.E)).Bytes()
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(eBytes)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{
				{
					"kty": "RSA",
					"use": "sig",
					"kid": "test-key",
					"alg": "RS256",
					"n":   n,
					"e":   e,
				},
			},
		})
	}))
	t.Cleanup(jwksServer.Close)
	issuerURL = jwksServer.URL

	validator, err := auth.NewJWTValidator(issuerURL)
	if err != nil {
		t.Fatalf("Failed to initialize JWT validator: %v", err)
	}

	testSigningKey = key
	testIssuerURL = issuerURL

	return validator
}

var (
	testSigningKey *rsa.PrivateKey
	testIssuerURL  string
)

func newSignedBearerToken(t *testing.T) string {
	t.Helper()

	if testSigningKey == nil || testIssuerURL == "" {
		t.Fatal("test JWT signer is not initialized")
	}

	claims := &auth.CustomClaims{
		Sub: "test-user",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuerURL,
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"

	signedToken, err := token.SignedString(testSigningKey)
	if err != nil {
		t.Fatalf("Failed to sign JWT: %v", err)
	}

	return "Bearer " + signedToken
}
