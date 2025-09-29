use proxy_wasm::traits::*;
use proxy_wasm::types::*;
use log::info;

proxy_wasm::main! {{
    proxy_wasm::set_log_level(LogLevel::Info);
    proxy_wasm::set_root_context(|_| -> Box<dyn RootContext> {
        Box::new(MinimalRoot)
    });
}}

struct MinimalRoot;

impl Context for MinimalRoot {}

impl RootContext for MinimalRoot {
    fn on_vm_start(&mut self, _vm_configuration_size: usize) -> bool {
        info!("Minimal Rust filter: VM started");
        true
    }

    fn create_http_context(&self, _context_id: u32) -> Option<Box<dyn HttpContext>> {
        Some(Box::new(MinimalHttp))
    }

    fn get_type(&self) -> Option<ContextType> {
        Some(ContextType::HttpContext)
    }
}

struct MinimalHttp;

impl Context for MinimalHttp {}

impl HttpContext for MinimalHttp {
    fn on_http_request_headers(&mut self, _num_headers: usize, _end_of_stream: bool) -> Action {
        info!("Minimal Rust filter: request received");
        Action::Continue
    }
}