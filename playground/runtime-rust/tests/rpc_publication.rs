#![cfg(feature = "rpc")]
#[path = "support/connection.rs"]
mod connection;
use mini_go::{
    ffi::Cancellation,
    rpc::{control, publication::*, router::*, *},
};
use std::{
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::{Duration, Instant},
};
use tokio::sync::{Mutex, mpsc};

struct Staged {
    snapshot: control::PublicationSnapshot,
    entered: Cancellation,
    release: Cancellation,
    acknowledgements: AtomicUsize,
    fail_ready: bool,
    fail_published: bool,
}
impl control::PublicationHandler for Staged {
    fn snapshot(&self, _: CallContext) -> BoxFuture<'_, Result<(control::PublicationSnapshot,)>> {
        Box::pin(async { Ok((self.snapshot.clone(),)) })
    }
    fn resolve(
        &self,
        _: CallContext,
        _: String,
        _: String,
    ) -> BoxFuture<'_, Result<(control::ContractBundle,)>> {
        Box::pin(async { Err(Status::new("not_found", "no bundle")) })
    }
    fn ready(&self, context: CallContext, _: String) -> BoxFuture<'_, Result<()>> {
        Box::pin(async move {
            self.entered.cancel();
            context
                .run(async {
                    self.release.cancelled().await;
                    Ok(())
                })
                .await?;
            if self.fail_ready {
                Err(Status::new("unavailable", "ready failed"))
            } else {
                Ok(())
            }
        })
    }
    fn published(&self, _: CallContext, _: String) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            let attempts = self.acknowledgements.fetch_add(1, Ordering::SeqCst);
            if self.fail_published || attempts == 0 {
                Err(Status::new("unavailable", "acknowledgement lost"))
            } else {
                Ok(())
            }
        })
    }
}

fn candidate(
    process: &str,
    generation: u64,
    blocked: bool,
    fail_ready: bool,
    fail_published: bool,
) -> (Arc<Staged>, Arc<Endpoint>, Arc<Endpoint>, Contract) {
    let provider: Arc<dyn Provider> = Arc::new(
        StaticProvider::new(vec![MethodBinding {
            method: Method {
                id: "fixture.Echo.Call".into(),
                service: "fixture.Echo".into(),
                name: "Call".into(),
                contract_hash: "a".repeat(64),
                resource_type_hash: String::new(),
            },
            invoke: Some(Arc::new(|_, values| Box::pin(async { Ok(values) }))),
        }])
        .unwrap(),
    );
    let contract = provider.contract();
    let handler = Arc::new(Staged {
        snapshot: normalize_snapshot(control::PublicationSnapshot {
            protocol: PUBLICATION_PROTOCOL.into(),
            process_id: process.into(),
            generation,
            id: String::new(),
            providers: Some(vec![control::PublishedProvider {
                id: "echo".into(),
                contract: (&contract).into(),
                options: control::RegistrationOptions {
                    name: "echo".into(),
                    priority: 0,
                    weight: 1,
                    max_leases: 0,
                    labels: None,
                },
                references: None,
            }]),
        })
        .unwrap(),
        entered: Cancellation::default(),
        release: Cancellation::default(),
        acknowledgements: AtomicUsize::new(0),
        fail_ready,
        fail_published,
    });
    if !blocked {
        handler.release.cancel();
    }
    let runtime = tokio::runtime::Handle::current();
    let binder = Arc::new(
        LocalBinder::new(
            runtime.clone(),
            Limits::default(),
            vec![
                provider,
                control::publication_provider(handler.clone()).unwrap(),
            ],
        )
        .unwrap(),
    );
    let (a, ar) = mpsc::channel(4);
    let (b, br) = mpsc::channel(4);
    let closed = Cancellation::default();
    let left = Endpoint::open(
        runtime.clone(),
        Arc::new(connection::Pipe {
            send: a,
            receive: Mutex::new(br),
            closed: closed.clone(),
        }),
        None,
        EndpointOptions::default(),
    )
    .unwrap();
    let right = Endpoint::open(
        runtime,
        Arc::new(connection::Pipe {
            send: b,
            receive: Mutex::new(ar),
            closed,
        }),
        Some(binder),
        EndpointOptions::default(),
    )
    .unwrap();
    (handler, left, right, contract)
}

#[tokio::test(flavor = "current_thread")]
async fn slow_preparation_does_not_block_other_process_and_failed_ready_is_atomic() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::new(runtime, router.clone());
    let (slow, left, right, _) = candidate("slow", 1, true, true, false);
    let owner = registry.clone();
    let pending = tokio::spawn(async move { owner.attach(left, PeerInfo::default()).await });
    slow.entered.cancelled().await;
    assert_eq!(router.revision().route_epoch, 0);
    let (fast, left2, right2, _) = candidate("fast", 1, false, false, false);
    let identity = registry.attach(left2, PeerInfo::default()).await.unwrap();
    assert_eq!(fast.acknowledgements.load(Ordering::SeqCst), 2);
    let revision = router.revision();
    slow.release.cancel();
    assert!(pending.await.unwrap().is_err());
    assert_eq!(router.revision(), revision);
    registry.detach(&identity.0, &identity.1).await.unwrap();
    right.shutdown().await.unwrap();
    right2.shutdown().await.unwrap();
    router.force_shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn reconnect_identity_old_detach_and_forced_retirement_preserve_current_publication() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::with_options(
        runtime,
        router.clone(),
        PublicationRegistryOptions {
            drain_timeout: Duration::from_millis(20),
            ..Default::default()
        },
    )
    .unwrap();
    let peer = PeerInfo {
        identity: "trusted".into(),
        ..Default::default()
    };
    let (_, left, right, contract) = candidate("worker", 1, false, false, false);
    let first = registry.attach(left.clone(), peer.clone()).await.unwrap();
    let lease = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    let (_, intruder, intruder_owner, _) = candidate("worker", 2, false, false, false);
    assert_eq!(
        registry
            .attach(
                intruder,
                PeerInfo {
                    identity: "other".into(),
                    ..Default::default()
                }
            )
            .await
            .unwrap_err()
            .code,
        "permission_denied"
    );
    intruder_owner.shutdown().await.unwrap();
    let (_, replacement, replacement_owner, _) = candidate("worker", 1, false, false, false);
    let second = registry.attach(replacement, peer).await.unwrap();
    assert_ne!(first, second);
    registry.detach(&first.0, &first.1).await.unwrap();
    let current = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    let call = Call {
        method: contract.methods[0].clone(),
        receiver: None,
        arguments: vec![],
    };
    current
        .invoke(CallContext::default(), call.clone())
        .await
        .unwrap()
        .discard()
        .await
        .unwrap();
    let deadline = Instant::now() + Duration::from_secs(2);
    while lease
        .invoke(CallContext::default(), call.clone())
        .await
        .is_ok()
    {
        assert!(
            Instant::now() < deadline,
            "retirement did not abort old lease"
        );
        tokio::task::yield_now().await;
    }
    current.shutdown().await.unwrap();
    lease.shutdown().await.unwrap();
    registry.detach(&second.0, &second.1).await.unwrap();
    right.shutdown().await.unwrap();
    replacement_owner.shutdown().await.unwrap();
    router.force_shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn failed_published_confirmation_revokes_committed_candidate() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::with_options(
        runtime,
        router.clone(),
        PublicationRegistryOptions {
            control_timeout: Duration::from_millis(100),
            ..Default::default()
        },
    )
    .unwrap();
    let (_, left, right, _) = candidate("failed", 1, false, false, true);
    assert!(registry.attach(left, PeerInfo::default()).await.is_err());
    assert!(router.revision().registrations.is_empty());
    right.shutdown().await.unwrap();
    router.force_shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn superseded_preparation_cannot_replace_the_winning_commit() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::new(runtime, router.clone());
    let (slow, left, right, _) = candidate("worker", 1, true, false, false);
    let owner = registry.clone();
    let pending = tokio::spawn(async move { owner.attach(left, PeerInfo::default()).await });
    slow.entered.cancelled().await;
    let (_, new, new_owner, _) = candidate("worker", 2, false, false, false);
    let identity = registry.attach(new, PeerInfo::default()).await.unwrap();
    let revision = router.revision();
    slow.release.cancel();
    assert_eq!(
        pending.await.unwrap().unwrap_err().code,
        "failed_precondition"
    );
    assert_eq!(router.revision(), revision);
    registry.detach(&identity.0, &identity.1).await.unwrap();
    right.shutdown().await.unwrap();
    new_owner.shutdown().await.unwrap();
    router.force_shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn canceled_attach_does_not_commit_and_releases_preparation_capacity() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::with_options(
        runtime,
        router.clone(),
        PublicationRegistryOptions {
            max_publications: 1,
            ..Default::default()
        },
    )
    .unwrap();
    let (slow, left, right, _) = candidate("worker", 1, true, false, false);
    let owner = registry.clone();
    let pending = tokio::spawn(async move { owner.attach(left, PeerInfo::default()).await });
    slow.entered.cancelled().await;
    pending.abort();
    let _ = pending.await;
    slow.release.cancel();
    let (_, new, new_owner, _) = candidate("worker", 2, false, false, false);
    let deadline = Instant::now() + Duration::from_secs(2);
    let identity = loop {
        match registry.attach(new.clone(), PeerInfo::default()).await {
            Ok(value) => break value,
            Err(error) => {
                assert_eq!(error.code, "resource_exhausted");
                assert!(Instant::now() < deadline, "preparation capacity leaked");
                tokio::task::yield_now().await;
            }
        }
    };
    assert_eq!(router.revision().route_epoch, 1);
    registry.detach(&identity.0, &identity.1).await.unwrap();
    right.shutdown().await.unwrap();
    new_owner.shutdown().await.unwrap();
    router.force_shutdown().await.unwrap();
}
