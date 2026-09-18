#![cfg(feature = "rpc")]
#[path = "../../../testdata/rpc/generated/rust/service.rs"]
pub mod rpcservice;
#[path = "../../../testdata/rpc/generated/rust/types.rs"]
pub mod rpctypes;

use mini_go::rpc::*;
use rpcservice::{LaboratoryClient, Node, Packet, laboratory_provider};
use rpctypes::{Details, Mode};
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

#[path = "support/laboratory.rs"]
mod laboratory;
#[path = "support/scalars.rs"]
mod scalars;
use laboratory::Laboratory;

struct DecodeResource(Arc<AtomicUsize>);

#[tokio::test]
async fn generated_resource_retries_failed_close() {
    let closed = Arc::new(AtomicUsize::new(0));
    let provider = laboratory_provider(Arc::new(Laboratory(closed.clone()))).unwrap();
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider],
    )
    .unwrap();
    laboratory::exercise_resource_retry(CallContext::default(), &binder)
        .await
        .unwrap();
    assert_eq!(closed.load(Ordering::SeqCst), 1);
}
impl rpctypes::CounterHandler for DecodeResource {
    fn add(&self, _: CallContext, _: i64) -> BoxFuture<'_, Result<(i64,)>> {
        Box::pin(async { Ok((0,)) })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            self.0.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }
}

#[tokio::test(flavor = "current_thread")]
async fn later_typed_decode_failure_discards_all_resources_before_client_close() {
    let released = Arc::new(AtomicUsize::new(0));
    let contract = laboratory_provider(Arc::new(Laboratory(released.clone())))
        .unwrap()
        .contract();
    let bindings = contract
        .methods
        .into_iter()
        .map(|method| {
            let resource = !method.resource_type_hash.is_empty();
            let released = released.clone();
            MethodBinding {
                method,
                invoke: if resource {
                    None
                } else {
                    Some(Arc::new(move |context, _| {
                        let released = released.clone();
                        Box::pin(async move {
                            let first = rpctypes::CounterResource::export(
                                &context,
                                Some(Arc::new(DecodeResource(released.clone()))),
                            )?;
                            let malformed_details = rpctypes::CounterResource::export(
                                &context,
                                Some(Arc::new(DecodeResource(released))),
                            )?;
                            Ok(vec![first, malformed_details])
                        })
                    }))
                },
            }
        })
        .collect();
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![Arc::new(StaticProvider::new(bindings).unwrap())],
    )
    .unwrap();
    let client = LaboratoryClient::bind(CallContext::default(), &binder, BindOptions::default())
        .await
        .unwrap();
    assert_eq!(
        client
            .open(CallContext::default(), 0)
            .await
            .err()
            .unwrap()
            .code,
        "protocol"
    );
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(2);
    while released.load(Ordering::SeqCst) != 2 {
        assert!(
            std::time::Instant::now() < deadline,
            "decode failure leaked resources"
        );
        tokio::task::yield_now().await;
    }
    client.close().await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 2);
}

#[tokio::test(flavor = "current_thread")]
async fn imported_types_collections_recursion_and_resources() {
    let closed = Arc::new(AtomicUsize::new(0));
    let provider = laboratory_provider(Arc::new(Laboratory(closed.clone()))).unwrap();
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider],
    )
    .unwrap();
    let client = LaboratoryClient::bind(CallContext::default(), &binder, BindOptions::default())
        .await
        .unwrap();
    for data in [None, Some(Vec::new()), Some(vec![0, 255])] {
        let packet = Packet {
            scalars: Some(scalars::scalar_boundaries()),
            data: data.clone(),
            values: Some(vec![-1, i64::MAX]),
            labels: Some(std::collections::BTreeMap::from([
                ("b".into(), "2".into()),
                ("a".into(), "1".into()),
            ])),
            optional_data: Some(data),
            details: Details {
                label: "中".into(),
                mode: Mode(99),
            },
        };
        assert_eq!(
            client
                .echo(CallContext::default(), packet.clone())
                .await
                .unwrap()
                .0,
            packet
        );
    }
    let node = Node {
        value: 1,
        next: Some(Box::new(Node {
            value: 2,
            next: None,
        })),
    };
    assert_eq!(
        client
            .tree(CallContext::default(), node.clone())
            .await
            .unwrap()
            .0,
        node
    );
    let (counter, details) = client.open(CallContext::default(), 40).await.unwrap();
    let counter = counter.unwrap();
    assert_eq!(details.mode, Mode::READY);
    assert_eq!(counter.add(CallContext::default(), 2).await.unwrap().0, 42);
    assert_eq!(
        client
            .read(CallContext::default(), Some(counter.clone()))
            .await
            .unwrap()
            .0,
        42
    );
    client.close().await.unwrap();
    assert_eq!(counter.add(CallContext::default(), 0).await.unwrap().0, 42);
    counter.close(CallContext::default()).await.unwrap();
    assert_eq!(closed.load(Ordering::SeqCst), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn shared_resource_scenarios() {
    #[derive(serde::Deserialize)]
    struct Scenario {
        name: String,
        actions: Vec<String>,
        released: Vec<usize>,
    }
    let scenarios: Vec<Scenario> = serde_json::from_str(include_str!(
        "../../../testdata/rpc/scenarios/resources.json"
    ))
    .unwrap();
    for scenario in scenarios {
        let closed = Arc::new(AtomicUsize::new(0));
        let provider = laboratory_provider(Arc::new(Laboratory(closed.clone()))).unwrap();
        let contract = provider.contract();
        let method = contract
            .methods
            .iter()
            .find(|method| method.name == "Open")
            .unwrap()
            .clone();
        let binder = LocalBinder::new(
            tokio::runtime::Handle::current(),
            Limits::default(),
            vec![provider],
        )
        .unwrap();
        let routes = binder
            .bind(CallContext::default(), BindRequest::new(contract))
            .await
            .unwrap();
        let mut pending = None;
        let mut reference = None;
        for (action, expected) in scenario.actions.iter().zip(scenario.released) {
            match action.as_str() {
                "open" => {
                    let result = routes
                        .invoke(
                            CallContext::default(),
                            Call {
                                method: method.clone(),
                                receiver: None,
                                arguments: vec![Value::new("int64", Data::Int(40))],
                            },
                        )
                        .await
                        .unwrap();
                    let Data::Resource(resource) = &result.values[0].data else {
                        panic!("resource missing")
                    };
                    reference = Some(resource.clone());
                    pending = Some(result);
                }
                "discard" => pending.take().unwrap().discard().await.unwrap(),
                "accept" => {
                    pending.take().unwrap().accept().await.unwrap().consume();
                }
                "release" => routes
                    .drop_resource(CallContext::default(), reference.clone().unwrap())
                    .await
                    .unwrap(),
                "wrong_epoch" => {
                    let mut wrong = reference.clone().unwrap();
                    wrong.epoch += 1;
                    assert_eq!(
                        routes
                            .drop_resource(CallContext::default(), wrong)
                            .await
                            .unwrap_err()
                            .code,
                        "invalid_argument"
                    );
                }
                "wrong_owner" => {
                    let handle = routes.bind_resource(reference.clone().unwrap()).unwrap();
                    let other = binder
                        .bind(
                            CallContext::default(),
                            BindRequest::new(routes.contract.clone()),
                        )
                        .await
                        .unwrap();
                    assert_eq!(
                        handle.reference(&other).unwrap_err().code,
                        "invalid_argument"
                    );
                    other.shutdown().await.unwrap();
                }
                "alias_release" => {
                    let first = routes.bind_resource(reference.clone().unwrap()).unwrap();
                    let second = routes.bind_resource(reference.clone().unwrap()).unwrap();
                    first.close(CallContext::default()).await.unwrap();
                    second.close(CallContext::default()).await.unwrap();
                }
                "close" => routes.shutdown().await.unwrap(),
                action => panic!("unknown action {action}"),
            }
            assert_eq!(
                closed.load(Ordering::SeqCst),
                expected,
                "{} after {action}",
                scenario.name
            );
        }
    }
}
