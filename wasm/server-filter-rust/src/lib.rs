use proxy_wasm::traits::*;
use proxy_wasm::types::*;
use log::info;
use serde::{Deserialize, Serialize};

const PDP_SERVICE_CLUSTER: &str = "sgnl-pdp-service";
const PDP_SERVICE_PATH: &str = "/access/v2/evaluations";

proxy_wasm::main! {{
    proxy_wasm::set_log_level(LogLevel::Info);
    proxy_wasm::set_root_context(|_| -> Box<dyn RootContext> {
        Box::new(ServerFilterRoot)
    });
}}

struct ServerFilterRoot;

impl Context for ServerFilterRoot {}

impl RootContext for ServerFilterRoot {
    fn on_vm_start(&mut self, _vm_configuration_size: usize) -> bool {
        info!("[Server WASM Rust] VM started");
        true
    }

    fn create_http_context(&self, _context_id: u32) -> Option<Box<dyn HttpContext>> {
        Some(Box::new(ServerFilterHttp::default()))
    }

    fn get_type(&self) -> Option<ContextType> {
        Some(ContextType::HttpContext)
    }
}

#[derive(Default)]
struct ServerFilterHttp {
    jwt_token: String,
    principal_id: String,
    asset_id: String,
}

#[derive(Serialize)]
struct Principal {
    id: String,
}

#[derive(Serialize)]
struct Query {
    #[serde(rename = "assetId")]
    asset_id: String,
    action: String,
}

#[derive(Serialize)]
struct EvaluationRequest {
    principal: Principal,
    queries: Vec<Query>,
}

#[derive(Deserialize)]
struct Decision {
    decision: String,
    reason: String,
}

#[derive(Deserialize)]
struct EvaluationResponse {
    decisions: Vec<Decision>,
}

impl Context for ServerFilterHttp {
    fn on_http_call_response(&mut self, _token_id: u32, _num_headers: usize, body_size: usize, _num_trailers: usize) {
        info!("[Server WASM Rust] Received PDP response (body size: {})", body_size);

        // Get response body
        let response_body = match self.get_http_call_response_body(0, body_size) {
            Some(body) => body,
            None => {
                info!("[Server WASM Rust] Failed to get PDP response body");
                self.send_forbidden_response("Policy evaluation failed", "");
                return;
            }
        };

        // Parse PDP response
        let eval_resp: EvaluationResponse = match serde_json::from_slice(&response_body) {
            Ok(resp) => resp,
            Err(e) => {
                info!("[Server WASM Rust] Failed to parse PDP response: {}", e);
                self.send_forbidden_response("Policy evaluation failed", "");
                return;
            }
        };

        if eval_resp.decisions.is_empty() {
            info!("[Server WASM Rust] No decisions in PDP response");
            self.send_forbidden_response("Policy evaluation failed", "");
            return;
        }

        let decision = &eval_resp.decisions[0];
        info!("[Server WASM Rust] PDP decision: {} ({})", decision.decision, decision.reason);

        if decision.decision != "Allow" {
            // Access denied - send 403
            self.send_forbidden_response("Access denied by policy", &decision.reason);
            return;
        }

        // Access allowed - add headers to indicate PDP validation succeeded
        self.add_http_request_header("X-PDP-Decision", "Allow");
        self.add_http_request_header("X-PDP-Reason", &decision.reason);
        self.add_http_request_header("X-Principal-ID", &self.principal_id);

        info!("[Server WASM Rust] Access granted, resuming request");

        // Resume the request to service-b
        self.resume_http_request();
    }
}

impl HttpContext for ServerFilterHttp {
    fn on_http_request_headers(&mut self, _num_headers: usize, _end_of_stream: bool) -> Action {
        // Get request path and method for context
        let path = match self.get_http_request_header(":path") {
            Some(p) => p,
            None => {
                info!("[Server WASM Rust] No path header found");
                return Action::Continue;
            }
        };

        let method = match self.get_http_request_header(":method") {
            Some(m) => m,
            None => "GET".to_string(),
        };

        info!("[Server WASM Rust] Intercepted inbound request: {} {}", method, path);

        // Extract JWT token from Authorization header
        let auth_header = match self.get_http_request_header("Authorization") {
            Some(h) => h,
            None => {
                info!("[Server WASM Rust] Missing Authorization header");
                self.send_unauthorized_response("Missing Authorization header");
                return Action::Pause;
            }
        };

        // Parse Bearer token
        if !auth_header.starts_with("Bearer ") {
            info!("[Server WASM Rust] Invalid Authorization header format");
            self.send_unauthorized_response("Invalid Authorization header format");
            return Action::Pause;
        }

        self.jwt_token = auth_header.trim_start_matches("Bearer ").to_string();
        info!("[Server WASM Rust] JWT token extracted (length: {})", self.jwt_token.len());

        // Get principal from X-Service-ID header (simplified - in production, decode JWT)
        self.principal_id = self.get_http_request_header("X-Service-ID")
            .unwrap_or_else(|| "service-a".to_string());

        // Extract asset ID from query parameters
        self.asset_id = self.extract_asset_from_path(&path);
        if self.asset_id.is_empty() {
            self.asset_id = "default-asset".to_string();
        }

        info!("[Server WASM Rust] Calling PDP: principal={}, asset={}", self.principal_id, self.asset_id);

        // Call PDP to evaluate authorization
        let eval_request = EvaluationRequest {
            principal: Principal {
                id: self.principal_id.clone(),
            },
            queries: vec![Query {
                asset_id: self.asset_id.clone(),
                action: "call".to_string(),
            }],
        };

        let request_body = match serde_json::to_vec(&eval_request) {
            Ok(body) => body,
            Err(e) => {
                info!("[Server WASM Rust] Failed to marshal PDP request: {}", e);
                self.send_unauthorized_response("Internal error");
                return Action::Pause;
            }
        };

        // Make HTTP callout to PDP
        let headers = vec![
            (":method", "POST"),
            (":path", PDP_SERVICE_PATH),
            (":authority", "sgnl-pdp-service:8082"),
            ("content-type", "application/json"),
        ];

        match self.dispatch_http_call(
            PDP_SERVICE_CLUSTER,
            headers,
            Some(&request_body),
            vec![],
            std::time::Duration::from_secs(5),
        ) {
            Ok(call_id) => {
                info!("[Server WASM Rust] Dispatched HTTP call to PDP (call_id: {})", call_id);
                Action::Pause
            }
            Err(e) => {
                info!("[Server WASM Rust] Failed to dispatch HTTP call to PDP: {:?}", e);
                self.send_forbidden_response("Policy evaluation failed", "");
                Action::Pause
            }
        }
    }
}

impl ServerFilterHttp {
    fn extract_asset_from_path(&self, path: &str) -> String {
        // Simple parsing of ?asset=value
        if let Some(idx) = path.find("asset=") {
            let start = idx + 6;
            let asset = &path[start..];
            if let Some(end) = asset.find('&') {
                asset[..end].to_string()
            } else {
                asset.to_string()
            }
        } else {
            String::new()
        }
    }

    fn send_unauthorized_response(&self, message: &str) {
        let body = format!(r#"{{"error":"{}"}}"#, message);
        self.send_http_response(
            401,
            vec![("content-type", "application/json")],
            Some(body.as_bytes()),
        );
    }

    fn send_forbidden_response(&self, message: &str, reason: &str) {
        let body = format!(
            r#"{{"error":"{}","pdp_response":{{"decision":"Deny","reason":"{}"}}}}"#,
            message, reason
        );
        self.send_http_response(
            403,
            vec![("content-type", "application/json")],
            Some(body.as_bytes()),
        );
    }
}