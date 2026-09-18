use mini_go::{
    ffi::Cancellation,
    rpc::{BoxFuture, MessageConn, Result, Status},
};
use serde::Serialize;
use std::{
    collections::{BTreeMap, VecDeque},
    sync::{
        Arc, Mutex,
        atomic::{AtomicUsize, Ordering},
    },
};
use tokio::sync::{mpsc, oneshot};

const MAX_BYTES: usize = 8 << 20;
const MAX_FRAMES: usize = 64;
#[derive(Serialize)]
pub struct Outbound {
    pub id: u32,
    pub payload: Vec<u8>,
}
struct Writes {
    next: u32,
    queue: VecDeque<Outbound>,
    pending: BTreeMap<u32, oneshot::Sender<Result<()>>>,
}
pub struct Transport {
    reads: tokio::sync::Mutex<mpsc::Receiver<Vec<u8>>>,
    sender: mpsc::Sender<Vec<u8>>,
    bytes: AtomicUsize,
    writes: Mutex<Writes>,
    closing: Cancellation,
}
impl Transport {
    pub fn new() -> Arc<Self> {
        let (sender, reads) = mpsc::channel(MAX_FRAMES);
        Arc::new(Self {
            reads: tokio::sync::Mutex::new(reads),
            sender,
            bytes: AtomicUsize::new(0),
            writes: Mutex::new(Writes {
                next: 0,
                queue: VecDeque::new(),
                pending: BTreeMap::new(),
            }),
            closing: Cancellation::default(),
        })
    }
    pub fn receive(&self, bytes: Vec<u8>) -> Result<()> {
        if self.closing.is_cancelled() {
            return Err(Status::new("unavailable", "WASM transport closed"));
        }
        if bytes.len() > 1 << 20 {
            self.close();
            return Err(Status::new("resource_exhausted", "WASM frame limit"));
        }
        let length = bytes.len();
        if self
            .bytes
            .fetch_add(length, Ordering::Relaxed)
            .saturating_add(length)
            > MAX_BYTES
        {
            self.bytes.fetch_sub(length, Ordering::Relaxed);
            self.close();
            return Err(Status::new("resource_exhausted", "WASM receive budget"));
        }
        if self.sender.try_send(bytes).is_err() {
            self.bytes.fetch_sub(length, Ordering::Relaxed);
            self.close();
            return Err(Status::new("resource_exhausted", "WASM receive queue"));
        }
        Ok(())
    }
    pub fn drain(&self) -> Vec<Outbound> {
        self.writes.lock().unwrap().queue.drain(..).collect()
    }
    pub fn sent(&self, id: u32) {
        if let Some(send) = self.writes.lock().unwrap().pending.remove(&id) {
            let _ = send.send(Ok(()));
        }
    }
}
impl MessageConn for Transport {
    fn read(&self) -> BoxFuture<'_, Result<Vec<u8>>> {
        Box::pin(async {
            let mut reads = self.reads.lock().await;
            tokio::select! { biased;
                _ = self.closing.cancelled() => Err(Status::new("unavailable", "WASM transport closed")),
                value = reads.recv() => {
                    let bytes = value.ok_or_else(|| Status::new("unavailable", "WASM transport ended"))?;
                    self.bytes.fetch_sub(bytes.len(), Ordering::Relaxed);
                    Ok(bytes)
                }
            }
        })
    }
    fn write(&self, payload: Vec<u8>) -> BoxFuture<'_, Result<()>> {
        Box::pin(async move {
            let receive = {
                let mut writes = self.writes.lock().unwrap();
                if self.closing.is_cancelled()
                    || writes.pending.len() >= MAX_FRAMES
                    || payload.len() > 1 << 20
                {
                    return Err(Status::new(
                        "resource_exhausted",
                        "WASM write capacity unavailable",
                    ));
                }
                let id = writes.next.checked_add(1).ok_or_else(|| {
                    Status::new("resource_exhausted", "WASM transport identity exhausted")
                })?;
                writes.next = id;
                let (send, receive) = oneshot::channel();
                writes.pending.insert(id, send);
                writes.queue.push_back(Outbound { id, payload });
                receive
            };
            crate::notify();
            tokio::select! { biased; _ = self.closing.cancelled() => Err(Status::new("unavailable", "WASM transport closed")), result = receive => result.unwrap_or_else(|_| Err(Status::new("unavailable", "WASM write abandoned"))) }
        })
    }
    fn close(&self) {
        self.closing.cancel();
        let mut writes = self.writes.lock().unwrap();
        writes.queue.clear();
        writes.pending.clear();
        drop(writes);
        crate::notify();
    }
}
