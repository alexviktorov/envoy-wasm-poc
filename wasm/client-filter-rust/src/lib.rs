use proxy_wasm::traits::*;
use proxy_wasm::types::*;
use log::info;
use serde::{Deserialize, Serialize};

const JWT_VENDING_SERVICE_CLUSTER: &str = "jwt-vending-service";
const JWT_VENDING_SERVICE_PATH: &str = "/token/valid";

proxy_wasm::main! {{
    proxy_wasm::set_log_level(LogLevel::Info);
    proxy_wasm::set_root_context(|_| -> Box<dyn RootContext> {
        Box::new(ClientFilterRoot)
    });
}}

struct ClientFilterRoot;

impl Context for ClientFilterRoot {}

impl RootContext for ClientFilterRoot {
    fn on_vm_start(&mut self, _vm_configuration_size: usize) -> bool {
        info!("[Client WASM Rust] VM started");
        true
    }

    fn create_http_context(&self, context_id: u32) -> Option<Box<dyn HttpContext>> {
        Some(Box::new(ClientFilterHttp {
            context_id,
        }))
    }

    fn get_type(&self) -> Option<ContextType> {
        Some(ContextType::HttpContext)
    }
}

struct ClientFilterHttp {
    context_id: u32,
}

#[derive(Deserialize)]
struct TokenResponse {
    token: String,
    #[allow(dead_code)]
    expires_in: i64,
}

#[derive(Serialize)]
struct TokenRequest {
    service_id: String,
}

impl Context for ClientFilterHttp {
    fn on_http_call_response(&mut self, _token_id: u32, num_headers: usize, body_size: usize, _num_trailers: usize) {
        info!("[Client WASM Rust] Received JWT response (headers: {}, body: {})", num_headers, body_size);

        // Get response body
        let response_body = match self.get_http_call_response_body(0, body_size) {
            Some(body) => body,
            None => {
                info!("[Client WASM Rust] Failed to get response body");
                self.resume_http_request();
                return;
            }
        };

        // Parse token response
        let token_resp: TokenResponse = match serde_json::from_slice(&response_body) {
            Ok(resp) => resp,
            Err(e) => {
                info!("[Client WASM Rust] Failed to parse token response: {}", e);
                self.resume_http_request();
                return;
            }
        };

        if token_resp.token.is_empty() {
            info!("[Client WASM Rust] Empty token received from JWT vending service");
            self.resume_http_request();
            return;
        }

        info!("[Client WASM Rust] Successfully obtained JWT token (length: {})", token_resp.token.len());

        // Inject JWT token into the Authorization header
        let auth_header = format!("Bearer {}", token_resp.token);
        self.set_http_request_header("Authorization", Some(&auth_header));
        info!("[Client WASM Rust] Injected JWT token into Authorization header");

        // Resume the request
        self.resume_http_request();
    }
}

impl HttpContext for ClientFilterHttp {
    fn on_http_request_headers(&mut self, _num_headers: usize, _end_of_stream: bool) -> Action {
        // Get the target service from the authority header
        let authority = match self.get_http_request_header(":authority") {
            Some(auth) => auth,
            None => {
                info!("[Client WASM Rust] No authority header found");
                return Action::Continue;
            }
        };

        // Only process requests to service-b
        if authority != "service-b:8083" && authority != "service-b" && authority != "envoy-service-b:10001" {
            info!("[Client WASM Rust] Skipping JWT injection for non-service-b request: {}", authority);
            return Action::Continue;
        }

        info!("[Client WASM Rust] Intercepted request to {}, fetching JWT token", authority);

        // Prepare request body
        let request_body = match serde_json::to_vec(&TokenRequest {
            service_id: "service-a".to_string(),
        }) {
            Ok(body) => body,
            Err(e) => {
                info!("[Client WASM Rust] Failed to serialize request: {}", e);
                return Action::Continue;
            }
        };

        // Make HTTP callout to JWT vending service
        let headers = vec![
            (":method", "POST"),
            (":path", JWT_VENDING_SERVICE_PATH),
            (":authority", "jwt-vending-service:8081"),
            ("content-type", "application/json"),
        ];

        match self.dispatch_http_call(
            JWT_VENDING_SERVICE_CLUSTER,
            headers,
            Some(&request_body),
            vec![],
            std::time::Duration::from_secs(5),
        ) {
            Ok(call_id) => {
                info!("[Client WASM Rust] Dispatched HTTP call to JWT vending service (call_id: {})", call_id);
                Action::Pause
            }
            Err(e) => {
                info!("[Client WASM Rust] Failed to dispatch HTTP call: {:?}", e);
                Action::Continue
            }
        }
    }

    fn on_http_response_headers(&mut self, _num_headers: usize, _end_of_stream: bool) -> Action {
        // Log response status for debugging
        if let Some(status) = self.get_http_response_header(":status") {
            info!("[Client WASM Rust] Response status: {}", status);
        }
        Action::Continue
    }
}