use mini_go::{
    error::RuntimeError,
    ffi::{self, Bridge, Cancellation, Completion, Reply},
    instance::{ExecutionLimits, Instance, PollStatus},
    loader::{DecodedImage, LoadLimits},
    program::Program,
};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
    time::{Duration, Instant},
};

pub const FORMAT: &str = "mini-go-tools";
pub const VERSION: u64 = 2;
const MAX_BYTES: usize = 64 << 20;

#[derive(Default)]
struct ControlState {
    canceled: bool,
    completion: Option<Completion>,
}
#[derive(Clone, Default)]
struct Control(Arc<Mutex<BTreeMap<String, ControlState>>>);
impl Control {
    fn cancel(&self, token: &str) {
        let completion = {
            let mut states = self.0.lock().unwrap();
            let Some(state) = states.get_mut(token) else {
                return;
            };
            state.canceled = true;
            state.completion.take()
        };
        if let Some(completion) = completion {
            completion.complete(Reply::new(b"canceled".to_vec(), None, None));
        }
    }
}
impl Bridge for Control {
    fn open(&self, _: Cancellation) -> Result<Box<dyn ffi::Session>, RuntimeError> {
        Ok(Box::new(self.clone()))
    }
    fn capabilities(&self) -> Vec<String> {
        vec!["minigo.tools.control".into()]
    }
}
struct ControlCall {
    control: Control,
    token: String,
}
impl ffi::Call for ControlCall {
    fn cancel(&self) {
        self.control.0.lock().unwrap().remove(&self.token);
    }
}
impl ffi::Session for Control {
    fn start(
        &self,
        _: Cancellation,
        request: ffi::Request,
        completion: Completion,
    ) -> Result<Box<dyn ffi::Call>, RuntimeError> {
        if request.route != "minigo.tools.control" {
            return Err(RuntimeError::new(
                "provider",
                "tools",
                "unknown tools route",
            ));
        }
        let request: Value = serde_json::from_slice(&request.payload).map_err(json_error)?;
        let token = request["Token"]
            .as_str()
            .ok_or_else(|| RuntimeError::new("invalid_argument", "tools", "missing token"))?
            .to_owned();
        let operation = request["Operation"].as_str().unwrap_or("");
        match operation {
            "wait" => {
                let mut states = self.0.lock().unwrap();
                let state = states
                    .get_mut(&token)
                    .ok_or_else(|| RuntimeError::new("stale", "tools", "unknown token"))?;
                if state.completion.is_some() {
                    return Err(RuntimeError::new(
                        "invalid_argument",
                        "tools",
                        "duplicate wait",
                    ));
                }
                if state.canceled {
                    drop(states);
                    completion.complete(Reply::new(b"canceled".to_vec(), None, None));
                } else {
                    state.completion = Some(completion);
                }
            }
            "finish" => {
                let state = self.0.lock().unwrap().remove(&token);
                if let Some(wait) = state.and_then(|state| state.completion) {
                    wait.complete(Reply::new(Vec::new(), None, None));
                }
                completion.complete(Reply::new(Vec::new(), None, None));
            }
            _ => {
                return Err(RuntimeError::new(
                    "invalid_argument",
                    "tools",
                    "invalid control operation",
                ));
            }
        }
        Ok(Box::new(ControlCall {
            control: self.clone(),
            token,
        }))
    }
    fn shutdown(&self, _: Cancellation) -> Result<(), RuntimeError> {
        self.0.lock().unwrap().clear();
        Ok(())
    }
}

fn json_error(error: serde_json::Error) -> RuntimeError {
    RuntimeError::new("invalid_argument", "tools", error.to_string())
}

/// One owner polls the compiler VM. Cancellation is delivered through the guest
/// context bridge before a bounded hard-stop fallback discards the instance.
pub struct CompilerSession {
    program: Arc<Program>,
    machine: Option<Instance>,
    control: Control,
    next_token: u64,
    session: String,
    revision: String,
    snapshot: String,
    recovery: Option<Value>,
    uncertain: Option<Value>,
    epoch: u64,
    closed: bool,
    in_flight: bool,
}
impl CompilerSession {
    pub async fn bundled() -> Result<Self, RuntimeError> {
        Self::new(include_bytes!("../assets/compiler.json.gz")).await
    }
    pub fn stats(&self) -> Option<mini_go::instance::stats::Stats> {
        self.machine.as_ref().map(Instance::stats)
    }
    /// Prepare an upgraded image and workspace before replacing the active VM.
    pub async fn upgrade(
        &mut self,
        image: &[u8],
        cancel: &Cancellation,
    ) -> Result<(), RuntimeError> {
        if self.closed || self.in_flight {
            return Err(RuntimeError::new(
                "closed",
                "tools",
                "session unavailable for upgrade",
            ));
        }
        let mut candidate = Self::new(image).await?;
        candidate.epoch = self.epoch.checked_add(1).ok_or_else(|| {
            RuntimeError::new("budget", "tools", "instance generations exhausted")
        })?;
        if let Some(recovery) = self.recovery.clone() {
            candidate.call(recovery, cancel).await?;
            candidate
                .call(json!({"Operation":"workspace/analyze"}), cancel)
                .await?;
        }
        if cancel.is_cancelled() {
            return Err(RuntimeError::new("canceled", "tools", "upgrade canceled"));
        }
        let mut previous = std::mem::replace(self, candidate);
        previous.close().await
    }
    pub async fn new(image: &[u8]) -> Result<Self, RuntimeError> {
        let decoded = if image.starts_with(&[0x1f, 0x8b]) {
            DecodedImage::decode_gzip(image, LoadLimits::compiler())?
        } else {
            DecodedImage::decode(image, LoadLimits::compiler())?
        };
        let program = Arc::new(Program::prepare(decoded)?);
        let mut session = Self {
            program,
            machine: None,
            control: Control::default(),
            next_token: 0,
            session: String::new(),
            revision: String::new(),
            snapshot: String::new(),
            recovery: None,
            uncertain: None,
            epoch: 1,
            closed: false,
            in_flight: false,
        };
        session.initialize().await?;
        let hello = session
            .call(json!({"Operation":"hello"}), &Cancellation::default())
            .await?;
        if hello["Format"] != FORMAT || hello["Version"] != VERSION {
            return Err(RuntimeError::new(
                "identity",
                "tools",
                "unsupported compiler tools ABI",
            ));
        }
        Ok(session)
    }
    async fn initialize(&mut self) -> Result<(), RuntimeError> {
        let mut machine = Instance::with_bridge(
            self.program.clone(),
            ExecutionLimits::compiler(),
            &self.control,
        )?;
        let until = Instant::now() + Duration::from_secs(30);
        while machine.poll_initialize(&Cancellation::default(), 4096)? != PollStatus::Ready {
            if Instant::now() >= until {
                return Err(RuntimeError::new(
                    "deadline",
                    "tools",
                    "compiler initialization deadline",
                ));
            }
            tokio::task::yield_now().await;
        }
        self.machine = Some(machine);
        Ok(())
    }
    pub async fn call(
        &mut self,
        request: Value,
        cancel: &Cancellation,
    ) -> Result<Value, RuntimeError> {
        if self.closed {
            return Err(RuntimeError::new(
                "closed",
                "tools",
                "compiler session closed",
            ));
        }
        if cancel.is_cancelled() {
            return Err(RuntimeError::new("canceled", "tools", "request canceled"));
        }
        if self.in_flight
            && let Some(mut machine) = self.machine.take()
        {
            let _ = machine.close();
        }
        self.in_flight = true;
        if self.machine.is_none() {
            self.epoch = self.epoch.checked_add(1).ok_or_else(|| {
                RuntimeError::new("budget", "tools", "instance generations exhausted")
            })?;
            self.control.0.lock().unwrap().clear();
            self.initialize().await?;
            self.session.clear();
            self.snapshot.clear();
            if let Some(recovery) = self.recovery.clone() {
                self.invoke(recovery, cancel).await?;
            }
            if let Some(pending) = self.uncertain.clone() {
                self.invoke(pending, cancel).await?;
                self.uncertain = None;
            }
            if !self.session.is_empty() {
                self.invoke(json!({"Operation":"workspace/analyze"}), cancel)
                    .await?;
            }
        }
        let mutation = matches!(
            request["Operation"].as_str(),
            Some("workspace/open" | "workspace/update" | "document/update")
        );
        if mutation {
            self.uncertain = Some(request.clone());
        }
        let result = self.invoke(request, cancel).await;
        self.in_flight = false;
        if self.machine.is_some() {
            self.uncertain = None;
        }
        result
    }
    async fn invoke(
        &mut self,
        mut request: Value,
        cancel: &Cancellation,
    ) -> Result<Value, RuntimeError> {
        if cancel.is_cancelled() {
            return Err(RuntimeError::new("canceled", "tools", "request canceled"));
        }
        self.next_token = self
            .next_token
            .checked_add(1)
            .ok_or_else(|| RuntimeError::new("budget", "tools", "request IDs exhausted"))?;
        let token = self.next_token.to_string();
        request["Format"] = json!(FORMAT);
        request["Version"] = json!(VERSION);
        request["Token"] = json!(token);
        request["Epoch"] = json!(self.epoch.to_string());
        if request.get("Deadline").is_none() {
            let deadline = std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map_err(|error| RuntimeError::new("clock", "tools", error.to_string()))?
                + Duration::from_secs(30);
            request["Deadline"] = json!(deadline.as_nanos().to_string());
        }
        if request.get("Session").is_none() {
            request["Session"] = json!(self.session);
        }
        if request.get("Revision").is_none() {
            request["Revision"] = json!(self.revision);
        }
        let input = serde_json::to_vec(&request).map_err(json_error)?;
        if input.len() > MAX_BYTES {
            return Err(RuntimeError::new("budget", "tools", "request too large"));
        }
        let machine = self
            .machine
            .as_mut()
            .ok_or_else(|| RuntimeError::new("closed", "tools", "compiler session closed"))?;
        self.control
            .0
            .lock()
            .unwrap()
            .insert(token.clone(), ControlState::default());
        let result = async {
            machine.start_bytes("tools", &input)?;
            let until = Instant::now() + Duration::from_secs(30);
            let mut canceled_at = None;
            loop {
                if cancel.is_cancelled() && canceled_at.is_none() {
                    self.control.cancel(&token);
                    canceled_at = Some(Instant::now());
                }
                if Instant::now() >= until
                    || canceled_at.is_some_and(|time| time.elapsed() > Duration::from_secs(2))
                {
                    return Err(RuntimeError::new(
                        "deadline",
                        "tools",
                        "compiler request hard deadline",
                    ));
                }
                match machine.poll_steps(4096)? {
                    PollStatus::Ready => break,
                    PollStatus::Pending => tokio::time::sleep(Duration::from_millis(1)).await,
                    PollStatus::Paused => {
                        return Err(RuntimeError::new(
                            "internal",
                            "tools",
                            "compiler unexpectedly paused",
                        ));
                    }
                    PollStatus::Running => tokio::task::yield_now().await,
                }
            }
            let snapshot = machine.snapshot_results(Default::default())?;
            let root = snapshot
                .roots
                .first()
                .ok_or_else(|| RuntimeError::new("internal", "tools", "missing compiler result"))?;
            let bytes = snapshot.bytes(root)?;
            if bytes.len() > MAX_BYTES {
                return Err(RuntimeError::new(
                    "budget",
                    "tools",
                    "compiler result too large",
                ));
            }
            serde_json::from_slice::<Value>(&bytes).map_err(json_error)
        }
        .await;
        self.control.0.lock().unwrap().remove(&token);
        let response = match result {
            Ok(value) => value,
            Err(error) => {
                if let Some(mut machine) = self.machine.take() {
                    let _ = machine.close();
                }
                return Err(error);
            }
        };
        if response["Format"] != FORMAT || response["Version"] != VERSION {
            if let Some(mut machine) = self.machine.take() {
                let _ = machine.close();
            }
            return Err(RuntimeError::new(
                "identity",
                "tools",
                "invalid tools response",
            ));
        }
        if let Some(error) = response["Error"].as_object() {
            let code = match error["Code"].as_str().unwrap_or("internal") {
                "canceled" => "canceled",
                "deadline" => "deadline",
                "stale" => "stale",
                "closed" => "closed",
                "budget" => "budget",
                "invalid_argument" => "invalid_argument",
                _ => "internal",
            };
            return Err(RuntimeError::new(
                code,
                "tools",
                error["Message"]
                    .as_str()
                    .unwrap_or("compiler request failed"),
            ));
        }
        if let Some(session) = response["Session"].as_str() {
            self.session = session.to_owned();
        }
        if let Some(revision) = response["Revision"]
            .as_str()
            .filter(|value| !value.is_empty())
        {
            self.revision = revision.to_owned();
        }
        if let Some(snapshot) = response["Analysis"]["Snapshot"].as_str() {
            self.snapshot = snapshot.to_owned();
        }
        if response["Recovery"].is_object() {
            self.recovery = Some(response["Recovery"].clone());
        }
        Ok(response)
    }
    pub fn revision(&self) -> &str {
        &self.revision
    }
    pub fn snapshot(&self) -> &str {
        &self.snapshot
    }
    /// Owned confirmed inputs, suitable for persistence and workspace providers.
    pub fn confirmed_input(&self) -> Option<Value> {
        self.recovery.clone()
    }
    pub async fn close(&mut self) -> Result<(), RuntimeError> {
        self.closed = true;
        self.recovery = None;
        self.uncertain = None;
        if let Some(mut machine) = self.machine.take() {
            machine.close()?;
        }
        self.control.0.lock().unwrap().clear();
        Ok(())
    }
}
impl Drop for CompilerSession {
    fn drop(&mut self) {
        if let Some(mut machine) = self.machine.take() {
            let _ = machine.close();
        }
    }
}
