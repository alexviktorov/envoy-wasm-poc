package main

import (
	"encoding/json"
	"fmt"

	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm"
	"github.com/tetratelabs/proxy-wasm-go-sdk/proxywasm/types"
)

const (
	jwtVendingServiceCluster = "jwt-vending-service"
	jwtVendingServicePath    = "/token/valid"
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
// This filter runs on service-a's Envoy sidecar and intercepts outbound requests
type httpContext struct {
	types.DefaultHttpContext
	contextID uint32
	token     string
}

// TokenResponse represents the JWT vending service response
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

// OnHttpRequestHeaders is called when request headers are received
// This is where we intercept outbound requests and fetch JWT tokens
func (ctx *httpContext) OnHttpRequestHeaders(numHeaders int, endOfStream bool) types.Action {
	// Get the target service from the authority or host header
	authority, err := proxywasm.GetHttpRequestHeader(":authority")
	if err != nil {
		proxywasm.LogErrorf("failed to get authority header: %v", err)
		return types.ActionContinue
	}

	// Only process requests to service-b
	if authority != "service-b:8083" && authority != "service-b" {
		proxywasm.LogInfof("skipping JWT injection for non-service-b request: %s", authority)
		return types.ActionContinue
	}

	proxywasm.LogInfof("[Client WASM] Intercepted request to %s, fetching JWT token", authority)

	// Prepare request to JWT vending service
	// Request body: {"service_id": "service-a"}
	requestBody := `{"service_id":"service-a"}`

	// Make HTTP callout to JWT vending service
	headers := [][2]string{
		{":method", "POST"},
		{":path", jwtVendingServicePath},
		{":authority", "jwt-vending-service:8081"},
		{"content-type", "application/json"},
	}

	// DispatchHttpCall in v0.24.0 takes a callback function
	_, err = proxywasm.DispatchHttpCall(
		jwtVendingServiceCluster,
		headers,
		[]byte(requestBody),
		nil,
		5000, // 5 second timeout
		ctx.handleJWTResponse,
	)

	if err != nil {
		proxywasm.LogErrorf("[Client WASM] Failed to dispatch HTTP call to JWT vending service: %v", err)
		// Continue without JWT rather than blocking the request
		return types.ActionContinue
	}

	proxywasm.LogInfof("[Client WASM] Dispatched HTTP call to JWT vending service")

	// Pause the request until we get the JWT token
	return types.ActionPause
}

// handleJWTResponse is called when the HTTP callout response is received
func (ctx *httpContext) handleJWTResponse(numHeaders, bodySize, numTrailers int) {
	proxywasm.LogInfof("[Client WASM] Received JWT response (headers: %d, body: %d)", numHeaders, bodySize)

	// Get response body
	responseBody, err := proxywasm.GetHttpCallResponseBody(0, bodySize)
	if err != nil {
		proxywasm.LogErrorf("[Client WASM] Failed to get response body: %v", err)
		proxywasm.ResumeHttpRequest()
		return
	}

	// Parse token response
	var tokenResp TokenResponse
	if err := json.Unmarshal(responseBody, &tokenResp); err != nil {
		proxywasm.LogErrorf("[Client WASM] Failed to parse token response: %v", err)
		proxywasm.ResumeHttpRequest()
		return
	}

	if tokenResp.Token == "" {
		proxywasm.LogErrorf("[Client WASM] Empty token received from JWT vending service")
		proxywasm.ResumeHttpRequest()
		return
	}

	proxywasm.LogInfof("[Client WASM] Successfully obtained JWT token (length: %d)", len(tokenResp.Token))

	// Inject JWT token into the Authorization header
	authHeader := fmt.Sprintf("Bearer %s", tokenResp.Token)
	if err := proxywasm.ReplaceHttpRequestHeader("Authorization", authHeader); err != nil {
		proxywasm.LogErrorf("[Client WASM] Failed to set Authorization header: %v", err)
	} else {
		proxywasm.LogInfof("[Client WASM] Injected JWT token into Authorization header")
	}

	// Resume the request
	proxywasm.ResumeHttpRequest()
}

// OnHttpResponseHeaders is called when response headers are received
func (ctx *httpContext) OnHttpResponseHeaders(numHeaders int, endOfStream bool) types.Action {
	// Log response status for debugging
	status, err := proxywasm.GetHttpResponseHeader(":status")
	if err == nil {
		proxywasm.LogInfof("[Client WASM] Response status: %s", status)
	}
	return types.ActionContinue
}