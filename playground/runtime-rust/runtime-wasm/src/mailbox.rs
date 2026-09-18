//! IDs and owned bytes cross the JS boundary; JS objects stay in the worker.
use mini_go::{
    RuntimeError,
    ffi::{self, Bridge, Call, Cancellation, Completion, Reply, Session},
};
use serde::Serialize;
use std::{
    collections::{BTreeMap, VecDeque},
    sync::{Arc, Mutex},
    task::{Poll, Waker},
};

#[derive(Serialize)]
#[serde(tag = "kind", rename_all = "camelCase")]
pub enum Action {
    Call {
        id: u32,
        route: String,
        payload: Vec<u8>,
    },
    Cancel {
        id: u32,
    },
    Decision {
        id: u32,
        accepted: bool,
    },
    Close,
}
struct PendingCall {
    completion: Option<Completion>,
    decided: bool,
}
struct State {
    next: u32,
    pending: BTreeMap<u32, PendingCall>,
    actions: VecDeque<Action>,
    closing: bool,
    closed: bool,
    waiter: Option<Waker>,
}
#[derive(Clone)]
pub struct Mailbox {
    state: Arc<Mutex<State>>,
    capabilities: Vec<String>,
    limit: usize,
}
impl Mailbox {
    pub fn new(capabilities: Vec<String>, limit: usize) -> Self {
        Self {
            state: Arc::new(Mutex::new(State {
                next: 0,
                pending: BTreeMap::new(),
                actions: VecDeque::new(),
                closing: false,
                closed: false,
                waiter: None,
            })),
            capabilities,
            limit,
        }
    }
    pub fn drain(&self) -> Vec<Action> {
        self.state.lock().unwrap().actions.drain(..).collect()
    }
    pub fn complete(
        &self,
        id: u32,
        payload: Vec<u8>,
        error: Option<RuntimeError>,
    ) -> Result<(), RuntimeError> {
        let completion = self
            .state
            .lock()
            .unwrap()
            .pending
            .get_mut(&id)
            .and_then(|call| call.completion.take())
            .ok_or_else(|| {
                RuntimeError::new("stale_call", "ffi", "unknown or completed host call")
            })?;
        let discard = self.clone();
        let consumed = self.clone();
        completion.complete(
            Reply::new(
                payload,
                error,
                Some(Box::new(move || discard.decide(id, false))),
            )
            .on_consumed(move || consumed.decide(id, true)),
        );
        Ok(())
    }
    fn decide(&self, id: u32, accepted: bool) {
        let mut state = self.state.lock().unwrap();
        if let Some(call) = state.pending.get_mut(&id)
            && !call.decided
        {
            call.decided = true;
            state.actions.push_back(Action::Decision { id, accepted });
        }
        drop(state);
        crate::notify();
    }
    pub fn decision_done(&self, id: u32) {
        let mut state = self.state.lock().unwrap();
        if state.pending.get(&id).is_some_and(|call| call.decided) {
            state.pending.remove(&id);
        }
    }
    pub fn closed(&self) {
        let mut state = self.state.lock().unwrap();
        state.closed = true;
        let waiter = state.waiter.take();
        drop(state);
        if let Some(waiter) = waiter {
            waiter.wake();
        }
    }
}
struct HostCall {
    mailbox: Mailbox,
    id: u32,
}
impl Call for HostCall {
    fn cancel(&self) {
        let mut state = self.mailbox.state.lock().unwrap();
        if state.pending.contains_key(&self.id) {
            state.actions.push_back(Action::Cancel { id: self.id });
        }
        drop(state);
        crate::notify();
    }
}
impl Bridge for Mailbox {
    fn open(&self, _: Cancellation) -> Result<Box<dyn Session>, RuntimeError> {
        Ok(Box::new(self.clone()))
    }
    fn capabilities(&self) -> Vec<String> {
        self.capabilities.clone()
    }
}
impl Session for Mailbox {
    fn start(
        &self,
        _: Cancellation,
        request: ffi::Request,
        completion: Completion,
    ) -> Result<Box<dyn Call>, RuntimeError> {
        let mut state = self.state.lock().unwrap();
        if state.closing || state.pending.len() >= self.limit {
            return Err(RuntimeError::new(
                "resource_exhausted",
                "ffi",
                "host call capacity unavailable",
            ));
        }
        let id = state.next.checked_add(1).ok_or_else(|| {
            RuntimeError::new("resource_exhausted", "ffi", "host identity exhausted")
        })?;
        state.next = id;
        state.pending.insert(
            id,
            PendingCall {
                completion: Some(completion),
                decided: false,
            },
        );
        state.actions.push_back(Action::Call {
            id,
            route: request.route,
            payload: request.payload,
        });
        Ok(Box::new(HostCall {
            mailbox: self.clone(),
            id,
        }))
    }
    fn shutdown(&self, _: Cancellation) -> Result<(), RuntimeError> {
        Err(RuntimeError::new(
            "async_required",
            "ffi",
            "WASM session cleanup requires polling",
        ))
    }
    fn shutdown_async(&self) -> ffi::Shutdown<'_> {
        Box::pin(std::future::poll_fn(move |cx| {
            let mut state = self.state.lock().unwrap();
            if !state.closing {
                state.closing = true;
                state.actions.push_back(Action::Close);
            }
            if state.closed {
                Poll::Ready(Ok(()))
            } else {
                state.waiter = Some(cx.waker().clone());
                Poll::Pending
            }
        }))
    }
}
