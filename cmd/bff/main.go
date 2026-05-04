package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/echomessenger/bff/internal/auth"
	"github.com/echomessenger/bff/internal/config"
	"github.com/echomessenger/bff/internal/log"
	"github.com/echomessenger/bff/internal/proxy"
	"github.com/echomessenger/bff/internal/ratelimit"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger := log.New(cfg.LogLevel)

	logger.Info("BFF service starting", map[string]interface{}{
		"port": cfg.Port,
	})

	// Initialize JWT validator
	validator, err := auth.NewJWTValidator(cfg.KeycloakIssuerURI)
	if err != nil {
		logger.Error("failed to initialize JWT validator", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	// Initialize rate limiter
	limiter := ratelimit.NewTokenBucketLimiter(cfg.RateLimitPerMinute)

	// Start cleanup goroutine for rate limiter (runs every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			limiter.Cleanup()
		}
	}()

	// Create router
	router := proxy.NewRouter(
		cfg.AuditServiceURL,
		cfg.TaskTrackerServiceURL,
	)

	// Create proxy handler
	proxyHandler := proxy.NewHandler(router, logger)

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Health check endpoint (no auth, no rate limiting)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Readiness check endpoint (no auth, no rate limiting)
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Check downstream service connectivity
		if checkDownstreamHealth(cfg, logger) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
	})

	// BFF proxy routes with middleware chain
	// Order matters: Logging → RateLimit → Auth → Proxy
	bffHandler := http.Handler(proxyHandler)
	bffHandler = auth.AuthMiddleware(validator)(bffHandler)
	bffHandler = ratelimit.RateLimitMiddleware(limiter)(bffHandler)
	bffHandler = log.LoggingMiddleware(logger)(bffHandler)

	mux.Handle("/bff/v1/", bffHandler)

	// 404 handler for unknown paths
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logger.Warn("not found", map[string]interface{}{
			"path": r.URL.Path,
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	})

	// Create server
	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Channel for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		logger.Info("listening", map[string]interface{}{
			"address": addr,
		})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-sigChan

	logger.Info("shutdown signal received", map[string]interface{}{})

	// Graceful shutdown with 30-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}

	logger.Info("server stopped", map[string]interface{}{})
}

// checkDownstreamHealth checks if downstream services are reachable
func checkDownstreamHealth(cfg *config.Config, logger *log.Logger) bool {
	services := map[string]struct {
		baseURL    string
		healthPort int
		healthPath string
	}{
		"audit": {
			baseURL:    cfg.AuditServiceURL,
			healthPort: cfg.AuditServiceHealthPort,
			healthPath: cfg.AuditServiceHealthPath,
		},
		"tasktracker": {
			baseURL:    cfg.TaskTrackerServiceURL,
			healthPort: cfg.TaskTrackerServiceHealthPort,
			healthPath: cfg.TaskTrackerServiceHealthPath,
		},
	}

	for name, svc := range services {
		healthURL := buildHealthURL(svc.baseURL, svc.healthPort, svc.healthPath)
		if !checkServiceHealth(healthURL, 2*time.Second) {
			logger.Warn("downstream service unavailable", map[string]interface{}{
				"service":    name,
				"url":        svc.baseURL,
				"healthURL":  healthURL,
				"healthPath": svc.healthPath,
				"healthPort": svc.healthPort,
			})
			return false
		}
	}

	return true
}

func buildHealthURL(baseURL string, healthPort int, healthPath string) string {
	return replacePort(baseURL, healthPort) + healthPath
}

// checkServiceHealth checks if a service is reachable by health URL.
func checkServiceHealth(healthURL string, timeout time.Duration) bool {
	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// replacePort replaces the port in a URL with the specified port
func replacePort(urlStr string, port int) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}

	// Get the host without port
	host := u.Hostname()
	if host == "" {
		host = u.Host
	}

	// Reconstruct URL with new port
	u.Host = fmt.Sprintf("%s:%d", host, port)
	return u.Scheme + "://" + u.Host
}
