use crate::{rpcservice::*, rpctypes::*};
use mini_go::rpc::*;
use std::sync::{
    Arc,
    atomic::{AtomicBool, AtomicI64, AtomicUsize, Ordering},
};

pub struct Laboratory(pub Arc<AtomicUsize>);

struct Counter {
    value: AtomicI64,
    closed: Arc<AtomicUsize>,
    fail_close: AtomicBool,
}
impl CounterHandler for Counter {
    fn add(&self, _: CallContext, delta: i64) -> BoxFuture<'_, Result<(i64,)>> {
        Box::pin(async move { Ok((self.value.fetch_add(delta, Ordering::SeqCst) + delta,)) })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            if self.fail_close.swap(false, Ordering::SeqCst) {
                return Err(Status::new("failed_precondition", "injected close failure"));
            }
            self.closed.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }
}
impl LaboratoryHandler for Laboratory {
    fn echo(&self, _: CallContext, packet: Packet) -> BoxFuture<'_, Result<(Packet,)>> {
        Box::pin(async { Ok((packet,)) })
    }
    fn tree(&self, _: CallContext, node: Node) -> BoxFuture<'_, Result<(Node,)>> {
        Box::pin(async { Ok((node,)) })
    }
    fn open(
        &self,
        _: CallContext,
        initial: i64,
    ) -> BoxFuture<'_, Result<(Option<Arc<dyn CounterHandler>>, Details)>> {
        Box::pin(async move {
            Ok((
                Some(Arc::new(Counter {
                    value: AtomicI64::new(initial),
                    closed: self.0.clone(),
                    // Reserved by the conformance fixture for one failed close.
                    fail_close: AtomicBool::new(initial == i64::MIN),
                }) as Arc<dyn CounterHandler>),
                Details {
                    label: "counter".into(),
                    mode: Mode::READY,
                },
            ))
        })
    }
    fn read(
        &self,
        context: CallContext,
        counter: Option<Arc<dyn CounterHandler>>,
    ) -> BoxFuture<'_, Result<(i64,)>> {
        Box::pin(async move {
            counter
                .ok_or_else(|| Status::new("invalid_argument", "counter required"))?
                .add(context, 0)
                .await
        })
    }
    fn wait(&self, context: CallContext) -> BoxFuture<'_, Result<(i64,)>> {
        Box::pin(async move {
            context.cancellation.cancelled().await;
            Err(Status::new("canceled", "canceled"))
        })
    }
}

// This fixture is also imported by VM/FFI tests that exercise other contracts.
#[allow(dead_code)]
pub async fn exercise_resource_retry(context: CallContext, binder: &dyn Binder) -> Result<()> {
    let client = LaboratoryClient::bind(context.clone(), binder, BindOptions::default()).await?;
    let resource = client
        .open(context.clone(), i64::MIN)
        .await?
        .0
        .ok_or_else(|| Status::new("internal", "missing resource"))?;
    match resource.close(context.clone()).await {
        Err(error) if error.code == "failed_precondition" => {}
        _ => return Err(Status::new("internal", "first resource close must fail")),
    }
    if resource.add(context.clone(), 0).await.is_ok() {
        return Err(Status::new(
            "internal",
            "closing resource still accepts calls",
        ));
    }
    resource.close(context.clone()).await?;
    resource.close(context).await?;
    client.close().await?;
    client.routes.shutdown().await
}
