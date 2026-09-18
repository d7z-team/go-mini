#[path = "../../playground/runtime-rust/tests/support/gateway_peer.rs"]
mod gateway_peer;
#[path = "../../playground/runtime-rust/tests/support/laboratory.rs"]
mod laboratory;
#[path = "../../testdata/rpc/generated/rust/service.rs"]
pub mod rpcservice;
#[path = "../../testdata/rpc/generated/rust/types.rs"]
pub mod rpctypes;
#[path = "../../playground/runtime-rust/tests/support/scalars.rs"]
mod scalars;
use mini_go::{ffi::Cancellation, rpc::*};
use std::{
    sync::{
        Arc,
        atomic::{AtomicUsize, Ordering},
    },
    time::{Duration, Instant},
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{
        TcpStream,
        tcp::{OwnedReadHalf, OwnedWriteHalf},
    },
    sync::Mutex,
};

struct Connection {
    read: Mutex<OwnedReadHalf>,
    write: Mutex<OwnedWriteHalf>,
    closed: Cancellation,
}
impl MessageConn for Connection {
    fn read(&self) -> BoxFuture<'_, Result<Vec<u8>>> {
        Box::pin(async {
            tokio::select! { _=self.closed.cancelled()=>Err(Status::new("unavailable","connection closed")), result=async {
                let mut read=self.read.lock().await;let size=read.read_u32().await.map_err(io_error)?;
                if size>256{return Err(Status::new("protocol","peer fragment exceeds limit"));}
                let mut data=vec![0;size as usize];read.read_exact(&mut data).await.map_err(io_error)?;Ok(data)
            }=>result }
        })
    }
    fn write(&self, data: Vec<u8>) -> BoxFuture<'_, Result<()>> {
        Box::pin(async move {
            if data.len() > 256 {
                return Err(Status::new("protocol", "peer fragment exceeds limit"));
            }
            tokio::select! {_=self.closed.cancelled()=>Err(Status::new("unavailable","connection closed")),result=async {
                let mut write=self.write.lock().await;write.write_u32(data.len() as u32).await.map_err(io_error)?;write.write_all(&data).await.map_err(io_error)
            }=>result}
        })
    }
    fn close(&self) {
        self.closed.cancel();
    }
}
fn io_error(error: std::io::Error) -> Status {
    Status::new("unavailable", error.to_string())
}

async fn exercise(endpoint: &Endpoint) -> Result<serde_json::Value> {
    use rpcservice::*;
    use rpctypes::*;
    let context = CallContext::with_deadline(Instant::now() + Duration::from_secs(10));
    let client = LaboratoryClient::bind(context.clone(), endpoint, BindOptions::default()).await?;
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
                label: "中".repeat(512),
                mode: Mode(99),
            },
        };
        let actual = client.echo(context.clone(), packet.clone()).await?.0;
        if actual.encode()? != packet.encode()?
            || actual.scalars.as_ref().unwrap().f32.to_bits() != 0x80000000
            || actual.scalars.as_ref().unwrap().c128.im.to_bits() != (-0.0f64).to_bits()
        {
            return Err(Status::new("internal", "packet round trip mismatch"));
        }
    }
    let node = Node {
        value: 1,
        next: Some(Box::new(Node {
            value: 2,
            next: None,
        })),
    };
    if client.tree(context.clone(), node.clone()).await?.0 != node {
        return Err(Status::new("internal", "recursive message mismatch"));
    }
    let (counter, details) = client.open(context.clone(), 40).await?;
    let counter = counter.ok_or_else(|| Status::new("internal", "counter missing"))?;
    if details.label != "counter"
        || counter.add(context.clone(), 2).await?.0 != 42
        || client.read(context.clone(), Some(counter.clone())).await?.0 != 42
    {
        return Err(Status::new("internal", "resource mismatch"));
    }
    client.close().await?;
    if counter.add(context.clone(), 0).await?.0 != 42 {
        return Err(Status::new("internal", "retained resource mismatch"));
    }
    counter.close(context.clone()).await?;
    laboratory::exercise_resource_retry(context.clone(), endpoint).await?;
    let client = LaboratoryClient::bind(context, endpoint, BindOptions::default()).await?;
    let wait = CallContext::with_deadline(Instant::now() + Duration::from_millis(30));
    match client.wait(wait).await {
        Err(error) if error.code == "deadline_exceeded" => {}
        other => {
            return Err(Status::new(
                "internal",
                format!("Wait cancellation: {other:?}"),
            ));
        }
    }
    client.close().await?;
    Ok(serde_json::json!({"echo":3,"recursive":true,"resource":42,"canceled":true}))
}

#[tokio::main(flavor = "current_thread")]
async fn main() -> std::result::Result<(), Box<dyn std::error::Error>> {
    let arguments = std::env::args().collect::<Vec<_>>();
    if arguments.len() != 3 {
        return Err("usage: mini-go-rpc-peer-rust server|client address".into());
    }
    if matches!(arguments[1].as_str(), "gateway-server" | "gateway-client") {
        return gateway_peer::run(&arguments[1], &arguments[2]).await;
    }
    let server = match arguments[1].as_str() {
        "server" => true,
        "client" => false,
        _ => return Err("invalid peer mode".into()),
    };
    let stream = if server {
        let listener = tokio::net::TcpListener::bind(&arguments[2]).await?;
        println!(
            "{}",
            serde_json::json!({"address":listener.local_addr()?.to_string()})
        );
        tokio::time::timeout(Duration::from_secs(15), listener.accept())
            .await??
            .0
    } else {
        TcpStream::connect(&arguments[2]).await?
    };
    let (read, write) = stream.into_split();
    let closed = Cancellation::default();
    let released = Arc::new(AtomicUsize::new(0));
    let provider =
        rpcservice::laboratory_provider(Arc::new(laboratory::Laboratory(released.clone())))?;
    let binder = Arc::new(LocalBinder::new(
        tokio::runtime::Handle::current(),
        Limits::default(),
        vec![provider],
    )?);
    let endpoint = Endpoint::open(
        tokio::runtime::Handle::current(),
        Arc::new(Connection {
            read: Mutex::new(read),
            write: Mutex::new(write),
            closed: closed.clone(),
        }),
        Some(binder),
        EndpointOptions {
            limits: Limits {
                max_frame_bytes: 256,
                ..Limits::default()
            },
            ..EndpointOptions::default()
        },
    )?;
    let report = exercise(&endpoint).await?;
    println!("{report}");
    if server {
        tokio::time::timeout(Duration::from_secs(15), closed.cancelled()).await?;
    } else {
        tokio::task::spawn_blocking(|| {
            let mut line = String::new();
            std::io::stdin().read_line(&mut line)
        })
        .await??;
    }
    endpoint.shutdown().await?;
    if released.load(Ordering::SeqCst) != 2 {
        return Err(format!("resource close count {}", released.load(Ordering::SeqCst)).into());
    }
    Ok(())
}
