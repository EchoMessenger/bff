package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the BFF service
type Config struct {
	Port                         int
	KeycloakIssuerURI            string
	LogLevel                     string
	AuditServiceURL              string
	TaskTrackerServiceURL        string
	AuditServiceHealthPort       int
	AuditServiceHealthPath       string
	TaskTrackerServiceHealthPort int
	TaskTrackerServiceHealthPath string
	RateLimitPerMinute           int
	CORSAllowedOrigins           []string
	CORSAllowCredentials         bool
}

// LoadConfig loads configuration from environment variables
// Returns Config with defaults and environment variable overrides
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:                         getEnvInt("BFF_PORT", 7000),
		KeycloakIssuerURI:            getEnv("KEYCLOAK_ISSUER_URI", "http://localhost:8180/realms/echo"),
		LogLevel:                     getEnv("LOG_LEVEL", "info"),
		AuditServiceURL:              getEnv("AUDIT_SERVICE_URL", "http://localhost:8080"),
		TaskTrackerServiceURL:        getEnv("TASKTRACKER_SERVICE_URL", "http://localhost:8000"),
		AuditServiceHealthPort:       getEnvInt("AUDIT_SERVICE_HEALTH_PORT", 8081),
		AuditServiceHealthPath:       getEnv("AUDIT_SERVICE_HEALTH_PATH", "/health"),
		TaskTrackerServiceHealthPort: getEnvInt("TASKTRACKER_SERVICE_HEALTH_PORT", 8000),
		TaskTrackerServiceHealthPath: getEnv("TASKTRACKER_SERVICE_HEALTH_PATH", "/health"),
		RateLimitPerMinute:           getEnvInt("RATE_LIMIT_PER_MINUTE", 100),
		CORSAllowedOrigins:           getEnvCSV("CORS_ALLOWED_ORIGINS"),
		CORSAllowCredentials:         getEnvBool("CORS_ALLOW_CREDENTIALS", false),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}

	if c.KeycloakIssuerURI == "" {
		return fmt.Errorf("KEYCLOAK_ISSUER_URI must be set")
	}

	if c.AuditServiceURL == "" {
		return fmt.Errorf("AUDIT_SERVICE_URL must be set")
	}

	if c.TaskTrackerServiceURL == "" {
		return fmt.Errorf("TASKTRACKER_SERVICE_URL must be set")
	}

	if c.RateLimitPerMinute < 1 {
		return fmt.Errorf("RATE_LIMIT_PER_MINUTE must be greater than 0")
	}

	for _, origin := range c.CORSAllowedOrigins {
		if err := validateCORSOrigin(origin); err != nil {
			return fmt.Errorf("invalid CORS origin %q", origin)
		}
	}

	if err := validateHealthPath("AUDIT_SERVICE_HEALTH_PATH", c.AuditServiceHealthPath); err != nil {
		return err
	}

	if err := validateHealthPath("TASKTRACKER_SERVICE_HEALTH_PATH", c.TaskTrackerServiceHealthPath); err != nil {
		return err
	}

	return nil
}

func validateHealthPath(name, path string) error {
	if path == "" {
		return fmt.Errorf("%s must be set", name)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%s must start with '/'", name)
	}
	return nil
}

func validateCORSOrigin(origin string) error {
	if origin == "" || strings.ContainsAny(origin, " \t\r\n") {
		return fmt.Errorf("invalid origin")
	}

	parsed, err := url.ParseRequestURI(origin)
	if err != nil {
		return err
	}

	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("origin must include scheme and host")
	}

	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("origin must not include credentials, query, or fragment")
	}

	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("origin must not include a path")
	}

	if parsed.Host != parsed.Hostname() && parsed.Port() == "" {
		return fmt.Errorf("invalid host")
	}

	return nil
}

// getEnv returns environment variable or default value
func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

// getEnvInt returns environment variable as int or default value
func getEnvInt(key string, defaultVal int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func getEnvCSV(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}
