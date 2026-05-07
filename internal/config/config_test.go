package config

import "testing"

func TestValidateCORSOrigin(t *testing.T) {
	validOrigins := []string{
		"http://192.168.56.1:8080",
		"http://www.echo-messenger.ru",
		"https://api.echo-messenger.ru",
		"http://localhost:3000/",
	}

	for _, origin := range validOrigins {
		t.Run("valid "+origin, func(t *testing.T) {
			if err := validateCORSOrigin(origin); err != nil {
				t.Fatalf("Expected origin to be valid, got error: %v", err)
			}
		})
	}

	invalidOrigins := []string{
		"",
		"://",
		"http:// a",
		"http://example.com/path",
		"http://example.com?x=1",
		"http://example.com#fragment",
		"http://user@example.com",
		"example.com",
	}

	for _, origin := range invalidOrigins {
		t.Run("invalid "+origin, func(t *testing.T) {
			if err := validateCORSOrigin(origin); err == nil {
				t.Fatal("Expected origin to be invalid")
			}
		})
	}
}
