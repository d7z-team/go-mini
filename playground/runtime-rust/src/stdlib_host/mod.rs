//! Optional native providers for Mini-Go standard-library capabilities.
mod blocking;
pub use blocking::BlockingPool;
pub mod console;
#[path = "console_generated.rs"]
pub mod console_binding;
pub mod environment;
pub mod filesystem;
pub mod memory;
#[path = "os_generated.rs"]
pub mod os_binding;

use crate::{RuntimeError, ffi, rpc};
use std::sync::Arc;
use tokio::{
    runtime::Handle,
    sync::{oneshot, watch},
};

pub type OwnedClose = Box<dyn FnOnce() -> rpc::BoxFuture<'static, rpc::Result<()>> + Send>;
pub struct Provider {
    pub capability: String,
    pub rpc: Arc<dyn rpc::Provider>,
    pub close: Option<OwnedClose>,
}
pub struct HostBuilder {
    pub rpc: rpc::HostOptions,
    pub required: Vec<String>,
    pub providers: Vec<Provider>,
}
pub struct MemoryHostOptions {
    pub files: std::collections::BTreeMap<String, Vec<u8>>,
    pub environment: Vec<String>,
    pub input: Option<Arc<dyn console::Input>>,
    pub stdout: Option<console::Output>,
    pub stderr: Option<console::Output>,
    pub blocking_capacity: u32,
}
impl Default for MemoryHostOptions {
    fn default() -> Self {
        Self {
            files: Default::default(),
            environment: Vec::new(),
            input: None,
            stdout: None,
            stderr: None,
            blocking_capacity: 16,
        }
    }
}
impl HostBuilder {
    pub fn new(runtime: Handle) -> Self {
        Self {
            rpc: rpc::HostOptions::new(runtime),
            required: Vec::new(),
            providers: Vec::new(),
        }
    }
    pub fn memory(runtime: Handle, options: MemoryHostOptions) -> rpc::Result<Self> {
        let pool = BlockingPool::new(runtime.clone(), options.blocking_capacity)?;
        let backend = memory::MemoryFilesystem::new(options.files)
            .map_err(|error| rpc::Status::new(error.code, error.message))?;
        let mut builder = Self::new(runtime);
        builder.providers = vec![
            Provider {
                capability: "console".into(),
                rpc: Arc::new(console::Streams {
                    input: options.input,
                    stdout: options.stdout,
                    stderr: options.stderr,
                    pool: pool.clone(),
                })
                .provider()?,
                close: None,
            },
            Provider {
                capability: "environment".into(),
                rpc: environment::Environment::snapshot(options.environment).provider()?,
                close: None,
            },
            Provider {
                capability: "filesystem".into(),
                rpc: filesystem::FilesystemProvider {
                    backend,
                    pool: pool.clone(),
                }
                .provider()?,
                close: Some(Box::new(move || {
                    Box::pin(async move { pool.shutdown().await })
                })),
            },
        ];
        Ok(builder)
    }
    pub async fn build(self) -> rpc::Result<Arc<Host>> {
        let runtime = self.rpc.runtime.clone();
        let (send, receive) = oneshot::channel();
        runtime.clone().spawn(async move {
            let mut options = self.rpc;
            let mut cleanups = Vec::new();
            let mut capabilities = std::collections::BTreeSet::new();
            let mut failure = None;
            for provider in self.providers {
                if let Some(close) = provider.close {
                    cleanups.push(close);
                }
                if provider.capability.is_empty() || !capabilities.insert(provider.capability) {
                    failure = Some(rpc::Status::new(
                        "invalid_argument",
                        "duplicate or empty host capability",
                    ));
                }
                options.providers.push(provider.rpc);
            }
            let mut required = std::collections::BTreeSet::new();
            for capability in self.required {
                if !capabilities.contains(&capability) || !required.insert(capability) {
                    failure = Some(rpc::Status::new(
                        "failed_precondition",
                        "required host capability missing or repeated",
                    ));
                }
            }
            let host = if let Some(error) = failure {
                Err(error)
            } else {
                rpc::invoke_user(async { rpc::Host::new(options) }).await
            };
            match host {
                Err(error) => {
                    for close in cleanups.into_iter().rev() {
                        let _ = rpc::invoke_user(async { close().await }).await;
                    }
                    let _ = send.send(Err(error));
                }
                Ok(host) => {
                    let rpc = Arc::new(host);
                    let closing = ffi::Cancellation::default();
                    let (done, completed) = watch::channel(None);
                    let owner = rpc.clone();
                    let stopped = closing.clone();
                    runtime.spawn(async move {
                        stopped.cancelled().await;
                        let mut result = owner.shutdown().await;
                        for close in cleanups.into_iter().rev() {
                            if let Err(error) = rpc::invoke_user(async { close().await }).await {
                                result = Err(error);
                            }
                        }
                        done.send_replace(Some(result));
                    });
                    let _ = send.send(Ok(Arc::new(Host {
                        rpc,
                        capabilities: capabilities.into_iter().collect(),
                        closing,
                        done: completed,
                    })));
                }
            }
        });
        receive
            .await
            .map_err(|_| rpc::Status::new("internal", "standard-library host owner failed"))?
    }
}
pub struct Host {
    rpc: Arc<rpc::Host>,
    capabilities: Vec<String>,
    closing: ffi::Cancellation,
    done: watch::Receiver<Option<rpc::Result<()>>>,
}
impl Host {
    pub fn begin_shutdown(&self) {
        self.rpc.begin_shutdown();
        self.closing.cancel();
    }
    pub async fn shutdown(&self) -> rpc::Result<()> {
        self.begin_shutdown();
        let mut done = self.done.clone();
        let result = done
            .wait_for(|value| value.is_some())
            .await
            .map_err(|_| rpc::Status::new("internal", "standard-library host cleanup failed"))?;
        result.as_ref().unwrap().clone()
    }
    pub fn stats(&self) -> rpc::HostStats {
        self.rpc.stats()
    }
}
impl ffi::Bridge for Host {
    fn open(&self, context: ffi::Cancellation) -> Result<Box<dyn ffi::Session>, RuntimeError> {
        self.rpc.open(context)
    }
    fn capabilities(&self) -> Vec<String> {
        self.capabilities.clone()
    }
}
impl Drop for Host {
    fn drop(&mut self) {
        self.begin_shutdown();
    }
}
