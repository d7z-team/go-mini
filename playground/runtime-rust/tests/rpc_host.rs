#![cfg(feature = "rpc")]
#[path = "support/laboratory.rs"]
mod laboratory;
#[path = "../../../testdata/rpc/generated/rust/service.rs"]
pub mod rpcservice;
#[path = "../../../testdata/rpc/generated/rust/types.rs"]
pub mod rpctypes;
#[path = "support/rpc.rs"]
mod support;
use mini_go::ffi::{self, Bridge};
use mini_go::rpc::protocol::FfiRequest;
use mini_go::rpc::*;
use std::sync::Arc;

fn method() -> Method {
    Method {
        id: "fixture.Echo.echo".into(),
        service: "fixture.Echo".into(),
        name: "echo".into(),
        contract_hash: "a".repeat(64),
        resource_type_hash: String::new(),
    }
}

#[tokio::test(flavor = "current_thread")]
async fn ffi_owned_result_and_single_worker_execution() {
    let provider = Arc::new(
        StaticProvider::new(vec![MethodBinding {
            method: method(),
            invoke: Some(Arc::new(|_, values| Box::pin(async { Ok(values) }))),
        }])
        .unwrap(),
    );
    let contract = provider.contract();
    let mut options = HostOptions::new(tokio::runtime::Handle::current());
    options.providers.push(provider);
    let host = Arc::new(Host::new(options).unwrap());
    let worker = host.clone();
    VmExecutor::new(tokio::runtime::Handle::current(), 1)
        .unwrap()
        .run(move || {
            let mut session = support::Session::new(worker.as_ref());
            let lease = session
                .call(FfiRequest {
                    operation: "open".into(),
                    contract,
                    ..FfiRequest::default()
                })
                .lease;
            let values = vec![Value::new("[]uint8", Data::Bytes(vec![0, 255]))];
            let result = session.call(FfiRequest {
                operation: "call_owned".into(),
                lease,
                method: method(),
                payload: encode_values(&values, &Limits::default()).unwrap(),
                ..FfiRequest::default()
            });
            assert_eq!(
                decode_values(&result.payload, &Limits::default()).unwrap(),
                values
            );
            assert_ne!(result.request_id, 0);
            session.call(FfiRequest {
                operation: "accept_result".into(),
                lease,
                request_id: result.request_id,
                ..FfiRequest::default()
            });
            session.call(FfiRequest {
                operation: "close".into(),
                lease,
                ..FfiRequest::default()
            });
            session.shutdown();
        })
        .await
        .unwrap();
    host.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn guest_provider_accept_and_respond() {
    let router = Arc::new(router::Router::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        None,
    ));
    let mut options = HostOptions::new(tokio::runtime::Handle::current());
    options.publish_provider = Some(router.clone());
    let host = Arc::new(Host::new(options).unwrap());
    let worker = host.clone();
    let (ready, published) = tokio::sync::oneshot::channel();
    let (stop, stopped) = std::sync::mpsc::channel();
    let guest = tokio::spawn(async move {
        VmExecutor::new(tokio::runtime::Handle::current(), 1)
            .unwrap()
            .run(move || {
                let mut session = support::Session::new(worker.as_ref());
                let lease = session
                    .call(FfiRequest {
                        operation: "open_provider".into(),
                        target: "guest".into(),
                        contract: Contract::new(vec![method()]),
                        ..FfiRequest::default()
                    })
                    .lease;
                ready.send(()).unwrap();
                // A completed FFI accept whose delivery is discarded must be
                // available to the next accept, without losing the caller.
                drop(session.request(FfiRequest {
                    operation: "accept".into(),
                    lease,
                    ..FfiRequest::default()
                }));
                let event = session.call(FfiRequest {
                    operation: "accept".into(),
                    lease,
                    ..FfiRequest::default()
                });
                assert_eq!(event.operation, "call");
                assert_eq!(event.method, method());
                session.call(FfiRequest {
                    operation: "respond".into(),
                    lease,
                    request_id: event.request_id,
                    payload: event.payload,
                    ..FfiRequest::default()
                });
                stopped.recv().unwrap();
                session.call(FfiRequest {
                    operation: "close_provider".into(),
                    lease,
                    ..FfiRequest::default()
                });
                session.shutdown();
            })
            .await
            .unwrap()
    });
    published.await.unwrap();
    let routes = router
        .bind(
            CallContext::default(),
            BindRequest::new(Contract::new(vec![method()])),
        )
        .await
        .unwrap();
    let values = vec![Value::new("int64", Data::Int(42))];
    let result = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method(),
                receiver: None,
                arguments: values.clone(),
            },
        )
        .await
        .unwrap();
    assert_eq!(result.accept().await.unwrap().consume(), values);
    routes.shutdown().await.unwrap();
    stop.send(()).unwrap();
    guest.await.unwrap();
    host.shutdown().await.unwrap();
    router.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn accepted_resources_follow_the_final_ffi_delivery_receipt() {
    use mini_go::rpc::protocol::FfiResponse;
    use std::sync::atomic::{AtomicUsize, Ordering};
    for consume in [false, true] {
        let released = Arc::new(AtomicUsize::new(0));
        let provider =
            rpcservice::laboratory_provider(Arc::new(laboratory::Laboratory(released.clone())))
                .unwrap();
        let contract = provider.contract();
        let open = contract
            .methods
            .iter()
            .find(|method| method.name == "Open")
            .unwrap()
            .clone();
        let add = contract
            .methods
            .iter()
            .find(|method| method.name == "Add")
            .unwrap()
            .clone();
        let mut options = HostOptions::new(tokio::runtime::Handle::current());
        options.providers.push(provider);
        let host = Arc::new(Host::new(options).unwrap());
        let worker = host.clone();
        VmExecutor::new(tokio::runtime::Handle::current(), 1)
            .unwrap()
            .run(move || {
                let mut session = support::Session::new(worker.as_ref());
                let lease = session
                    .call(FfiRequest {
                        operation: "open".into(),
                        contract,
                        ..FfiRequest::default()
                    })
                    .lease;
                let result = session.call(FfiRequest {
                    operation: "call_owned".into(),
                    lease,
                    method: open,
                    payload: encode_values(
                        &[Value::new("int64", Data::Int(40))],
                        &Limits::default(),
                    )
                    .unwrap(),
                    ..FfiRequest::default()
                });
                let values = decode_values(&result.payload, &Limits::default()).unwrap();
                let Data::Resource(reference) = &values[0].data else {
                    panic!("missing resource")
                };
                let receipt = session.request(FfiRequest {
                    operation: "accept_result".into(),
                    lease,
                    request_id: result.request_id,
                    ..FfiRequest::default()
                });
                if consume {
                    receipt.consume().unwrap();
                } else {
                    drop(receipt);
                }
                let reply = session
                    .request(FfiRequest {
                        operation: "call_owned".into(),
                        lease,
                        method: add,
                        receiver: Some(reference.clone()),
                        payload: encode_values(
                            &[Value::new("int64", Data::Int(0))],
                            &Limits::default(),
                        )
                        .unwrap(),
                        ..FfiRequest::default()
                    })
                    .consume()
                    .unwrap();
                let reply = FfiResponse::decode(&reply, &Limits::default()).unwrap();
                if consume {
                    assert!(reply.code.is_empty(), "{}", reply.message);
                } else {
                    assert!(reply.code == "invalid_argument" || reply.code == "not_found");
                }
                session.call(FfiRequest {
                    operation: "close".into(),
                    lease,
                    ..FfiRequest::default()
                });
                session.shutdown();
            })
            .await
            .unwrap();
        host.shutdown().await.unwrap();
        assert_eq!(host.stats(), HostStats::default());
        assert_eq!(released.load(Ordering::SeqCst), 1);
    }
}

#[tokio::test(flavor = "current_thread")]
async fn session_limit_includes_sessions_still_cleaning_up() {
    let mut options = HostOptions::new(tokio::runtime::Handle::current());
    options.max_sessions = 1;
    let host = Host::new(options).unwrap();
    let first = host.open(ffi::Cancellation::default()).unwrap();

    assert_eq!(
        host.open(ffi::Cancellation::default()).err().unwrap().code,
        "ffi_limit"
    );
    drop(first);
    // Dropping the public session only requests cleanup. The cleanup owner
    // retains the admission slot until it publishes its terminal state.
    assert_eq!(
        host.open(ffi::Cancellation::default()).err().unwrap().code,
        "ffi_limit"
    );

    let second = loop {
        match host.open(ffi::Cancellation::default()) {
            Ok(session) => break session,
            Err(error) if error.code == "ffi_limit" => tokio::task::yield_now().await,
            Err(error) => panic!("unexpected session admission error: {error}"),
        }
    };
    drop(second);
    host.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn completed_session_releases_admission_while_public_handle_is_retained() {
    let mut options = HostOptions::new(tokio::runtime::Handle::current());
    options.max_sessions = 1;
    let host = Host::new(options).unwrap();
    let first = host.open(ffi::Cancellation::default()).unwrap();
    first.shutdown_async().await.unwrap();

    let second = host.open(ffi::Cancellation::default()).unwrap();
    drop(second);
    drop(first);
    host.shutdown().await.unwrap();
}
