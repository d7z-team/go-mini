use crate::{ffi, rpc};
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use tokio::{
    runtime::Handle,
    sync::{Semaphore, watch},
};

/// Owns blocking operations even when the awaiting RPC future is canceled.
pub struct BlockingPool {
    runtime: Handle,
    slots: Arc<Semaphore>,
    capacity: u32,
    closing: AtomicBool,
    active_cleanups: watch::Sender<usize>,
}

impl BlockingPool {
    pub fn new(runtime: Handle, capacity: u32) -> rpc::Result<Arc<Self>> {
        if capacity == 0 {
            return Err(rpc::Status::new(
                "invalid_argument",
                "blocking capacity must be positive",
            ));
        }
        Ok(Arc::new(Self {
            runtime,
            slots: Arc::new(Semaphore::new(capacity as usize)),
            capacity,
            closing: AtomicBool::new(false),
            active_cleanups: watch::channel(0).0,
        }))
    }
    pub async fn run<T: Send + 'static>(
        &self,
        operation: impl FnOnce() -> T + Send + 'static,
    ) -> rpc::Result<T> {
        let permit = self.slots.clone().try_acquire_owned().map_err(|_| {
            rpc::Status::new("resource_exhausted", "blocking operation limit exceeded")
        })?;
        if self.closing.load(Ordering::Acquire) {
            return Err(rpc::Status::new("unavailable", "blocking pool closed"));
        }
        self.runtime
            .spawn_blocking(move || {
                let _permit = permit;
                operation()
            })
            .await
            .map_err(|error| rpc::Status::new("internal", error.to_string()))
    }
    pub async fn shutdown(&self) -> rpc::Result<()> {
        self.closing.store(true, Ordering::Release);
        let permit = self
            .slots
            .acquire_many(self.capacity)
            .await
            .map_err(|_| rpc::Status::new("internal", "blocking pool failed"))?;
        drop(permit);
        let mut cleanups = self.active_cleanups.subscribe();
        cleanups
            .wait_for(|count| *count == 0)
            .await
            .map_err(|_| rpc::Status::new("internal", "file cleanup owner failed"))?;
        Ok(())
    }

    // Cleanup retains the pool and waits for capacity even after shutdown starts.
    // A canceled waiter must not abandon a backend resource or its accounting.
    pub(super) fn spawn_cleanup(
        self: &Arc<Self>,
        closing: ffi::Cancellation,
        cleanup: impl FnOnce() -> rpc::Result<()> + Send + 'static,
    ) -> watch::Receiver<Option<rpc::Result<()>>> {
        let (send, done) = watch::channel(None);
        let owner = self.clone();
        self.active_cleanups.send_modify(|count| *count += 1);
        self.runtime.spawn(async move {
            closing.cancelled().await;
            let permit = owner.slots.clone().acquire_owned().await;
            let result = owner
                .runtime
                .spawn_blocking(move || {
                    let _permit = permit;
                    cleanup()
                })
                .await
                .unwrap_or_else(|error| Err(rpc::Status::new("internal", error.to_string())));
            send.send_replace(Some(result));
            owner.active_cleanups.send_modify(|count| *count -= 1);
        });
        done
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{future::Future, task::Poll};

    #[tokio::test]
    async fn shutdown_waits_for_cleanup_after_its_receiver_is_dropped() {
        let pool = BlockingPool::new(Handle::current(), 1).unwrap();
        let closing = ffi::Cancellation::default();
        let (entered, started) = tokio::sync::oneshot::channel();
        let (release, released) = std::sync::mpsc::channel();
        let completed = Arc::new(AtomicBool::new(false));
        let observed = completed.clone();
        let done = pool.spawn_cleanup(closing.clone(), move || {
            let _ = entered.send(());
            released.recv().unwrap();
            observed.store(true, Ordering::Release);
            Ok(())
        });
        drop(done);
        let mut shutdown = std::pin::pin!(pool.shutdown());
        std::future::poll_fn(|context| {
            assert!(shutdown.as_mut().poll(context).is_pending());
            Poll::Ready(())
        })
        .await;
        closing.cancel();
        started.await.unwrap();
        std::future::poll_fn(|context| {
            assert!(shutdown.as_mut().poll(context).is_pending());
            Poll::Ready(())
        })
        .await;
        release.send(()).unwrap();
        shutdown.await.unwrap();
        assert!(completed.load(Ordering::Acquire));
    }

    #[tokio::test]
    async fn cleanup_panic_publishes_failure_and_releases_shutdown() {
        let pool = BlockingPool::new(Handle::current(), 1).unwrap();
        let closing = ffi::Cancellation::default();
        let mut done = pool.spawn_cleanup(closing.clone(), || panic!("cleanup failed"));
        closing.cancel();
        let result = done.wait_for(Option::is_some).await.unwrap();
        assert_eq!(
            result.as_ref().unwrap().as_ref().unwrap_err().code,
            "internal"
        );
        drop(result);
        pool.shutdown().await.unwrap();
    }
}
