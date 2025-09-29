package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// Principal represents the service making the request
type Principal struct {
	ID string `json:"id"` // e.g., "service-a"
}

// Query represents an authorization query
type Query struct {
	AssetID string `json:"assetId"` // e.g., "asset-x", "asset-y"
	Action  string `json:"action"`  // e.g., "call"
}

// EvaluationRequest represents the SGNL API request format
type EvaluationRequest struct {
	Principal Principal `json:"principal"`
	Queries   []Query   `json:"queries"`
}

// Decision represents a single authorization decision
type Decision struct {
	Decision string `json:"decision"` // "Allow" or "Deny"
	Reason   string `json:"reason"`
}

// EvaluationResponse represents the SGNL API response format
type EvaluationResponse struct {
	Decisions []Decision `json:"decisions"`
}

// Policy rules for the demo
// service-a can access asset-x but not asset-y
var policyRules = map[string]map[string]bool{
	"service-a": {
		"asset-x": true,  // Allow
		"asset-y": false, // Deny
	},
	"service-b": {
		"asset-x": true,
		"asset-y": true,
	},
}

// evaluateAccess checks if the principal can access the asset
func evaluateAccess(principalID, assetID string) Decision {
	// Check if principal exists in policy
	assetRules, principalExists := policyRules[principalID]
	if !principalExists {
		return Decision{
			Decision: "Deny",
			Reason:   "Principal " + principalID + " not found in policy",
		}
	}

	// Check if asset access is allowed
	allowed, assetExists := assetRules[assetID]
	if !assetExists {
		return Decision{
			Decision: "Deny",
			Reason:   "Asset " + assetID + " not found in policy for principal " + principalID,
		}
	}

	if allowed {
		return Decision{
			Decision: "Allow",
			Reason:   "Service " + principalID + " is allowed to access " + assetID,
		}
	}

	return Decision{
		Decision: "Deny",
		Reason:   "Service " + principalID + " is not allowed to access " + assetID,
	}
}

// handleEvaluation handles the POST /access/v2/evaluations endpoint
func handleEvaluation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error decoding request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Principal.ID == "" {
		http.Error(w, "principal.id is required", http.StatusBadRequest)
		return
	}

	if len(req.Queries) == 0 {
		http.Error(w, "At least one query is required", http.StatusBadRequest)
		return
	}

	log.Printf("Evaluating access for principal: %s", req.Principal.ID)

	// Evaluate each query
	var decisions []Decision
	for _, query := range req.Queries {
		if query.AssetID == "" {
			decisions = append(decisions, Decision{
				Decision: "Deny",
				Reason:   "assetId is required",
			})
			continue
		}

		decision := evaluateAccess(req.Principal.ID, query.AssetID)
		log.Printf("  Query: action=%s, assetId=%s -> Decision: %s (%s)",
			query.Action, query.AssetID, decision.Decision, decision.Reason)
		decisions = append(decisions, decision)
	}

	response := EvaluationResponse{
		Decisions: decisions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth returns health status
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// handlePolicies returns the current policy rules for debugging
func handlePolicies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"policies": policyRules,
		"description": "Policy rules: service-a can access asset-x (Allow) but not asset-y (Deny)",
	})
}

func main() {
	http.HandleFunc("/access/v2/evaluations", handleEvaluation)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/policies", handlePolicies)

	port := ":8082"
	log.Printf("Mock SGNL PDP Service starting on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /access/v2/evaluations - Evaluate authorization")
	log.Printf("  GET /health - Health check")
	log.Printf("  GET /policies - View current policy rules")
	log.Printf("")
	log.Printf("Policy Rules:")
	log.Printf("  service-a -> asset-x: ALLOW")
	log.Printf("  service-a -> asset-y: DENY")

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}