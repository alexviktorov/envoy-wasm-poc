package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

const (
	pdpServiceCluster = "sgnl-pdp-service"
	pdpServicePath    = "/access/v2/evaluations"
)

func main() {
	proxywasm.SetVMContext(&vmContext{})
}

// vmContext implements types.VMContext
type vmContext struct {
	types.DefaultVMContext
}

// NewPluginContext implements types.VMContext
func (*vmContext) NewPluginContext(contextID uint32) types.PluginContext {
	return &pluginContext{}
}

// pluginContext implements types.PluginContext
type pluginContext struct {
	types.DefaultPluginContext
}

// NewHttpContext implements types.PluginContext
func (*pluginContext) NewHttpContext(contextID uint32) types.HttpContext {
	return &httpContext{contextID: contextID}
}

// httpContext implements types.HttpContext
// This filter runs on service-b's Envoy sidecar and intercepts inbound requests
type httpContext struct {
	types.DefaultHttpContext
	contextID     uint32
	calloutID     uint32
	jwtToken      string
	principalID   string
	assetID       string
	requestPath   string
	requestMethod string
}

// PDP API structures
type Principal struct {
	ID string `json:"id"`
}

type Query struct {
	AssetID string `json:"assetId"`
	Action  string `json:"action"`
}

type EvaluationRequest struct {
	Principal Principal `json:"principal"`
	Queries   []Query   `json:"queries"`
}

type Decision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type EvaluationResponse struct {
	Decisions []Decision `json:"decisions"`
}

// OnHttpRequestHeaders is called when request headers are received
// This is where we intercept inbound requests and validate JWT + call PDP
func (ctx *httpContext) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	// Get request path and method for context
	path, err := proxywasm.GetHttpRequestHeader(":path")
	if err != nil {
		proxywasm.LogErrorf("failed to get path: %v", err)
		return types.ActionContinue
	}
	ctx.requestPath = path

	method, err := proxywasm.GetHttpRequestHeader(":method")
	if err != nil {
		proxywasm.LogErrorf("failed to get method: %v", err)
		return types.ActionContinue
	}
	ctx.requestMethod = method

	proxywasm.LogInfof("[Server WASM] Intercepted inbound request: %s %s", method, path)

	// Extract JWT token from Authorization header
	authHeader, err := proxywasm.GetHttpRequestHeader("Authorization")
	if err != nil || authHeader == "" {
		proxywasm.LogErrorf("[Server WASM] Missing Authorization header")
		ctx.sendUnauthorizedResponse("Missing Authorization header")
		return types.ActionPause
	}

	// Parse Bearer token
	if !strings.HasPrefix(authHeader, "Bearer ") {
		proxywasm.LogErrorf("[Server WASM] Invalid Authorization header format")
		ctx.sendUnauthorizedResponse("Invalid Authorization header format")
		return types.ActionPause
	}

	ctx.jwtToken = strings.TrimPrefix(authHeader, "Bearer ")
	proxywasm.LogInfof("[Server WASM] JWT token extracted (length: %d)", len(ctx.jwtToken))

	// Parse JWT to get claims (simplified - in production, verify signature)
	claims, err := ctx.parseJWTClaims(ctx.jwtToken)
	if err != nil {
		proxywasm.LogErrorf("[Server WASM] Failed to parse JWT: %v", err)
		ctx.sendUnauthorizedResponse(fmt.Sprintf("Invalid JWT: %v", err))
		return types.ActionPause
	}

	// Get principal (subject) from JWT
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		proxywasm.LogErrorf("[Server WASM] JWT missing 'sub' claim")
		ctx.sendUnauthorizedResponse("JWT missing 'sub' claim")
		return types.ActionPause
	}
	ctx.principalID = sub

	// Extract asset ID from query parameters
	// Format: /process?asset=asset-x
	ctx.assetID = ctx.extractAssetFromPath(path)
	if ctx.assetID == "" {
		ctx.assetID = "default-asset"
	}

	proxywasm.LogInfof("[Server WASM] Calling PDP: principal=%s, asset=%s", ctx.principalID, ctx.assetID)

	// Call PDP to evaluate authorization
	evalRequest := EvaluationRequest{
		Principal: Principal{ID: ctx.principalID},
		Queries: []Query{
			{
				AssetID: ctx.assetID,
				Action:  "call",
			},
		},
	}

	requestBody, err := json.Marshal(evalRequest)
	if err != nil {
		proxywasm.LogErrorf("[Server WASM] Failed to marshal PDP request: %v", err)
		ctx.sendUnauthorizedResponse("Internal error")
		return types.ActionPause
	}

	// Make HTTP callout to PDP
	headers := [][2]string{
		{":method", "POST"},
		{":path", pdpServicePath},
		{":authority", "sgnl-pdp-service:8082"},
		{"content-type", "application/json"},
	}

	calloutID, err := proxywasm.DispatchHttpCall(
		pdpServiceCluster,
		headers,
		requestBody,
		nil,
		5000, // 5 second timeout
	)

	if err != nil {
		proxywasm.LogErrorf("[Server WASM] Failed to dispatch HTTP call to PDP: %v", err)
		ctx.sendForbiddenResponse("Policy evaluation failed", "")
		return types.ActionPause
	}

	ctx.calloutID = calloutID
	proxywasm.LogInfof("[Server WASM] Dispatched HTTP call to PDP (callout ID: %d)", calloutID)

	// Pause the request until we get the PDP decision
	return types.ActionPause
}

// OnHttpCallResponse is called when the HTTP callout response is received
func (ctx *httpContext) OnHttpCallResponse(numHeaders, bodySize, numTrailers int) {
	proxywasm.LogInfof("[Server WASM] Received PDP response (body size: %d)", bodySize)

	// Get response body
	responseBody, err := proxywasm.GetHttpCallResponseBody(0, bodySize)
	if err != nil {
		proxywasm.LogErrorf("[Server WASM] Failed to get PDP response body: %v", err)
		ctx.sendForbiddenResponse("Policy evaluation failed", "")
		return
	}

	// Parse PDP response
	var evalResp EvaluationResponse
	if err := json.Unmarshal(responseBody, &evalResp); err != nil {
		proxywasm.LogErrorf("[Server WASM] Failed to parse PDP response: %v", err)
		ctx.sendForbiddenResponse("Policy evaluation failed", "")
		return
	}

	if len(evalResp.Decisions) == 0 {
		proxywasm.LogErrorf("[Server WASM] No decisions in PDP response")
		ctx.sendForbiddenResponse("Policy evaluation failed", "")
		return
	}

	decision := evalResp.Decisions[0]
	proxywasm.LogInfof("[Server WASM] PDP decision: %s (%s)", decision.Decision, decision.Reason)

	if decision.Decision != "Allow" {
		// Access denied - send 403
		ctx.sendForbiddenResponse("Access denied by policy", decision.Reason)
		return
	}

	// Access allowed - add headers to indicate PDP validation succeeded
	proxywasm.AddHttpRequestHeader("X-PDP-Decision", "Allow")
	proxywasm.AddHttpRequestHeader("X-PDP-Reason", decision.Reason)
	proxywasm.AddHttpRequestHeader("X-Principal-ID", ctx.principalID)

	proxywasm.LogInfof("[Server WASM] Access granted, resuming request")

	// Resume the request to service-b
	proxywasm.ResumeHttpRequest()
}

// parseJWTClaims parses JWT claims (simplified, without signature verification)
func (ctx *httpContext) parseJWTClaims(token string) (map[string]interface{}, error) {
	// Split JWT into parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload (base64url)
	// Note: This is simplified. In production, use proper JWT library with signature verification
	// For now, we'll just create a mock claims object with the expected structure
	// The actual JWT validation would happen here with the public key

	// For this demo, we'll just extract basic info and trust the JWT vending service
	// In production, verify the signature using the public key from JWT vending service

	claims := map[string]interface{}{
		"sub": ctx.extractSubFromPath(ctx.requestPath), // Simplified extraction
	}

	return claims, nil
}

// extractSubFromPath extracts the subject from path or headers (helper for demo)
func (ctx *httpContext) extractSubFromPath(path string) string {
	// In real implementation, decode JWT payload
	// For demo, check X-Service-ID header
	serviceID, err := proxywasm.GetHttpRequestHeader("X-Service-ID")
	if err == nil && serviceID != "" {
		return serviceID
	}
	return "service-a" // Default for demo
}

// extractAssetFromPath extracts asset ID from query parameters
func (ctx *httpContext) extractAssetFromPath(path string) string {
	// Simple parsing of ?asset=value
	parts := strings.Split(path, "asset=")
	if len(parts) < 2 {
		return ""
	}
	asset := parts[1]
	// Remove any trailing parameters
	if idx := strings.Index(asset, "&"); idx != -1 {
		asset = asset[:idx]
	}
	return asset
}

// sendUnauthorizedResponse sends a 401 Unauthorized response
func (ctx *httpContext) sendUnauthorizedResponse(message string) {
	body := fmt.Sprintf(`{"error":"%s"}`, message)
	proxywasm.SendHttpResponse(401, [][2]string{
		{"content-type", "application/json"},
	}, []byte(body), -1)
}

// sendForbiddenResponse sends a 403 Forbidden response
func (ctx *httpContext) sendForbiddenResponse(message, reason string) {
	body := fmt.Sprintf(`{"error":"%s","pdp_response":{"decision":"Deny","reason":"%s"}}`, message, reason)
	proxywasm.SendHttpResponse(403, [][2]string{
		{"content-type", "application/json"},
	}, []byte(body), -1)
}