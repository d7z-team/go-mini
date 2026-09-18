//! Native adapters for the shared compiler tools image and runtime debugger.
pub mod dap;
#[cfg(not(target_arch = "wasm32"))]
pub mod language;
#[cfg(not(target_arch = "wasm32"))]
pub mod lsp;
#[cfg(not(target_arch = "wasm32"))]
pub mod server;
#[cfg(not(target_arch = "wasm32"))]
pub mod session;
#[cfg(not(target_arch = "wasm32"))]
pub mod sources;
#[cfg(not(target_arch = "wasm32"))]
pub mod transport;
