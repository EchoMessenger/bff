# Backend for Frontend (BFF) Service

The BFF service is a reverse proxy and aggregation layer that sits between the React web frontend and backend microservices (Tinode, Audit, TaskTracker).

## Purpose

- **Single Entry Point**: Provides unified API gateway for the frontend
- **Authentication**: Validates JWT tokens from Keycloak before forwarding to backends
- **Rate Limiting**: Protects backend services with per-IP request throttling (token bucket algorithm)
- **CORS Handling**: Eliminates cross-origin request issues
- **Structured Logging**: Tracks all requests with timing and user information
- **Request/Response Forwarding**: Proxies all HTTP methods and headers to backend services

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      React Webapp                               │
├─────────────────────────────────────────────────────────────────┤
│                    BFF Service (Go)                             │
│  Port 7000, Path: /bff/v1/*                                     │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ Logging → RateLimit → Auth (JWT) → Proxy → Backend       │  │
│  └──────────────────────────────────────────────────────────┘  │
├─────────────────────────────────────────────────────────────────┤
│  ↓         ↓              ↓                                      │
│ Tinode   Audit      TaskTracker                                 │
│(WS+REST)(REST)      (REST)                                      │
└─────────────────────────────────────────────────────────────────┘
```

## Features

### JWT Validation
- Validates Bearer tokens against Keycloak issuer URI
- Preserves Authorization header for downstream services (defense in depth)
- Returns 401 Unauthorized for invalid/missing tokens

### Rate Limiting
- Token bucket algorithm per IP address
- Configurable requests per minute (default: 100 req/min)
- Returns 429 Too Many Requests when exceeded
- Automatic cleanup of idle buckets

### Structured Logging
- JSON-formatted logs with timestamp, method, path, status code, latency, userId, client IP
- Logs all requests for monitoring and debugging
- Log level configurable (info, warn, error, debug)

### HTTP Proxy
- Forwards all HTTP methods (GET, POST, PUT, DELETE, PATCH, etc.)
- Copies all headers from original request
- Forwards request body for POST/PUT/PATCH
- Preserves response status codes and headers

## API Routes

### Health Checks
- `GET /health` - Liveness probe (always responds with 200 OK)
- `GET /ready` - Readiness probe (checks downstream service connectivity)

### Proxy Routes
- `/bff/v1/audit/*` → Routes to Audit service
- `/bff/v1/tasktracker/*` → Routes to TaskTracker service

**Authentication required** for `/bff/v1/*` routes (Bearer token in Authorization header).

## Development

### Prerequisites
- Go 1.21+
- Make
- Docker (optional, for Docker-based development)

### Local Setup

```bash
cd bff

# Copy environment template
cp .env.example .env.local

# Build the service
make build

# Run locally
make run
```

### Build Commands

```bash
# Build binary to bin/bff
make build

# Run locally (requires .env.local)
make run

# Build Docker image
make docker-build

# Run tests
make test

# Clean build artifacts
make clean
```

## Configuration

See `.env.example` for all available options.

### Required Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `BFF_PORT` | `7000` | HTTP server port |
| `LOG_LEVEL` | `info` | Logging level: debug, info, warn, error |
| `KEYCLOAK_ISSUER_URI` | Required | JWT issuer URI for token validation |
| `AUDIT_SERVICE_URL` | Required | Audit service base URL |
| `TASKTRACKER_SERVICE_URL` | Required | TaskTracker service base URL |
| `AUDIT_SERVICE_HEALTH_PORT` | `8081` | Audit service health-check port |
| `TASKTRACKER_SERVICE_HEALTH_PORT` | `8000` | TaskTracker service health-check port |
| `AUDIT_SERVICE_HEALTH_PATH` | `/health` | Audit service health-check path |
| `TASKTRACKER_SERVICE_HEALTH_PATH` | `/health` | TaskTracker service health-check path |
| `RATE_LIMIT_PER_MINUTE` | `100` | Requests per minute per IP address |

### Example .env.local

```bash
BFF_PORT=7000
LOG_LEVEL=info
KEYCLOAK_ISSUER_URI=http://localhost:8180/realms/echo
AUDIT_SERVICE_URL=http://localhost:8081
TASKTRACKER_SERVICE_URL=http://localhost:8000
AUDIT_SERVICE_HEALTH_PORT=8081
TASKTRACKER_SERVICE_HEALTH_PORT=8000
AUDIT_SERVICE_HEALTH_PATH=/actuator/health/readiness
TASKTRACKER_SERVICE_HEALTH_PATH=/health
RATE_LIMIT_PER_MINUTE=100
```

## Docker Deployment

The service is included in the root `docker-compose.yml`:

```bash
# Start all services including BFF
docker-compose up -d

# View logs
docker-compose logs -f bff

# Stop services
docker-compose down
```

BFF runs on port 7000 inside the container, exposed to port 7000 on the host.

## Project Structure

```
bff/
├── cmd/bff/
│   └── main.go              # Entry point, server setup
├── internal/
│   ├── auth/
│   │   ├── auth.go         # JWT validation logic
│   │   └── middleware.go   # Auth middleware
│   ├── config/
│   │   └── config.go       # Configuration loading
│   ├── log/
│   │   ├── logger.go       # Structured logging
│   │   └── middleware.go   # Logging middleware
│   ├── proxy/
│   │   ├── handler.go      # HTTP proxy handler
│   │   └── routes.go       # Route matching logic
│   └── ratelimit/
│       ├── ratelimit.go    # Token bucket limiter
│       └── middleware.go   # Rate limiting middleware
├── tests/
│   └── integration_test.go # Integration tests
├── Dockerfile              # Multi-stage Docker build
├── Makefile               # Build targets
├── go.mod                 # Go module definition
├── go.sum                 # Dependency lock file
├── .env.example           # Environment template
└── README.md             # This file
```

## Routing Conflicts

The BFF service uses path prefix `/bff/v1/*` which **does not conflict** with existing Tinode routes:

- Tinode uses `/socket` for WebSocket connections (not proxied by BFF)
- Tinode REST API routes (if any) would be on `/` but are not used by the frontend
- BFF is completely separate from Tinode's native communication

## Error Handling

The service returns appropriate HTTP status codes:

| Status | Meaning |
|--------|---------|
| 200 | OK - Request succeeded |
| 401 | Unauthorized - Invalid or missing Bearer token |
| 404 | Not Found - Route not found or upstream service returned 404 |
| 429 | Too Many Requests - Rate limit exceeded |
| 500 | Internal Server Error - BFF error |
| 502 | Bad Gateway - Upstream service error |

## Future Enhancements

- [ ] Circuit breaker pattern for upstream services
- [ ] Request/response caching
- [ ] Metrics export (Prometheus format)
- [ ] Per-user rate limits (instead of per-IP)
- [ ] Request/response transformation (adapting schemas)
- [ ] Request retry logic with exponential backoff
- [ ] Connection pooling optimization

## Debugging

### Enable Debug Logging
```bash
LOG_LEVEL=debug make run
```

### Test Health Endpoints
```bash
# Check liveness
curl http://localhost:7000/health

# Check readiness
curl http://localhost:7000/ready
```

### Test Proxy (with valid JWT token)
```bash
curl -H "Authorization: Bearer <your-jwt-token>" \
  http://localhost:7000/bff/v1/audit/events
```

## Monitoring

Logs are printed in JSON format for easy parsing:

```json
{"timestamp":"2026-04-18T02:15:30Z","level":"info","msg":"request","method":"GET","path":"/bff/v1/audit/events","status_code":200,"latency_ms":45,"userId":"user123","client_ip":"127.0.0.1"}
```

Parse with jq:
```bash
# Show only slow requests (>100ms)
docker-compose logs bff | jq 'select(.latency_ms > 100)'

# Show all errors
docker-compose logs bff | jq 'select(.level == "error")'
```

## License

Apache 2.0
