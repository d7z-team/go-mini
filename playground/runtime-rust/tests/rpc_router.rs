#![cfg(feature = "rpc")]
use mini_go::rpc::{router::*, *};
use std::sync::Arc;

fn provider(text: &'static str) -> Arc<dyn Provider> {
    Arc::new(
        StaticProvider::new(vec![MethodBinding {
            method: Method {
                id: "sample.Echo.echo".into(),
                service: "sample.Echo".into(),
                name: "echo".into(),
                contract_hash: "a".repeat(64),
                resource_type_hash: String::new(),
            },
            invoke: Some(Arc::new(move |_, _| {
                Box::pin(async move { Ok(vec![Value::new("string", Data::String(text.into()))]) })
            })),
        }])
        .unwrap(),
    )
}

async fn selected(router: &Router, contract: &Contract, options: BindOptions) -> String {
    let mut request = BindRequest::new(contract.clone());
    request.options = options;
    let routes = router.bind(CallContext::default(), request).await.unwrap();
    let result = routes
        .invoke(
            CallContext::default(),
            Call {
                method: contract.methods[0].clone(),
                receiver: None,
                arguments: vec![],
            },
        )
        .await
        .unwrap()
        .accept()
        .await
        .unwrap()
        .consume();
    routes.shutdown().await.unwrap();
    let Data::String(value) = &result[0].data else {
        panic!("missing selected provider")
    };
    value.clone()
}

#[tokio::test(flavor = "current_thread")]
async fn selection_observes_weights_labels_affinity_health_priority_and_capacity() {
    let router = Router::new(tokio::runtime::Handle::current(), Limits::default(), None);
    let first = provider("first");
    let contract = first.contract();
    let first = router
        .register(
            first,
            RegistrationOptions {
                name: "first".into(),
                weight: 2,
                labels: std::collections::BTreeMap::from([("zone".into(), "east".into())]),
                ..Default::default()
            },
        )
        .unwrap();
    let second = router
        .register(
            provider("second"),
            RegistrationOptions {
                name: "second".into(),
                weight: 1,
                ..Default::default()
            },
        )
        .unwrap();
    let mut observed = Vec::new();
    for _ in 0..6 {
        observed.push(selected(&router, &contract, BindOptions::default()).await);
    }
    assert_eq!(observed.iter().filter(|name| *name == "first").count(), 4);
    let affinity = BindOptions {
        affinity_key: "tenant".into(),
        ..Default::default()
    };
    let pinned = selected(&router, &contract, affinity.clone()).await;
    for _ in 0..3 {
        assert_eq!(selected(&router, &contract, affinity.clone()).await, pinned);
    }
    assert_eq!(
        selected(
            &router,
            &contract,
            BindOptions {
                labels: std::collections::BTreeMap::from([("zone".into(), "east".into())]),
                ..Default::default()
            }
        )
        .await,
        "first"
    );
    first.set_healthy(false);
    assert_eq!(
        selected(&router, &contract, BindOptions::default()).await,
        "second"
    );
    first.set_healthy(true);
    let priority = router
        .register(
            provider("priority"),
            RegistrationOptions {
                priority: 10,
                max_leases: 1,
                ..Default::default()
            },
        )
        .unwrap();
    assert_eq!(
        selected(&router, &contract, BindOptions::default()).await,
        "priority"
    );
    let held = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    assert_eq!(priority.status().active_leases, 1);
    assert_ne!(
        selected(&router, &contract, BindOptions::default()).await,
        "priority"
    );
    held.shutdown().await.unwrap();
    assert_eq!(
        selected(&router, &contract, BindOptions::default()).await,
        "priority"
    );
    second.drain();
    assert!(second.status().retired);
    router.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn publication_replacement_pins_leases_and_shutdown_owns_retired_generation() {
    let router = Router::new(tokio::runtime::Handle::current(), Limits::default(), None);
    let initial = router.revision();
    let original = provider("original");
    let contract = original.contract();
    let publication = router
        .publish(vec![ProviderEntry {
            bundles: Vec::new(),
            provider: original,
            options: RegistrationOptions::default(),
        }])
        .unwrap();
    let old = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    let plan = router
        .prepare_publication(
            Some(publication.clone()),
            vec![ProviderEntry {
                bundles: Vec::new(),
                provider: provider("replacement"),
                options: RegistrationOptions::default(),
            }],
        )
        .unwrap();
    let before = router.revision();
    assert_eq!(before.route_epoch, initial.route_epoch + 1);
    let _replacement = plan.commit().unwrap();
    assert_eq!(router.revision().route_epoch, before.route_epoch + 1);
    assert_eq!(router.revision().router_id, initial.router_id);
    let new = router
        .bind(CallContext::default(), BindRequest::new(contract.clone()))
        .await
        .unwrap();
    let call = Call {
        method: contract.methods[0].clone(),
        receiver: None,
        arguments: Vec::new(),
    };
    for (routes, expected) in [(&old, "original"), (&new, "replacement")] {
        let values = routes
            .invoke(CallContext::default(), call.clone())
            .await
            .unwrap()
            .accept()
            .await
            .unwrap()
            .consume();
        assert_eq!(
            values,
            vec![Value::new("string", Data::String(expected.into()))]
        );
    }
    tokio::time::timeout(std::time::Duration::from_secs(2), router.force_shutdown())
        .await
        .unwrap()
        .unwrap();
    assert_eq!(
        old.invoke(CallContext::default(), call.clone())
            .await
            .err()
            .unwrap()
            .code,
        "unavailable"
    );
    assert_eq!(
        new.invoke(CallContext::default(), call)
            .await
            .err()
            .unwrap()
            .code,
        "unavailable"
    );
    old.shutdown().await.unwrap();
    new.shutdown().await.unwrap();
    publication.close().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn failed_publication_preparation_preserves_routes_and_mount_hops_are_bounded() {
    let router = Arc::new(Router::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        None,
    ));
    let implementation = provider("live");
    let contract = implementation.contract();
    router
        .register(implementation, RegistrationOptions::default())
        .unwrap();
    let before = router.revision();
    let entries = (0..2)
        .map(|_| ProviderEntry {
            bundles: Vec::new(),
            provider: provider("invalid"),
            options: RegistrationOptions {
                name: "duplicate".into(),
                ..RegistrationOptions::default()
            },
        })
        .collect();
    assert_eq!(
        router
            .prepare_publication(None, entries)
            .err()
            .unwrap()
            .code,
        "invalid_argument"
    );
    assert_eq!(router.revision(), before);
    let mounted =
        MountedProvider::new(router.clone(), contract.clone(), &Limits::default()).unwrap();
    let mut request = BindRequest::new(contract.clone());
    request.hops = 0;
    assert_eq!(
        mounted
            .bind(CallContext::default(), request.clone())
            .await
            .err()
            .unwrap()
            .code,
        "resource_exhausted"
    );
    request.hops = -1;
    assert_eq!(
        router
            .bind(CallContext::default(), request)
            .await
            .err()
            .unwrap()
            .code,
        "resource_exhausted"
    );
    let routes = router
        .bind(CallContext::default(), BindRequest::new(contract))
        .await
        .unwrap();
    routes.shutdown().await.unwrap();
    router.shutdown().await.unwrap();
}
