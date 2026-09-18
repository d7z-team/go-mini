//! RPC host routing and guest publication over the worker's endpoint.
use mini_go::{
    RuntimeError,
    ffi::{self, Cancellation},
    rpc::{self, BoxFuture, Result as RpcResult},
};
use std::sync::Arc;

pub struct Bridge {
    pub local: crate::mailbox::Mailbox,
    pub remote: Arc<rpc::Host>,
}
pub struct Publisher(pub Arc<rpc::router::Router>);
struct Publication(Arc<rpc::router::Registration>);
impl rpc::ProviderPublisher for Publisher {
    fn publish(
        &self,
        context: rpc::CallContext,
        name: String,
        provider: Arc<dyn rpc::Provider>,
    ) -> BoxFuture<'_, RpcResult<Arc<dyn rpc::ProviderPublication>>> {
        Box::pin(async move {
            context.check()?;
            let registration = self.0.register(
                provider,
                rpc::router::RegistrationOptions {
                    name,
                    ..Default::default()
                },
            )?;
            Ok(Arc::new(Publication(registration)) as Arc<dyn rpc::ProviderPublication>)
        })
    }
}
impl rpc::ProviderPublication for Publication {
    fn close(&self) -> BoxFuture<'_, RpcResult<()>> {
        Box::pin(async {
            self.0.abort();
            self.0.close().await
        })
    }
}
struct Session {
    local: Box<dyn ffi::Session>,
    remote: Box<dyn ffi::Session>,
}
impl ffi::Bridge for Bridge {
    fn capabilities(&self) -> Vec<String> {
        ffi::Bridge::capabilities(&self.local)
    }
    fn open(&self, cancellation: Cancellation) -> Result<Box<dyn ffi::Session>, RuntimeError> {
        Ok(Box::new(Session {
            local: ffi::Bridge::open(&self.local, cancellation.clone())?,
            remote: ffi::Bridge::open(self.remote.as_ref(), cancellation)?,
        }))
    }
}
impl ffi::Session for Session {
    fn start(
        &self,
        cancellation: Cancellation,
        request: ffi::Request,
        completion: ffi::Completion,
    ) -> Result<Box<dyn ffi::Call>, RuntimeError> {
        let session = if request.route == rpc::FFI_ROUTE {
            &self.remote
        } else {
            &self.local
        };
        session.start(cancellation, request, completion)
    }
    fn shutdown(&self, _: Cancellation) -> Result<(), RuntimeError> {
        Err(RuntimeError::new(
            "async_required",
            "shutdown",
            "WASM cleanup requires polling",
        ))
    }
    fn shutdown_async(&self) -> ffi::Shutdown<'_> {
        Box::pin(async {
            let (local, remote) =
                tokio::join!(self.local.shutdown_async(), self.remote.shutdown_async());
            local.and(remote)
        })
    }
}
