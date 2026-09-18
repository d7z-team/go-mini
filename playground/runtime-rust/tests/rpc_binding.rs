#![cfg(feature = "rpc")]
use mini_go::rpc::*;
use std::sync::{
    Arc, Mutex, Weak,
    atomic::{AtomicUsize, Ordering},
};
use tokio::sync::Notify;

fn method(name: &str) -> Method {
    Method {
        id: format!("sample.Service.{name}"),
        service: "sample.Service".into(),
        name: name.into(),
        contract_hash: "a".repeat(64),
        resource_type_hash: String::new(),
    }
}
struct Service {
    closed: Arc<AtomicUsize>,
    released: Arc<AtomicUsize>,
    started: Arc<Notify>,
}
impl Provider for Service {
    fn contract(&self) -> Contract {
        Contract::new(vec![method("open"), method("wait"), method("pair")])
    }
    fn bind(
        &self,
        _: CallContext,
        _: BindRequest,
    ) -> BoxFuture<'_, Result<Arc<dyn ProviderLease>>> {
        Box::pin(async {
            Ok(Arc::new(Service {
                closed: self.closed.clone(),
                released: self.released.clone(),
                started: self.started.clone(),
            }) as Arc<dyn ProviderLease>)
        })
    }
}
impl ProviderLease for Service {
    fn invoke(
        &self,
        context: CallContext,
        method: Method,
        arguments: Vec<Value>,
    ) -> BoxFuture<'_, Result<ProviderResult>> {
        Box::pin(async move {
            let value = context.export(Arc::new(Object(self.released.clone())), "b".repeat(64))?;
            if method.name == "pair" {
                let second =
                    context.export(Arc::new(Object(self.released.clone())), "b".repeat(64))?;
                return Ok(ProviderResult::new(vec![value, second]));
            }
            if method.name == "wait" {
                for argument in &arguments {
                    if let Data::Resource(reference) = &argument.data {
                        context.resolve(reference)?;
                    }
                }
                self.started.notify_one();
                std::future::pending::<()>().await;
            }
            Ok(ProviderResult::new(vec![value]))
        })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            self.closed.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }
}

#[tokio::test(flavor = "current_thread")]
async fn partial_export_failure_reclaims_earlier_exports_and_quota() {
    let released = Arc::new(AtomicUsize::new(0));
    let provider = Arc::new(Service {
        closed: Arc::new(AtomicUsize::new(0)),
        released: released.clone(),
        started: Arc::new(Notify::new()),
    });
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits {
            max_resources: 1,
            ..Default::default()
        },
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    let result = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("pair"),
                receiver: None,
                arguments: vec![],
            },
        )
        .await;
    assert_eq!(result.err().unwrap().code, "resource_exhausted");
    assert_eq!(released.load(Ordering::SeqCst), 1);
    assert_eq!(routes.resource_count(), 0);
    routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("open"),
                receiver: None,
                arguments: vec![],
            },
        )
        .await
        .unwrap()
        .discard()
        .await
        .unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 2);
    routes.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn resource_arguments_remain_open_until_the_call_finishes() {
    let released = Arc::new(AtomicUsize::new(0));
    let started = Arc::new(Notify::new());
    let provider = Arc::new(Service {
        released: released.clone(),
        closed: Arc::new(AtomicUsize::new(0)),
        started: started.clone(),
    });
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    let values = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("open"),
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
    let Data::Resource(reference) = &values[0].data else {
        panic!("resource missing")
    };
    let handle = routes.bind_resource(reference.clone()).unwrap();
    let owner = routes.clone();
    let call = tokio::spawn(async move {
        owner
            .invoke(
                CallContext::default(),
                Call {
                    method: method("wait"),
                    receiver: None,
                    arguments: values,
                },
            )
            .await
    });
    started.notified().await;
    let mut closing = Box::pin(handle.close(CallContext::default()));
    assert!(
        std::future::poll_fn(|cx| std::task::Poll::Ready(closing.as_mut().poll(cx).is_pending()))
            .await
    );
    assert_eq!(released.load(Ordering::SeqCst), 0);
    call.abort();
    let _ = call.await;
    closing.await.unwrap();
    routes.shutdown().await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 2);
}
struct Object(Arc<AtomicUsize>);

struct WaitingProvider {
    started: Arc<Notify>,
    finished: Arc<Notify>,
}
struct NotifyOnDrop(Arc<Notify>);
impl Drop for NotifyOnDrop {
    fn drop(&mut self) {
        self.0.notify_one();
    }
}
impl Provider for WaitingProvider {
    fn contract(&self) -> Contract {
        Contract::new(vec![method("wait")])
    }
    fn bind(
        &self,
        _: CallContext,
        _: BindRequest,
    ) -> BoxFuture<'_, Result<Arc<dyn ProviderLease>>> {
        Box::pin(async move {
            let _finished = NotifyOnDrop(self.finished.clone());
            self.started.notify_one();
            std::future::pending().await
        })
    }
}

#[tokio::test(flavor = "current_thread")]
async fn abandoned_bind_cancels_the_provider_future() {
    let started = Arc::new(Notify::new());
    let finished = Arc::new(Notify::new());
    let provider = Arc::new(WaitingProvider {
        started: started.clone(),
        finished: finished.clone(),
    });
    let request = BindRequest::new(provider.contract());
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider],
    )
    .unwrap();
    let task = tokio::spawn(async move { binder.bind(CallContext::default(), request).await });
    started.notified().await;
    task.abort();
    let _ = task.await;
    tokio::time::timeout(std::time::Duration::from_secs(2), finished.notified())
        .await
        .unwrap();
}
impl Resource for Object {
    fn invoke(
        &self,
        _: CallContext,
        _: String,
        _: Vec<Value>,
    ) -> BoxFuture<'_, Result<Vec<Value>>> {
        Box::pin(async { Ok(Vec::new()) })
    }
    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            self.0.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }
}

#[tokio::test(flavor = "current_thread")]
async fn result_handoffs_and_shared_resource_close() {
    let released = Arc::new(AtomicUsize::new(0));
    let closed = Arc::new(AtomicUsize::new(0));
    let provider = Arc::new(Service {
        released: released.clone(),
        closed: closed.clone(),
        started: Arc::new(Notify::new()),
    });
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    let call = || Call {
        method: method("open"),
        receiver: None,
        arguments: Vec::new(),
    };
    routes
        .invoke(CallContext::default(), call())
        .await
        .unwrap()
        .discard()
        .await
        .unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 1);
    let pending = routes.invoke(CallContext::default(), call()).await.unwrap();
    let Data::Resource(reference) = &pending.values[0].data else {
        panic!("resource missing")
    };
    let handle = routes.bind_resource(reference.clone()).unwrap();
    let alias = routes.bind_resource(reference.clone()).unwrap();
    assert!(Arc::ptr_eq(&handle, &alias));
    pending.accept().await.unwrap().consume();
    drop(alias);
    assert_eq!(released.load(Ordering::SeqCst), 1);
    handle.close(CallContext::default()).await.unwrap();
    handle.close(CallContext::default()).await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 2);
    // Accept followed by failed delivery still releases that reply's new object.
    drop(
        routes
            .invoke(CallContext::default(), call())
            .await
            .unwrap()
            .accept()
            .await
            .unwrap(),
    );
    routes.shutdown().await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 3);
    assert_eq!(closed.load(Ordering::SeqCst), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn dropped_invocation_releases_exports_and_binding() {
    let released = Arc::new(AtomicUsize::new(0));
    let closed = Arc::new(AtomicUsize::new(0));
    let started = Arc::new(Notify::new());
    let provider = Arc::new(Service {
        released: released.clone(),
        closed: closed.clone(),
        started: started.clone(),
    });
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    let owner = routes.clone();
    let task = tokio::spawn(async move {
        owner
            .invoke(
                CallContext::default(),
                Call {
                    method: method("wait"),
                    receiver: None,
                    arguments: Vec::new(),
                },
            )
            .await
    });
    started.notified().await;
    task.abort();
    let _ = task.await;
    routes.shutdown().await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 1);
    assert_eq!(closed.load(Ordering::SeqCst), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn result_quota_is_recovered_after_discard_and_shutdown() {
    let released = Arc::new(AtomicUsize::new(0));
    let closed = Arc::new(AtomicUsize::new(0));
    let provider = Arc::new(Service {
        released: released.clone(),
        closed: closed.clone(),
        started: Arc::new(Notify::new()),
    });
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits {
            max_pending_results: 1,
            max_pending_calls: 2,
            max_resources: 1,
            ..Limits::default()
        },
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    let call = Call {
        method: method("open"),
        receiver: None,
        arguments: Vec::new(),
    };
    let pending = routes
        .invoke(CallContext::default(), call.clone())
        .await
        .unwrap();
    assert_eq!(routes.resource_count(), 1);
    assert_eq!(
        routes
            .invoke(CallContext::default(), call.clone())
            .await
            .err()
            .unwrap()
            .code,
        "resource_exhausted"
    );
    pending.discard().await.unwrap();
    assert_eq!(released.load(Ordering::SeqCst), 1);
    let pending = routes.invoke(CallContext::default(), call).await.unwrap();
    drop(pending);
    tokio::time::timeout(std::time::Duration::from_secs(1), async {
        while released.load(Ordering::SeqCst) != 2 {
            tokio::task::yield_now().await;
        }
    })
    .await
    .unwrap();
    routes.shutdown().await.unwrap();
    assert_eq!(routes.resource_count(), 0);
    assert_eq!(released.load(Ordering::SeqCst), 2);
    assert_eq!(closed.load(Ordering::SeqCst), 1);
}

const RESOURCE_HASH: &str = "b";

#[tokio::test(flavor = "current_thread")]
async fn borrowed_argument_remains_resolvable_while_close_waits() {
    let entered = Arc::new(Notify::new());
    let resume = Arc::new(Notify::new());
    let closed = Arc::new(AtomicUsize::new(0));
    let export_closed = closed.clone();
    let handler_entered = entered.clone();
    let handler_resume = resume.clone();
    let provider = Arc::new(
        StaticProvider::new(vec![
            MethodBinding {
                method: method("open"),
                invoke: Some(Arc::new(move |context, _| {
                    let closed = export_closed.clone();
                    Box::pin(async move {
                        Ok(vec![
                            context.export(Arc::new(Object(closed)), "b".repeat(64))?,
                        ])
                    })
                })),
            },
            MethodBinding {
                method: method("use"),
                invoke: Some(Arc::new(move |context, values| {
                    let entered = handler_entered.clone();
                    let resume = handler_resume.clone();
                    Box::pin(async move {
                        entered.notify_one();
                        resume.notified().await;
                        let Data::Resource(reference) = &values[0].data else {
                            panic!("missing resource")
                        };
                        context.resolve(reference)?;
                        Ok(Vec::new())
                    })
                })),
            },
        ])
        .unwrap(),
    );
    let contract = provider.contract();
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
    let values = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("open"),
                receiver: None,
                arguments: Vec::new(),
            },
        )
        .await
        .unwrap()
        .accept()
        .await
        .unwrap()
        .consume();
    let Data::Resource(reference) = &values[0].data else {
        panic!("missing resource")
    };
    let handle = routes.bind_resource(reference.clone()).unwrap();
    let invoke_routes = routes.clone();
    let invocation = tokio::spawn(async move {
        invoke_routes
            .invoke(
                CallContext::default(),
                Call {
                    method: method("use"),
                    receiver: None,
                    arguments: values,
                },
            )
            .await
    });
    entered.notified().await;
    let drop_handle = handle.clone();
    let dropping = tokio::spawn(async move { drop_handle.close(CallContext::default()).await });
    tokio::time::timeout(std::time::Duration::from_secs(1), async {
        while handle.reference(&routes).is_ok() {
            tokio::task::yield_now().await;
        }
    })
    .await
    .unwrap();
    assert_eq!(closed.load(Ordering::SeqCst), 0);
    resume.notify_one();
    invocation.await.unwrap().unwrap().discard().await.unwrap();
    dropping.await.unwrap().unwrap();
    assert_eq!(closed.load(Ordering::SeqCst), 1);
    routes.shutdown().await.unwrap();
}

fn resource_method(name: &str) -> Method {
    Method {
        id: format!("sample.Service.{name}"),
        service: "sample.Service".into(),
        name: name.into(),
        contract_hash: "a".repeat(64),
        resource_type_hash: RESOURCE_HASH.repeat(64),
    }
}

struct BorrowProvider {
    started: Arc<AtomicUsize>,
    release: Arc<Notify>,
    close_count: Arc<AtomicUsize>,
    self_close: bool,
    routes: Arc<Mutex<Weak<RouteSet>>>,
    self_close_error: Arc<Mutex<Option<String>>>,
}

impl BorrowProvider {
    fn contract() -> Contract {
        Contract::new(vec![
            method("open"),
            resource_method("use"),
            resource_method("self_close"),
        ])
    }
}

impl Provider for BorrowProvider {
    fn contract(&self) -> Contract {
        Self::contract()
    }

    fn bind(
        &self,
        _: CallContext,
        _: BindRequest,
    ) -> BoxFuture<'_, Result<Arc<dyn ProviderLease>>> {
        Box::pin(async {
            Ok(Arc::new(BorrowLease {
                started: self.started.clone(),
                release: self.release.clone(),
                close_count: self.close_count.clone(),
                self_close: self.self_close,
                routes: self.routes.clone(),
                self_close_error: self.self_close_error.clone(),
            }) as Arc<dyn ProviderLease>)
        })
    }
}

struct BorrowLease {
    started: Arc<AtomicUsize>,
    release: Arc<Notify>,
    close_count: Arc<AtomicUsize>,
    self_close: bool,
    routes: Arc<Mutex<Weak<RouteSet>>>,
    self_close_error: Arc<Mutex<Option<String>>>,
}

impl ProviderLease for BorrowLease {
    fn invoke(
        &self,
        context: CallContext,
        method: Method,
        _: Vec<Value>,
    ) -> BoxFuture<'_, Result<ProviderResult>> {
        Box::pin(async move {
            if method.name != "open" {
                return Err(Status::new("unimplemented", "unexpected service method"));
            }
            let first = Arc::new(BorrowResource {
                started: self.started.clone(),
                release: self.release.clone(),
                close_count: self.close_count.clone(),
                self_close: self.self_close,
                routes: self.routes.clone(),
                self_close_error: self.self_close_error.clone(),
                reference: Mutex::new(None),
            });
            let first_value = context.export(first.clone(), RESOURCE_HASH.repeat(64))?;
            let Data::Resource(first_reference) = &first_value.data else {
                return Err(Status::new("internal", "resource export returned a scalar"));
            };
            *first.reference.lock().unwrap() = Some(first_reference.clone());
            if self.self_close {
                return Ok(ProviderResult::new(vec![first_value]));
            }
            let second = Arc::new(BorrowResource {
                started: self.started.clone(),
                release: self.release.clone(),
                close_count: self.close_count.clone(),
                self_close: false,
                routes: self.routes.clone(),
                self_close_error: self.self_close_error.clone(),
                reference: Mutex::new(None),
            });
            let second_value = context.export(second.clone(), RESOURCE_HASH.repeat(64))?;
            let Data::Resource(second_reference) = &second_value.data else {
                return Err(Status::new("internal", "resource export returned a scalar"));
            };
            *second.reference.lock().unwrap() = Some(second_reference.clone());
            Ok(ProviderResult::new(vec![first_value, second_value]))
        })
    }

    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async { Ok(()) })
    }
}

struct BorrowResource {
    started: Arc<AtomicUsize>,
    release: Arc<Notify>,
    close_count: Arc<AtomicUsize>,
    self_close: bool,
    routes: Arc<Mutex<Weak<RouteSet>>>,
    self_close_error: Arc<Mutex<Option<String>>>,
    reference: Mutex<Option<ResourceRef>>,
}

impl Resource for BorrowResource {
    fn invoke(
        &self,
        context: CallContext,
        _: String,
        _: Vec<Value>,
    ) -> BoxFuture<'_, Result<Vec<Value>>> {
        Box::pin(async move {
            if self.self_close {
                let routes = self
                    .routes
                    .lock()
                    .unwrap()
                    .upgrade()
                    .ok_or_else(|| Status::new("unavailable", "test routes dropped"))?;
                let reference = self
                    .reference
                    .lock()
                    .unwrap()
                    .clone()
                    .ok_or_else(|| Status::new("internal", "resource reference missing"))?;
                let error = routes
                    .drop_resource(context, reference)
                    .await
                    .expect_err("a borrowed resource must not close synchronously");
                *self.self_close_error.lock().unwrap() = Some(error.code);
                return Ok(Vec::new());
            }
            self.started.fetch_add(1, Ordering::SeqCst);
            self.release.notified().await;
            Ok(Vec::new())
        })
    }

    fn close(&self) -> BoxFuture<'_, Result<()>> {
        Box::pin(async move {
            self.close_count.fetch_add(1, Ordering::SeqCst);
            Ok(())
        })
    }
}

async fn bind_borrow_provider(provider: Arc<BorrowProvider>) -> Arc<RouteSet> {
    let binder = LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider.clone()],
    )
    .unwrap();
    let routes = binder
        .bind(
            CallContext::default(),
            BindRequest::new(provider.contract()),
        )
        .await
        .unwrap();
    *provider.routes.lock().unwrap() = Arc::downgrade(&routes);
    routes
}

#[tokio::test(flavor = "current_thread")]
async fn multi_resource_borrows_wait_in_reference_order_without_deadlock() {
    let provider = Arc::new(BorrowProvider {
        started: Arc::new(AtomicUsize::new(0)),
        release: Arc::new(Notify::new()),
        close_count: Arc::new(AtomicUsize::new(0)),
        self_close: false,
        routes: Arc::new(Mutex::new(Weak::new())),
        self_close_error: Arc::new(Mutex::new(None)),
    });
    let routes = bind_borrow_provider(provider.clone()).await;
    let values = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("open"),
                receiver: None,
                arguments: Vec::new(),
            },
        )
        .await
        .unwrap()
        .accept()
        .await
        .unwrap()
        .consume();
    let Data::Resource(first) = &values[0].data else {
        panic!("first resource missing")
    };
    let Data::Resource(second) = &values[1].data else {
        panic!("second resource missing")
    };
    let first = first.clone();
    let second = second.clone();
    let first_call = {
        let routes = routes.clone();
        let first = first.clone();
        let second = second.clone();
        tokio::spawn(async move {
            routes
                .invoke(
                    CallContext::default(),
                    Call {
                        method: resource_method("use"),
                        receiver: Some(first),
                        arguments: vec![Value::resource(second)],
                    },
                )
                .await
        })
    };
    while provider.started.load(Ordering::SeqCst) != 1 {
        tokio::task::yield_now().await;
    }
    let second_call = {
        let routes = routes.clone();
        let first = first.clone();
        let second = second.clone();
        tokio::spawn(async move {
            routes
                .invoke(
                    CallContext::default(),
                    Call {
                        method: resource_method("use"),
                        receiver: Some(second),
                        arguments: vec![Value::resource(first)],
                    },
                )
                .await
        })
    };
    while provider.started.load(Ordering::SeqCst) != 2 {
        tokio::task::yield_now().await;
    }
    provider.release.notify_one();
    provider.release.notify_one();
    first_call.await.unwrap().unwrap().discard().await.unwrap();
    second_call.await.unwrap().unwrap().discard().await.unwrap();

    routes
        .drop_resource(CallContext::default(), first)
        .await
        .unwrap();
    routes
        .drop_resource(CallContext::default(), second)
        .await
        .unwrap();
    routes.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn a_resource_cannot_close_from_its_own_handler_scope() {
    let self_close_error = Arc::new(Mutex::new(None));
    let provider = Arc::new(BorrowProvider {
        started: Arc::new(AtomicUsize::new(0)),
        release: Arc::new(Notify::new()),
        close_count: Arc::new(AtomicUsize::new(0)),
        self_close: true,
        routes: Arc::new(Mutex::new(Weak::new())),
        self_close_error: self_close_error.clone(),
    });
    let routes = bind_borrow_provider(provider.clone()).await;
    let values = routes
        .invoke(
            CallContext::default(),
            Call {
                method: method("open"),
                receiver: None,
                arguments: Vec::new(),
            },
        )
        .await
        .unwrap()
        .accept()
        .await
        .unwrap()
        .consume();
    let Data::Resource(reference) = &values[0].data else {
        panic!("resource missing")
    };
    let reference = reference.clone();
    routes
        .invoke(
            CallContext::default(),
            Call {
                method: resource_method("self_close"),
                receiver: Some(reference.clone()),
                arguments: Vec::new(),
            },
        )
        .await
        .unwrap()
        .accept()
        .await
        .unwrap()
        .consume();
    assert_eq!(
        self_close_error.lock().unwrap().as_deref(),
        Some("failed_precondition")
    );
    routes
        .drop_resource(CallContext::default(), reference)
        .await
        .unwrap();
    routes.shutdown().await.unwrap();
}
