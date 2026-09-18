//! Cooperative tasks and wait registrations, mutated only by the instance owner.

use super::*;

pub(super) struct Task {
    pub id: u64,
    pub scope: u64,
    pub frames: Vec<Frame>,
    pub blocked: Option<Blocked>,
}

#[derive(Clone)]
pub(super) enum Blocked {
    Send {
        channel: Value,
        value: Value,
    },
    Receive {
        channel: Value,
        with_ok: bool,
        token: Handle,
    },
    WaitSet(Value),
    Ffi(u64),
}

impl Trace for Blocked {
    fn trace(&self, visit: &mut dyn FnMut(Handle)) {
        match self {
            Self::Send { channel, value } => {
                channel.trace(visit);
                value.trace(visit);
            }
            Self::Receive { channel, token, .. } => {
                channel.trace(visit);
                visit(*token);
            }
            Self::WaitSet(value) => value.trace(visit),
            Self::Ffi(_) => {}
        }
    }
}

#[derive(Debug)]
pub enum Resource {
    Channel {
        capacity: usize,
        pending_capacity: usize,
        closed: bool,
        values: VecDeque<Value>,
        receive_tokens: Vec<Handle>,
        send_tokens: Vec<Handle>,
    },
    Token {
        signaled: bool,
        canceled: bool,
        registrations: Vec<(Handle, bool)>,
    },
    WaitSet(Vec<Handle>),
}

impl Clone for Resource {
    fn clone(&self) -> Self {
        fn copy_capacity<T: Clone>(values: &Vec<T>) -> Vec<T> {
            let mut copy = Vec::with_capacity(values.capacity());
            copy.extend_from_slice(values);
            copy
        }
        match self {
            Self::Channel {
                capacity,
                pending_capacity,
                closed,
                values,
                receive_tokens,
                send_tokens,
            } => Self::Channel {
                capacity: *capacity,
                pending_capacity: *pending_capacity,
                closed: *closed,
                values: values.clone(),
                receive_tokens: copy_capacity(receive_tokens),
                send_tokens: copy_capacity(send_tokens),
            },
            Self::Token {
                signaled,
                canceled,
                registrations,
            } => Self::Token {
                signaled: *signaled,
                canceled: *canceled,
                registrations: registrations.clone(),
            },
            Self::WaitSet(tokens) => Self::WaitSet(copy_capacity(tokens)),
        }
    }
}

impl Resource {
    pub(crate) fn logical_bytes(&self) -> u64 {
        128 + match self {
            Self::Channel {
                capacity,
                pending_capacity,
                receive_tokens,
                send_tokens,
                ..
            } => {
                (*capacity + *pending_capacity + receive_tokens.capacity() + send_tokens.capacity())
                    as u64
                    * 16
            }
            Self::Token { registrations, .. } => registrations.len() as u64 * 16,
            Self::WaitSet(tokens) => tokens.capacity() as u64 * 16,
        }
    }
}

impl Trace for Resource {
    fn trace(&self, visit: &mut dyn FnMut(Handle)) {
        match self {
            Self::Channel {
                values,
                receive_tokens,
                send_tokens,
                ..
            } => {
                for value in values {
                    value.trace(visit);
                }
                for token in receive_tokens.iter().chain(send_tokens) {
                    visit(*token);
                }
            }
            Self::Token { registrations, .. } => {
                for (resource, _) in registrations {
                    visit(*resource);
                }
            }
            Self::WaitSet(tokens) => {
                for token in tokens {
                    visit(*token);
                }
            }
        }
    }
}

impl Instance {
    pub(super) fn close_channel(&mut self, channel: &Value) -> Result<(), RuntimeError> {
        if matches!(channel.data, Data::Nil) {
            return Err(RuntimeError::new("panic", "close", "close of nil channel"));
        }
        let handle = Self::resource_handle(channel)?;
        let mut resource = self.resource(handle)?.clone();
        let Resource::Channel { closed, .. } = &mut resource else {
            return Err(RuntimeError::new("type_error", "close", "expected channel"));
        };
        if *closed {
            return Err(RuntimeError::new(
                "panic",
                "close",
                "close of closed channel",
            ));
        }
        *closed = true;
        self.store_resource(handle, resource)?;
        self.signal_channel(handle)
    }

    fn ffi_values(&mut self, reply: crate::ffi::Reply) -> Result<Vec<Value>, RuntimeError> {
        let (mut message, mut status) = match reply.error() {
            Some(error) => (
                error.message.clone(),
                if error.code == "route_unavailable" {
                    1
                } else {
                    2
                },
            ),
            None => (String::new(), 0),
        };
        let typ = TypeIdentity::Slice(std::sync::Arc::new(TypeIdentity::Primitive(
            wire::PrimitiveUint8,
        )));
        let imported = self
            .charge_guest(128 + reply.payload().len() as u64)
            .and_then(|()| {
                self.make_bytes(
                    typ.clone(),
                    reply.payload().len(),
                    reply.payload().len(),
                    reply.payload(),
                )
            });
        let value = match imported {
            Ok(value) => value,
            Err(error) => {
                message = error.message;
                status = 2;
                Value {
                    typ,
                    data: Data::Nil,
                }
            }
        };
        if status == 0 {
            reply.consume()?;
        }
        Ok(vec![
            value,
            Value {
                typ: TypeIdentity::Primitive(wire::PrimitiveString),
                data: Data::String(message.into_bytes().into()),
            },
            Value::int(status),
        ])
    }
    pub(super) fn allocate_task_id(&mut self) -> Result<u64, RuntimeError> {
        let id = self.next_task;
        self.next_task = id.checked_add(1).ok_or_else(|| {
            RuntimeError::new("task_limit", "scheduler", "task identifiers exhausted")
        })?;
        Ok(id)
    }

    pub(super) fn yield_task(&mut self) {
        if !self.frames.is_empty() {
            self.runnable.push_back(Task {
                id: self.current_task,
                scope: self.current_scope,
                frames: std::mem::take(&mut self.frames),
                blocked: None,
            });
        }
    }

    pub(super) fn schedule_next(&mut self) -> bool {
        if let Some(task) = self.runnable.pop_front() {
            self.current_task = task.id;
            self.current_scope = task.scope;
            self.frames = task.frames;
            true
        } else {
            false
        }
    }

    pub(super) fn park(&mut self, operation: Blocked) {
        if matches!(operation, Blocked::Ffi(_)) {
            self.scope_work
                .entry(self.current_scope)
                .or_default()
                .ffi_calls += 1;
            self.changed_scopes.insert(self.current_scope);
        }
        let mut dependencies = Vec::new();
        match &operation {
            Blocked::Send { channel, .. } | Blocked::Receive { channel, .. } => {
                if let Data::ResourceRef(handle) = channel.data {
                    dependencies.push(handle);
                }
            }
            Blocked::WaitSet(value) => {
                if let Data::ResourceRef(handle) = value.data {
                    dependencies.push(handle);
                    if let Ok(Resource::WaitSet(tokens)) = self.resource(handle) {
                        dependencies.extend(tokens.iter().copied());
                    }
                }
            }
            Blocked::Ffi(_) => {}
        }
        self.blocked.push(
            Task {
                id: self.current_task,
                scope: self.current_scope,
                frames: std::mem::take(&mut self.frames),
                blocked: Some(operation),
            },
            dependencies,
        );
    }

    pub(super) fn park_receive(
        &mut self,
        channel: Value,
        with_ok: bool,
    ) -> Result<(), RuntimeError> {
        let token = self.allocate(Value {
            typ: TypeIdentity::Any,
            data: Data::Resource(Box::new(Resource::Token {
                signaled: false,
                canceled: false,
                registrations: Vec::new(),
            })),
        })?;
        self.subscribe_channel(&channel, token, false)?;
        self.park(Blocked::Receive {
            channel,
            with_ok,
            token,
        });
        Ok(())
    }

    pub(super) fn reserve_pending_send(
        &mut self,
        channel: &Value,
        value: &Value,
    ) -> Result<usize, RuntimeError> {
        let Data::ResourceRef(handle) = channel.data else {
            return Ok(0);
        };
        let resource = self.resource(handle)?;
        let Resource::Channel {
            pending_capacity,
            capacity,
            values,
            ..
        } = resource
        else {
            unreachable!()
        };
        let queued = if *capacity == 0 { values.len() } else { 0 };
        let pending = self.blocked.send_count(handle);
        let needed = queued + pending + 1;
        if needed > self.limits.max_sequence_elements {
            return Err(RuntimeError::new(
                "value_limit",
                "send",
                "pending send count exceeds limit",
            ));
        }
        if needed <= *pending_capacity {
            return Ok(*pending_capacity);
        }
        let growth = needed - *pending_capacity;
        self.memory.allocation_roots = vec![channel.clone(), value.clone()];
        let charged = self.charge_guest(growth as u64 * 16);
        self.memory.allocation_roots.clear();
        charged?;
        let mut resource = self.resource(handle)?.clone();
        let Resource::Channel {
            pending_capacity, ..
        } = &mut resource
        else {
            unreachable!()
        };
        *pending_capacity = needed;
        self.store_resource(handle, resource)?;
        Ok(needed)
    }

    pub(super) fn resource(&self, handle: Handle) -> Result<&Resource, RuntimeError> {
        match &self.heap.get(handle)?.data {
            Data::Resource(resource) => Ok(resource),
            _ => Err(RuntimeError::new(
                "type_error",
                "resource",
                "invalid resource handle",
            )),
        }
    }

    fn store_resource(&mut self, handle: Handle, resource: Resource) -> Result<(), RuntimeError> {
        self.blocked.notify_resource(handle);
        let value = Value {
            typ: TypeIdentity::Any,
            data: Data::Resource(Box::new(resource)),
        };
        let bytes = value.logical_bytes()? + 128;
        value.trace(&mut |handle| self.transient_roots.push(handle));
        self.prepare_heap_replacements(&[(handle, bytes)])?;
        self.heap
            .replace(handle, value, bytes)
            .map_err(|(error, _)| error)
    }

    pub(super) fn resource_handle(value: &Value) -> Result<Handle, RuntimeError> {
        match value.data {
            Data::ResourceRef(handle) => Ok(handle),
            _ => Err(RuntimeError::new(
                "type_error",
                "resource",
                "expected resource reference",
            )),
        }
    }

    pub(super) fn channel_ready(
        &self,
        channel: &Value,
        sending: bool,
    ) -> Result<bool, RuntimeError> {
        if matches!(channel.data, Data::Nil) {
            return Ok(false);
        }
        let handle = Self::resource_handle(channel)?;
        let Resource::Channel {
            capacity,
            closed,
            values,
            receive_tokens,
            ..
        } = self.resource(handle)?
        else {
            return Err(RuntimeError::new(
                "type_error",
                "channel",
                "expected channel",
            ));
        };
        Ok(*closed
            || if sending {
                values.len() < *capacity
                    || *capacity == 0 && !receive_tokens.is_empty()
                    || self.blocked.first(handle, false).is_some()
            } else {
                !values.is_empty() || self.blocked.first(handle, true).is_some()
            })
    }

    pub(super) fn try_send(
        &mut self,
        channel: &Value,
        value: Value,
        allow_waiting_select: bool,
    ) -> Result<bool, RuntimeError> {
        let value = self.coerce(value, &self.element_type(&channel.typ)?)?;
        if matches!(channel.data, Data::Nil) {
            return Ok(false);
        }
        let handle = Self::resource_handle(channel)?;
        let mut resource = self.resource(handle)?.clone();
        let Resource::Channel {
            capacity,
            pending_capacity,
            closed,
            values,
            receive_tokens,
            ..
        } = &mut resource
        else {
            return Err(RuntimeError::new("type_error", "send", "expected channel"));
        };
        if *closed {
            return Err(RuntimeError::new("panic", "send", "send on closed channel"));
        }
        if *capacity == 0 && !allow_waiting_select {
            return Ok(false);
        }
        if *capacity == 0 && self.channel_ready(channel, true)? {
            *pending_capacity = self.reserve_pending_send(channel, &value)?;
        }
        if let Some(index) = self.blocked.first(handle, false) {
            let mut task = self.blocked.remove(index).task;
            let Some(Blocked::Receive { with_ok, token, .. }) = task.blocked.take() else {
                unreachable!()
            };
            self.finish_token(token, false)?;
            let frame = task.frames.last_mut().unwrap();
            frame.stack.push(value);
            if with_ok {
                frame.stack.push(Value::boolean(true));
            }
            self.runnable.push_back(task);
            return Ok(true);
        }
        if values.len() >= *capacity
            && !(allow_waiting_select && *capacity == 0 && !receive_tokens.is_empty())
        {
            return Ok(false);
        }
        values.push_back(value);
        self.store_resource(handle, resource)?;
        self.signal_channel(handle)?;
        Ok(true)
    }

    pub(super) fn try_receive(
        &mut self,
        channel: &Value,
    ) -> Result<Option<(Value, bool)>, RuntimeError> {
        if matches!(channel.data, Data::Nil) {
            return Ok(None);
        }
        let handle = Self::resource_handle(channel)?;
        let mut resource = self.resource(handle)?.clone();
        let Resource::Channel { closed, values, .. } = &mut resource else {
            return Err(RuntimeError::new(
                "type_error",
                "receive",
                "expected channel",
            ));
        };
        if let Some(value) = values.pop_front() {
            value.trace(&mut |handle| self.transient_roots.push(handle));
            self.store_resource(handle, resource)?;
            self.signal_channel(handle)?;
            return Ok(Some((value, true)));
        }
        if *closed {
            return Ok(Some((
                self.zero(&self.element_type(&channel.typ)?, 0)?,
                false,
            )));
        }
        if let Some(index) = self.blocked.first(handle, true) {
            let mut task = self.blocked.remove(index).task;
            let Some(Blocked::Send { value, .. }) = task.blocked.take() else {
                unreachable!()
            };
            value.trace(&mut |handle| self.transient_roots.push(handle));
            self.runnable.push_back(task);
            self.signal_channel(handle)?;
            return Ok(Some((value, true)));
        }
        Ok(None)
    }

    pub(super) fn finish_token(
        &mut self,
        handle: Handle,
        cancel: bool,
    ) -> Result<(), RuntimeError> {
        let mut resource = self.resource(handle)?.clone();
        let Resource::Token {
            signaled,
            canceled,
            registrations,
        } = &mut resource
        else {
            return Err(RuntimeError::new(
                "type_error",
                "token",
                "expected wait token",
            ));
        };
        if *canceled {
            return Ok(());
        }
        *canceled = cancel;
        *signaled = !cancel;
        let registrations = std::mem::take(registrations);
        self.store_resource(handle, resource)?;
        for (channel, sending) in registrations {
            let mut resource = self.resource(channel)?.clone();
            let Resource::Channel {
                send_tokens,
                receive_tokens,
                ..
            } = &mut resource
            else {
                continue;
            };
            if sending {
                send_tokens.retain(|token| *token != handle);
            } else {
                receive_tokens.retain(|token| *token != handle);
            }
            self.store_resource(channel, resource)?;
        }
        Ok(())
    }

    pub(super) fn signal_channel(&mut self, handle: Handle) -> Result<(), RuntimeError> {
        let Resource::Channel {
            closed,
            values,
            capacity,
            receive_tokens,
            send_tokens,
            ..
        } = self.resource(handle)?
        else {
            return Err(RuntimeError::new(
                "type_error",
                "channel",
                "expected channel",
            ));
        };
        let mut tokens = Vec::new();
        let has_sender = self.blocked.first(handle, true).is_some();
        let has_receiver = self.blocked.first(handle, false).is_some();
        if *closed {
            tokens.extend(receive_tokens);
            tokens.extend(send_tokens);
        } else {
            if !values.is_empty() || has_sender {
                tokens.extend(receive_tokens.first());
            }
            if values.len() < *capacity || has_receiver {
                tokens.extend(send_tokens.first());
            }
        }
        for token in tokens {
            self.finish_token(token, false)?;
        }
        Ok(())
    }

    fn wait_set_index(&mut self, value: &Value) -> Result<i64, RuntimeError> {
        let Resource::WaitSet(tokens) = self.resource(Self::resource_handle(value)?)? else {
            return Err(RuntimeError::new(
                "type_error",
                "waitset",
                "expected wait set",
            ));
        };
        let mut ready = Vec::new();
        for (index, token) in tokens.iter().enumerate() {
            if matches!(
                self.resource(*token)?,
                Resource::Token {
                    signaled: true,
                    canceled: false,
                    ..
                }
            ) {
                ready.push(index);
            }
        }
        Ok(self.choose_ready(&ready))
    }

    pub(super) fn choose_ready(&mut self, ready: &[usize]) -> i64 {
        if ready.is_empty() {
            return -1;
        }
        if ready.len() == 1 {
            return ready[0] as i64;
        }
        let mut state = if self.select_state == 0 {
            0x9e3779b97f4a7c15
        } else {
            self.select_state
        };
        state ^= state << 13;
        state ^= state >> 7;
        state ^= state << 17;
        self.select_state = state;
        ready[state as usize % ready.len()] as i64
    }

    pub(super) fn subscribe_channel(
        &mut self,
        channel: &Value,
        token: Handle,
        sending: bool,
    ) -> Result<(), RuntimeError> {
        if self.channel_ready(channel, sending)? {
            self.finish_token(token, false)?;
        } else if !matches!(channel.data, Data::Nil) {
            let handle = Self::resource_handle(channel)?;
            let mut token_resource = self.resource(token)?.clone();
            let Resource::Token {
                signaled,
                canceled,
                registrations,
            } = &mut token_resource
            else {
                return Err(RuntimeError::new(
                    "type_error",
                    "subscribe",
                    "expected token",
                ));
            };
            if !*signaled && !*canceled && !registrations.contains(&(handle, sending)) {
                registrations.push((handle, sending));
                let mut channel_resource = self.resource(handle)?.clone();
                let Resource::Channel {
                    send_tokens,
                    receive_tokens,
                    ..
                } = &mut channel_resource
                else {
                    return Err(RuntimeError::new(
                        "type_error",
                        "subscribe",
                        "expected channel",
                    ));
                };
                let waiters = if sending { send_tokens } else { receive_tokens };
                if waiters.len() >= self.limits.max_sequence_elements {
                    return Err(RuntimeError::new(
                        "value_limit",
                        "subscribe",
                        "waiter count exceeds limit",
                    ));
                }
                let growth = usize::from(waiters.len() == waiters.capacity());
                self.memory.allocation_roots = vec![
                    channel.clone(),
                    Value {
                        typ: TypeIdentity::Any,
                        data: Data::ResourceRef(token),
                    },
                ];
                let charged = self.charge_guest((growth as u64 + 1) * 16);
                self.memory.allocation_roots.clear();
                charged?;
                if growth != 0 {
                    waiters.reserve_exact(1);
                }
                waiters.push(token);
                let token_value = Value {
                    typ: TypeIdentity::Any,
                    data: Data::Resource(Box::new(token_resource)),
                };
                let channel_value = Value {
                    typ: TypeIdentity::Any,
                    data: Data::Resource(Box::new(channel_resource)),
                };
                let token_bytes =
                    token_value
                        .logical_bytes()?
                        .checked_add(128)
                        .ok_or_else(|| {
                            RuntimeError::new(
                                "allocation_limit",
                                "subscribe",
                                "token size overflow",
                            )
                        })?;
                let channel_bytes =
                    channel_value
                        .logical_bytes()?
                        .checked_add(128)
                        .ok_or_else(|| {
                            RuntimeError::new(
                                "allocation_limit",
                                "subscribe",
                                "channel size overflow",
                            )
                        })?;
                token_value.trace(&mut |handle| self.transient_roots.push(handle));
                channel_value.trace(&mut |handle| self.transient_roots.push(handle));
                self.prepare_heap_replacements(&[(token, token_bytes), (handle, channel_bytes)])?;
                self.blocked.notify_resource(token);
                self.blocked.notify_resource(handle);
                self.heap.replace_pair([
                    (token, token_value, token_bytes),
                    (handle, channel_value, channel_bytes),
                ])?;
            }
        }
        Ok(())
    }

    pub(super) fn resume_blocked(&mut self) -> Result<(), RuntimeError> {
        for call in self.ffi_calls.take_ready_ids() {
            self.blocked.notify_call(call);
        }
        while let Some(index) = self.blocked.next_ready() {
            // Removing the task prevents a send from rendezvousing with itself.
            let waiting = self.blocked.remove(index);
            self.resuming_task = Some(waiting.task);
            let operation = self
                .resuming_task
                .as_ref()
                .unwrap()
                .blocked
                .clone()
                .unwrap();
            let outcome = (|| -> Result<bool, RuntimeError> {
                Ok(match &operation {
                    Blocked::Ffi(id) => {
                        if let Some(reply) = self.ffi_calls.take(*id) {
                            let values = self.ffi_values(reply)?;
                            self.resuming_task
                                .as_mut()
                                .unwrap()
                                .frames
                                .last_mut()
                                .unwrap()
                                .stack
                                .extend(values);
                            true
                        } else {
                            false
                        }
                    }
                    Blocked::Send { channel, value } => {
                        self.try_send(channel, value.clone(), false)?
                    }
                    Blocked::Receive {
                        channel,
                        with_ok,
                        token,
                    } => {
                        if let Some((value, ok)) = self.try_receive(channel)? {
                            self.finish_token(*token, false)?;
                            let frame = self
                                .resuming_task
                                .as_mut()
                                .unwrap()
                                .frames
                                .last_mut()
                                .unwrap();
                            frame.stack.push(value);
                            if *with_ok {
                                frame.stack.push(Value::boolean(ok));
                            }
                            true
                        } else {
                            false
                        }
                    }
                    Blocked::WaitSet(value) => {
                        let selected = self.wait_set_index(value)?;
                        if selected >= 0 {
                            self.resuming_task
                                .as_mut()
                                .unwrap()
                                .frames
                                .last_mut()
                                .unwrap()
                                .stack
                                .push(Value::int(selected));
                            true
                        } else {
                            false
                        }
                    }
                })
            })();
            let mut task = self.resuming_task.take().unwrap();
            let ready = match outcome {
                Ok(ready) => ready,
                Err(error) if error.code == "panic" => {
                    let frame = task.frames.last_mut().unwrap();
                    if matches!(
                        frame.resume,
                        Some(super::reflect_async::IntrinsicResume::Send)
                    ) {
                        frame.resume = None;
                        frame
                            .stack
                            .extend([Value::string(error.message), Value::boolean(false)]);
                    } else {
                        frame.returning = Some(Vec::new());
                        frame.panic = Some(Arc::new(Value {
                            typ: TypeIdentity::Primitive(wire::PrimitiveString),
                            data: Data::String(error.message.into_bytes().into()),
                        }));
                    }
                    true
                }
                Err(error) => {
                    task.blocked = Some(operation);
                    self.blocked
                        .insert(index, waiters::WaitingTask { task, ..waiting });
                    return Err(error);
                }
            };
            if ready {
                if matches!(operation, Blocked::Ffi(_)) {
                    self.scope_work.entry(task.scope).or_default().ffi_calls -= 1;
                    self.changed_scopes.insert(task.scope);
                }
                task.blocked = None;
                self.runnable.push_back(task);
            } else {
                task.blocked = Some(operation);
                self.blocked
                    .insert(index, waiters::WaitingTask { task, ..waiting });
            }
        }
        Ok(())
    }

    pub(super) fn execute_wait(
        &mut self,
        instruction: Instruction,
        module: &str,
    ) -> Result<(), RuntimeError> {
        use Instruction::*;
        let mut result = None;
        match instruction {
            CallFfi(_) => {
                let payload = self.pop()?;
                let route = self.pop()?;
                let Data::String(route) = route.data else {
                    return Err(RuntimeError::new(
                        "type_error",
                        "ffi",
                        "route must be a string",
                    ));
                };
                let route = String::from_utf8(route.to_vec())
                    .map_err(|_| RuntimeError::new("type_error", "ffi", "route is not UTF-8"))?;
                let payload = self.slice_bytes(&payload)?;
                let error = if let Some(session) = &self.ffi_session {
                    let request_bytes =
                        route.len().checked_add(payload.len()).ok_or_else(|| {
                            RuntimeError::new("boundary_limit", "ffi", "request size overflow")
                        })?;
                    let (id, cancellation, completion) = self
                        .ffi_calls
                        .reserve(request_bytes, self.limits.max_ffi_result_bytes)?;
                    let started = session.start(
                        cancellation,
                        crate::ffi::Request { route, payload },
                        completion,
                    );
                    match self.ffi_calls.started(id, started) {
                        Ok(()) => {
                            self.park(Blocked::Ffi(id));
                            return Ok(());
                        }
                        Err(error) => error,
                    }
                } else {
                    RuntimeError::new("route_unavailable", "ffi", "FFI route unavailable")
                };
                let values =
                    self.ffi_values(crate::ffi::Reply::new(Vec::new(), Some(error), None))?;
                self.frames.last_mut().unwrap().stack.extend(values);
            }
            Spawn(payload) => {
                if self.runnable.len() + self.blocked.len() + 1 >= self.limits.max_tasks {
                    return Err(RuntimeError::new(
                        "task_limit",
                        "spawn",
                        "task limit exceeded",
                    ));
                }
                let mut arguments = self.pop_values(payload.arg_count as usize + 1)?;
                let value = arguments.remove(0);
                let Data::Function(callee) = value.data else {
                    return Err(RuntimeError::new(
                        "nil_function",
                        "spawn",
                        "expected callable",
                    ));
                };
                for frame in &self.frames {
                    frame.trace(&mut |handle| self.transient_roots.push(handle));
                }
                self.suspended_frames = std::mem::take(&mut self.frames);
                let created =
                    self.push_frame(callee, arguments, payload.result_count as usize, false);
                let child =
                    std::mem::replace(&mut self.frames, std::mem::take(&mut self.suspended_frames));
                created?;
                self.charge_guest(128)?;
                let id = self.allocate_task_id()?;
                self.scope_work.entry(self.current_scope).or_default().tasks += 1;
                self.changed_scopes.insert(self.current_scope);
                self.runnable.push_back(Task {
                    id,
                    scope: self.current_scope,
                    frames: child,
                    blocked: None,
                });
                self.yield_task();
            }
            MakeWaitable(payload) => {
                let capacity = self.pop()?.integer()?;
                if capacity < 0 || capacity as u64 > self.limits.max_sequence_elements as u64 {
                    return Err(RuntimeError::new(
                        "value_limit",
                        "channel",
                        "invalid capacity",
                    ));
                }
                self.charge_guest_object(capacity as usize, 0)?;
                let typ = self.types.resolve(module, &payload.r#type)?;
                let resource = Resource::Channel {
                    capacity: capacity as usize,
                    pending_capacity: 0,
                    closed: false,
                    values: VecDeque::new(),
                    receive_tokens: Vec::new(),
                    send_tokens: Vec::new(),
                };
                let handle = self.allocate(Value {
                    typ: TypeIdentity::Any,
                    data: Data::Resource(Box::new(resource)),
                })?;
                result = Some(Value {
                    typ,
                    data: Data::ResourceRef(handle),
                });
            }
            WaitableSend | WaitableTrySend => {
                let blocking = matches!(instruction, WaitableSend);
                let value = self.pop()?;
                let channel = self.pop()?;
                let sent = self.try_send(&channel, value.clone(), !blocking)?;
                if !sent && blocking {
                    self.reserve_pending_send(&channel, &value)?;
                    let handle = if let Data::ResourceRef(handle) = channel.data {
                        Some(handle)
                    } else {
                        None
                    };
                    self.park(Blocked::Send { channel, value });
                    if let Some(handle) = handle {
                        self.signal_channel(handle)?;
                    }
                } else if !blocking {
                    result = Some(Value::boolean(sent));
                }
            }
            WaitableRecv | WaitableRecvOk | WaitableTryRecv => {
                let with_ok = !matches!(instruction, WaitableRecv);
                let blocking = !matches!(instruction, WaitableTryRecv);
                let channel = self.pop()?;
                if let Some((value, ok)) = self.try_receive(&channel)? {
                    self.frames.last_mut().unwrap().stack.push(value);
                    if with_ok {
                        result = Some(Value::boolean(ok));
                    }
                } else if blocking {
                    let handle = if let Data::ResourceRef(handle) = channel.data {
                        Some(handle)
                    } else {
                        None
                    };
                    self.park_receive(channel, with_ok)?;
                    if let Some(handle) = handle {
                        self.signal_channel(handle)?;
                    }
                } else {
                    let zero = self.zero(&self.element_type(&channel.typ)?, 0)?;
                    self.frames.last_mut().unwrap().stack.push(zero);
                    result = Some(Value::boolean(false));
                }
            }
            WaitableCanRecv | WaitableCanSend => {
                let channel = self.pop()?;
                result = Some(Value::boolean(
                    self.channel_ready(&channel, matches!(instruction, WaitableCanSend))?,
                ));
            }
            WaitableClose => {
                let channel = self.pop()?;
                self.close_channel(&channel)?;
            }
            MakeWaitToken | MakeWaitSet => {
                self.charge_guest(128)?;
                let token = matches!(instruction, MakeWaitToken);
                let resource = if token {
                    Resource::Token {
                        signaled: false,
                        canceled: false,
                        registrations: Vec::new(),
                    }
                } else {
                    Resource::WaitSet(Vec::new())
                };
                let handle = self.allocate(Value {
                    typ: TypeIdentity::Any,
                    data: Data::Resource(Box::new(resource)),
                })?;
                result = Some(Value {
                    typ: TypeIdentity::Primitive(if token {
                        wire::PrimitiveWaitToken
                    } else {
                        wire::PrimitiveWaitSet
                    }),
                    data: Data::ResourceRef(handle),
                });
            }
            WaitTokenSignal | WaitTokenCancel => {
                let token = self.pop()?;
                self.finish_token(
                    Self::resource_handle(&token)?,
                    matches!(instruction, WaitTokenCancel),
                )?;
            }
            WaitSetAdd => {
                let mut values = self.pop_values(2)?;
                let token = values.pop().unwrap();
                let set = values.pop().unwrap();
                let handle = Self::resource_handle(&set)?;
                let token = Self::resource_handle(&token)?;
                if !matches!(self.resource(token)?, Resource::Token { .. }) {
                    return Err(RuntimeError::new("type_error", "waitset", "expected token"));
                }
                let mut resource = self.resource(handle)?.clone();
                let Resource::WaitSet(tokens) = &mut resource else {
                    return Err(RuntimeError::new(
                        "type_error",
                        "waitset",
                        "expected wait set",
                    ));
                };
                if tokens.len() >= self.limits.max_sequence_elements {
                    return Err(RuntimeError::new(
                        "value_limit",
                        "waitset",
                        "wait set limit exceeded",
                    ));
                }
                if tokens.len() == tokens.capacity() {
                    let capacity = tokens
                        .len()
                        .saturating_mul(2)
                        .max(tokens.len() + 1)
                        .min(self.limits.max_sequence_elements);
                    self.charge_guest((capacity - tokens.capacity()) as u64 * 16)?;
                    tokens.reserve_exact(capacity - tokens.len());
                }
                tokens.push(token);
                self.store_resource(handle, resource)?;
                result = Some(set);
            }
            WaitSetPoll | WaitSetPark => {
                let set = self.pop()?;
                let selected = self.wait_set_index(&set)?;
                if selected < 0 && matches!(instruction, WaitSetPark) {
                    self.park(Blocked::WaitSet(set));
                } else {
                    result = Some(Value::int(selected));
                }
            }
            WaitSetCancel => {
                let set = self.pop()?;
                let handle = Self::resource_handle(&set)?;
                let Resource::WaitSet(tokens) = self.resource(handle)?.clone() else {
                    return Err(RuntimeError::new(
                        "type_error",
                        "waitset",
                        "expected wait set",
                    ));
                };
                for token in tokens {
                    self.finish_token(token, true)?;
                }
                self.store_resource(handle, Resource::WaitSet(Vec::new()))?;
            }
            WaitableSubscribeRecv | WaitableSubscribeSend => {
                let token = self.pop()?;
                let channel = self.pop()?;
                let token = Self::resource_handle(&token)?;
                let sending = matches!(instruction, WaitableSubscribeSend);
                self.subscribe_channel(&channel, token, sending)?;
            }
            _ => unreachable!("wait dispatch only receives wait instructions"),
        }
        if let Some(value) = result {
            self.frames.last_mut().unwrap().stack.push(value);
        }
        Ok(())
    }
}
