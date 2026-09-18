//! Single-owner execution. All guest mutation requires an exclusive instance
//! borrow; polling executes an exact bounded number of guest instructions.

use crate::{
    contract_generated as wire,
    error::RuntimeError,
    ffi::{Bridge, Cancellation, PendingCalls, Session, Wake},
    heap::{Handle, Heap, HeapStats, Trace},
    program::{Instruction, Program},
    types::{TypeIdentity, TypeRegistry},
    value::{Address, Data, FunctionValue, PathElement, SliceValue, Value},
};
use std::{
    collections::{BTreeMap, HashMap, HashSet, VecDeque},
    sync::Arc,
};

mod address;
mod collection;
mod conversion;
pub mod debug;
mod debug_variables;
mod frame;
mod gc;
use frame::Frame;
#[cfg(test)]
mod budget_tests;
mod host;
mod intrinsic;
pub mod memory;
pub mod patch;
mod reflect;
mod reflect_async;
mod reflect_method;
mod reflect_value;
pub mod scheduler;
pub mod stats;
#[cfg(test)]
mod test_helpers;
mod timer;
mod waiters;

/// Disables the cumulative instruction budget while retaining poll quanta.
pub const UNLIMITED_STEPS: i64 = -1;

#[derive(Clone, Copy, Debug)]
pub struct ExecutionLimits {
    pub max_frames: usize,
    /// Physical idle frame storage, independent of guest allocation accounting.
    pub max_frame_cache_bytes: usize,
    /// Includes the current revision and the candidate being published.
    pub max_retained_revisions: usize,
    pub max_dynamic_types: usize,
    pub max_dynamic_type_bytes: u64,
    pub max_objects: usize,
    pub max_heap_bytes: u64,
    pub max_allocated_bytes: u64,
    pub max_string_bytes: usize,
    /// Scope instruction budget: 0 uses the default, -1 is unlimited.
    pub max_steps: i64,
    pub max_value_depth: usize,
    pub max_sequence_elements: usize,
    pub max_tasks: usize,
    pub max_pending_calls: usize,
    pub max_ffi_bytes: usize,
    pub max_ffi_result_bytes: usize,
}

impl Default for ExecutionLimits {
    fn default() -> Self {
        Self {
            max_frames: 1024,
            max_frame_cache_bytes: 8 << 20,
            max_retained_revisions: 128,
            max_dynamic_types: 4096,
            max_dynamic_type_bytes: 16 << 20,
            max_objects: 100_000,
            max_heap_bytes: 64 << 20,
            max_allocated_bytes: 8 << 30,
            max_string_bytes: 64 << 20,
            max_steps: 100_000_000,
            max_value_depth: 128,
            max_sequence_elements: 1_000_000,
            max_tasks: 4096,
            max_pending_calls: 65_536,
            max_ffi_bytes: 64 << 20,
            max_ffi_result_bytes: 64 << 20,
        }
    }
}

impl ExecutionLimits {
    /// Validate and resolve the step budget before allocating host resources.
    pub fn normalize(mut self) -> Result<Self, RuntimeError> {
        if self.max_steps < UNLIMITED_STEPS {
            return Err(RuntimeError::new(
                "invalid_limits",
                "max_steps",
                "expected -1, zero or a positive step limit",
            ));
        }
        if self.max_steps == 0 {
            self.max_steps = Self::default().max_steps;
        }
        Ok(self)
    }
    /// Resource envelope for a persistent, precompiled compiler guest.
    pub fn compiler() -> Self {
        Self {
            max_heap_bytes: 128 << 20,
            max_objects: 500_000,
            max_sequence_elements: 4 << 20,
            max_dynamic_types: 16_384,
            max_dynamic_type_bytes: 64 << 20,
            max_ffi_bytes: 64 << 20,
            max_ffi_result_bytes: 64 << 20,
            ..Self::default()
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum PollStatus {
    Ready,
    Running,
    Pending,
    Paused,
}

pub struct Instance {
    revision: Arc<crate::program::Revision>,
    types: crate::types::TypeRegistry,
    heap: Heap<Value>,
    memory: memory::GuestMemory,
    transient_roots: Vec<Handle>,
    globals: BTreeMap<(String, String), Handle>,
    constant_values: BTreeMap<(u64, usize), Value>,
    call_bindings: HashMap<(u64, usize, usize), (u64, usize)>,
    frames: Vec<Frame>,
    frame_pool: frame::FramePool,
    suspended_frames: Vec<Frame>,
    popped_frame: Option<(u64, usize)>,
    initialized: HashSet<String>,
    initializing: HashSet<String>,
    failed_initializations: BTreeMap<String, RuntimeError>,
    root_initialization: Option<u64>,
    cleanup: Option<crate::ffi::Shutdown<'static>>,
    cleanup_result: Option<Result<(), RuntimeError>>,
    results: Vec<Value>,
    collection_roots: Vec<Handle>,
    collect_after_bytes: u64,
    collect_after_objects: usize,
    limits: ExecutionLimits,
    steps: u64,
    pub(crate) last_poll_steps: usize,
    scheduling_phase: u8,
    scope_steps: BTreeMap<u64, u64>,
    scope_work: BTreeMap<u64, stats::ScopeWork>,
    pub(crate) changed_scopes: std::collections::BTreeSet<u64>,
    closed: bool,
    faulted: bool,
    current_task: u64,
    current_scope: u64,
    next_task: u64,
    foreground: Option<u64>,
    runnable: VecDeque<scheduler::Task>,
    blocked: waiters::Waiters,
    resuming_task: Option<scheduler::Task>,
    select_state: u64,
    ffi_session: Option<Box<dyn Session>>,
    ffi_calls: PendingCalls,
    wake: Arc<Wake>,
    lifetime: Cancellation,
    clock: Arc<dyn crate::environment::Clock>,
    entropy: Arc<dyn crate::environment::Entropy>,
    timers: timer::TimerQueue,
    reflected_types: Vec<(TypeIdentity, Value)>,
    reflected_type_indices: HashMap<TypeIdentity, usize>,
    reflected_type_keys: HashMap<String, usize>,
    // Method-set validation is immutable within a published revision. Keep only
    // successful assignments, with a fixed bound on this owner-local memo.
    interface_assignments: std::cell::RefCell<HashSet<(TypeIdentity, TypeIdentity)>>,
    scope_cancellations: BTreeMap<u64, crate::ffi::CancellationListener>,
    cancellation_events: Arc<crate::ffi::CancellationEvents>,
    pub(crate) debug: debug::DebugState,
    host_capabilities: HashSet<String>,
    retired_revisions: BTreeMap<u64, std::sync::Weak<crate::program::Revision>>,
}

impl Drop for Instance {
    fn drop(&mut self) {
        #[cfg(not(target_arch = "wasm32"))]
        let _ = self.close();
        #[cfg(target_arch = "wasm32")]
        self.begin_close();
    }
}

impl Instance {
    pub fn new(program: Arc<Program>, limits: ExecutionLimits) -> Result<Self, RuntimeError> {
        if !program.image().capabilities.is_empty() {
            return Err(RuntimeError::new(
                "capability_unavailable",
                "instance",
                "program requires host capabilities",
            ));
        }
        Self::create(program, limits)
    }

    fn create(program: Arc<Program>, limits: ExecutionLimits) -> Result<Self, RuntimeError> {
        let limits = limits.normalize()?;
        let wake = Arc::new(Wake::default());
        let mut instance = Self {
            types: program.decoded.types().clone(),
            revision: Arc::new(crate::program::Revision {
                generation: 1,
                program,
            }),
            heap: Heap::new(limits.max_objects, limits.max_heap_bytes)?,
            memory: memory::GuestMemory::default(),
            transient_roots: Vec::new(),
            globals: BTreeMap::new(),
            constant_values: BTreeMap::new(),
            call_bindings: HashMap::new(),
            frames: Vec::new(),
            frame_pool: frame::FramePool::default(),
            suspended_frames: Vec::new(),
            popped_frame: None,
            initialized: HashSet::new(),
            initializing: HashSet::new(),
            failed_initializations: BTreeMap::new(),
            root_initialization: None,
            cleanup: None,
            cleanup_result: None,
            results: Vec::new(),
            collection_roots: Vec::new(),
            collect_after_bytes: (256 << 10).min(limits.max_heap_bytes),
            collect_after_objects: 256.min(limits.max_objects),
            limits,
            steps: 0,
            last_poll_steps: 0,
            scheduling_phase: 0,
            scope_steps: BTreeMap::new(),
            scope_work: BTreeMap::new(),
            changed_scopes: Default::default(),
            closed: false,
            faulted: false,
            current_task: 0,
            current_scope: 0,
            next_task: 1,
            foreground: None,
            runnable: VecDeque::new(),
            blocked: waiters::Waiters::default(),
            resuming_task: None,
            select_state: 0,
            ffi_session: None,
            ffi_calls: PendingCalls::new(
                limits.max_pending_calls,
                limits.max_ffi_bytes,
                wake.clone(),
            ),
            wake: wake.clone(),
            lifetime: Cancellation::default(),
            clock: Arc::new(crate::environment::SystemClock::default()),
            entropy: Arc::new(crate::environment::SystemEntropy),
            timers: timer::TimerQueue::default(),
            reflected_types: Vec::new(),
            reflected_type_indices: HashMap::new(),
            reflected_type_keys: HashMap::new(),
            interface_assignments: std::cell::RefCell::new(HashSet::new()),
            scope_cancellations: BTreeMap::new(),
            cancellation_events: Arc::new(crate::ffi::CancellationEvents::new(wake.clone())),
            debug: debug::DebugState::default(),
            host_capabilities: HashSet::new(),
            retired_revisions: BTreeMap::new(),
        };
        let globals: Vec<_> = instance
            .revision
            .program
            .decoded
            .artifacts()
            .iter()
            .flat_map(|(module, artifact)| {
                artifact
                    .globals
                    .iter()
                    .map(move |global| (module.clone(), global.clone()))
            })
            .collect();
        for (module, global) in globals {
            let typ = instance.types.resolve(&module, &global.r#type)?;
            let value = Value {
                typ,
                data: Data::Uninitialized,
            };
            let handle = instance.allocate(value)?;
            instance.globals.insert((module, global.id), handle);
        }
        Ok(instance)
    }

    pub fn with_bridge(
        program: Arc<Program>,
        limits: ExecutionLimits,
        bridge: &dyn Bridge,
    ) -> Result<Self, RuntimeError> {
        let mut capabilities = HashSet::new();
        for capability in bridge.capabilities() {
            if capability.is_empty()
                || capability.trim() != capability
                || !capabilities.insert(capability)
            {
                return Err(RuntimeError::new(
                    "invalid_capabilities",
                    "instance",
                    "host capabilities must be nonempty, trimmed and unique",
                ));
            }
        }
        if program
            .image()
            .capabilities
            .iter()
            .any(|capability| !capabilities.contains(capability))
        {
            return Err(RuntimeError::new(
                "capability_unavailable",
                "instance",
                "program requires an unavailable host capability",
            ));
        }
        let mut instance = Self::create(program, limits)?;
        instance.host_capabilities = capabilities;
        instance.ffi_session = Some(bridge.open(instance.lifetime.clone())?);
        Ok(instance)
    }

    pub fn wake(&self) -> Arc<Wake> {
        self.wake.clone()
    }

    pub fn root_ready(&self) -> bool {
        !self.closed
            && self.root_initialization.is_none()
            && self
                .initialized
                .contains(&self.revision.program.image().root)
    }

    /// Complete root initialization before publishing an instance to callers.
    #[cfg(not(target_arch = "wasm32"))]
    pub fn initialize_root(&mut self, cancellation: &Cancellation) -> Result<(), RuntimeError> {
        loop {
            let observed = self.wake.epoch();
            match self.poll_initialize(cancellation, 4096) {
                Ok(PollStatus::Ready) => return Ok(()),
                Ok(PollStatus::Running) => continue,
                Ok(PollStatus::Pending) => self.wake.wait(
                    observed,
                    self.next_timer_delay()
                        .unwrap_or(std::time::Duration::from_millis(10))
                        .min(std::time::Duration::from_millis(10)),
                ),
                Ok(PollStatus::Paused) => {
                    return Err(RuntimeError::new(
                        "paused",
                        "instance",
                        "initialization paused",
                    ));
                }
                Err(error) => {
                    let _ = self.close();
                    return Err(error);
                }
            }
        }
    }

    /// Advance initialization using the same budget and events as normal calls.
    pub fn poll_initialize(
        &mut self,
        cancellation: &Cancellation,
        count: usize,
    ) -> Result<PollStatus, RuntimeError> {
        if self.closed || self.faulted {
            return Err(RuntimeError::new(
                "closed",
                "instance",
                "initialization is unavailable",
            ));
        }
        if cancellation.is_cancelled() {
            self.begin_close();
            return Err(RuntimeError::new(
                "canceled",
                "instance",
                "initialization canceled",
            ));
        }
        let root = self.revision.program.image().root.clone();
        if self.root_initialization.is_none() && self.initialized.contains(&root) {
            return Ok(PollStatus::Ready);
        }
        if self.root_initialization.is_none()
            && (self.foreground.is_some() || !self.frames.is_empty())
        {
            return Err(RuntimeError::new(
                "busy",
                "instance",
                "instance has active work",
            ));
        }
        let result = (|| {
            if self.root_initialization.is_none() {
                if !self
                    .revision
                    .program
                    .functions
                    .contains_key(&(root.clone(), "fn.init".to_owned()))
                {
                    self.initialized.insert(root);
                    return Ok(PollStatus::Ready);
                }
                let task = self.allocate_task_id()?;
                self.current_task = task;
                self.current_scope = task;
                self.scope_steps.insert(task, 0);
                self.scope_work.insert(
                    task,
                    stats::ScopeWork {
                        tasks: 1,
                        ..Default::default()
                    },
                );
                self.changed_scopes.insert(task);
                self.root_initialization = Some(task);
                self.initialize_module(&root)?;
                self.foreground = Some(task);
            }
            let status = self.poll_steps(count)?;
            if status == PollStatus::Ready {
                let task = self.root_initialization.take().unwrap();
                if !self.scope_active(task) {
                    self.release_scope(task);
                }
            }
            Ok(status)
        })();
        if result.is_err() {
            self.begin_close();
        }
        result
    }

    pub fn set_environment(
        &mut self,
        clock: Arc<dyn crate::environment::Clock>,
        entropy: Arc<dyn crate::environment::Entropy>,
    ) -> Result<(), RuntimeError> {
        if self.foreground.is_some()
            || !self.frames.is_empty()
            || !self.runnable.is_empty()
            || !self.blocked.is_empty()
            || !self.timers.is_empty()
        {
            return Err(RuntimeError::new(
                "busy",
                "environment",
                "instance has live work",
            ));
        }
        self.clock = clock;
        self.entropy = entropy;
        Ok(())
    }

    /// Results remain rooted until the next start or close. Guest references in
    /// cloned values belong to this instance and are not persistent host roots.
    pub fn results(&self) -> &[Value] {
        &self.results
    }

    pub fn snapshot_results(
        &self,
        limits: crate::snapshot::SnapshotLimits,
    ) -> Result<crate::snapshot::HostSnapshot, RuntimeError> {
        crate::snapshot::HostSnapshot::capture(&self.heap, &self.types, &self.results, limits)
    }
    pub fn heap_stats(&self) -> HeapStats {
        self.heap.stats()
    }
    pub fn steps(&self) -> u64 {
        self.steps
    }

    pub fn start(&mut self, entry: &str, arguments: Vec<Value>) -> Result<(), RuntimeError> {
        if self.closed {
            return Err(RuntimeError::new(
                "closed",
                "instance",
                "instance is closed",
            ));
        }
        if self.faulted {
            return Err(RuntimeError::new(
                "faulted",
                "instance",
                "instance execution failed",
            ));
        }
        if self.foreground.is_some() {
            return Err(RuntimeError::new("busy", "instance", "execution is active"));
        }
        self.types
            .use_context(self.revision.program.decoded.types());
        let entry = self
            .revision
            .program
            .image()
            .entries
            .iter()
            .find(|value| value.name == entry)
            .cloned()
            .ok_or_else(|| RuntimeError::new("missing_entry", "instance", entry))?;
        let task_id = self.allocate_task_id()?;
        let result_count = self
            .revision
            .program
            .function(&entry.module_path, &entry.function_id)?
            .declaration
            .signature
            .results
            .len();
        for frame in &self.frames {
            frame.trace(&mut |handle| self.transient_roots.push(handle));
        }
        self.suspended_frames = std::mem::take(&mut self.frames);
        let initialized = self.initialized.clone();
        let initializing = self.initializing.clone();
        let result = self
            .push_frame(
                FunctionValue {
                    index: None,
                    revision: None,
                    module: entry.module_path.clone().into(),
                    function: entry.function_id.into(),
                    captures: Vec::new(),
                },
                arguments,
                result_count,
                false,
            )
            .and_then(|()| {
                let frames = std::mem::take(&mut self.frames);
                let result = self.charge_guest(128);
                self.frames = frames;
                result
            });
        let preparing_initialization = result.is_ok();
        let result = result.and_then(|()| self.initialize_module(&entry.module_path).map(|_| ()));
        let prepared_frames =
            std::mem::replace(&mut self.frames, std::mem::take(&mut self.suspended_frames));
        if let Err(error) = result {
            self.initialized = initialized;
            self.initializing = initializing;
            if preparing_initialization {
                for frame in &prepared_frames {
                    self.memory.recycle_frame_storage(
                        frame.revision.generation,
                        frame.prepared.module_index,
                        frame.prepared.index,
                        frame.memory,
                    );
                }
            }
            drop(prepared_frames);
            self.collect_at_boundary()?;
            return Err(error);
        }
        self.frame_pool.recycle_operands(
            std::mem::take(&mut self.results),
            self.limits.max_frame_cache_bytes,
        );
        let active: HashSet<_> = self
            .runnable
            .iter()
            .chain(self.blocked.iter())
            .map(|task| task.scope)
            .chain(self.timers.iter().map(|timer| timer.scope))
            .chain((!self.frames.is_empty()).then_some(self.current_scope))
            .collect();
        self.scope_steps.retain(|scope, _| active.contains(scope));
        self.scope_work.retain(|scope, _| active.contains(scope));
        self.debug
            .scope_profile_dropped
            .retain(|scope, _| active.contains(scope));
        self.yield_task();
        self.current_task = task_id;
        self.current_scope = task_id;
        self.foreground = Some(task_id);
        self.scope_steps.insert(task_id, 0);
        self.scope_work.insert(
            task_id,
            stats::ScopeWork {
                tasks: 1,
                ..Default::default()
            },
        );
        self.changed_scopes.insert(task_id);
        self.frames = prepared_frames;
        self.transient_roots.clear();
        Ok(())
    }

    /// Starts a binary ABI entry with an owned guest byte slice.
    pub fn start_bytes(&mut self, entry: &str, bytes: &[u8]) -> Result<(), RuntimeError> {
        if self.closed {
            return Err(RuntimeError::new(
                "closed",
                "instance",
                "instance is closed",
            ));
        }
        if self.faulted {
            return Err(RuntimeError::new(
                "faulted",
                "instance",
                "instance execution failed",
            ));
        }
        if self.foreground.is_some() {
            return Err(RuntimeError::new("busy", "instance", "execution is active"));
        }
        if bytes.len() > self.limits.max_sequence_elements
            || bytes.len() as u64 > self.limits.max_heap_bytes
        {
            return Err(RuntimeError::new(
                "boundary_limit",
                "argument",
                "byte argument exceeds instance limits",
            ));
        }
        let argument = self.make_bytes(
            TypeIdentity::Slice(std::sync::Arc::new(TypeIdentity::Primitive(
                wire::PrimitiveUint8,
            ))),
            bytes.len(),
            bytes.len(),
            bytes,
        )?;
        let result = self
            .charge_guest(128 + bytes.len() as u64)
            .and_then(|()| self.start(entry, vec![argument]));
        if result.is_err() {
            self.collect_at_boundary()?;
        }
        result
    }

    pub fn poll_steps(&mut self, count: usize) -> Result<PollStatus, RuntimeError> {
        self.poll(count, false)
    }

    pub fn poll_background(&mut self, count: usize) -> Result<PollStatus, RuntimeError> {
        if self.foreground.is_some() {
            return Err(RuntimeError::new(
                "busy",
                "instance",
                "foreground execution is active",
            ));
        }
        self.poll(count, true)
    }

    fn poll(&mut self, count: usize, background: bool) -> Result<PollStatus, RuntimeError> {
        self.last_poll_steps = 0;
        if self.closed {
            return Err(RuntimeError::new(
                "closed",
                "instance",
                "instance is closed",
            ));
        }
        if self.faulted {
            return Err(RuntimeError::new(
                "faulted",
                "instance",
                "instance execution failed",
            ));
        }
        while self.last_poll_steps < count {
            self.cancel_requested_scopes()?;
            if self.debug.paused {
                return Ok(PollStatus::Paused);
            }
            if !background && self.foreground.is_none()
                || background
                    && self.frames.is_empty()
                    && self.runnable.is_empty()
                    && self.blocked.is_empty()
                    && self.timers.is_empty()
            {
                return Ok(PollStatus::Ready);
            }
            if let Err(error) = self.deliver_timers().and_then(|()| self.resume_blocked()) {
                self.faulted = true;
                self.abort()?;
                return Err(error);
            }
            if self.frames.is_empty() && !self.schedule_next() {
                return Ok(PollStatus::Pending);
            }
            let frame = self.frames.last().unwrap();
            let instruction_step = frame.returning.is_none()
                && frame.resume.is_none()
                && frame.tail_return.is_none()
                && frame.pc < frame.prepared.code.len();
            if instruction_step && self.debug_before_instruction() {
                return Ok(PollStatus::Paused);
            }
            if instruction_step
                && self.limits.max_steps > 0
                && self.scope_steps[&self.current_scope] >= self.limits.max_steps as u64
            {
                let foreground = self.foreground == Some(self.current_scope);
                self.cancel_scope(self.current_scope)?;
                if !foreground {
                    continue;
                }
                return Err(RuntimeError::new(
                    "step_limit",
                    "instance",
                    "instruction budget exhausted",
                ));
            }
            let scope = self.current_scope;
            let outcome = self.step();
            if instruction_step {
                self.steps = self.steps.saturating_add(1);
                let steps = self.scope_steps.entry(scope).or_default();
                *steps = steps.saturating_add(1);
                self.last_poll_steps += 1;
                self.scheduling_phase = (self.scheduling_phase + 1) % 64;
                if self.debug.profile_every != 0 {
                    self.debug.profile_phase =
                        if self.debug.profile_phase >= self.debug.profile_every - 1 {
                            0
                        } else {
                            self.debug.profile_phase + 1
                        };
                }
                self.changed_scopes.insert(scope);
            }
            if let Err(mut error) = outcome {
                if error.code == "panic" && !self.frames.is_empty() {
                    let frame = self.frames.last_mut().unwrap();
                    frame.panic = Some(Arc::new(Value {
                        typ: TypeIdentity::Primitive(wire::PrimitiveString),
                        data: Data::String(error.message.into_bytes().into()),
                    }));
                    frame.returning = Some(Vec::new());
                    self.debug_panic();
                } else {
                    let initialization_failed =
                        error.code == "internal" && error.path == "module_init";
                    if let Some(frame) = self.frames.last() {
                        error.path = format!(
                            "{}/{}@{}: {}",
                            frame.module,
                            frame.function,
                            frame.pc.saturating_sub(1),
                            error.path
                        );
                    }
                    if matches!(
                        error.code,
                        "allocation_limit"
                            | "value_limit"
                            | "string_limit"
                            | "task_limit"
                            | "frame_limit"
                            | "dynamic_type_limit"
                            | "dynamic_type_bytes_limit"
                            | "boundary_limit"
                    ) || initialization_failed
                    {
                        let foreground = self.foreground == Some(scope);
                        self.cancel_scope(scope)?;
                        if !foreground {
                            continue;
                        }
                    } else {
                        self.faulted = true;
                        self.abort()?;
                    }
                    return Err(error);
                }
            }
            self.transient_roots.clear();
            if let Some((task, index)) = self.popped_frame.take() {
                let frames = if task == self.current_task {
                    Some(&mut self.frames)
                } else {
                    self.runnable
                        .iter_mut()
                        .chain(self.blocked.iter_mut())
                        .find(|candidate| candidate.id == task)
                        .map(|task| &mut task.frames)
                };
                if let Some(frame) = frames.and_then(|frames| frames.get_mut(index)) {
                    frame.popped_roots.clear();
                }
            }
            if self.debug.paused {
                return Ok(PollStatus::Paused);
            }
            if instruction_step
                && self.scheduling_phase == 0
                && !self
                    .frames
                    .iter()
                    .any(|frame| frame.prepared.declaration.no_switch)
            {
                self.yield_task();
            }
        }
        Ok(
            if !background && self.foreground.is_none()
                || background
                    && self.frames.is_empty()
                    && self.runnable.is_empty()
                    && self.blocked.is_empty()
                    && self.timers.is_empty()
            {
                PollStatus::Ready
            } else {
                PollStatus::Running
            },
        )
    }

    pub fn cancel(&mut self) -> Result<(), RuntimeError> {
        if let Some(scope) = self.foreground {
            self.cancel_scope(scope)?;
        }
        Ok(())
    }

    pub(crate) fn foreground_scope(&self) -> Option<u64> {
        self.foreground
    }

    pub(crate) fn watch_scope_cancellation(&mut self, scope: u64, cancellation: Cancellation) {
        let listener = cancellation.listen(scope, &self.cancellation_events);
        self.scope_cancellations.insert(scope, listener);
    }

    pub(crate) fn cancel_requested_scopes(&mut self) -> Result<Vec<u64>, RuntimeError> {
        let scopes = self.cancellation_events.take();
        for scope in &scopes {
            if self.scope_cancellations.contains_key(scope) {
                self.cancel_scope(*scope)?;
            }
        }
        Ok(scopes.into_iter().collect())
    }

    pub(crate) fn release_scope(&mut self, scope: u64) {
        self.debug.scope_profile_dropped.remove(&scope);
        self.scope_cancellations.remove(&scope);
        self.scope_steps.remove(&scope);
        self.scope_work.remove(&scope);
    }

    pub(crate) fn scope_active(&self, scope: u64) -> bool {
        self.scope_work
            .get(&scope)
            .is_some_and(|work| work.tasks != 0 || work.timers != 0)
    }

    pub(crate) fn cancel_scope(&mut self, scope: u64) -> Result<(), RuntimeError> {
        self.scope_work.insert(scope, stats::ScopeWork::default());
        self.changed_scopes.insert(scope);
        self.debug_cancel_scope(scope);
        self.scope_cancellations.remove(&scope);
        let mut removed = Vec::new();
        if self.current_scope == scope && !self.frames.is_empty() {
            removed.push(scheduler::Task {
                id: self.current_task,
                scope,
                frames: std::mem::take(&mut self.frames),
                blocked: None,
            });
        }
        let mut runnable = VecDeque::new();
        for task in self.runnable.drain(..) {
            if task.scope == scope {
                removed.push(task);
            } else {
                runnable.push_back(task);
            }
        }
        self.runnable = runnable;
        removed.extend(self.blocked.remove_scope(scope));
        for task in removed {
            for frame in &task.frames {
                if frame.initializing {
                    self.initializing.remove(frame.module.as_ref());
                    self.failed_initializations.insert(
                        frame.module.to_string(),
                        RuntimeError::new(
                            "internal",
                            "module_init",
                            "module initialization aborted",
                        ),
                    );
                }
                self.memory.recycle_frame_storage(
                    frame.revision.generation,
                    frame.prepared.module_index,
                    frame.prepared.index,
                    frame.memory,
                );
                if let Some(reflect_async::IntrinsicResume::Select { tokens, .. }) = &frame.resume {
                    for token in tokens {
                        self.finish_token(*token, true)?;
                    }
                }
            }
            if let Some(scheduler::Blocked::Ffi(id)) = task.blocked {
                self.ffi_calls.cancel(id);
            } else if let Some(scheduler::Blocked::Receive { token, .. }) = task.blocked {
                self.finish_token(token, true)?;
            } else if let Some(scheduler::Blocked::WaitSet(set)) = task.blocked {
                let scheduler::Resource::WaitSet(tokens) =
                    self.resource(Self::resource_handle(&set)?)?.clone()
                else {
                    unreachable!()
                };
                for token in tokens {
                    self.finish_token(token, true)?;
                }
            }
        }
        self.timers.retain(|timer| timer.scope != scope);
        if self.foreground == Some(scope) {
            self.foreground = None;
            self.results.clear();
        }
        self.collect_at_boundary().map(|_| ())
    }

    #[cfg(not(target_arch = "wasm32"))]
    pub fn close(&mut self) -> Result<(), RuntimeError> {
        self.begin_close();
        let waker = std::task::Waker::from(self.wake.clone());
        let mut cx = std::task::Context::from_waker(&waker);
        loop {
            let observed = self.wake.epoch();
            if let std::task::Poll::Ready(result) = self.poll_close(&mut cx) {
                return result;
            }
            self.wake
                .wait(observed, std::time::Duration::from_millis(10));
        }
    }

    pub fn begin_close(&mut self) {
        if self.cleanup.is_some() || self.cleanup_result.is_some() {
            return;
        }
        self.debug = debug::DebugState::default();
        self.closed = true;
        self.lifetime.cancel();
        self.ffi_calls.close();
        self.globals.clear();
        self.constant_values.clear();
        self.call_bindings.clear();
        self.frame_pool = frame::FramePool::default();
        self.retired_revisions.clear();
        self.reflected_types.clear();
        self.reflected_type_indices.clear();
        self.reflected_type_keys.clear();
        self.interface_assignments.get_mut().clear();
        self.failed_initializations.clear();
        self.types.clear_dynamic();
        let accounting = self.heap.set_external_bytes(0);
        let aborted = self.abort();
        self.memory.stats.live_bytes = 0;
        self.memory.stats.allocated_since_sweep = 0;
        self.memory.retain_revisions(|_| false);
        self.scope_steps.clear();
        let session = self.ffi_session.take();
        self.cleanup = Some(Box::pin(async move {
            let cleanup = match session {
                Some(session) => session.shutdown_async().await,
                None => Ok(()),
            };
            accounting?;
            aborted?;
            cleanup
        }));
    }

    pub fn poll_close(
        &mut self,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Result<(), RuntimeError>> {
        self.begin_close();
        if let Some(result) = &self.cleanup_result {
            return std::task::Poll::Ready(result.clone());
        }
        let result = match self.cleanup.as_mut() {
            Some(cleanup) => match cleanup.as_mut().poll(cx) {
                std::task::Poll::Pending => return std::task::Poll::Pending,
                std::task::Poll::Ready(result) => result,
            },
            None => Ok(()),
        };
        self.cleanup = None;
        self.cleanup_result = Some(result.clone());
        std::task::Poll::Ready(result)
    }

    fn abort(&mut self) -> Result<(), RuntimeError> {
        // A failed initializer must not publish a partially initialized module.
        // Poison the instance if initialization was interrupted.
        if !self.initializing.is_empty() {
            self.closed = true;
            self.globals.clear();
        }
        self.initializing.clear();
        for frame in self
            .frames
            .iter()
            .chain(&self.suspended_frames)
            .chain(self.runnable.iter().flat_map(|task| &task.frames))
            .chain(self.blocked.iter().flat_map(|task| &task.frames))
        {
            self.memory.recycle_frame_storage(
                frame.revision.generation,
                frame.prepared.module_index,
                frame.prepared.index,
                frame.memory,
            );
        }
        self.frames.clear();
        self.suspended_frames.clear();
        for task in self.blocked.iter() {
            if let Some(scheduler::Blocked::Ffi(id)) = task.blocked {
                self.ffi_calls.cancel(id);
            }
        }
        self.runnable.clear();
        self.blocked.clear();
        self.changed_scopes.extend(self.scope_work.keys().copied());
        for work in self.scope_work.values_mut() {
            *work = stats::ScopeWork::default();
        }
        self.foreground = None;
        self.scope_cancellations.clear();
        self.timers.clear();
        self.results.clear();
        self.collect_at_boundary().map(|_| ())
    }

    fn load_prepared_constant(
        &mut self,
        revision: &Arc<crate::program::Revision>,
        index: usize,
    ) -> Result<Value, RuntimeError> {
        let key = (revision.generation, index);
        if let Some(value) = self.constant_values.get(&key) {
            return Ok(value.clone());
        }
        match &revision.program.constant_data[index] {
            crate::program::PreparedConstant::Scalar(value) => Ok(value.clone()),
            crate::program::PreparedConstant::Failure(error) => Err(error.clone()),
            crate::program::PreparedConstant::Metadata => Err(RuntimeError::new(
                "invalid_constant",
                "constant",
                "untyped constant cannot be executed",
            )),
            crate::program::PreparedConstant::Bytes { typ, bytes } => {
                if bytes.len() > self.limits.max_sequence_elements {
                    return Err(RuntimeError::new(
                        "value_limit",
                        "constant",
                        "byte constant exceeds collection limit",
                    ));
                }
                let value = self.make_bytes(typ.clone(), bytes.len(), bytes.len(), bytes)?;
                self.constant_values.insert(key, value.clone());
                Ok(value)
            }
        }
    }

    fn zero(&self, typ: &TypeIdentity, depth: usize) -> Result<Value, RuntimeError> {
        let mut remaining = self.limits.max_heap_bytes;
        Value::zero_with_budget(
            &self.types,
            typ,
            depth,
            self.limits.max_value_depth,
            self.limits.max_sequence_elements,
            &mut remaining,
        )
    }

    fn initialize_module(&mut self, module: &str) -> Result<bool, RuntimeError> {
        if let Some(error) = self.failed_initializations.get(module) {
            return Err(error.clone());
        }
        if self.initialized.contains(module) {
            return Ok(false);
        }
        if self.initializing.contains(module) {
            return Err(RuntimeError::new(
                "internal",
                "module_init",
                "recursive module initialization",
            ));
        }
        if !self
            .revision
            .program
            .functions
            .contains_key(&(module.to_owned(), "fn.init".to_owned()))
        {
            self.initialized.insert(module.to_owned());
            return Ok(false);
        }
        let prepared = self.push_frame(
            FunctionValue {
                index: None,
                revision: None,
                module: module.into(),
                function: "fn.init".into(),
                captures: Vec::new(),
            },
            Vec::new(),
            0,
            true,
        );
        if let Err(error) = prepared {
            self.failed_initializations
                .insert(module.to_owned(), error.clone());
            return Err(error);
        }
        self.initializing.insert(module.to_owned());
        Ok(true)
    }

    fn pop(&mut self) -> Result<Value, RuntimeError> {
        let value = self
            .frames
            .last_mut()
            .and_then(|frame| frame.stack.pop())
            .ok_or_else(|| {
                RuntimeError::new("invalid_stack", "frame", "operand stack underflow")
            })?;
        value.trace(&mut |handle| self.transient_roots.push(handle));
        Ok(value)
    }

    fn pop_values(&mut self, count: usize) -> Result<Vec<Value>, RuntimeError> {
        let mut values = self.frame_pool.take_operands(count);
        self.popped_frame = Some((self.current_task, self.frames.len() - 1));
        let frame = self.frames.last_mut().unwrap();
        frame.memory.popped = memory::grow_frame_buffer(frame.memory.popped, count, false)?;
        let stack = &mut frame.stack;
        let start = stack.len().checked_sub(count).ok_or_else(|| {
            RuntimeError::new("invalid_stack", "frame", "operand stack underflow")
        })?;
        values.extend(stack.drain(start..));
        frame.popped_roots.capture(&values);
        for value in &values {
            value.trace(&mut |handle| self.transient_roots.push(handle));
        }
        Ok(values)
    }

    fn coerce(&self, mut value: Value, typ: &TypeIdentity) -> Result<Value, RuntimeError> {
        if let Data::String(bytes) = &value.data {
            self.check_string_size(bytes.len())?;
        }
        let registry = &self.types;
        if registry.identical(&value.typ, typ)? {
            value.typ = typ.clone();
            return Ok(value);
        }
        let interface = registry.is_interface(typ)?;
        if interface {
            if let Data::Interface(dynamic) = value.data {
                value = *dynamic;
            }
            if matches!(value.data, Data::Nil) && registry.is_interface(&value.typ)? {
                return Ok(Value {
                    typ: typ.clone(),
                    data: Data::Nil,
                });
            }
            let assignment = (value.typ.clone(), typ.clone());
            if !self.interface_assignments.borrow().contains(&assignment) {
                if !registry.implements(&value.typ, typ)? {
                    return Err(RuntimeError::new(
                        "type_error",
                        "assignment",
                        "dynamic type does not implement interface",
                    ));
                }
                let mut assignments = self.interface_assignments.borrow_mut();
                if assignments.len() == 256 {
                    assignments.clear();
                }
                assignments.insert(assignment);
            }
            return Ok(Value {
                typ: typ.clone(),
                data: Data::Interface(Box::new(value)),
            });
        }
        if registry.channel_assignable(&value.typ, typ)? {
            value.typ = typ.clone();
            return Ok(value);
        }
        if value.typ == TypeIdentity::Any
            && matches!(value.data, Data::Nil)
            && registry.nil_assignable(typ)?
        {
            return self.zero(typ, 0);
        }
        if let Data::Function(callee) = &value.data {
            let revision = callee.revision.as_ref().unwrap_or(&self.revision);
            let function = revision
                .program
                .function(&callee.module, &callee.function)?;
            if let Some((module, node)) = registry.node(typ)?
                && let Some(signature) = &node.signature
                && registry.signature_identical_from(
                    registry,
                    module,
                    signature,
                    revision.program.decoded.types(),
                    &callee.module,
                    &function.declaration.signature,
                )?
            {
                value.typ = typ.clone();
                return Ok(value);
            }
        }
        let source = registry.underlying(&value.typ)?;
        let destination = registry.underlying(typ)?;
        if (!matches!(value.typ, TypeIdentity::Named(_)) || !matches!(typ, TypeIdentity::Named(_)))
            && registry.identical(&source, &destination)?
        {
            value.typ = typ.clone();
            return Ok(value);
        }
        Err(RuntimeError::new(
            "type_error",
            "assignment",
            format!("cannot assign {:?} to {:?}", value.typ, typ),
        ))
    }

    fn element_type(&self, typ: &TypeIdentity) -> Result<TypeIdentity, RuntimeError> {
        if let TypeIdentity::Slice(element) = typ {
            return Ok((**element).clone());
        }
        let (module, node) = self
            .types
            .node(typ)?
            .ok_or_else(|| RuntimeError::new("type_error", "container", "missing element type"))?;
        self.types.resolve(module, &node.elem)
    }

    fn check_string_size(&self, length: usize) -> Result<(), RuntimeError> {
        if length > self.limits.max_string_bytes {
            return Err(RuntimeError::new(
                "string_limit",
                "string",
                "string exceeds byte limit",
            ));
        }
        if length as u64 > self.limits.max_heap_bytes {
            return Err(RuntimeError::new(
                "allocation_limit",
                "string",
                "string exceeds heap budget",
            ));
        }
        Ok(())
    }

    fn slice_bytes(&self, value: &Value) -> Result<Vec<u8>, RuntimeError> {
        if let Data::String(bytes) = &value.data {
            return Ok(bytes.to_vec());
        }
        if let Data::Slice(slice) = &value.data {
            let backing = self.borrow_address(&slice.storage)?;
            if let Data::Bytes(bytes) = &backing.data {
                return bytes
                    .get(slice.start..slice.start + slice.length)
                    .map(<[u8]>::to_vec)
                    .ok_or_else(|| {
                        RuntimeError::new("invalid_slice", "bytes", "invalid byte view")
                    });
            }
        }
        self.slice_values(value)?
            .into_iter()
            .map(|value| match value.data {
                Data::Unsigned(byte) if byte <= 255 => Ok(byte as u8),
                _ => Err(RuntimeError::new(
                    "type_error",
                    "bytes",
                    "expected byte elements",
                )),
            })
            .collect()
    }

    fn slice_values(&self, value: &Value) -> Result<Vec<Value>, RuntimeError> {
        match &value.data {
            Data::Slice(slice) => {
                let backing = self.borrow_address(&slice.storage)?;
                if let Data::Bytes(bytes) = &backing.data {
                    return bytes
                        .get(slice.start..slice.start + slice.length)
                        .ok_or_else(|| {
                            RuntimeError::new("invalid_slice", "slice", "invalid byte view")
                        })
                        .map(|bytes| {
                            bytes
                                .iter()
                                .map(|byte| Value {
                                    typ: TypeIdentity::Primitive(wire::PrimitiveUint8),
                                    data: Data::Unsigned(u64::from(*byte)),
                                })
                                .collect()
                        });
                }
                let Data::Array(values) = &backing.data else {
                    return Err(RuntimeError::new(
                        "invalid_slice",
                        "slice",
                        "invalid backing",
                    ));
                };
                Ok(values
                    .get(slice.start..slice.start + slice.length)
                    .ok_or_else(|| RuntimeError::new("invalid_slice", "slice", "invalid view"))?
                    .to_vec())
            }
            Data::Nil => Ok(Vec::new()),
            Data::String(bytes) => Ok(bytes
                .iter()
                .map(|byte| Value {
                    typ: TypeIdentity::Primitive(wire::PrimitiveUint8),
                    data: Data::Unsigned(u64::from(*byte)),
                })
                .collect()),
            _ => Err(RuntimeError::new("type_error", "slice", "expected slice")),
        }
    }

    fn make_bytes(
        &mut self,
        typ: TypeIdentity,
        length: usize,
        capacity: usize,
        initial: &[u8],
    ) -> Result<Value, RuntimeError> {
        if initial.len() > length
            || length > capacity
            || capacity > self.limits.max_sequence_elements
            || (capacity as u64).saturating_add(272) > self.limits.max_heap_bytes
        {
            return Err(RuntimeError::new(
                "allocation_limit",
                "bytes",
                "byte backing exceeds instance limits",
            ));
        }
        let mut bytes = Vec::new();
        bytes.try_reserve_exact(capacity).map_err(|_| {
            RuntimeError::new(
                "allocation_limit",
                "bytes",
                "byte backing allocation failed",
            )
        })?;
        bytes.extend_from_slice(initial);
        bytes.resize(capacity, 0);
        let root = self.allocate(Value {
            typ: TypeIdentity::Any,
            data: Data::Bytes(bytes),
        })?;
        Ok(Value {
            typ,
            data: Data::Slice(SliceValue {
                identity: std::sync::Arc::default(),
                storage: Address {
                    identity: std::sync::Arc::default(),
                    root,
                    path: Vec::new(),
                },
                start: 0,
                length,
                capacity,
            }),
        })
    }

    fn make_slice(
        &mut self,
        typ: TypeIdentity,
        length: usize,
        capacity: usize,
        initial: Vec<Value>,
    ) -> Result<Value, RuntimeError> {
        let element = self.element_type(&typ)?;
        if self
            .types
            .identical(&element, &TypeIdentity::Primitive(wire::PrimitiveUint8))?
        {
            if !matches!(typ, TypeIdentity::Slice(_))
                && !self
                    .types
                    .node(&typ)?
                    .is_some_and(|(_, node)| node.kind == wire::Slice)
            {
                return Err(RuntimeError::new(
                    "type_error",
                    "slice",
                    "expected slice type",
                ));
            }
            let bytes = initial
                .into_iter()
                .map(|value| {
                    let value = self.coerce(value, &element)?;
                    match value.data {
                        Data::Unsigned(byte) => Ok(byte as u8),
                        _ => Err(RuntimeError::new(
                            "type_error",
                            "slice",
                            "expected byte element",
                        )),
                    }
                })
                .collect::<Result<Vec<_>, _>>()?;
            return self.make_bytes(typ, length, capacity, &bytes);
        }
        self.make_slot_slice(typ, length, capacity, initial)
    }

    fn make_slot_slice(
        &mut self,
        typ: TypeIdentity,
        length: usize,
        capacity: usize,
        initial: Vec<Value>,
    ) -> Result<Value, RuntimeError> {
        if !matches!(typ, TypeIdentity::Slice(_))
            && !self
                .types
                .node(&typ)?
                .is_some_and(|(_, node)| node.kind == wire::Slice)
        {
            return Err(RuntimeError::new(
                "type_error",
                "slice",
                "expected slice type",
            ));
        }
        if length > capacity || capacity > self.limits.max_sequence_elements {
            return Err(RuntimeError::new(
                "value_limit",
                "slice",
                "invalid or excessive slice capacity",
            ));
        }
        let element = self.element_type(&typ)?;
        if initial.len() > length {
            return Err(RuntimeError::new(
                "invalid_slice",
                "slice",
                "too many initial elements",
            ));
        }
        let zero = self.zero(&element, 0)?;
        let initial = initial
            .into_iter()
            .map(|value| self.coerce(value, &element))
            .collect::<Result<Vec<_>, _>>()?;
        let zero_bytes = zero.logical_bytes()?.saturating_sub(16);
        let mut logical = (capacity as u64)
            .checked_mul(16)
            .and_then(|bytes| bytes.checked_add(272))
            .and_then(|bytes| {
                zero_bytes
                    .checked_mul((capacity - initial.len()) as u64)?
                    .checked_add(bytes)
            })
            .ok_or_else(|| {
                RuntimeError::new("allocation_limit", "slice", "backing size overflow")
            })?;
        for value in &initial {
            logical = logical
                .checked_add(value.logical_bytes()?.saturating_sub(16))
                .ok_or_else(|| {
                    RuntimeError::new("allocation_limit", "slice", "backing size overflow")
                })?;
        }
        if logical > self.limits.max_heap_bytes {
            return Err(RuntimeError::new(
                "allocation_limit",
                "slice",
                "backing exceeds heap budget",
            ));
        }
        let mut values = Vec::new();
        values.try_reserve_exact(capacity).map_err(|_| {
            RuntimeError::new(
                "allocation_limit",
                "slice",
                "slot backing allocation failed",
            )
        })?;
        values.resize(capacity, zero);
        for (destination, value) in values.iter_mut().zip(initial) {
            *destination = value;
        }
        let root = self.allocate(Value {
            typ: TypeIdentity::Any,
            data: Data::Array(values),
        })?;
        Ok(Value {
            typ,
            data: Data::Slice(SliceValue {
                identity: std::sync::Arc::default(),
                storage: Address {
                    identity: std::sync::Arc::default(),
                    root,
                    path: Vec::new(),
                },
                start: 0,
                length,
                capacity,
            }),
        })
    }

    fn index_value(&mut self, object: &Value, key: &Value) -> Result<(Value, bool), RuntimeError> {
        if let Data::Map(root) = object.data {
            let key = self.map_key(&object.typ, key.clone())?;
            let Data::MapEntries(entries) = &self.heap.get(root)?.data else {
                return Err(RuntimeError::new("invalid_map", "map", "invalid backing"));
            };
            if let Some(index) = entries.find(&key, &self.types)? {
                return Ok((entries[index].1.clone(), true));
            }
            return Ok((self.zero(&self.element_type(&object.typ)?, 0)?, false));
        }
        if matches!(object.data, Data::Nil)
            && self
                .types
                .node(&object.typ)?
                .is_some_and(|(_, node)| node.kind == wire::Map)
        {
            self.map_key(&object.typ, key.clone())?;
            return Ok((self.zero(&self.element_type(&object.typ)?, 0)?, false));
        }
        let index = usize::try_from(key.integer()?)
            .map_err(|_| RuntimeError::new("panic", "index", "negative index"))?;
        let value = match &object.data {
            Data::Array(values) => values.get(index).cloned(),
            Data::String(bytes) => bytes.get(index).map(|byte| Value {
                typ: TypeIdentity::Primitive(wire::PrimitiveUint8),
                data: Data::Unsigned(u64::from(*byte)),
            }),
            Data::Slice(slice) if index < slice.length => {
                let mut address = slice.storage.clone();
                address.path.push(PathElement::Index(slice.start + index));
                Some(self.read_address(&address)?)
            }
            Data::Pointer(address) => {
                let value = self.read_address(address)?;
                return self.index_value(&value, key);
            }
            _ => None,
        }
        .ok_or_else(|| RuntimeError::new("panic", "index", "index outside sequence"))?;
        Ok((value, true))
    }

    fn store_index(&mut self, object: Value, key: Value, value: Value) -> Result<(), RuntimeError> {
        if let Data::Map(root) = object.data {
            let key = self.map_key(&object.typ, key)?;
            let value = self.coerce(value, &self.element_type(&object.typ)?)?;
            let Data::MapEntries(entries) = &self.heap.get(root)?.data else {
                return Err(RuntimeError::new("invalid_map", "map", "invalid backing"));
            };
            let position = entries.find(&key, &self.types)?;
            let mut old_edges = Vec::new();
            let mut new_edges = Vec::new();
            value.trace(&mut |handle| new_edges.push(handle));
            let previous_bytes = if let Some(index) = position {
                entries[index].1.trace(&mut |handle| old_edges.push(handle));
                entries[index].1.logical_bytes()?
            } else {
                if entries.len() >= self.limits.max_sequence_elements {
                    return Err(RuntimeError::new(
                        "value_limit",
                        "map",
                        "entry limit exceeded",
                    ));
                }
                key.trace(&mut |handle| new_edges.push(handle));
                0
            };
            let added = value.logical_bytes()?.checked_add(if position.is_none() {
                key.logical_bytes()?
            } else {
                0
            });
            let allocation = self.heap.allocation_bytes(root)?;
            let bytes = added
                .and_then(|added| allocation.checked_sub(previous_bytes)?.checked_add(added))
                .ok_or_else(|| {
                    RuntimeError::new("allocation_limit", "map", "logical size overflow")
                })?;
            self.transient_roots.extend(new_edges.iter().copied());
            key.trace(&mut |handle| self.transient_roots.push(handle));
            self.prepare_heap_replacements(&[(root, bytes)])?;
            if position.is_none() {
                self.charge_guest(32)?;
            }
            return self
                .heap
                .update(root, bytes, old_edges == new_edges, |backing| {
                    let Data::MapEntries(entries) = &mut backing.data else {
                        unreachable!()
                    };
                    if let Some(index) = position {
                        entries.set_value(index, value);
                    } else {
                        entries.insert(key, value);
                    }
                });
        }
        if matches!(object.data, Data::Nil) {
            return Err(RuntimeError::new(
                "panic",
                "store_index",
                "assignment to nil container",
            ));
        }
        let index = usize::try_from(key.integer()?)
            .map_err(|_| RuntimeError::new("panic", "store_index", "negative index"))?;
        let mut address = match object.data {
            Data::Pointer(address) => address,
            Data::Slice(slice) => {
                if index >= slice.length {
                    return Err(RuntimeError::new(
                        "panic",
                        "store_index",
                        "index outside slice",
                    ));
                }
                let mut address = slice.storage;
                address.path.push(PathElement::Index(slice.start + index));
                return self.write_address(&address, value);
            }
            _ => {
                return Err(RuntimeError::new(
                    "invalid_address",
                    "store_index",
                    "expected addressable sequence",
                ));
            }
        };
        address.path.push(PathElement::Index(index));
        self.write_address(&address, value)
    }

    fn step(&mut self) -> Result<(), RuntimeError> {
        self.types
            .use_context(self.frames.last().unwrap().revision.program.decoded.types());
        if self.frames.last().unwrap().panic.is_some() {
            self.frames.last_mut().unwrap().resume = None;
            self.frames.last_mut().unwrap().tail_return = None;
        }
        if let Some(resume) = self.frames.last_mut().unwrap().resume.take() {
            resume.trace(&mut |handle| self.transient_roots.push(handle));
            return self.resume_reflect(resume);
        }
        if let Some(count) = self.frames.last_mut().unwrap().tail_return.take() {
            let values = self.pop_values(count)?;
            return self.begin_return(values);
        }
        let frame = self.frames.last().unwrap();
        if frame.returning.is_some() {
            return self.finish_frame();
        }
        let revision = frame.revision.clone();
        let function = frame.prepared.clone();
        let module = &function.module;
        let pc = if let Some(pc) = self.frames.last_mut().unwrap().after_init.take() {
            pc
        } else {
            let pc = self.frames.last().unwrap().pc;
            if pc == function.code.len() {
                self.frames.last_mut().unwrap().returning = Some(Vec::new());
                return self.finish_frame();
            }
            self.frames.last_mut().unwrap().pc += 1;
            pc
        };
        let instruction = &function.code[pc];
        use Instruction::*;
        let initialization_target = match &instruction {
            CallDirect(payload) | TailCallDirect(payload) => Some(&payload.module_path),
            LoadExport(payload) => Some(&payload.module_path),
            AddressOf(payload) if payload.kind == "export" => Some(&payload.module_path),
            _ => None,
        };
        if let Some(target) = initialization_target
            && !target.is_empty()
            && target.as_str() != module.as_ref()
            && !self.initialized.contains(target)
            && !self.initializing.contains(target)
        {
            let caller = self.frames.len() - 1;
            if self.initialize_module(target)? {
                self.frames[caller].after_init = Some(pc);
                return Ok(());
            }
        }
        let mut result = None;
        use crate::program::PreparedInstruction as Prepared;
        match function.execution[pc] {
            Prepared::Intrinsic(intrinsic) => self.execute_intrinsic(intrinsic)?,
            Prepared::Constant(index) => {
                result = Some(self.load_prepared_constant(&revision, index)?)
            }
            Prepared::Local {
                index,
                store,
                rebind,
            } => {
                let root = self.frames.last().unwrap().locals[index];
                if !store {
                    result = Some(self.load_slot(root)?);
                } else {
                    let value = self.pop()?;
                    if rebind {
                        let typ = self.heap.get(root)?.typ.clone();
                        let handle = self.allocate(self.zero(&typ, 0)?)?;
                        self.frames.last_mut().unwrap().locals[index] = handle;
                        self.store_slot(handle, value)?;
                    } else {
                        self.store_slot(root, value)?;
                    }
                }
            }
            Prepared::Upvalue { index, store } => {
                let address = self.frames.last().unwrap().upvalues[index].clone();
                if store {
                    let value = self.pop()?;
                    self.write_address(&address, value)?;
                } else {
                    result = Some(self.read_address(&address)?);
                }
            }
            Prepared::Global { index, store } => {
                let root = self.frames.last().unwrap().globals[index];
                if store {
                    let value = self.pop()?;
                    self.store_slot(root, value)?;
                } else {
                    result = Some(self.load_slot(root)?);
                }
            }
            Prepared::Jump {
                target,
                conditional,
            } => {
                let jump = if conditional {
                    let value = self.pop()?;
                    let Data::Bool(condition) = value.data else {
                        return Err(RuntimeError::new("type_error", "jump_if", "expected bool"));
                    };
                    condition
                } else {
                    true
                };
                if jump {
                    self.frames.last_mut().unwrap().pc = target;
                }
            }
            Prepared::Unary(operator) => {
                let value = self.pop()?;
                result = Some(crate::operators::unary(operator, value, &self.types)?);
            }
            Prepared::Binary(operator) => {
                let right = self.pop()?;
                let left = self.pop()?;
                if operator == crate::operators::Operator::Add
                    && let (Data::String(left), Data::String(right)) = (&left.data, &right.data)
                {
                    let length = left.len().checked_add(right.len()).ok_or_else(|| {
                        RuntimeError::new("string_limit", "string", "concatenation size overflow")
                    })?;
                    self.check_string_size(length)?;
                    self.charge_guest(length as u64)?;
                }
                result = Some(crate::operators::binary(
                    operator,
                    left,
                    right,
                    &self.types,
                )?);
            }
            Prepared::Operand => match instruction {
                Zero(_) => {
                    result = Some(self.zero(function.operand_types[pc].as_ref().unwrap(), 0)?);
                }
                Pop => {
                    self.pop()?;
                }
                AddressOf(payload) => {
                    self.charge_guest(128)?;
                    let mut address = self.resolve_address(payload)?;
                    address.identity = Arc::new(crate::value::PointerIdentity {
                        path_root: address.identity.path_root,
                        ..Default::default()
                    });
                    let pointee = if address.path.is_empty() {
                        self.heap.get(address.root)?.typ.clone()
                    } else {
                        self.borrow_address(&address)?.typ.clone()
                    };
                    let typ = TypeIdentity::Pointer(std::sync::Arc::new(pointee));
                    result = Some(Value {
                        typ,
                        data: Data::Pointer(address),
                    });
                }
                LoadIndirect => {
                    let value = self.pop()?;
                    let Data::Pointer(address) = value.data else {
                        return Err(RuntimeError::new(
                            if matches!(value.data, Data::Nil) {
                                "panic"
                            } else {
                                "type_error"
                            },
                            "load_indirect",
                            "cannot dereference value",
                        ));
                    };
                    result = Some(self.read_address(&address)?);
                }
                StoreIndirect => {
                    let value = self.pop()?;
                    let pointer = self.pop()?;
                    let Data::Pointer(address) = pointer.data else {
                        return Err(RuntimeError::new(
                            if matches!(pointer.data, Data::Nil) {
                                "panic"
                            } else {
                                "type_error"
                            },
                            "store_indirect",
                            "cannot dereference value",
                        ));
                    };
                    self.write_address(&address, value)?;
                }
                MakeStruct(payload) => {
                    let values = self.pop_values(payload.fields.len())?;
                    self.charge_guest_object(payload.fields.len(), 0)?;
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    let mut value = self.zero(&typ, 0)?;
                    let Data::Struct(fields) = &mut value.data else {
                        return Err(RuntimeError::new(
                            "type_error",
                            "make_struct",
                            "expected struct type",
                        ));
                    };
                    for (name, value) in payload.fields.iter().zip(values) {
                        let destination = fields.get_mut(name).ok_or_else(|| {
                            RuntimeError::new("missing_field", "make_struct", name)
                        })?;
                        *destination = self.coerce(value, &destination.typ)?;
                    }
                    result = Some(value);
                }
                MakeSequence(payload) => {
                    let values = self.pop_values(payload.element_count as usize)?;
                    self.charge_guest_object(values.len(), 0)?;
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    if self
                        .types
                        .node(&typ)?
                        .is_some_and(|(_, node)| node.kind == wire::Slice)
                    {
                        result = Some(self.make_slice(typ, values.len(), values.len(), values)?);
                    } else {
                        let mut value = self.zero(&typ, 0)?;
                        let Data::Array(elements) = &mut value.data else {
                            return Err(RuntimeError::new(
                                "type_error",
                                "make_sequence",
                                "expected sequence type",
                            ));
                        };
                        if elements.len() != values.len() {
                            return Err(RuntimeError::new(
                                "invalid_sequence",
                                "make_sequence",
                                "element count mismatch",
                            ));
                        }
                        *elements = values;
                        result = Some(value);
                    }
                }
                MakeSlice(payload) => {
                    let values = self.pop_values(if payload.has_capacity { 2 } else { 1 })?;
                    let length = values[0].integer()?;
                    let capacity = if payload.has_capacity {
                        values[1].integer()?
                    } else {
                        length
                    };
                    if length < 0 || capacity < length {
                        return Err(RuntimeError::new(
                            "panic",
                            "make_slice",
                            "invalid slice length or capacity",
                        ));
                    }
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    if capacity as u64 > self.limits.max_sequence_elements as u64 {
                        return Err(RuntimeError::new(
                            "value_limit",
                            "make_slice",
                            "slice capacity exceeds limit",
                        ));
                    }
                    self.charge_guest_object(capacity as usize, 0)?;
                    result = Some(self.make_slice(
                        typ,
                        length as usize,
                        capacity as usize,
                        Vec::new(),
                    )?);
                }
                MakeMap(payload) => {
                    let entry_values = payload.entry_count as usize * 2;
                    let mut values =
                        self.pop_values(entry_values + usize::from(payload.has_capacity))?;
                    let mut requested_capacity = payload.entry_count as usize;
                    if payload.has_capacity {
                        let capacity = values.pop().unwrap().integer()?;
                        if capacity < 0
                            || capacity as u64 > self.limits.max_sequence_elements as u64
                        {
                            return Err(RuntimeError::new(
                                "value_limit",
                                "make_map",
                                "invalid map capacity",
                            ));
                        }
                        requested_capacity = requested_capacity.max(capacity as usize);
                    }
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    if requested_capacity > self.limits.max_sequence_elements {
                        return Err(RuntimeError::new(
                            "value_limit",
                            "make_map",
                            "map capacity exceeds limit",
                        ));
                    }
                    let mut entries = crate::value::MapStorage::default();
                    let element = self.element_type(&typ)?;
                    for pair in values.as_chunks::<2>().0 {
                        let key = self.map_key(&typ, pair[0].clone())?;
                        let value = self.coerce(pair[1].clone(), &element)?;
                        if let Some(index) = entries.find(&key, &self.types)? {
                            entries.set_value(index, value);
                        } else {
                            entries.insert(key, value);
                        }
                    }
                    self.charge_guest_object(0, requested_capacity)?;
                    let root = self.allocate(Value {
                        typ: TypeIdentity::Any,
                        data: Data::MapEntries(entries),
                    })?;
                    let map = Value {
                        typ,
                        data: Data::Map(root),
                    };
                    result = Some(map);
                }
                LoadField(payload) => {
                    let mut value = self.pop()?;
                    if matches!(value.data, Data::Nil)
                        && self.types.pointer_element(&value.typ)?.is_some()
                    {
                        return Err(RuntimeError::new(
                            "panic",
                            "load_field",
                            "nil pointer dereference",
                        ));
                    }
                    if let Data::Pointer(address) = value.data {
                        value = self.read_address(&address)?;
                    }
                    let Data::Struct(fields) = value.data else {
                        return Err(RuntimeError::new(
                            "type_error",
                            "load_field",
                            "expected struct",
                        ));
                    };
                    result = Some(fields.get(&payload.field).cloned().ok_or_else(|| {
                        RuntimeError::new("missing_field", "load_field", &payload.field)
                    })?);
                }
                StoreField(payload) => {
                    let value = self.pop()?;
                    let object = self.pop()?;
                    let Data::Pointer(mut address) = object.data else {
                        return Err(RuntimeError::new(
                            "invalid_address",
                            "store_field",
                            "expected struct pointer",
                        ));
                    };
                    address.path.push(PathElement::Field(payload.field.clone()));
                    self.write_address(&address, value)?;
                }
                LoadIndex | LoadIndexOk => {
                    let with_ok = matches!(
                        function.code[self.frames.last().unwrap().pc - 1],
                        LoadIndexOk
                    );
                    let key = self.pop()?;
                    let object = self.pop()?;
                    let (value, ok) = self.index_value(&object, &key)?;
                    if with_ok {
                        self.frames.last_mut().unwrap().stack.push(value);
                        result = Some(Value::boolean(ok));
                    } else {
                        result = Some(value);
                    }
                }
                StoreIndex => {
                    let mut values = self.pop_values(3)?;
                    let value = values.pop().unwrap();
                    let key = values.pop().unwrap();
                    let object = values.pop().unwrap();
                    self.store_index(object, key, value)?;
                }
                MakeClosure(payload) => {
                    self.charge_guest_object(payload.captures.len(), 0)?;
                    let captures = payload
                        .captures
                        .iter()
                        .map(|address| self.resolve_address(address))
                        .collect::<Result<Vec<_>, _>>()?;
                    let target = &revision.program.function_table[function.targets[pc].unwrap()];
                    result = Some(Value {
                        typ: TypeIdentity::Any,
                        data: Data::Function(FunctionValue {
                            index: Some(target.index),
                            revision: Some(revision.clone()),
                            module: target.module.clone(),
                            function: target.name.clone(),
                            captures,
                        }),
                    });
                }
                CallDirect(payload) | TailCallDirect(payload) | CallValue(payload) => {
                    let tail = matches!(
                        function.code[self.frames.last().unwrap().pc - 1],
                        TailCallDirect(_)
                    );
                    let indirect = matches!(
                        function.code[self.frames.last().unwrap().pc - 1],
                        CallValue(_)
                    );
                    let mut arguments = if tail {
                        let stack = &self.frames.last().unwrap().stack;
                        let start = stack
                            .len()
                            .checked_sub(payload.arg_count as usize)
                            .ok_or_else(|| {
                                RuntimeError::new(
                                    "stack_underflow",
                                    "tail_call",
                                    "missing call arguments",
                                )
                            })?;
                        stack[start..].to_vec()
                    } else {
                        self.pop_values(payload.arg_count as usize + usize::from(indirect))?
                    };
                    let callee = if indirect {
                        let value = arguments.remove(0);
                        if matches!(value.data, Data::DynamicFunction(_)) {
                            return self.start_dynamic_callback(
                                value,
                                arguments,
                                payload.result_count as usize,
                                false,
                            );
                        }
                        match value.data {
                            Data::Function(callee) => callee,
                            Data::Method { function, receiver } => {
                                if let Some(receiver) = receiver {
                                    arguments.insert(0, *receiver);
                                }
                                function
                            }
                            _ => {
                                return Err(RuntimeError::new(
                                    "panic",
                                    "call",
                                    "call of nil function",
                                ));
                            }
                        }
                    } else {
                        let source =
                            &revision.program.function_table[function.targets[pc].unwrap()];
                        let bound_revision;
                        let target_index;
                        if source.declaration.revision_local {
                            bound_revision = revision.clone();
                            target_index = source.index;
                        } else {
                            bound_revision = self.revision.clone();
                            let key = (revision.generation, function.index, pc);
                            target_index = match self.call_bindings.get(&key) {
                                Some((generation, index))
                                    if *generation == bound_revision.generation =>
                                {
                                    *index
                                }
                                _ => {
                                    let index = bound_revision
                                        .program
                                        .function(&source.module, &source.name)?
                                        .index;
                                    self.call_bindings
                                        .insert(key, (bound_revision.generation, index));
                                    index
                                }
                            };
                        }
                        let target = &bound_revision.program.function_table[target_index];
                        FunctionValue {
                            index: Some(target_index),
                            module: target.module.clone(),
                            function: target.name.clone(),
                            revision: Some(bound_revision),
                            captures: Vec::new(),
                        }
                    };
                    let caller = self.frames.last().unwrap();
                    let replace = tail
                        && caller.defers.is_empty()
                        && caller.module == callee.module
                        && self.debug.breakpoints.is_empty()
                        && self.debug.resume.is_none()
                        && !self.debug.pause.load(std::sync::atomic::Ordering::Acquire);
                    if replace {
                        self.suspended_frames.push(self.frames.pop().unwrap());
                    }
                    let frame_index = self.frames.len();
                    let created =
                        self.push_frame(callee, arguments, payload.result_count as usize, false);
                    let old = if replace {
                        self.suspended_frames.pop()
                    } else {
                        None
                    };
                    if let Err(error) = created {
                        if let Some(old) = old {
                            self.frames.push(old);
                        }
                        return Err(error);
                    }
                    if let Some(mut old) = old {
                        self.cache_frame(&mut old)?;
                        self.memory.recycle_frame_storage(
                            old.revision.generation,
                            old.prepared.module_index,
                            old.prepared.index,
                            old.memory,
                        );
                        let frame = &mut self.frames[frame_index];
                        frame.initializing = old.initializing;
                        frame.deferred = old.deferred;
                        frame.recovered = old.recovered;
                        frame.expected_results = old.expected_results;
                    } else if tail {
                        let caller = &mut self.frames[frame_index - 1];
                        caller
                            .stack
                            .truncate(caller.stack.len() - payload.arg_count as usize);
                        caller.tail_return = Some(payload.result_count as usize);
                    }
                }
                CallInterface(payload) => {
                    let mut arguments = self.pop_values(payload.arg_count as usize + 1)?;
                    let value = arguments.remove(0);
                    let receiver = match value.data {
                        Data::Interface(value) => *value,
                        _ => value,
                    };
                    let (owner, method) = self
                        .types
                        .method(&receiver.typ, &payload.method)?
                        .ok_or_else(|| {
                            RuntimeError::new(
                                "panic",
                                "call_interface",
                                "dynamic type has no matching method",
                            )
                        })?;
                    let expected = self.types.resolve(&owner, &method.receiver)?;
                    let receiver = self.method_receiver(receiver, &expected)?;
                    arguments.insert(0, receiver);
                    self.push_frame(
                        FunctionValue {
                            index: None,
                            revision: None,
                            module: if method.module_path.is_empty() {
                                owner.into()
                            } else {
                                method.module_path.into()
                            },
                            function: method.function_id.into(),
                            captures: Vec::new(),
                        },
                        arguments,
                        payload.result_count as usize,
                        false,
                    )?;
                }
                Return(payload) => {
                    let values = self.pop_values(payload.result_count as usize)?;
                    self.begin_return(values)?;
                }
                DeferPush(payload) => {
                    let value = self.pop()?;
                    let Data::Function(callee) = value.data else {
                        return Err(RuntimeError::new(
                            "nil_function",
                            "defer",
                            "value is not callable",
                        ));
                    };
                    let owner = self
                        .frames
                        .len()
                        .checked_sub(payload.owner_depth as usize + 1)
                        .ok_or_else(|| {
                            RuntimeError::new(
                                "invalid_defer",
                                "defer",
                                "owner depth exceeds call stack",
                            )
                        })?;
                    let frame = &mut self.frames[owner];
                    frame.memory.deferred = memory::grow_frame_buffer(
                        frame.memory.deferred,
                        frame.defers.len() + 1,
                        true,
                    )?;
                    frame.defers.push(callee);
                }
                Panic => {
                    let mut value = self.pop()?;
                    if matches!(value.data, Data::Nil) {
                        value = Value {
                            typ: TypeIdentity::Primitive(wire::PrimitiveString),
                            data: Data::String((&b"panic called with nil argument"[..]).into()),
                        };
                    }
                    let frame = self.frames.last_mut().unwrap();
                    frame.panic = Some(Arc::new(value));
                    frame.returning = Some(Vec::new());
                    self.debug_panic();
                }
                Recover => {
                    result = Some(Value {
                        typ: TypeIdentity::Any,
                        data: Data::Nil,
                    });
                    let mut top = self.frames.len() - 1;
                    if !self.frames[top].deferred && top > 0 {
                        top -= 1;
                    }
                    if top > 0
                        && self.frames[top].deferred
                        && self.frames[top].recovered.is_none()
                        && let Some(panic) = self.frames[top - 1].panic.clone()
                    {
                        result = Some((*panic).clone());
                        self.frames[top].recovered = Some(panic);
                    }
                }
                LoadExport(payload) => {
                    let revision = self.revision.clone();
                    let export = revision
                        .program
                        .export(&payload.module_path, &payload.export)?;
                    result = Some(match export.kind.as_str() {
                        "const" => {
                            let index = revision.program.constants
                                [&(payload.module_path.clone(), export.id.clone())];
                            self.load_prepared_constant(&revision, index)?
                        }
                        "function" => Value {
                            typ: TypeIdentity::Any,
                            data: Data::Function(FunctionValue {
                                index: None,
                                revision: Some(self.revision.clone()),
                                module: payload.module_path.clone().into(),
                                function: export.id.clone().into(),
                                captures: Vec::new(),
                            }),
                        },
                        "global" => self
                            .heap
                            .get(self.globals[&(payload.module_path.clone(), export.id.clone())])?
                            .clone(),
                        _ => {
                            return Err(RuntimeError::new(
                                "invalid_export",
                                "load_export",
                                "unsupported export kind",
                            ));
                        }
                    });
                }
                InitModule(payload) => {
                    self.initialize_module(&payload.module_path)?;
                }
                Len | Cap => {
                    let capacity = matches!(function.code[self.frames.last().unwrap().pc - 1], Cap);
                    let mut value = self.pop()?;
                    let pointee = self.types.pointer_element(&value.typ)?;
                    if let Some(elem) = pointee
                        && let Some((_, node)) = self.types.node(&elem)?
                        && node.kind == wire::Array
                    {
                        result = Some(Value::int(node.length));
                    }
                    if result.is_some() {
                        // The type determines the length even for a nil pointer.
                        self.frames
                            .last_mut()
                            .unwrap()
                            .stack
                            .push(result.take().unwrap());
                        return Ok(());
                    }
                    if let Data::Pointer(address) = &value.data {
                        value = self.read_address(address)?;
                    }
                    let length = match value.data {
                        Data::String(bytes) if !capacity => bytes.len(),
                        Data::Array(values) => values.len(),
                        Data::Nil => 0,
                        Data::Slice(slice) => {
                            if capacity {
                                slice.capacity
                            } else {
                                slice.length
                            }
                        }
                        Data::Map(root) if !capacity => {
                            let Data::MapEntries(entries) = &self.heap.get(root)?.data else {
                                return Err(RuntimeError::new(
                                    "invalid_map",
                                    "len",
                                    "invalid backing",
                                ));
                            };
                            entries.len()
                        }
                        Data::ResourceRef(root) => {
                            let scheduler::Resource::Channel {
                                capacity: size,
                                values,
                                ..
                            } = self.resource(root)?
                            else {
                                return Err(RuntimeError::new(
                                    "type_error",
                                    "len",
                                    "expected channel",
                                ));
                            };
                            if capacity {
                                *size
                            } else if *size == 0 {
                                0
                            } else {
                                values.len()
                            }
                        }
                        _ => {
                            return Err(RuntimeError::new(
                                "type_error",
                                "len",
                                "expected container",
                            ));
                        }
                    };
                    result = Some(Value {
                        typ: TypeIdentity::Primitive(wire::PrimitiveInt),
                        data: Data::Integer(length as i64),
                    });
                }
                Slice => {
                    let mut values = self.pop_values(4)?;
                    let max = values.pop().unwrap();
                    let high = values.pop().unwrap().integer()?;
                    let low = values.pop().unwrap().integer()?;
                    let object = values.pop().unwrap();
                    result = Some(self.slice_value(object, low, high, max)?);
                }
                Append(payload) => {
                    let mut values = self.pop_values(payload.count as usize + 1)?;
                    let object = values.remove(0);
                    if payload.expand {
                        values = self.slice_values(&values[0])?;
                    }
                    result = Some(self.append_values(object, values)?);
                }
                Copy => {
                    let source = self.pop()?;
                    let destination = self.pop()?;
                    result = Some(Value::int(self.copy_values(destination, source)? as i64));
                }
                Delete => {
                    let key = self.pop()?;
                    let object = self.pop()?;
                    self.delete_key(object, key)?;
                }
                Clear => {
                    let object = self.pop()?;
                    match &object.data {
                        Data::Map(root) => {
                            let Data::MapEntries(entries) = &self.heap.get(*root)?.data else {
                                return Err(RuntimeError::new(
                                    "invalid_map",
                                    "clear",
                                    "invalid backing",
                                ));
                            };
                            let backing = Value {
                                typ: TypeIdentity::Any,
                                data: Data::MapEntries(entries.cleared()),
                            };
                            let bytes = backing.logical_bytes()? + 128;
                            self.heap
                                .replace(*root, backing, bytes)
                                .map_err(|(error, _)| error)?;
                        }
                        Data::Slice(slice) => {
                            let zero = self.zero(&self.element_type(&object.typ)?, 0)?;
                            for index in 0..slice.length {
                                self.store_index(
                                    object.clone(),
                                    Value::int(index as i64),
                                    zero.clone(),
                                )?;
                            }
                        }
                        Data::Nil => {}
                        _ => {
                            return Err(RuntimeError::new(
                                "type_error",
                                "clear",
                                "expected map or slice",
                            ));
                        }
                    }
                }
                MapIterInit(payload) => {
                    let object = self.pop()?;
                    self.init_map_iterator(&payload.local, object)?;
                }
                MapIterNext(payload) => {
                    let (key, value, ok) = self.next_map_entry(&payload.local)?;
                    self.frames.last_mut().unwrap().stack.extend([key, value]);
                    result = Some(Value::boolean(ok));
                }
                MapIterClose(payload) => {
                    self.frames
                        .last_mut()
                        .unwrap()
                        .map_iterators
                        .remove(&payload.local);
                }
                MapKeys => {
                    let object = self.pop()?;
                    let values = match object.data {
                        Data::Map(root) => {
                            let Data::MapEntries(entries) = &self.heap.get(root)?.data else {
                                return Err(RuntimeError::new(
                                    "invalid_map",
                                    "map_keys",
                                    "invalid backing",
                                ));
                            };
                            entries.iter().map(|(key, _)| key.clone()).collect()
                        }
                        Data::Nil => Vec::new(),
                        _ => {
                            return Err(RuntimeError::new(
                                "type_error",
                                "map_keys",
                                "expected map",
                            ));
                        }
                    };
                    let (module, node) = self.types.node(&object.typ)?.ok_or_else(|| {
                        RuntimeError::new("type_error", "map_keys", "missing map type")
                    })?;
                    let key = self.types.resolve(module, &node.key)?;
                    result = Some(self.make_slice(
                        TypeIdentity::Slice(std::sync::Arc::new(key)),
                        values.len(),
                        values.len(),
                        values,
                    )?);
                }
                StringRuneAt | StringNextRuneIndex => {
                    let next_index = matches!(
                        function.code[self.frames.last().unwrap().pc - 1],
                        StringNextRuneIndex
                    );
                    let index = usize::try_from(self.pop()?.integer()?)
                        .map_err(|_| RuntimeError::new("panic", "string", "negative rune index"))?;
                    let object = self.pop()?;
                    let Data::String(bytes) = object.data else {
                        return Err(RuntimeError::new("type_error", "string", "expected string"));
                    };
                    if index >= bytes.len() {
                        return Err(RuntimeError::new(
                            "panic",
                            "string",
                            "rune index exceeds byte length",
                        ));
                    }
                    let (rune, width) = crate::value::decode_rune(&bytes[index..]);
                    result = Some(if next_index {
                        Value::int((index + width) as i64)
                    } else {
                        Value {
                            typ: TypeIdentity::Primitive(wire::PrimitiveInt32),
                            data: Data::Integer(i64::from(rune)),
                        }
                    });
                }
                TypeAssert(_) | TypeAssertOk(_) => {
                    let with_ok = matches!(
                        function.code[self.frames.last().unwrap().pc - 1],
                        TypeAssertOk(_)
                    );
                    let value = self.pop()?;
                    let nil_interface = matches!(value.data, Data::Nil)
                        && (value.typ == TypeIdentity::Any
                            || value.typ == TypeIdentity::Primitive(wire::PrimitiveError)
                            || self
                                .types
                                .node(&value.typ)?
                                .is_some_and(|(_, node)| node.kind == wire::Interface));
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    let value = match value.data {
                        Data::Interface(value) => *value,
                        _ => value,
                    };
                    let ok = !nil_interface
                        && (self.types.identical(&value.typ, &typ)?
                            || self.types.implements(&value.typ, &typ)?);
                    if !with_ok && !ok {
                        return Err(RuntimeError::new(
                            "panic",
                            "type_assert",
                            "dynamic type does not match assertion",
                        ));
                    }
                    let value = if ok {
                        self.coerce(value, &typ)?
                    } else {
                        self.zero(&typ, 0)?
                    };
                    if with_ok {
                        self.frames.last_mut().unwrap().stack.push(value);
                        result = Some(Value::boolean(ok));
                    } else {
                        result = Some(value);
                    }
                }
                Convert(_) => {
                    let value = self.pop()?;
                    let typ = function.operand_types[pc].as_ref().unwrap().clone();
                    result = Some(self.convert_value(value, typ)?);
                }
                instruction @ (CallFfi(_)
                | Spawn(_)
                | MakeWaitable(_)
                | WaitableSend
                | WaitableRecv
                | WaitableRecvOk
                | WaitableTryRecv
                | WaitableTrySend
                | WaitableCanRecv
                | WaitableCanSend
                | WaitableClose
                | WaitableSubscribeRecv
                | WaitableSubscribeSend
                | MakeWaitToken
                | WaitTokenSignal
                | WaitTokenCancel
                | MakeWaitSet
                | WaitSetAdd
                | WaitSetPoll
                | WaitSetPark
                | WaitSetCancel) => {
                    self.execute_wait(instruction.clone(), module)?;
                }
                CallIntrinsic(_) | Unary(_) | Binary(_) | Const(_) | LoadLocal(_)
                | StoreLocal(_) | LoadUpvalue(_) | StoreUpvalue(_) | LoadGlobal(_)
                | StoreGlobal(_) | Jump(_) | JumpIf(_) | Label(_) => {
                    unreachable!("indexed operation")
                }
            },
        }
        if let Some(value) = result {
            if let Data::String(bytes) = &value.data {
                self.check_string_size(bytes.len())?;
            }
            self.frames.last_mut().unwrap().stack.push(value);
        }
        Ok(())
    }
}
