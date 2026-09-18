#![cfg(feature = "rpc")]
use mini_go::rpc::*;
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

struct RetryResource {
    attempts: Arc<AtomicUsize>,
    failures: usize,
    gate: Option<Arc<(tokio::sync::Notify, tokio::sync::Notify)>>,
}
impl Resource for RetryResource {
    fn invoke(
        &self,
        _: CallContext,
        _: String,
        _: Vec<Value>,
    ) -> BoxFuture<'_, Result<Vec<Value>>> {
        Box::pin(async { Ok(vec![]) })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            let attempt = self.attempts.fetch_add(1, Ordering::SeqCst);
            if attempt == 0
                && let Some(gate) = &self.gate
            {
                gate.0.notify_one();
                gate.1.notified().await;
            }
            if attempt < self.failures {
                return Err(Status::new("deadline_exceeded", "injected close failure"));
            }
            Ok(())
        })
    }
}

#[tokio::test]
async fn failed_resource_close_remains_owned() {
    #[derive(serde::Deserialize)]
    struct Scenario {
        name: String,
        failures: usize,
        retry: bool,
        terminal_error: bool,
        attempts: usize,
        #[serde(default)]
        shutdown_during_close: bool,
    }
    let mut scenarios: Vec<Scenario> =
        serde_json::from_str(include_str!("../../../testdata/rpc/scenarios/close.json")).unwrap();
    scenarios.push(Scenario {
        name: "shutdown-during-failed-close".into(),
        failures: 1,
        retry: false,
        terminal_error: false,
        attempts: 2,
        shutdown_during_close: true,
    });
    for scenario in scenarios {
        let attempts = Arc::new(AtomicUsize::new(0));
        let gate = scenario
            .shutdown_during_close
            .then(|| Arc::new((tokio::sync::Notify::new(), tokio::sync::Notify::new())));
        let resource = Arc::new(RetryResource {
            attempts: attempts.clone(),
            failures: scenario.failures,
            gate: gate.clone(),
        });
        let method = Method {
            id: "sample.Service.Open".into(),
            service: "sample.Service".into(),
            name: "Open".into(),
            contract_hash: "a".repeat(64),
            resource_type_hash: String::new(),
        };
        let provider = Arc::new(
            StaticProvider::new(vec![MethodBinding {
                method: method.clone(),
                invoke: Some(Arc::new(move |ctx, _| {
                    let resource = resource.clone();
                    Box::pin(async move { Ok(vec![ctx.export(resource, "b".repeat(64))?]) })
                })),
            }])
            .unwrap(),
        );
        let contract = provider.contract();
        let binder = LocalBinder::new(
            tokio::runtime::Handle::current(),
            Limits {
                max_resources: 1,
                ..Limits::default()
            },
            vec![provider],
        )
        .unwrap();
        let routes = binder
            .bind(CallContext::default(), BindRequest::new(contract))
            .await
            .unwrap();
        let pending = routes
            .invoke(
                CallContext::default(),
                Call {
                    method: method.clone(),
                    receiver: None,
                    arguments: vec![],
                },
            )
            .await
            .unwrap();
        let Data::Resource(reference) = &pending.values[0].data else {
            panic!("missing resource")
        };
        let handle = routes.bind_resource(reference.clone()).unwrap();
        pending.accept().await.unwrap().consume();
        if let Some(gate) = gate {
            let closing = handle.clone();
            let first = tokio::spawn(async move { closing.close(CallContext::default()).await });
            gate.0.notified().await;
            let context = CallContext::default();
            let cancellation = context.cancellation.clone();
            let second = handle.close(context);
            tokio::pin!(second);
            tokio::select! {
                biased;
                _ = &mut second => panic!("concurrent close did not join the running attempt"),
                _ = std::future::ready(()) => {}
            }
            cancellation.cancel();
            assert_eq!(second.await.unwrap_err().code, "canceled");
            assert_eq!(attempts.load(Ordering::SeqCst), 1);
            let shutdown = routes.shutdown();
            tokio::pin!(shutdown);
            tokio::select! {
                biased;
                _ = &mut shutdown => panic!("shutdown completed while resource cleanup is running"),
                _ = std::future::ready(()) => {}
            }
            gate.1.notify_one();
            assert_eq!(first.await.unwrap().unwrap_err().code, "deadline_exceeded");
            shutdown.await.unwrap();
        } else {
            assert_eq!(
                handle.close(CallContext::default()).await.unwrap_err().code,
                "deadline_exceeded"
            );
            let result = routes
                .invoke(
                    CallContext::default(),
                    Call {
                        method,
                        receiver: None,
                        arguments: vec![],
                    },
                )
                .await;
            assert!(matches!(result, Err(error) if error.code == "resource_exhausted"));
        }
        assert!(handle.reference(&routes).is_err());
        if scenario.retry {
            handle.close(CallContext::default()).await.unwrap();
        }
        assert_eq!(
            routes.shutdown().await.is_err(),
            scenario.terminal_error,
            "{}",
            scenario.name
        );
        assert_eq!(
            handle.close(CallContext::default()).await.is_err(),
            scenario.terminal_error,
            "{}",
            scenario.name
        );
        assert_eq!(
            attempts.load(Ordering::SeqCst),
            scenario.attempts,
            "{}",
            scenario.name
        );
    }
}
