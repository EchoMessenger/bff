# BFF Service - JWT Validation & Request Logging Implementation

## Overview
This implementation adds production-ready JWT authentication middleware, request logging, and rate limiting to the BFF service.

## Implemented Components

### 1. JWT Validation (`internal/auth/jwt.go`)
- **JWTValidator struct**: Handles token validation with in-memory JWKS caching
  - Fetches and caches public keys from Keycloak JWKS endpoint
  - Automatic cache refresh every 24 hours (configurable via `cacheTTL`)
  - Thread-safe JWKS key access with RWMutex
  
- **Key Features**:
  - Converts JWKS RSA keys to `crypto/rsa.PublicKey` for validation
  - Validates token signature against Keycloak issuer's RSA keys
  - Parses standard JWT claims: `sub` (userId), `email`, `preferred_username`
  - Validates token issuer claim matches configured Keycloak issuer URI
  - Handles token refresh by kid (key ID) lookup
  - Supports both "Bearer TOKEN" and raw TOKEN formats

- **CustomClaims struct**: Extends JWT RegisteredClaims with Keycloak-specific fields

### 2. Auth Middleware (`internal/auth/middleware.go`)
- **AuthMiddleware function**: HTTP middleware wrapper for JWT validation
  - Extracts and validates Authorization header
  - Returns 401 Unauthorized for missing/invalid tokens
  - Attaches `userId` to request context from token claims
  - **Preserves Authorization header** for downstream service use
  - Error responses in standardized JSON format

### 3. Logging Middleware (`internal/log/middleware.go`)
- **LoggingMiddleware function**: Structured request/response logging
  - Captures request start time and calculates latency in milliseconds
  - Wraps ResponseWriter to capture status code and bytes written
  - Logs structured JSON with fields:
    - `timestamp`: RFC3339 format
    - `method`: HTTP method (GET, POST, etc)
    - `path`: Request URI
    - `status_code`: HTTP response status
    - `latency_ms`: Request processing time in milliseconds
    - `user_id`: Extracted from context (or "-" if missing)
    - `client_ip`: From X-Forwarded-For header or RemoteAddr
  - Single JSON line per request via `logger.Info()`

### 4. Rate Limiting
- **TokenBucketLimiter** (`internal/ratelimit/ratelimit.go`):
  - Per-client-IP token bucket rate limiting
  - Configurable requests per minute (from config)
  - Automatic token refill based on elapsed time
  - Thread-safe with RWMutex
  - Cleanup goroutine removes idle buckets (>30 min)

- **RateLimitMiddleware** (`internal/ratelimit/middleware.go`):
  - Returns 429 Too Many Requests when rate limit exceeded
  - Sets `Retry-After: 60` header on rate limit responses
  - Extracts client IP from X-Forwarded-For or RemoteAddr

### 5. Main Integration (`cmd/bff/main.go`)
- **Middleware Chain Order** (important for security):
  1. LoggingMiddleware - logs all requests
  2. RateLimitMiddleware - enforces per-IP rate limits
  3. AuthMiddleware - validates JWT tokens
  4. ProxyHandler - forwards to backend services

- **Exception Routes** (no auth/rate limiting):
  - `/health` - liveness check
  - `/ready` - readiness check with downstream health verification

- **JWT Validator Initialization**:
  - Creates validator with Keycloak issuer URI from config
  - Fails fast if initial JWKS fetch fails
  - Logs error if JWT initialization fails

- **Rate Limiter Cleanup**:
  - Background goroutine runs cleanup every 5 minutes
  - Removes idle bucket entries to prevent memory leaks

## Error Handling

### Auth Errors (401 Unauthorized)
```json
{
  "error": "unauthorized",
  "message": "missing authorization header|invalid token|invalid claims format|..."
}
```

### Rate Limit Errors (429 Too Many Requests)
```json
{
  "error": "rate_limited",
  "retry_after": 60
}
```

Response also includes `Retry-After: 60` header per HTTP standards.

## Configuration

All configuration via environment variables (see `internal/config/config.go`):
- `KEYCLOAK_ISSUER_URI`: Keycloak realm issuer URI (required)
- `RATE_LIMIT_PER_MINUTE`: Requests per minute per client IP (default: 100)
- `LOG_LEVEL`: Log verbosity (default: info)

## Testing

### Build
```bash
go build ./cmd/bff/
```

### Linting
```bash
go vet ./...
```

### Local Testing
1. Start local Keycloak instance
2. Set `KEYCLOAK_ISSUER_URI` environment variable
3. Run `go run ./cmd/bff/main.go`
4. Test with curl:
   ```bash
   # Without token → 401
   curl http://localhost:7000/bff/v1/audit/events
   
   # With valid token → proxied to audit service
   curl -H "Authorization: Bearer <valid-token>" http://localhost:7000/bff/v1/audit/events
   
   # Rate limit test → 429 after 100 requests/min from same IP
   for i in {1..101}; do curl -H "Authorization: Bearer <token>" http://localhost:7000/bff/v1/audit/events; done
   ```

## Security Considerations

1. **JWKS Caching**: Keys cached for 24 hours; updates via cache refresh on kid mismatch
2. **RSA Key Validation**: All tokens must be signed with RSA keys from Keycloak
3. **Issuer Validation**: Token issuer claim must match configured Keycloak issuer URI
4. **Bearer Token Extraction**: Supports both "Bearer TOKEN" and "TOKEN" formats
5. **Header Preservation**: Authorization header passed to downstream services (important for audit)
6. **Rate Limiting**: Per-IP isolation prevents single client from starving other users

## Known Limitations

1. **JWKS Caching**: If Keycloak keys are rotated, old cached keys remain valid until TTL expires or kid mismatch triggers refresh
2. **Distributed Rate Limiting**: Rate limit buckets are in-memory; not shared across multiple BFF instances. Use external rate limiter (Redis, etc) for multi-instance deployments
3. **JWT Parsing**: Assumes RSA signatures only (most secure); HMAC and other algorithms rejected

## Future Enhancements

1. Add distributed rate limiting via Redis
2. Add JWT token caching to reduce parsing overhead
3. Add Prometheus metrics for middleware latency
4. Add configurable rate limit per endpoint
5. Add request/response body logging for debugging (disabled by default)
