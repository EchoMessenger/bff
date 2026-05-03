package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the BFF service
type Config struct {
	Port                  int
	KeycloakIssuerURI     string
	LogLevel              string
	AuditServiceURL       string
	TaskTrackerServiceURL string
	RateLimitPerMinute    int
}

// LoadConfig loads configuration from environment variables
// Returns Config with defaults and environment variable overrides
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:                  getEnvInt("BFF_PORT", 7000),
		KeycloakIssuerURI:     getEnv("KEYCLOAK_ISSUER_URI", "http://localhost:8180/realms/echo"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		AuditServiceURL:       getEnv("AUDIT_SERVICE_URL", "http://localhost:8080"),
		TaskTrackerServiceURL: getEnv("TASKTRACKER_SERVICE_URL", "http://localhost:8000"),
		RateLimitPerMinute:    getEnvInt("RATE_LIMIT_PER_MINUTE", 100),
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
