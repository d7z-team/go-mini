#![cfg(feature = "rpc-gateway")]
use mini_go::rpc::{gateway::*, publication::*, router::*, *};
use std::{
    sync::Arc,
    time::{Duration, Instant},
};

#[tokio::test(flavor = "current_thread")]
async fn websocket_publication_catalog_and_disconnect_release_routes() {
    let runtime = tokio::runtime::Handle::current();
    let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
    let registry = PublicationRegistry::new(runtime.clone(), router.clone());
    router
        .register(
            registry.catalog_provider().unwrap(),
            RegistrationOptions::default(),
        )
        .unwrap();
    let server = Server::new(
        runtime.clone(),
        router.clone(),
        ServerOptions {
            publications: Some(registry),
            ..ServerOptions::default()
        },
    )
    .unwrap();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let serving = server.clone();
    let task = tokio::spawn(async move { serving.serve(listener).await });
    let method = Method {
        id: "sample.Echo.echo".into(),
        service: "sample.Echo".into(),
        name: "echo".into(),
        contract_hash: "a".repeat(64),
        resource_type_hash: String::new(),
    };
    let echo: Arc<dyn Provider> = Arc::new(
        StaticProvider::new(vec![MethodBinding {
            method: method.clone(),
            invoke: Some(Arc::new(|_, values| Box::pin(async { Ok(values) }))),
        }])
        .unwrap(),
    );
    let publisher = Publisher::new(
        "worker".into(),
        1,
        vec![PublicationProvider {
            id: "echo".into(),
            provider: echo.clone(),
            bundles: Vec::new(),
        }],
    )
    .unwrap();
    let services = Arc::new(
        LocalBinder::new(
            runtime.clone(),
            Limits::default(),
            vec![echo.clone(), publisher.provider().unwrap()],
        )
        .unwrap(),
    );
    let publishing = dial(
        runtime.clone(),
        &format!("ws://{address}/publish"),
        DialOptions {
            binder: Some(services),
            ..DialOptions::default()
        },
    )
    .await
    .unwrap();
    publisher
        .wait_published(CallContext::with_deadline(
            Instant::now() + Duration::from_secs(5),
        ))
        .await
        .unwrap();
    let consumer = dial(
        runtime,
        &format!("ws://{address}/rpc"),
        DialOptions::default(),
    )
    .await
    .unwrap();
    let catalog = control::CatalogClient::bind(
        CallContext::default(),
        consumer.as_ref(),
        BindOptions::default(),
    )
    .await
    .unwrap();
    let (snapshot,) = catalog.snapshot(CallContext::default()).await.unwrap();
    assert_eq!(snapshot.gateway_id, router.revision().router_id);
    let routes = consumer
        .bind(CallContext::default(), BindRequest::new(echo.contract()))
        .await
        .unwrap();
    let values = vec![Value::new(
        "string",
        Data::String("through publication".repeat(100)),
    )];
    let call = Call {
        method,
        receiver: None,
        arguments: values.clone(),
    };
    assert_eq!(
        routes
            .invoke(CallContext::default(), call.clone())
            .await
            .unwrap()
            .accept()
            .await
            .unwrap()
            .consume(),
        values
    );
    publishing.shutdown().await.unwrap();
    assert!(
        routes
            .invoke(
                CallContext::with_deadline(Instant::now() + Duration::from_secs(2)),
                call
            )
            .await
            .is_err()
    );
    routes.shutdown().await.unwrap();
    catalog.close().await.unwrap();
    consumer.shutdown().await.unwrap();
    tokio::time::timeout(Duration::from_secs(5), server.shutdown())
        .await
        .unwrap()
        .unwrap();
    task.await.unwrap().unwrap();
    router.shutdown().await.unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn tls_authentication_and_bounded_handshake() {
    let runtime = tokio::runtime::Handle::current();
    let certificate = rustls::pki_types::CertificateDer::from(
        include_bytes!("../../../testdata/rpc/tls/localhost.der").to_vec(),
    );
    let key = rustls::pki_types::PrivatePkcs8KeyDer::from(
        include_bytes!("../../../testdata/rpc/tls/localhost-key.der").to_vec(),
    );
    let tls = rustls::ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(vec![certificate.clone()], key.into())
        .unwrap();
    let mut roots = rustls::RootCertStore::empty();
    roots.add(certificate).unwrap();
    let client_tls = Arc::new(
        rustls::ClientConfig::builder()
            .with_root_certificates(roots)
            .with_no_client_auth(),
    );
    let binder =
        Arc::new(LocalBinder::new(runtime.clone(), Limits::default(), Vec::new()).unwrap());
    let server = Server::new(
        runtime.clone(),
        binder,
        ServerOptions {
            tls: Some(Arc::new(tls)),
            authenticate: Some(Arc::new(|request| {
                if request
                    .headers()
                    .get("authorization")
                    .and_then(|value| value.to_str().ok())
                    != Some("test-token")
                {
                    return Err(Status::new("permission_denied", "credential required"));
                }
                Ok(PeerInfo {
                    identity: "verified".into(),
                    ..PeerInfo::default()
                })
            })),
            ..ServerOptions::default()
        },
    )
    .unwrap();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = format!("wss://{}/rpc", listener.local_addr().unwrap());
    let serving = server.clone();
    let task = tokio::spawn(async move { serving.serve(listener).await });
    assert!(
        dial(
            runtime.clone(),
            &address,
            DialOptions {
                tls: Some(client_tls.clone()),
                ..DialOptions::default()
            }
        )
        .await
        .is_err()
    );
    let mut options = DialOptions {
        tls: Some(client_tls),
        ..DialOptions::default()
    };
    options
        .headers
        .insert("authorization", "test-token".parse().unwrap());
    let endpoint = dial(runtime.clone(), &address, options).await.unwrap();
    endpoint.ping(CallContext::default()).await.unwrap();
    endpoint.shutdown().await.unwrap();
    server.shutdown().await.unwrap();
    task.await.unwrap().unwrap();
    let stalled = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let result = dial(
        runtime,
        &format!("ws://{}/rpc", stalled.local_addr().unwrap()),
        DialOptions {
            handshake_timeout: Duration::from_millis(20),
            ..DialOptions::default()
        },
    )
    .await;
    assert_eq!(result.err().unwrap().code, "deadline_exceeded");
}

#[cfg(unix)]
#[tokio::test(flavor = "current_thread")]
async fn unix_socket_transport() {
    let runtime = tokio::runtime::Handle::current();
    let socket = std::env::temp_dir().join(format!("mi-rpc-{}.sock", std::process::id()));
    let listener = tokio::net::UnixListener::bind(&socket).unwrap();
    struct SocketPath(std::path::PathBuf);
    impl Drop for SocketPath {
        fn drop(&mut self) {
            let _ = std::fs::remove_file(&self.0);
        }
    }
    let _socket = SocketPath(socket.clone());
    let binder =
        Arc::new(LocalBinder::new(runtime.clone(), Limits::default(), Vec::new()).unwrap());
    let server = Server::new(runtime.clone(), binder, ServerOptions::default()).unwrap();
    let serving = server.clone();
    let task = tokio::spawn(async move { serving.serve_unix(listener).await });
    let endpoint = dial(
        runtime,
        &format!("ws+unix://{}", socket.display()),
        DialOptions::default(),
    )
    .await
    .unwrap();
    endpoint.ping(CallContext::default()).await.unwrap();
    endpoint.shutdown().await.unwrap();
    server.shutdown().await.unwrap();
    task.await.unwrap().unwrap();
}

#[test]
fn gateway_addresses_preserve_explicit_paths() {
    assert_eq!(Address::parse("ws://localhost:8080").unwrap().path, "/rpc");
    assert_eq!(Address::parse("ws://localhost:8080/").unwrap().path, "/");
    assert_eq!(
        Address::parse("ws+unix:///tmp/rpc%20test.sock")
            .unwrap()
            .socket_path
            .as_deref(),
        Some("/tmp/rpc test.sock")
    );
    assert!(Address::parse("ws://localhost/rpc?token=1").is_err());
    assert!(Address::parse("ws://user@localhost/rpc").is_err());
}

#[test]
fn shared_catalog_identities_and_repository_rollback() {
    let fixture: serde_json::Value =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/catalog.json")).unwrap();
    let source = &fixture["bundle"];
    let file = &source["files"][0];
    let bundle = MrpcBundle::new(
        source["import_path"].as_str().unwrap().into(),
        vec![MrpcFile {
            path: file["path"].as_str().unwrap().into(),
            text: file["text"].as_str().unwrap().into(),
            hash: String::new(),
        }],
    )
    .unwrap();
    assert_eq!(bundle.hash, source["hash"].as_str().unwrap());
    assert_eq!(bundle.files[0].hash, file["hash"].as_str().unwrap());
    let publication = &fixture["publication"];
    let provider = &publication["providers"][0];
    let method = &provider["contract"]["Methods"][0];
    let snapshot = control::PublicationSnapshot {
        protocol: publication["protocol"].as_str().unwrap().into(),
        process_id: publication["processID"].as_str().unwrap().into(),
        generation: 2,
        id: publication["id"].as_str().unwrap().into(),
        providers: Some(vec![control::PublishedProvider {
            id: "echo".into(),
            contract: control::Contract {
                protocol: CONTRACT_PROTOCOL.into(),
                methods: Some(vec![control::Method {
                    id: method["ID"].as_str().unwrap().into(),
                    service: method["Service"].as_str().unwrap().into(),
                    name: method["Name"].as_str().unwrap().into(),
                    contract_hash: method["ContractHash"].as_str().unwrap().into(),
                    resource_type_hash: String::new(),
                }]),
            },
            options: control::RegistrationOptions {
                name: "echo".into(),
                priority: 0,
                weight: 0,
                max_leases: 0,
                labels: Some(vec![control::RouteLabel {
                    name: "zone".into(),
                    value: "a".into(),
                }]),
            },
            references: Some(vec![control::ContractReference {
                import_path: bundle.import_path.clone(),
                hash: bundle.hash.clone(),
            }]),
        }]),
    };
    assert_eq!(
        normalize_snapshot(snapshot.clone()).unwrap().id,
        snapshot.id
    );
    let mut changed = snapshot;
    changed.providers.as_mut().unwrap()[0]
        .options
        .labels
        .as_mut()
        .unwrap()[0]
        .value = "changed".into();
    assert!(normalize_snapshot(changed).is_err());
    let repository = ContractRepository::default();
    repository.publish(vec![bundle.clone()]).unwrap();
    let mut corrupt = bundle.clone();
    corrupt.files[0].text.push('!');
    assert!(repository.publish(vec![corrupt]).is_err());
    assert_eq!(repository.resolve(&bundle.reference()).unwrap(), bundle);
}
