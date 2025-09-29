package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT signing keys - valid and invalid
var (
	validPrivateKey   *rsa.PrivateKey
	invalidPrivateKey *rsa.PrivateKey
	validPublicKey    *rsa.PublicKey
)

// TokenRequest represents the request body for token generation
type TokenRequest struct {
	ServiceID string `json:"service_id"` // e.g., "service-a"
}

// TokenResponse represents the response containing the JWT
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"` // seconds
}

// JWTClaims represents the JWT claims structure
type JWTClaims struct {
	jwt.RegisteredClaims
}

func init() {
	// Generate RSA key pairs for valid tokens
	var err error
	validPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate valid private key: %v", err)
	}
	validPublicKey = &validPrivateKey.PublicKey

	// Generate a different RSA key pair for invalid tokens
	invalidPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate invalid private key: %v", err)
	}

	log.Println("JWT signing keys generated successfully")
}

// generateToken creates a JWT token signed with the specified key
func generateToken(serviceID string, privateKey *rsa.PrivateKey) (string, error) {
	now := time.Now()
	expiresAt := now.Add(5 * time.Minute)

	claims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   serviceID,
			Issuer:    "jwt-vending-service",
			Audience:  jwt.ClaimStrings{"service-mesh"},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// handleValidToken generates a valid JWT token
func handleValidToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ServiceID == "" {
		http.Error(w, "service_id is required", http.StatusBadRequest)
		return
	}

	tokenString, err := generateToken(req.ServiceID, validPrivateKey)
	if err != nil {
		log.Printf("Error generating valid token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	log.Printf("Generated valid token for service: %s", req.ServiceID)

	response := TokenResponse{
		Token:     tokenString,
		ExpiresIn: 300, // 5 minutes
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleInvalidToken generates an invalid JWT token (signed with wrong key)
func handleInvalidToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ServiceID == "" {
		http.Error(w, "service_id is required", http.StatusBadRequest)
		return
	}

	// Sign with the invalid private key
	tokenString, err := generateToken(req.ServiceID, invalidPrivateKey)
	if err != nil {
		log.Printf("Error generating invalid token: %v", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	log.Printf("Generated invalid token for service: %s", req.ServiceID)

	response := TokenResponse{
		Token:     tokenString,
		ExpiresIn: 300, // 5 minutes
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePublicKey returns the public key in PEM format for token validation
func handlePublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Export public key as PEM
	pubKeyBytes, err := jwt.MarshalRSAPublicKey(validPublicKey)
	if err != nil {
		log.Printf("Error marshaling public key: %v", err)
		http.Error(w, "Failed to export public key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Write(pubKeyBytes)
}

// handleHealth returns health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func main() {
	http.HandleFunc("/token/valid", handleValidToken)
	http.HandleFunc("/token/invalid", handleInvalidToken)
	http.HandleFunc("/public-key", handlePublicKey)
	http.HandleFunc("/health", handleHealth)

	port := ":8081"
	log.Printf("JWT Vending Service starting on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /token/valid - Generate valid JWT")
	log.Printf("  POST /token/invalid - Generate invalid JWT")
	log.Printf("  GET /public-key - Get public key for validation")
	log.Printf("  GET /health - Health check")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}