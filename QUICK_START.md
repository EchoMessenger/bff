# BFF Service Quick Start Guide

## Installation & Build

```bash
cd /Users/nikitadyuckov/Documents/GitHub/EchoMessenger/bff

# Install dependencies
go mod tidy

# Build service
go build -o bin/bff ./cmd/bff/

# Verify
go vet ./...
```

## Configuration

Set environment variables:

```bash
export KEYCLOAK_ISSUER_URI="http://localhost:8180/realms/echo"
export RATE_LIMIT_PER_MINUTE=100
export BFF_PORT=7000
export AUDIT_SERVICE_URL="http://localhost:8080"
export TASKTRACKER_SERVICE_URL="http://localhost:8000"
export RESTAUTH_SERVICE_URL="http://localhost:8000"
export LOG_LEVEL=info
```

## Running

```bash
# Start service
./bin/bff

# Or run directly
go run ./cmd/bff/main.go
```

Expected output:
```json
{"timestamp":"2024-04-18T...","level":"info","msg":"BFF service starting","port":7000}
{"timestamp":"2024-04-18T...","level":"info","msg":"listening","address":":7000"}
```

## API Endpoints

### Health Check (no auth required)
```bash
curl http://localhost:7000/health
# Output: {"status":"ok"}
```

### Readiness Check (no auth required)
```bash
curl http://localhost:7000/ready
# Output: {"status":"ready"} or {"status":"not ready"}
```

### Authenticated Request
```bash
# Get a valid JWT token from Keycloak first
TOKEN=$(get-keycloak-token)

# Make request with token
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:7000/bff/v1/audit/events

# Without token → 401 Unauthorized
curl http://localhost:7000/bff/v1/audit/events
# Output: {"error":"unauthorized","message":"missing authorization header"}
```

### Rate Limiting Test
```bash
TOKEN=$(get-keycloak-token)

# Make 101 requests (limit is 100 per minute)
for i in {1..101}; do
  curl -H "Authorization: Bearer $TOKEN" \
    http://localhost:7000/bff/v1/audit/events
done

# Request 101 will get: 429 Too Many Requests
# Output: {"error":"rate_limited","retry_after":60}
# Header: Retry-After: 60
```

## Monitoring

### Check Logs
```bash
# Watch logs in real-time
go run ./cmd/bff/main.go 2>&1 | jq .

# Example log entry:
# {
#   "timestamp":"2024-04-18T02:31:45Z",
#   "level":"info",
#   "msg":"request",
#   "method":"GET",
#   "path":"/bff/v1/audit/events",
#   "status_code":200,
#   "latency_ms":145,
#   "user_id":"550e8400-e29b-41d4-a716-446655440000",
#   "client_ip":"127.0.0.1"
# }
```

### Check Service Health
```bash
# Liveness probe
curl -I http://localhost:7000/health

# Readiness probe
curl -I http://localhost:7000/ready
```

## Troubleshooting

### JWT Validation Failures
```
Error: "invalid issuer: expected X got Y"
→ Check KEYCLOAK_ISSUER_URI matches Keycloak realm URL
→ Verify trailing slashes are handled correctly
```

### JWKS Fetch Failures
```
Warning: "failed to fetch JWKS"
→ Check Keycloak service is running
→ Verify network connectivity: curl $KEYCLOAK_ISSUER_URI/protocol/openid-connect/certs
→ Check firewall rules
```

### Rate Limiting Issues
```
Getting 429 Too Many Requests unexpectedly
→ Check client IP: curl -H "X-Forwarded-For: x.x.x.x" ...
→ For proxy setups, ensure X-Forwarded-For is set correctly
→ Check RATE_LIMIT_PER_MINUTE setting
```

### No Logs Appearing
```
Check LOG_LEVEL setting
→ Default is "info", set to "debug" for more verbose output
→ Ensure stdout is not being redirected or buffered
```

## Architecture

```
Request Flow:
  1. Client → BFF (port 7000)
  2. LoggingMiddleware - logs request
  3. RateLimitMiddleware - checks per-IP rate limit
  4. AuthMiddleware - validates JWT token
  5. ProxyHandler - forwards to backend service
  6. Backend Service (audit, tasktracker, restauth)
  7. Response logged by LoggingMiddleware
  8. Response → Client
```

## Key Files

- `cmd/bff/main.go` - Main entry point with middleware setup
- `internal/auth/jwt.go` - JWT validation with JWKS caching
- `internal/auth/middleware.go` - Auth middleware
- `internal/log/middleware.go` - Request logging
- `internal/ratelimit/ratelimit.go` - Token bucket rate limiter
- `internal/ratelimit/middleware.go` - Rate limit middleware

## Dependencies

- `github.com/golang-jwt/jwt/v5` - JWT token parsing and validation

## Performance Tips

1. **Keycloak**: Keep Keycloak service close (low latency) for JWKS caching to be effective
2. **Rate Limiting**: Adjust `RATE_LIMIT_PER_MINUTE` based on expected traffic
3. **Multi-Instance**: For distributed deployments, use Redis for shared rate limiting
4. **Logging**: Set `LOG_LEVEL=warn` in production to reduce I/O overhead

## Security Notes

- Always use HTTPS in production (use reverse proxy)
- Verify `KEYCLOAK_ISSUER_URI` matches your Keycloak realm exactly
- Consider IP whitelisting for `/health` and `/ready` endpoints
- Monitor rate limit violations for potential attacks
- Rotate Keycloak keys periodically (automatic via JWKS refresh)
