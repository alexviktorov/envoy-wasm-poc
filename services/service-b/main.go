package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ServiceBResponse represents the response from service B
type ServiceBResponse struct {
	Message    string                 `json:"message"`
	JWTClaims  map[string]interface{} `json:"jwt_claims,omitempty"`
	Authorized bool                   `json:"authorized"`
	Asset      string                 `json:"asset"`
	CallerID   string                 `json:"caller_id,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error       string                 `json:"error"`
	PDPResponse map[string]interface{} `json:"pdp_response,omitempty"`
}

var (
	serviceID = getEnv("SERVICE_ID", "service-b")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// extractJWT extracts the JWT token from the Authorization header
func extractJWT(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing Authorization header")
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", fmt.Errorf("invalid Authorization header format")
	}

	return parts[1], nil
}

// parseJWT parses and validates the JWT token (basic parsing without verification)
// In production, this would verify the signature using the public key
func parseJWT(tokenString string) (jwt.MapClaims, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid JWT claims")
	}

	return claims, nil
}

// handleProcess handles requests to service B
// In a real implementation, the WASM module would handle JWT validation and PDP checks
// For this demo, we'll do basic JWT parsing to show what the service receives
func handleProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get asset from query parameter
	asset := r.URL.Query().Get("asset")
	if asset == "" {
		asset = "unknown"
	}

	callerID := r.Header.Get("X-Service-ID")
	log.Printf("Received request from %s for asset: %s", callerID, asset)

	// Extract JWT token from Authorization header
	tokenString, err := extractJWT(r)
	if err != nil {
		log.Printf("Error extracting JWT: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Unauthorized: %v", err),
		})
		return
	}

	log.Printf("JWT token extracted successfully (length: %d)", len(tokenString))

	// Parse JWT claims (without verification for demo purposes)
	// In production, the WASM module validates the JWT signature
	claims, err := parseJWT(tokenString)
	if err != nil {
		log.Printf("Error parsing JWT: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: fmt.Sprintf("Invalid JWT: %v", err),
		})
		return
	}

	log.Printf("JWT claims parsed: sub=%v, iss=%v", claims["sub"], claims["iss"])

	// Check if authorization header indicates this was validated by WASM filter
	// The WASM filter would add custom headers after PDP validation
	pdpDecision := r.Header.Get("X-PDP-Decision")
	if pdpDecision == "Deny" {
		reason := r.Header.Get("X-PDP-Reason")
		log.Printf("Request denied by PDP: %s", reason)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: "Access denied by policy",
			PDPResponse: map[string]interface{}{
				"decision": "Deny",
				"reason":   reason,
			},
		})
		return
	}

	// Construct response with JWT claims
	claimsMap := make(map[string]interface{})
	for k, v := range claims {
		claimsMap[k] = v
	}

	response := ServiceBResponse{
		Message:    fmt.Sprintf("Call received from %s", callerID),
		JWTClaims:  claimsMap,
		Authorized: true,
		Asset:      asset,
		CallerID:   callerID,
	}

	log.Printf("Request authorized and processed successfully")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth returns health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "service": serviceID})
}

func main() {
	http.HandleFunc("/process", handleProcess)
	http.HandleFunc("/health", handleHealth)

	port := ":8083"
	log.Printf("Service B starting on port %s", port)
	log.Printf("Service ID: %s", serviceID)
	log.Printf("Endpoints:")
	log.Printf("  GET /process?asset=<asset-id> - Process request with JWT validation")
	log.Printf("  GET /health - Health check")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}