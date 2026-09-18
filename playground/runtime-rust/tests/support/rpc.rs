use mini_go::{
    ffi,
    rpc::{
        FFI_ROUTE, Limits,
        protocol::{FfiRequest, FfiResponse},
    },
};
use std::sync::Arc;
use std::time::{Duration, Instant};

pub struct Session {
    session: Box<dyn ffi::Session>,
    pending: ffi::PendingCalls,
    wake: Arc<ffi::Wake>,
}
impl Session {
    pub fn new(host: &dyn ffi::Bridge) -> Self {
        let wake = Arc::new(ffi::Wake::default());
        Self {
            session: host.open(ffi::Cancellation::default()).unwrap(),
            pending: ffi::PendingCalls::new(16, 1 << 20, wake.clone()),
            wake,
        }
    }
    pub fn request(&mut self, request: FfiRequest) -> ffi::Reply {
        let payload = request.encode();
        let (id, cancellation, completion) = self.pending.reserve(payload.len(), 1 << 19).unwrap();
        let started = self.session.start(
            cancellation,
            ffi::Request {
                route: FFI_ROUTE.into(),
                payload,
            },
            completion,
        );
        self.pending.started(id, started).unwrap();
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            let observed = self.wake.epoch();
            if let Some(reply) = self.pending.take(id) {
                return reply;
            }
            assert!(Instant::now() < deadline, "RPC FFI did not complete");
            self.wake.wait(observed, Duration::from_millis(100));
        }
    }
    pub fn call(&mut self, request: FfiRequest) -> FfiResponse {
        let response = FfiResponse::decode(
            &self.request(request).consume().unwrap(),
            &Limits::default(),
        )
        .unwrap();
        assert!(
            response.code.is_empty(),
            "{}: {}",
            response.code,
            response.message
        );
        response
    }
    pub fn shutdown(&mut self) {
        self.pending.close();
        self.session.shutdown(ffi::Cancellation::default()).unwrap();
    }
}
