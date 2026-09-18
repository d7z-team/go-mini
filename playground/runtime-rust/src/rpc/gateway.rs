//! WebSocket carriers for symmetric RPC endpoints.
use super::publication::PublicationRegistry;
use super::*;
use crate::ffi::Cancellation;
use futures_util::{SinkExt, StreamExt};
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::io::{AsyncRead, AsyncWrite};
use tokio::runtime::Handle;
use tokio::sync::{Semaphore, mpsc, oneshot};
use tokio_tungstenite::{
    WebSocketStream,
    tungstenite::{
        self, Message,
        client::IntoClientRequest,
        handshake::server::{Request, Response},
        http,
    },
};

pub use tokio_rustls::rustls;
pub type PeerAuthenticator = Arc<dyn Fn(&Request) -> Result<PeerInfo> + Send + Sync>;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Address {
    pub scheme: String,
    pub host: String,
    pub path: String,
    pub socket_path: Option<String>,
}
impl Address {
    pub fn parse(text: &str) -> Result<Self> {
        let url = url::Url::parse(text.trim())
            .map_err(|error| Status::new("invalid_argument", error.to_string()))?;
        if !url.username().is_empty()
            || url.password().is_some()
            || url.query().is_some()
            || url.fragment().is_some()
        {
            return Err(Status::new("invalid_argument", "invalid Gateway address"));
        }
        let path = if !text
            .trim()
            .split_once("://")
            .is_some_and(|(_, authority)| authority.contains('/'))
        {
            "/rpc".to_owned()
        } else {
            percent_encoding::percent_decode_str(url.path())
                .decode_utf8()
                .map_err(|_| Status::new("invalid_argument", "Gateway path is not UTF-8"))?
                .into_owned()
        };
        match url.scheme() {
            "ws" | "wss" => {
                if url.host_str().is_none() {
                    return Err(Status::new("invalid_argument", "Gateway host required"));
                }
                Ok(Self {
                    scheme: url.scheme().into(),
                    host: url[url::Position::BeforeHost..url::Position::AfterPort].into(),
                    path,
                    socket_path: None,
                })
            }
            "ws+unix" => {
                if url.host_str().is_some() || !url.path().starts_with('/') {
                    return Err(Status::new(
                        "invalid_argument",
                        "Gateway Unix socket path must be absolute",
                    ));
                }
                Ok(Self {
                    scheme: "ws+unix".into(),
                    host: "minigo.local".into(),
                    path: "/rpc".into(),
                    socket_path: Some(path),
                })
            }
            _ => Err(Status::new(
                "invalid_argument",
                "unsupported Gateway address scheme",
            )),
        }
    }
}

struct Write {
    data: Vec<u8>,
    done: oneshot::Sender<Result<()>>,
}
pub struct WebSocketConn {
    writes: mpsc::Sender<Write>,
    reads: tokio::sync::Mutex<mpsc::Receiver<Result<Vec<u8>>>>,
    closed: Cancellation,
}
impl WebSocketConn {
    pub fn new<S: AsyncRead + AsyncWrite + Unpin + Send + 'static>(
        runtime: &Handle,
        socket: WebSocketStream<S>,
        max_frame_bytes: usize,
    ) -> Arc<Self> {
        let (writes, mut outgoing) = mpsc::channel::<Write>(1);
        let (incoming, reads) = mpsc::channel(1);
        let closed = Cancellation::default();
        let (mut sink, mut stream) = socket.split();
        let stopped = closed.clone();
        runtime.spawn(async move {
            loop {
                let write = tokio::select! { _ = stopped.cancelled() => break, write = outgoing.recv() => write };
                let Some(write) = write else { break; };
                let result = tokio::select! {
                    _ = stopped.cancelled() => Err(Status::new("unavailable", "Gateway closed")),
                    result = sink.send(Message::Binary(write.data.into())) => result.map_err(|error| Status::new("unavailable", error.to_string())),
                };
                let failed = result.is_err(); let _ = write.done.send(result); if failed { break; }
            }
            stopped.cancel();
        });
        let stopped = closed.clone();
        runtime.spawn(async move {
            loop {
                let message = tokio::select! { _ = stopped.cancelled() => break, message = stream.next() => message };
                let value = match message {
                    Some(Ok(Message::Binary(data))) if data.len() <= max_frame_bytes => Ok(data.to_vec()),
                    Some(Ok(Message::Ping(_) | Message::Pong(_))) => continue,
                    Some(Ok(Message::Close(_))) | None => break,
                    Some(Ok(_)) => Err(Status::protocol("Gateway requires bounded binary messages")),
                    Some(Err(error)) => Err(Status::new("unavailable", error.to_string())),
                };
                let failed = value.is_err();
                tokio::select! { _ = stopped.cancelled() => break, result = incoming.send(value) => if result.is_err() { break; } }
                if failed { break; }
            }
            stopped.cancel();
        });
        Arc::new(Self {
            writes,
            reads: tokio::sync::Mutex::new(reads),
            closed,
        })
    }
}
impl MessageConn for WebSocketConn {
    fn read(&self) -> BoxFuture<'_, Result<Vec<u8>>> {
        Box::pin(async {
            let mut reads = self.reads.lock().await;
            tokio::select! { biased; message = reads.recv() => message.unwrap_or_else(|| Err(Status::new("unavailable", "Gateway closed"))), _ = self.closed.cancelled() => Err(Status::new("unavailable", "Gateway closed")) }
        })
    }
    fn write(&self, data: Vec<u8>) -> BoxFuture<'_, Result<()>> {
        Box::pin(async {
            let (done, completion) = oneshot::channel();
            tokio::select! { _ = self.closed.cancelled() => return Err(Status::new("unavailable", "Gateway closed")), result = self.writes.send(Write { data, done }) => result.map_err(|_| Status::new("unavailable", "Gateway closed"))? }
            completion
                .await
                .map_err(|_| Status::new("unavailable", "Gateway writer closed"))?
        })
    }
    fn close(&self) {
        self.closed.cancel();
    }
}
impl Drop for WebSocketConn {
    fn drop(&mut self) {
        self.closed.cancel();
    }
}

pub struct DialOptions {
    pub headers: http::HeaderMap,
    pub binder: Option<Arc<dyn Binder>>,
    pub endpoint: EndpointOptions,
    pub tls: Option<Arc<rustls::ClientConfig>>,
    pub handshake_timeout: Duration,
}
impl Default for DialOptions {
    fn default() -> Self {
        Self {
            headers: http::HeaderMap::new(),
            binder: None,
            endpoint: EndpointOptions::default(),
            tls: None,
            handshake_timeout: Duration::from_secs(10),
        }
    }
}
pub async fn dial(runtime: Handle, address: &str, options: DialOptions) -> Result<Arc<Endpoint>> {
    let DialOptions {
        headers,
        binder,
        endpoint: options,
        tls,
        handshake_timeout,
    } = options;
    let target = Address::parse(address)?;
    let mut uri = url::Url::parse(&format!(
        "{}://{}",
        if target.socket_path.is_some() {
            "ws"
        } else {
            &target.scheme
        },
        target.host,
    ))
    .map_err(|error| Status::new("invalid_argument", error.to_string()))?;
    uri.set_path(&target.path);
    let mut request = uri
        .as_str()
        .into_client_request()
        .map_err(|error| Status::new("invalid_argument", error.to_string()))?;
    for (name, value) in headers.iter() {
        request.headers_mut().insert(name.clone(), value.clone());
    }
    request.headers_mut().insert(
        "sec-websocket-protocol",
        http::HeaderValue::from_static(ENDPOINT_PROTOCOL),
    );
    let config = tungstenite::protocol::WebSocketConfig::default()
        .max_message_size(Some(options.limits.max_frame_bytes))
        .max_frame_size(Some(options.limits.max_frame_bytes));
    let connection = tokio::time::timeout(handshake_timeout, async {
        let connection: Arc<dyn MessageConn> = if let Some(socket_path) = target.socket_path {
            #[cfg(unix)]
            {
                let stream = tokio::net::UnixStream::connect(socket_path)
                    .await
                    .map_err(|error| Status::new("unavailable", error.to_string()))?;
                let (socket, response) =
                    tokio_tungstenite::client_async_with_config(request, stream, Some(config))
                        .await
                        .map_err(|error| Status::new("unavailable", error.to_string()))?;
                if response
                    .headers()
                    .get("sec-websocket-protocol")
                    .and_then(|v| v.to_str().ok())
                    != Some(ENDPOINT_PROTOCOL)
                {
                    return Err(Status::protocol("Gateway subprotocol mismatch"));
                }
                WebSocketConn::new(&runtime, socket, options.limits.max_frame_bytes)
            }
            #[cfg(not(unix))]
            {
                let _ = socket_path;
                return Err(Status::new(
                    "unimplemented",
                    "Unix sockets are unavailable on this platform",
                ));
            }
        } else {
            let (socket, response) = tokio_tungstenite::connect_async_tls_with_config(
                request,
                Some(config),
                false,
                tls.map(tokio_tungstenite::Connector::Rustls),
            )
            .await
            .map_err(|error| Status::new("unavailable", error.to_string()))?;
            if response
                .headers()
                .get("sec-websocket-protocol")
                .and_then(|v| v.to_str().ok())
                != Some(ENDPOINT_PROTOCOL)
            {
                return Err(Status::protocol("Gateway subprotocol mismatch"));
            }
            WebSocketConn::new(&runtime, socket, options.limits.max_frame_bytes)
        };
        Ok::<_, Status>(connection)
    })
    .await
    .map_err(|_| Status::new("deadline_exceeded", "Gateway handshake timed out"))??;
    Endpoint::open(runtime, connection, binder, options)
}

pub struct ServerOptions {
    pub endpoint: EndpointOptions,
    pub path: String,
    pub max_connections: usize,
    pub handshake_timeout: Duration,
    pub authenticate: Option<PeerAuthenticator>,
    pub tls: Option<Arc<rustls::ServerConfig>>,
    pub publications: Option<Arc<PublicationRegistry>>,
}
impl Default for ServerOptions {
    fn default() -> Self {
        Self {
            endpoint: EndpointOptions::default(),
            path: "/rpc".into(),
            max_connections: 4096,
            handshake_timeout: Duration::from_secs(10),
            authenticate: None,
            tls: None,
            publications: None,
        }
    }
}
pub struct Server {
    runtime: Handle,
    binder: Arc<dyn Binder>,
    options: ServerOptions,
    slots: Arc<Semaphore>,
    closing: Cancellation,
    endpoints: Mutex<Vec<std::sync::Weak<Endpoint>>>,
}
impl Server {
    pub fn new(
        runtime: Handle,
        binder: Arc<dyn Binder>,
        options: ServerOptions,
    ) -> Result<Arc<Self>> {
        if options.max_connections == 0 || options.max_connections > u32::MAX as usize {
            return Err(Status::new(
                "invalid_argument",
                "invalid Gateway connection limit",
            ));
        }
        options.endpoint.limits.validate_endpoint()?;
        Ok(Arc::new(Self {
            runtime,
            binder,
            slots: Arc::new(Semaphore::new(options.max_connections)),
            options,
            closing: Cancellation::default(),
            endpoints: Mutex::new(Vec::new()),
        }))
    }
    pub async fn serve(self: &Arc<Self>, listener: tokio::net::TcpListener) -> Result<()> {
        loop {
            let (stream, _) = tokio::select! { _ = self.closing.cancelled() => return Ok(()), result = listener.accept() => result.map_err(|error|Status::new("unavailable",error.to_string()))? };
            let permit = match self.slots.clone().try_acquire_owned() {
                Ok(permit) => permit,
                Err(_) => continue,
            };
            let server = self.clone();
            self.runtime.spawn(async move {
                if let Some(tls) = &server.options.tls {
                    let accepted = tokio::select! { _ = server.closing.cancelled() => None, stream = tokio::time::timeout(server.options.handshake_timeout,tokio_rustls::TlsAcceptor::from(tls.clone()).accept(stream)) => stream.ok().and_then(|result|result.ok()) };
                    if let Some(stream) = accepted { let _ = server.accept(stream).await; }
                } else { let _ = server.accept(stream).await; }
                drop(permit);
            });
        }
    }
    #[cfg(unix)]
    pub async fn serve_unix(self: &Arc<Self>, listener: tokio::net::UnixListener) -> Result<()> {
        loop {
            let (stream, _) = tokio::select! { _ = self.closing.cancelled() => return Ok(()), result = listener.accept() => result.map_err(|error|Status::new("unavailable",error.to_string()))? };
            let permit = match self.slots.clone().try_acquire_owned() {
                Ok(permit) => permit,
                Err(_) => continue,
            };
            let server = self.clone();
            self.runtime.spawn(async move {
                let _ = server.accept(stream).await;
                drop(permit);
            });
        }
    }
    async fn accept<S: AsyncRead + AsyncWrite + Unpin + Send + 'static>(
        &self,
        stream: S,
    ) -> Result<()> {
        let peer = Arc::new(Mutex::new(PeerInfo::default()));
        let authenticated = peer.clone();
        let publication = Arc::new(std::sync::atomic::AtomicBool::new(false));
        let publication_path = publication.clone();
        let allow_publication = self.options.publications.is_some();
        let path = self.options.path.clone();
        let authenticate = self.options.authenticate.clone();
        // Tungstenite's callback contract returns an HTTP response by value.
        #[allow(clippy::result_large_err)]
        let callback = move |request: &Request, mut response: Response| {
            let failure = |code, message: &str| {
                http::Response::builder()
                    .status(code)
                    .body(Some(message.to_owned()))
                    .unwrap()
            };
            let publishing = allow_publication && request.uri().path() == "/publish";
            if (!publishing && request.uri().path() != path) || request.uri().query().is_some() {
                return Err(failure(404, "Gateway path not found"));
            }
            publication_path.store(publishing, std::sync::atomic::Ordering::Release);
            if !request
                .headers()
                .get("sec-websocket-protocol")
                .and_then(|v| v.to_str().ok())
                .is_some_and(|v| v.split(',').any(|v| v.trim() == ENDPOINT_PROTOCOL))
            {
                return Err(failure(400, "Gateway subprotocol required"));
            }
            if let Some(authenticate) = &authenticate {
                match authenticate(request) {
                    Ok(peer) => *authenticated.lock().unwrap() = peer,
                    Err(_) => return Err(failure(403, "Gateway authentication failed")),
                }
            }
            response.headers_mut().insert(
                "sec-websocket-protocol",
                http::HeaderValue::from_static(ENDPOINT_PROTOCOL),
            );
            Ok(response)
        };
        let config = tungstenite::protocol::WebSocketConfig::default()
            .max_message_size(Some(self.options.endpoint.limits.max_frame_bytes))
            .max_frame_size(Some(self.options.endpoint.limits.max_frame_bytes));
        let socket = tokio::select! { _ = self.closing.cancelled() => return Ok(()), socket = tokio::time::timeout(self.options.handshake_timeout,tokio_tungstenite::accept_hdr_async_with_config(stream,callback,Some(config))) => socket.map_err(|_|Status::new("deadline_exceeded","Gateway handshake timed out"))?.map_err(|error|Status::new("protocol",error.to_string()))? };
        let connection = WebSocketConn::new(
            &self.runtime,
            socket,
            self.options.endpoint.limits.max_frame_bytes,
        );
        let closed = connection.closed.clone();
        let mut options = self.options.endpoint.clone();
        options.peer = peer.lock().unwrap().clone();
        let endpoint = Endpoint::open(
            self.runtime.clone(),
            connection,
            Some(self.binder.clone()),
            options,
        )?;
        {
            let mut endpoints = self.endpoints.lock().unwrap();
            endpoints.retain(|entry| entry.strong_count() != 0);
            endpoints.push(Arc::downgrade(&endpoint));
        }
        let mut attached = None;
        if publication.load(std::sync::atomic::Ordering::Acquire) {
            let registry = self.options.publications.as_ref().unwrap();
            let peer = peer.lock().unwrap().clone();
            let result = registry.attach(endpoint.clone(), peer).await;
            match result {
                Ok(identity) => attached = Some(identity),
                Err(error) => {
                    let _ = endpoint.shutdown().await;
                    return Err(error);
                }
            }
        }
        tokio::select! { _ = self.closing.cancelled() => {}, _ = closed.cancelled() => {} }
        let result = endpoint.shutdown().await;
        if let Some((process, id)) = attached {
            self.options
                .publications
                .as_ref()
                .unwrap()
                .detach(&process, &id)
                .await?;
        }
        result
    }
    pub async fn shutdown(&self) -> Result<()> {
        self.closing.cancel();
        let endpoints = self
            .endpoints
            .lock()
            .unwrap()
            .iter()
            .filter_map(std::sync::Weak::upgrade)
            .collect::<Vec<_>>();
        for endpoint in endpoints {
            endpoint.shutdown().await?;
        }
        let _permits = self
            .slots
            .acquire_many(self.options.max_connections as u32)
            .await;
        Ok(())
    }
}
impl Drop for Server {
    fn drop(&mut self) {
        self.closing.cancel();
    }
}
