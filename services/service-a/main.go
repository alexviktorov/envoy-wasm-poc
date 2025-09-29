package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// CallServiceBRequest represents the request to call service B
type CallServiceBRequest struct {
	Asset         string `json:"asset"`           // e.g., "asset-x" or "asset-y"
	UseValidToken bool   `json:"use_valid_token"` // true for valid, false for invalid
}

// CallServiceBResponse represents the response from calling service B
type CallServiceBResponse struct {
	Success      bool        `json:"success"`
	ResponseFrom interface{} `json:"response_from_b,omitempty"`
	Error        string      `json:"error,omitempty"`
}

// TokenRequest for JWT vending service
type TokenRequest struct {
	ServiceID string `json:"service_id"`
}

// TokenResponse from JWT vending service
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

var (
	jwtVendingServiceURL = getEnv("JWT_VENDING_URL", "http://jwt-vending-service:8081")
	serviceBURL          = getEnv("SERVICE_B_URL", "http://service-b:8083")
	serviceID            = getEnv("SERVICE_ID", "service-a")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getJWTToken fetches a JWT token from the vending service
func getJWTToken(useValid bool) (string, error) {
	endpoint := "/token/valid"
	if !useValid {
		endpoint = "/token/invalid"
	}

	reqBody := TokenRequest{
		ServiceID: serviceID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := jwtVendingServiceURL + endpoint
	log.Printf("Requesting JWT from: %s", url)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to call JWT vending service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("JWT vending service returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	log.Printf("Successfully obtained JWT token (expires in %d seconds)", tokenResp.ExpiresIn)
	return tokenResp.Token, nil
}

// callServiceB makes a request to service B with the JWT token
func callServiceB(asset, token string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/process?asset=%s", serviceBURL, asset)
	log.Printf("Calling service B: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add JWT token to Authorization header
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Service-ID", serviceID)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call service B: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("Service B responded with status: %d", resp.StatusCode)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		// If not JSON, return raw body
		result = map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(body),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("service B returned status %d", resp.StatusCode)
	}

	return result, nil
}

// handleCallServiceB handles the endpoint for calling service B
func handleCallServiceB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CallServiceBRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Default to valid token if not specified
	if req.Asset == "" {
		req.Asset = "asset-x"
	}

	log.Printf("Processing request to call service B (asset: %s, use_valid_token: %t)", req.Asset, req.UseValidToken)

	// Step 1: Get JWT token
	token, err := getJWTToken(req.UseValidToken)
	if err != nil {
		log.Printf("Error getting JWT token: %v", err)
		response := CallServiceBResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get JWT token: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Step 2: Call service B with the token
	result, err := callServiceB(req.Asset, token)
	if err != nil {
		log.Printf("Error calling service B: %v", err)
		response := CallServiceBResponse{
			Success:      false,
			ResponseFrom: result,
			Error:        fmt.Sprintf("Failed to call service B: %v", err),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // Return 200 but with error in body for demo purposes
		json.NewEncoder(w).Encode(response)
		return
	}

	// Success
	response := CallServiceBResponse{
		Success:      true,
		ResponseFrom: result,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth returns health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "service": serviceID})
}

func main() {
	http.HandleFunc("/call-service-b", handleCallServiceB)
	http.HandleFunc("/health", handleHealth)

	port := ":8080"
	log.Printf("Service A starting on port %s", port)
	log.Printf("Service ID: %s", serviceID)
	log.Printf("JWT Vending Service URL: %s", jwtVendingServiceURL)
	log.Printf("Service B URL: %s", serviceBURL)
	log.Printf("Endpoints:")
	log.Printf("  POST /call-service-b - Call service B with JWT")
	log.Printf("  GET /health - Health check")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}