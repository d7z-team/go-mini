use mini_go::{ffi::Cancellation, rpc::*};
use tokio::sync::{Mutex, mpsc};

pub struct Pipe {
    pub send: mpsc::Sender<Vec<u8>>,
    pub receive: Mutex<mpsc::Receiver<Vec<u8>>>,
    pub closed: Cancellation,
}
impl MessageConn for Pipe {
    fn read(&self) -> BoxFuture<'_, Result<Vec<u8>>> {
        Box::pin(async {
            tokio::select! { _ = self.closed.cancelled() => Err(Status::new("unavailable", "closed")), message = async { self.receive.lock().await.recv().await } => message.ok_or_else(|| Status::new("unavailable", "closed")) }
        })
    }
    fn write(&self, fragment: Vec<u8>) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            tokio::select! { _ = self.closed.cancelled() => Err(Status::new("unavailable", "closed")), result = self.send.send(fragment) => result.map_err(|_| Status::new("unavailable", "closed")) }
        })
    }
    fn close(&self) {
        self.closed.cancel();
    }
}
