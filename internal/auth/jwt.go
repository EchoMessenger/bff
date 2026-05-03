package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWKSKey represents a single key in the JWKS response
type JWKSKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

// JWKSResponse represents the JWKS endpoint response
type JWKSResponse struct {
	Keys []JWKSKey `json:"keys"`
}

// JWTValidator handles JWT token validation with cached JWKS keys
type JWTValidator struct {
	KeycloakIssuerURI string
	jwksURL           string
	cachedKeys        map[string]*rsa.PublicKey
	mu                sync.RWMutex
	lastFetch         time.Time
	cacheTTL          time.Duration
}

// CustomClaims represents JWT claims with standard fields
type CustomClaims struct {
	Sub                string `json:"sub"`
	Email              string `json:"email"`
	PreferredUsername  string `json:"preferred_username"`
	jwt.RegisteredClaims
}

// NewJWTValidator creates a new JWT validator with the given issuer URI
func NewJWTValidator(issuerURI string) (*JWTValidator, error) {
	if issuerURI == "" {
		return nil, fmt.Errorf("keycloak issuer URI must not be empty")
	}

	// Ensure issuer URI ends without trailing slash for consistency
	issuerURI = strings.TrimSuffix(issuerURI, "/")

	validator := &JWTValidator{
		KeycloakIssuerURI: issuerURI,
		jwksURL:           issuerURI + "/protocol/openid-connect/certs",
		cachedKeys:        make(map[string]*rsa.PublicKey),
		cacheTTL:          24 * time.Hour,
	}

	// Attempt initial JWKS fetch to validate issuer URI
	if err := validator.refreshJWKS(); err != nil {
		return nil, fmt.Errorf("failed to fetch initial JWKS: %w", err)
	}

	return validator, nil
}

// refreshJWKS fetches and updates the cached JWKS keys
func (v *JWTValidator) refreshJWKS() error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned status %d", resp.StatusCode)
	}

	var jwksResp JWKSResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwksResp); err != nil {
		return fmt.Errorf("failed to decode JWKS response: %w", err)
	}

	// Cache the keys by kid (key ID)
	v.mu.Lock()
	defer v.mu.Unlock()

	v.cachedKeys = make(map[string]*rsa.PublicKey)
	for _, key := range jwksResp.Keys {
		if key.Kty == "RSA" {
			publicKey, err := convertJWKSKeyToRSA(key)
			if err != nil {
				fmt.Printf("warning: failed to convert key %s: %v\n", key.Kid, err)
				continue
			}
			v.cachedKeys[key.Kid] = publicKey
		}
	}
	v.lastFetch = time.Now()

	return nil
}

// convertJWKSKeyToRSA converts a JWKS key to an RSA public key
func convertJWKSKeyToRSA(key JWKSKey) (*rsa.PublicKey, error) {
	// Decode N (modulus)
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode N: %w", err)
	}

	// Decode E (exponent)
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode E: %w", err)
	}

	// Convert E bytes to integer
	eBigInt := new(big.Int)
	eBigInt.SetBytes(eBytes)

	// Create RSA public key
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(eBigInt.Int64()),
	}, nil
}

// ValidateToken validates a JWT token
// It accepts tokens with or without "Bearer " prefix
func (v *JWTValidator) ValidateToken(tokenString string) (*jwt.Token, error) {
	// Extract token from Bearer format if needed
	if strings.HasPrefix(tokenString, "Bearer ") {
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	}

	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	// Check if cache needs refresh
	v.mu.RLock()
	needsRefresh := time.Since(v.lastFetch) > v.cacheTTL
	v.mu.RUnlock()

	if needsRefresh {
		if err := v.refreshJWKS(); err != nil {
			// Log but continue with cached keys
			fmt.Printf("warning: failed to refresh JWKS: %v\n", err)
		}
	}

	// Parse and validate token
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Get kid from token header
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("kid not found in token header")
		}

		// Get the key from cache
		v.mu.RLock()
		publicKey, ok := v.cachedKeys[kid]
		v.mu.RUnlock()

		if !ok {
			// Try refreshing JWKS in case key is new
			if err := v.refreshJWKS(); err != nil {
				return nil, fmt.Errorf("failed to refresh JWKS: %w", err)
			}

			v.mu.RLock()
			publicKey, ok = v.cachedKeys[kid]
			v.mu.RUnlock()

			if !ok {
				return nil, fmt.Errorf("kid %s not found in JWKS", kid)
			}
		}

		return publicKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Validate issuer
	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims format")
	}

	if claims.Issuer != v.KeycloakIssuerURI {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", v.KeycloakIssuerURI, claims.Issuer)
	}

	return token, nil
}

// GetUserIDFromToken extracts the user ID from the Authorization header
func (v *JWTValidator) GetUserIDFromToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header")
	}

	token, err := v.ValidateToken(authHeader)
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims format")
	}

	if claims.Sub == "" {
		return "", fmt.Errorf("subject claim is empty")
	}

	return claims.Sub, nil
}
