#![cfg(feature = "rpc")]
use mini_go::rpc::{
    catalog::{self, CatalogSource, MrpcBundle, MrpcFile},
    router::{Lifecycle, ProviderEntry, RegistrationOptions, Router},
    *,
};
use std::{sync::Arc, time::Duration};

#[tokio::test(flavor = "current_thread")]
async fn empty_router_shutdown_wakes_revision_waiters() {
    let router = Arc::new(Router::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        None,
    ));
    let revision = router.revision();
    let owner = router.clone();
    let waiting = tokio::spawn(async move {
        owner
            .watch(
                CallContext::default(),
                &revision.router_id,
                revision.route_epoch,
            )
            .await
    });
    tokio::task::yield_now().await;
    router.shutdown().await.unwrap();
    let result = tokio::time::timeout(Duration::from_secs(1), waiting)
        .await
        .expect("closed Router must notify Watch")
        .unwrap();
    assert_eq!(result.unwrap_err().code, "unavailable");
}

fn provider() -> Arc<dyn Provider> {
    Arc::new(
        StaticProvider::new(vec![MethodBinding {
            method: Method {
                id: "example.Echo.echo".into(),
                service: "example.Echo".into(),
                name: "echo".into(),
                contract_hash: "a".repeat(64),
                resource_type_hash: String::new(),
            },
            invoke: Some(Arc::new(|_, values| Box::pin(async { Ok(values) }))),
        }])
        .unwrap(),
    )
}
fn bundle(text: &str) -> MrpcBundle {
    MrpcBundle::new(
        "example/echo".into(),
        vec![MrpcFile {
            path: "echo.mrpc".into(),
            text: text.into(),
            hash: String::new(),
        }],
    )
    .unwrap()
}

#[tokio::test(flavor = "current_thread")]
async fn publication_commits_manifest_and_revision_atomically() {
    let router = Arc::new(Router::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        None,
    ));
    let first = bundle("first");
    let second = bundle("second");
    let original = router
        .publish(vec![ProviderEntry {
            provider: provider(),
            options: RegistrationOptions::default(),
            bundles: vec![first.clone()],
        }])
        .unwrap();
    let before = router.snapshot().unwrap();
    let make = || ProviderEntry {
        provider: provider(),
        options: RegistrationOptions::default(),
        bundles: vec![second.clone()],
    };
    assert!(router.prepare_publication(None, vec![make()]).is_err());
    assert!(router.resolve_contract(&second.reference()).is_err());
    let plan = router
        .prepare_publication(Some(original.clone()), vec![make()])
        .unwrap();
    assert_eq!(router.snapshot().unwrap(), before);
    drop(plan);
    assert!(router.resolve_contract(&second.reference()).is_err());
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![catalog::provider(router.clone()).unwrap()],
    )
    .unwrap();
    let client = catalog::Client::bind(CallContext::default(), &binder)
        .await
        .unwrap();
    assert_eq!(
        client.snapshot(CallContext::default()).await.unwrap(),
        before
    );
    let replacement = router
        .prepare_publication(Some(original), vec![make()])
        .unwrap()
        .commit()
        .unwrap();
    let after = client.watch(CallContext::default(), &before).await.unwrap();
    assert_eq!(after.route_epoch, before.route_epoch + 1);
    assert_eq!(after.references, [second.reference()]);
    assert_eq!(
        client
            .resolve(CallContext::default(), &first.reference())
            .await
            .unwrap(),
        first
    );
    client.close().await.unwrap();
    replacement.close().await.unwrap();
    router.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn graceful_shutdown_preserves_existing_lease_until_closed() {
    let router = Router::new(tokio::runtime::Handle::current(), Limits::default(), None);
    let provider = provider();
    let contract = provider.contract();
    router
        .register(provider, RegistrationOptions::default())
        .unwrap();
    let routes = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    router.begin_shutdown();
    assert_eq!(router.status().state, Lifecycle::Draining);
    assert!(
        router
            .bind(CallContext::default(), BindRequest::new(contract.clone()))
            .await
            .is_err()
    );
    let result = routes
        .invoke(
            CallContext::default(),
            Call {
                method: contract.methods[0].clone(),
                arguments: vec![Value::new("int64", Data::Int(42))],
                receiver: None,
            },
        )
        .await
        .unwrap();
    assert_eq!(
        result.accept().await.unwrap().consume()[0].data,
        Data::Int(42)
    );
    assert!(
        tokio::time::timeout(Duration::from_millis(10), router.shutdown())
            .await
            .is_err()
    );
    routes.shutdown().await.unwrap();
    router.shutdown().await.unwrap();
    assert_eq!(router.status().state, Lifecycle::Closed);
}

struct ForgedCatalog {
    bundle: MrpcBundle,
    corrupt_id: bool,
}
impl CatalogSource for ForgedCatalog {
    fn snapshot(&self) -> Result<catalog::Snapshot> {
        let mut snapshot = catalog::Snapshot::new(vec![self.bundle.reference()])?;
        if self.corrupt_id {
            snapshot.id = "forged".into();
        }
        snapshot.gateway_id = "gateway".into();
        Ok(snapshot)
    }
    fn watch(
        &self,
        _: CallContext,
        after: catalog::Snapshot,
    ) -> BoxFuture<'_, Result<catalog::Snapshot>> {
        Box::pin(async { Ok(after) })
    }
    fn resolve(&self, _: &catalog::ContractReference) -> Result<MrpcBundle> {
        Ok(self.bundle.clone())
    }
}
#[tokio::test(flavor = "current_thread")]
async fn catalog_client_validates_snapshot_watch_and_exact_resolve() {
    for corrupt_id in [false, true] {
        let binder = LocalBinder::new(
            tokio::runtime::Handle::current(),
            Limits::default(),
            vec![
                catalog::provider(Arc::new(ForgedCatalog {
                    bundle: bundle("source"),
                    corrupt_id,
                }))
                .unwrap(),
            ],
        )
        .unwrap();
        let client = catalog::Client::bind(CallContext::default(), &binder)
            .await
            .unwrap();
        let snapshot = client.snapshot(CallContext::default()).await;
        if corrupt_id {
            assert_eq!(snapshot.unwrap_err().code, "protocol");
        } else {
            let snapshot = snapshot.unwrap();
            assert!(
                client
                    .watch(CallContext::default(), &snapshot)
                    .await
                    .is_err()
            );
            assert!(
                client
                    .resolve(CallContext::default(), &bundle("different").reference())
                    .await
                    .is_err()
            );
        }
        client.close().await.unwrap();
    }
}
