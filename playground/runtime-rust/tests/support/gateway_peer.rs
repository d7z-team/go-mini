use mini_go::rpc::{
    catalog::{self, MrpcBundle, MrpcFile},
    gateway,
    publication::*,
    router::*,
    *,
};
use std::{
    sync::{Arc, atomic::AtomicUsize},
    time::{Duration, Instant},
};

pub async fn run(mode: &str, address: &str) -> std::result::Result<(), Box<dyn std::error::Error>> {
    let runtime = tokio::runtime::Handle::current();
    if mode == "gateway-server" {
        let router = Arc::new(Router::new(runtime.clone(), Limits::default(), None));
        let registry = PublicationRegistry::with_options(
            runtime.clone(),
            router.clone(),
            PublicationRegistryOptions {
                drain_timeout: Duration::from_secs(1),
                ..Default::default()
            },
        )?;
        router.register(
            catalog::provider(router.clone())?,
            RegistrationOptions::default(),
        )?;
        let server = gateway::Server::new(
            runtime,
            router.clone(),
            gateway::ServerOptions {
                publications: Some(registry),
                ..Default::default()
            },
        )?;
        let listener = tokio::net::TcpListener::bind(address).await?;
        println!(
            "{}",
            serde_json::json!({"address":format!("ws://{}",listener.local_addr()?)})
        );
        let owner = server.clone();
        let task = tokio::spawn(async move { owner.serve(listener).await });
        tokio::task::spawn_blocking(|| {
            let mut line = String::new();
            std::io::stdin().read_line(&mut line)
        })
        .await??;
        server.shutdown().await?;
        task.await??;
        router.force_shutdown().await?;
        return Ok(());
    }
    let context = CallContext::with_deadline(Instant::now() + Duration::from_secs(15));
    let consumer = gateway::dial(
        runtime.clone(),
        &format!("{address}/rpc"),
        gateway::DialOptions::default(),
    )
    .await?;
    let catalog = catalog::Client::bind(context.clone(), consumer.as_ref()).await?;
    let mut snapshot = catalog.snapshot(context.clone()).await?;
    let bundle = MrpcBundle::new(
        "fixture/control".into(),
        vec![MrpcFile {
            path: "control.mrpc".into(),
            text: "package control\n".into(),
            hash: String::new(),
        }],
    )?;
    let provider = crate::rpcservice::laboratory_provider(Arc::new(
        crate::laboratory::Laboratory(Arc::new(AtomicUsize::new(0))),
    ))?;
    let mut connections = Vec::new();
    let mut pinned = Vec::new();
    for generation in [1, 1, 2] {
        let publisher = Publisher::new(
            "peer-worker".into(),
            generation,
            vec![PublicationProvider {
                id: String::new(),
                provider: provider.clone(),
                bundles: vec![bundle.clone()],
            }],
        )?;
        let binder = Arc::new(LocalBinder::new(
            runtime.clone(),
            Limits::default(),
            vec![provider.clone(), publisher.provider()?],
        )?);
        let endpoint = gateway::dial(
            runtime.clone(),
            &format!("{address}/publish"),
            gateway::DialOptions {
                binder: Some(binder),
                ..Default::default()
            },
        )
        .await?;
        publisher.wait_published(context.clone()).await?;
        connections.push(endpoint);
        snapshot = catalog.watch(context.clone(), &snapshot).await?;
        if snapshot.references.len() != 1 {
            return Err("catalog references mismatch".into());
        }
        if catalog
            .resolve(context.clone(), &snapshot.references[0])
            .await?
            .hash
            != bundle.hash
        {
            return Err("resolved another bundle".into());
        }
        pinned.push(
            crate::rpcservice::LaboratoryClient::bind(
                context.clone(),
                consumer.as_ref(),
                BindOptions::default(),
            )
            .await?,
        );
        for lease in &pinned {
            if lease
                .tree(
                    context.clone(),
                    crate::rpcservice::Node {
                        value: 42,
                        next: None,
                    },
                )
                .await?
                .0
                .value
                != 42
            {
                return Err("retained lease mismatch".into());
            }
        }
    }
    crate::laboratory::exercise_resource_retry(context.clone(), consumer.as_ref()).await?;
    for lease in pinned {
        lease.close().await?;
        lease.routes.shutdown().await?;
    }
    for endpoint in connections {
        endpoint.shutdown().await?;
    }
    while !snapshot.references.is_empty() {
        snapshot = catalog.watch(context.clone(), &snapshot).await?;
    }
    catalog.close().await?;
    consumer.shutdown().await?;
    println!(
        "{}",
        serde_json::json!({"publications":3,"resolved":true,"retained":true,"revoked":true})
    );
    Ok(())
}
