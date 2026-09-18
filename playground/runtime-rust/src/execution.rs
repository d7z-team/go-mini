//! Shared execution control. An owner takes the machine out of the control lock
//! before running guest instructions or invoking any external host code.

use crate::{
    error::RuntimeError,
    ffi::{Cancellation, Wake},
    heap::HeapStats,
    instance::{ExecutionLimits, Instance, PollStatus},
    program::Program,
    snapshot::{HostSnapshot, SnapshotLimits},
    value::Value,
};
use std::{
    collections::BTreeMap,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, AtomicUsize, Ordering},
    },
    time::Duration,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ExecutionState {
    Running,
    Pending,
    Paused,
    Completed,
    Failed,
    Canceled,
}

#[derive(Clone, Debug)]
pub struct ScopeStats {
    pub id: u64,
    pub started: crate::instance::patch::RevisionInfo,
    pub root_state: ExecutionState,
    pub tasks: usize,
    pub timers: usize,
    pub ffi_calls: usize,
    pub steps: u64,
    pub done: bool,
    pub error: Option<RuntimeError>,
}

pub struct InstanceOptions {
    /// Cancellation applies to construction and root initialization only.
    pub cancellation: Cancellation,
    pub limits: ExecutionLimits,
    pub bridge: Option<Arc<dyn crate::ffi::Bridge>>,
    pub clock: Arc<dyn crate::environment::Clock>,
    pub entropy: Arc<dyn crate::environment::Entropy>,
}

impl Default for InstanceOptions {
    fn default() -> Self {
        Self {
            cancellation: Cancellation::default(),
            limits: ExecutionLimits::default(),
            bridge: None,
            clock: Arc::new(crate::environment::SystemClock::default()),
            entropy: Arc::new(crate::environment::SystemEntropy),
        }
    }
}

struct Outcome {
    state: ExecutionState,
    result: Option<Arc<HostSnapshot>>,
    error: Option<RuntimeError>,
}
struct Record {
    scope: u64,
    cancellation: Cancellation,
    settled: AtomicBool,
    outcome: Mutex<Outcome>,
    scope_error: Mutex<Option<RuntimeError>>,
    stats: Mutex<ScopeStats>,
    profile: Mutex<crate::instance::debug::Profile>,
}

impl Record {
    fn refresh(&self, machine: &Instance) {
        let work = machine.scope_work(self.scope);
        let root_state = self.outcome.lock().unwrap().state;
        let error = self.scope_error.lock().unwrap().clone();
        let mut stats = self.stats.lock().unwrap();
        stats.root_state = root_state;
        stats.tasks = work.tasks;
        stats.timers = work.timers;
        stats.ffi_calls = work.ffi_calls;
        stats.steps = stats.steps.max(work.steps);
        stats.done = !machine.scope_active(self.scope);
        stats.error = error;
    }
    fn fail(&self, error: RuntimeError, canceled: bool) {
        self.scope_error
            .lock()
            .unwrap()
            .get_or_insert_with(|| error.clone());
        let mut outcome = self.outcome.lock().unwrap();
        if matches!(
            outcome.state,
            ExecutionState::Running | ExecutionState::Pending | ExecutionState::Paused
        ) {
            outcome.state = if canceled {
                ExecutionState::Canceled
            } else {
                ExecutionState::Failed
            };
            outcome.error = Some(error);
        }
    }
}

struct State {
    machine: Option<Instance>,
    records: BTreeMap<u64, Arc<Record>>,
    shutdown: Option<Result<(), RuntimeError>>,
    stats: crate::instance::stats::Stats,
}
struct Control {
    state: Mutex<State>,
    wake: Arc<Wake>,
    closing: AtomicBool,
    handles: AtomicUsize,
    pause: Arc<AtomicBool>,
    close_sender: std::sync::mpsc::Sender<Arc<Control>>,
}

/// Cloneable public handle. Dropping the final instance handle starts shutdown;
/// outstanding Execution values retain only their results and control state.
pub struct SharedInstance {
    control: Arc<Control>,
}
pub struct Execution {
    control: Arc<Control>,
    record: Arc<Record>,
}

/// Inspection and resume requests use the same owner lease as execution.
pub struct Debugger {
    control: Arc<Control>,
}

impl Debugger {
    pub fn set_break_on_panic(&self, enabled: bool) -> Result<(), RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_mut()
            .unwrap()
            .set_break_on_panic(enabled)
    }
    pub fn pause(&self) {
        self.control.pause.store(true, Ordering::Release);
        self.control.wake.signal();
    }

    pub fn set_breakpoints(
        &self,
        module: &str,
        file: &str,
        lines: &[i64],
    ) -> Result<Vec<i64>, RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        owner
            .machine
            .as_mut()
            .unwrap()
            .set_breakpoints(module, file, lines)
    }

    pub fn events(&self) -> Result<Vec<crate::instance::debug::DebugEvent>, RuntimeError> {
        let owner = Owner::acquire(&self.control)?;
        Ok(owner.machine.as_ref().unwrap().debug_events())
    }

    pub fn threads(&self) -> Result<Vec<crate::instance::debug::ThreadInfo>, RuntimeError> {
        let owner = Owner::acquire(&self.control)?;
        Ok(owner.machine.as_ref().unwrap().debug_threads())
    }

    pub fn stack(&self) -> Result<Vec<crate::instance::debug::FrameInfo>, RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .debug_stack()
    }

    pub fn bindings(
        &self,
        frame: &crate::instance::debug::FrameRef,
        limits: SnapshotLimits,
    ) -> Result<crate::instance::debug::Bindings, RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .debug_bindings(frame, limits)
    }

    pub fn variables(
        &self,
        frame: &crate::instance::debug::FrameRef,
        offset: usize,
        limit: usize,
    ) -> Result<Vec<crate::instance::debug::VariableInfo>, RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .debug_variables(frame, offset, limit)
    }

    pub fn children(
        &self,
        reference: &crate::instance::debug::VariableRef,
        offset: usize,
        limit: usize,
    ) -> Result<Vec<crate::instance::debug::VariableInfo>, RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .debug_children(reference, offset, limit)
    }

    pub fn resume(&self, mode: crate::instance::debug::StepMode) -> Result<(), RuntimeError> {
        self.resume_selected(mode, None)
    }

    pub fn resume_task(
        &self,
        mode: crate::instance::debug::StepMode,
        task: u64,
    ) -> Result<(), RuntimeError> {
        self.resume_selected(mode, Some(task))
    }

    fn resume_selected(
        &self,
        mode: crate::instance::debug::StepMode,
        task: Option<u64>,
    ) -> Result<(), RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        let machine = owner.machine.as_mut().unwrap();
        if let Some(task) = task {
            machine.debug_resume_task(mode, task)?;
        } else {
            machine.debug_resume(mode)?;
        }
        if let Some(scope) = machine.foreground_scope()
            && let Some(record) = owner.records.get(&scope)
        {
            let mut outcome = record.outcome.lock().unwrap();
            if outcome.state == ExecutionState::Paused {
                outcome.state = ExecutionState::Running;
                machine.changed_scopes.insert(scope);
            }
        }
        owner.notify = true;
        Ok(())
    }

    pub fn start_profile(&self, sample_every: u64, max_samples: usize) -> Result<(), RuntimeError> {
        Owner::acquire(&self.control)?
            .machine
            .as_mut()
            .unwrap()
            .start_profile(sample_every, max_samples)
    }

    pub fn profile(&self) -> Result<crate::instance::debug::Profile, RuntimeError> {
        Ok(Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .profile())
    }
}

struct Owner {
    control: Arc<Control>,
    machine: Option<Instance>,
    records: BTreeMap<u64, Arc<Record>>,
    notify: bool,
    shutdown: Option<Result<(), RuntimeError>>,
}

impl Owner {
    fn acquire(control: &Arc<Control>) -> Result<Self, RuntimeError> {
        let mut state = control.state.lock().unwrap();
        let machine = state
            .machine
            .take()
            .ok_or_else(|| RuntimeError::new("busy", "owner", "instance already has an owner"))?;
        Ok(Self {
            control: control.clone(),
            machine: Some(machine),
            records: std::mem::take(&mut state.records),
            notify: false,
            shutdown: None,
        })
    }

    fn maintain(&mut self) -> Result<bool, RuntimeError> {
        if self.control.state.lock().unwrap().shutdown.is_some() {
            return Ok(false);
        }
        let machine = self.machine.as_mut().unwrap();
        if self.control.closing.load(Ordering::Acquire) {
            for record in self.records.values() {
                *record.profile.lock().unwrap() = machine.profile_for_scope(record.scope);
                record.fail(
                    RuntimeError::new("closed", "execution", "instance is closing"),
                    true,
                );
            }
            let waker = std::task::Waker::from(self.control.wake.clone());
            let mut cx = std::task::Context::from_waker(&waker);
            if let std::task::Poll::Ready(result) = machine.poll_close(&mut cx) {
                self.shutdown = Some(result);
            }
            self.notify = self.shutdown.is_some();
            return Ok(false);
        }
        for scope in machine.cancel_requested_scopes()? {
            if let Some(record) = self.records.get(&scope) {
                self.notify = true;
                record.fail(
                    RuntimeError::new("canceled", "execution", "execution was canceled"),
                    true,
                );
            }
        }
        Ok(true)
    }

    fn poll(
        &mut self,
        record: &Arc<Record>,
        count: usize,
    ) -> Result<(ExecutionState, usize), RuntimeError> {
        if !self.maintain()? {
            return Err(RuntimeError::new(
                "closed",
                "execution",
                "instance is closed",
            ));
        }
        let state = record.outcome.lock().unwrap().state;
        if !matches!(state, ExecutionState::Running | ExecutionState::Pending) {
            return Ok((state, 0));
        }
        let machine = self.machine.as_mut().unwrap();
        if machine.foreground_scope() != Some(record.scope) {
            return Err(RuntimeError::new(
                "stale_execution",
                "execution",
                "execution is no longer active",
            ));
        }
        let status = machine.poll_steps(count);
        let steps = machine.last_poll_steps;
        if record.cancellation.is_cancelled() {
            record.refresh(machine);
            machine.cancel_scope(record.scope)?;
            record.fail(
                RuntimeError::new("canceled", "execution", "execution was canceled"),
                true,
            );
            self.notify = true;
            return Ok((ExecutionState::Canceled, steps));
        }
        match status {
            Ok(status) => {
                let mut outcome = record.outcome.lock().unwrap();
                outcome.state = match status {
                    PollStatus::Running => ExecutionState::Running,
                    PollStatus::Pending => ExecutionState::Pending,
                    PollStatus::Paused => ExecutionState::Paused,
                    PollStatus::Ready => ExecutionState::Completed,
                };
                if status == PollStatus::Ready {
                    self.notify = true;
                    match machine.snapshot_results(SnapshotLimits::default()) {
                        Ok(result) => outcome.result = Some(Arc::new(result)),
                        Err(error) => {
                            outcome.state = ExecutionState::Failed;
                            outcome.error = Some(error.clone());
                            return Err(error);
                        }
                    }
                }
            }
            Err(error) => {
                self.notify = true;
                record.fail(error.clone(), false);
                return Err(error);
            }
        }
        machine.changed_scopes.insert(record.scope);
        self.maintain()?;
        Ok((record.outcome.lock().unwrap().state, steps))
    }
}

impl Drop for Owner {
    fn drop(&mut self) {
        let mut machine = self.machine.take().unwrap();
        for scope in std::mem::take(&mut machine.changed_scopes) {
            let Some(record) = self.records.get(&scope) else {
                continue;
            };
            if record.cancellation.is_cancelled() {
                record.fail(
                    RuntimeError::new("canceled", "execution", "execution was canceled"),
                    true,
                );
            }
            record.refresh(&machine);
            if !machine.scope_active(scope) {
                if matches!(
                    machine.state(),
                    crate::instance::stats::InstanceState::Open
                        | crate::instance::stats::InstanceState::Faulted
                ) {
                    *record.profile.lock().unwrap() = machine.profile_for_scope(scope);
                }
                machine.release_scope(scope);
                record.settled.store(true, Ordering::Release);
                self.notify = true;
                self.records.remove(&scope);
            }
        }
        let mut state = self.control.state.lock().unwrap();
        state.stats = machine.stats();
        state.records = std::mem::take(&mut self.records);
        state.machine = Some(machine);
        if self.shutdown.is_some() {
            state.shutdown = self.shutdown.take();
        }
        drop(state);
        if self.notify {
            self.control.wake.signal();
        }
    }
}

impl SharedInstance {
    #[cfg(not(target_arch = "wasm32"))]
    pub fn new(program: Arc<Program>, limits: ExecutionLimits) -> Result<Self, RuntimeError> {
        Self::from_machine(Instance::new(program, limits)?)
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub fn from_machine(mut machine: Instance) -> Result<Self, RuntimeError> {
        machine.initialize_root(&Cancellation::default())?;
        Self::attach(machine, true)
    }

    /// Attach an initialized machine to an external event-loop driver.
    /// Retain this handle and keep driving after `begin_shutdown` until
    /// `shutdown_result` is available, so asynchronous cleanup can finish.
    pub fn externally_driven(machine: Instance) -> Result<Self, RuntimeError> {
        if !machine.root_ready() {
            return Err(RuntimeError::new(
                "pending",
                "instance",
                "root initialization must complete before attaching a driver",
            ));
        }
        Self::attach(machine, false)
    }

    fn attach(machine: Instance, supervised: bool) -> Result<Self, RuntimeError> {
        let wake = machine.wake();
        let pause = machine.debug.pause.clone();
        let (close_sender, close_receiver) = std::sync::mpsc::channel();
        let control = Arc::new(Control {
            state: Mutex::new(State {
                stats: machine.stats(),
                machine: Some(machine),
                records: BTreeMap::new(),
                shutdown: None,
            }),
            wake,
            closing: AtomicBool::new(false),
            handles: AtomicUsize::new(1),
            pause,
            close_sender,
        });
        #[cfg(not(target_arch = "wasm32"))]
        if supervised {
            let weak = Arc::downgrade(&control);
            std::thread::Builder::new()
                .name("mini-go-runtime".to_owned())
                .spawn(move || {
                    let mut close_owner = None;
                    loop {
                        if close_owner.is_none() {
                            close_owner = close_receiver.try_recv().ok();
                        }
                        let Some(control) = close_owner.clone().or_else(|| weak.upgrade()) else {
                            return;
                        };
                        let observed = control.wake.epoch();
                        let mut running = false;
                        let mut delay = Duration::from_millis(1);
                        if let Ok(mut owner) = Owner::acquire(&control) {
                            delay = Duration::from_secs(60);
                            match owner.maintain() {
                                Ok(false) => {
                                    if owner.shutdown.is_some() {
                                        return;
                                    }
                                }
                                Err(error) => {
                                    for record in owner.records.values() {
                                        record.fail(error.clone(), false);
                                    }
                                }
                                Ok(true) => {
                                    let machine = owner.machine.as_mut().unwrap();
                                    if machine.foreground_scope().is_none() {
                                        match machine.poll_background(256) {
                                            Ok(PollStatus::Running) => running = true,
                                            Ok(_) => {}
                                            Err(error) => {
                                                for record in owner.records.values() {
                                                    record.fail(error.clone(), false);
                                                }
                                            }
                                        }
                                        delay = machine.next_timer_delay().unwrap_or(delay);
                                    }
                                }
                            }
                        }
                        let wake = control.wake.clone();
                        drop(control);
                        if running {
                            std::thread::yield_now();
                        } else {
                            wake.wait(observed, delay);
                        }
                    }
                })
                .map_err(|error| RuntimeError::new("supervisor", "instance", error.to_string()))?;
        }
        #[cfg(target_arch = "wasm32")]
        {
            let _ = (supervised, close_receiver);
        }
        Ok(Self { control })
    }

    /// Drive foreground, background, cancellation and cleanup from one owner.
    pub fn drive(&self, count: usize) -> Result<bool, RuntimeError> {
        if count == 0 {
            return Err(RuntimeError::new(
                "step_limit",
                "poll",
                "poll budget must be positive",
            ));
        }
        let mut owner = Owner::acquire(&self.control)?;
        if !owner.maintain()? {
            return Ok(false);
        }
        let scope = owner.machine.as_ref().unwrap().foreground_scope();
        if let Some(record) = scope.and_then(|scope| owner.records.get(&scope).cloned()) {
            return owner
                .poll(&record, count)
                .map(|(state, _)| state == ExecutionState::Running);
        }
        match owner.machine.as_mut().unwrap().poll_background(count) {
            Ok(status) => Ok(status == PollStatus::Running),
            Err(error) => {
                for record in owner.records.values() {
                    record.fail(error.clone(), false);
                }
                Err(error)
            }
        }
    }

    pub fn next_timer_delay(&self) -> Result<Option<Duration>, RuntimeError> {
        Ok(Owner::acquire(&self.control)?
            .machine
            .as_ref()
            .unwrap()
            .next_timer_delay())
    }

    pub fn begin_shutdown(&self) {
        if !self.control.closing.swap(true, Ordering::AcqRel) {
            self.control.wake.signal();
        }
    }

    pub fn shutdown_result(&self) -> Option<Result<(), RuntimeError>> {
        self.control.state.lock().unwrap().shutdown.clone()
    }

    pub fn start(&self, entry: &str, arguments: Vec<Value>) -> Result<Execution, RuntimeError> {
        self.start_invocation(|machine| machine.start(entry, arguments))
    }

    pub fn start_host(
        &self,
        entry: &str,
        arguments: &[crate::snapshot::HostValue],
    ) -> Result<Execution, RuntimeError> {
        self.start_invocation(|machine| machine.start_host(entry, arguments))
    }

    fn start_invocation(
        &self,
        start: impl FnOnce(&mut Instance) -> Result<(), RuntimeError>,
    ) -> Result<Execution, RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        if !owner.maintain()? {
            return Err(RuntimeError::new(
                "closed",
                "instance",
                "instance is closed",
            ));
        }
        let machine = owner.machine.as_mut().unwrap();
        start(machine)?;
        let record = Arc::new(Record {
            scope: machine.foreground_scope().unwrap(),
            cancellation: Cancellation::default(),
            settled: AtomicBool::new(false),
            outcome: Mutex::new(Outcome {
                state: ExecutionState::Running,
                result: None,
                error: None,
            }),
            scope_error: Mutex::new(None),
            profile: Mutex::new(Default::default()),
            stats: Mutex::new(ScopeStats {
                id: machine.foreground_scope().unwrap(),
                started: machine.revision(),
                root_state: ExecutionState::Running,
                tasks: 1,
                timers: 0,
                ffi_calls: 0,
                steps: 0,
                done: false,
                error: None,
            }),
        });
        machine.watch_scope_cancellation(record.scope, record.cancellation.clone());
        owner.records.insert(record.scope, record.clone());
        owner.notify = true;
        Ok(Execution {
            control: self.control.clone(),
            record,
        })
    }

    pub fn heap_stats(&self) -> HeapStats {
        self.control.state.lock().unwrap().stats.heap
    }

    pub fn collect_garbage(&self) -> Result<HeapStats, RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        owner.maintain()?;
        owner.machine.as_mut().unwrap().collect_garbage()
    }

    pub fn stats(&self) -> crate::instance::stats::Stats {
        let mut stats = self.control.state.lock().unwrap().stats.clone();
        if self.control.closing.load(Ordering::Acquire)
            && stats.state != crate::instance::stats::InstanceState::Closed
        {
            stats.state = crate::instance::stats::InstanceState::Closing;
        }
        stats
    }

    pub fn revision(&self) -> Result<crate::instance::patch::RevisionInfo, RuntimeError> {
        let owner = Owner::acquire(&self.control)?;
        Ok(owner.machine.as_ref().unwrap().revision())
    }

    pub fn prepare_patch(
        &self,
        target: Arc<Program>,
    ) -> Result<crate::instance::patch::PatchPlan, RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        owner.maintain()?;
        owner.machine.as_ref().unwrap().prepare_patch(target)
    }

    pub fn apply_patch(
        &self,
        plan: crate::instance::patch::PatchPlan,
    ) -> Result<crate::instance::patch::PatchResult, RuntimeError> {
        let mut owner = Owner::acquire(&self.control)?;
        owner.maintain()?;
        let result = owner.machine.as_mut().unwrap().apply_patch(plan)?;
        owner.notify = true;
        Ok(result)
    }

    pub fn debugger(&self) -> Debugger {
        Debugger {
            control: self.control.clone(),
        }
    }
    pub fn wake(&self) -> Arc<Wake> {
        self.control.wake.clone()
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub fn shutdown(&self, wait: &Cancellation) -> Result<(), RuntimeError> {
        self.begin_shutdown();
        loop {
            let observed = self.control.wake.epoch();
            if let Some(result) = &self.control.state.lock().unwrap().shutdown {
                return result.clone();
            }
            if wait.is_cancelled() {
                return Err(RuntimeError::new(
                    "canceled",
                    "shutdown",
                    "caller stopped waiting for shutdown",
                ));
            }
            self.control.wake.wait(observed, Duration::from_millis(10));
        }
    }
}

impl Clone for SharedInstance {
    fn clone(&self) -> Self {
        self.control.handles.fetch_add(1, Ordering::Relaxed);
        Self {
            control: self.control.clone(),
        }
    }
}

impl Drop for SharedInstance {
    fn drop(&mut self) {
        if self.control.handles.fetch_sub(1, Ordering::AcqRel) == 1 {
            if self.control.state.lock().unwrap().shutdown.is_some() {
                return;
            }
            self.control.closing.store(true, Ordering::Release);
            // Transfer the final cleanup reference to the existing supervisor.
            // Drop never needs to allocate another operating-system thread.
            let _ = self.control.close_sender.send(self.control.clone());
            self.control.wake.signal();
        }
    }
}

impl Execution {
    pub fn profile(&self) -> crate::instance::debug::Profile {
        if !self.scope_settled()
            && let Ok(owner) = Owner::acquire(&self.control)
        {
            *self.record.profile.lock().unwrap() = owner
                .machine
                .as_ref()
                .unwrap()
                .profile_for_scope(self.record.scope);
        }
        self.record.profile.lock().unwrap().clone()
    }
    pub fn scope_stats(&self) -> ScopeStats {
        self.record.stats.lock().unwrap().clone()
    }
    pub fn state(&self) -> ExecutionState {
        self.record.outcome.lock().unwrap().state
    }
    pub fn scope_settled(&self) -> bool {
        self.record.settled.load(Ordering::Acquire)
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub fn wait_scope(&self, cancellation: &Cancellation) -> Result<(), RuntimeError> {
        loop {
            let observed = self.control.wake.epoch();
            if self.scope_settled() {
                return self
                    .record
                    .scope_error
                    .lock()
                    .unwrap()
                    .clone()
                    .map_or(Ok(()), Err);
            }
            if cancellation.is_cancelled() {
                return Err(RuntimeError::new(
                    "canceled",
                    "scope",
                    "caller stopped waiting for scope",
                ));
            }
            self.control.wake.wait(observed, Duration::from_millis(10));
        }
    }
    pub fn cancel(&self) {
        self.record.cancellation.cancel();
        self.control.wake.signal();
    }

    pub fn poll_steps(&self, count: usize) -> Result<(ExecutionState, usize), RuntimeError> {
        if count == 0 {
            return Err(RuntimeError::new(
                "step_limit",
                "poll",
                "poll budget must be positive",
            ));
        }
        let state = self.state();
        if !matches!(state, ExecutionState::Running | ExecutionState::Pending) {
            return Ok((state, 0));
        }
        Owner::acquire(&self.control)?.poll(&self.record, count)
    }

    pub fn result(&self) -> Result<Arc<HostSnapshot>, RuntimeError> {
        let outcome = self.record.outcome.lock().unwrap();
        if let Some(error) = &outcome.error {
            return Err(error.clone());
        }
        outcome
            .result
            .clone()
            .ok_or_else(|| RuntimeError::new("pending", "result", "execution has not completed"))
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub fn wait(&self, cancellation: &Cancellation) -> Result<Arc<HostSnapshot>, RuntimeError> {
        loop {
            let observed = self.control.wake.epoch();
            if cancellation.is_cancelled() {
                self.cancel();
                return Err(RuntimeError::new(
                    "canceled",
                    "wait",
                    "caller stopped waiting",
                ));
            }
            match self.poll_steps(4096) {
                Ok((ExecutionState::Completed, _)) => return self.result(),
                Ok((ExecutionState::Canceled | ExecutionState::Failed, _)) => return self.result(),
                Ok((ExecutionState::Paused, _)) => {
                    return Err(RuntimeError::new("paused", "wait", "execution is paused"));
                }
                Ok((ExecutionState::Running, _)) => continue,
                Err(error) if error.code == "busy" => {}
                Err(error) => return Err(error),
                Ok(_) => {}
            }
            self.control.wake.wait(observed, Duration::from_millis(10));
        }
    }
}
