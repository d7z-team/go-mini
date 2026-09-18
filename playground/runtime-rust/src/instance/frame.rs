//! Bounded physical frame storage. The guest capacity ledger is independent.

use super::*;

pub(super) struct Frame {
    pub(super) prepared: Arc<crate::program::PreparedFunction>,
    pub(super) memory: memory::GuestFrameAccounting,
    pub(super) revision: Arc<crate::program::Revision>,
    pub(super) module: Arc<str>,
    pub(super) function: Arc<str>,
    pub(super) pc: usize,
    pub(super) locals: Vec<Handle>,
    pub(super) globals: Vec<Handle>,
    pub(super) escaped: Vec<bool>,
    pub(super) upvalues: Vec<Address>,
    pub(super) stack: Vec<Value>,
    pub(super) map_iterators: HashMap<String, MapIterator>,
    pub(super) popped_roots: memory::OperandRoots,
    pub(super) expected_results: usize,
    pub(super) initializing: bool,
    pub(super) defers: Vec<FunctionValue>,
    pub(super) returning: Option<Vec<Value>>,
    pub(super) panic: Option<Arc<Value>>,
    pub(super) recovered: Option<Arc<Value>>,
    pub(super) deferred: bool,
    pub(super) resume: Option<reflect_async::IntrinsicResume>,
    pub(super) tail_return: Option<usize>,
    pub(super) after_init: Option<usize>,
}

pub(super) struct MapIterator {
    pub object: Value,
    pub entries: Vec<u64>,
    pub position: usize,
}

impl Trace for Frame {
    fn trace(&self, visit: &mut dyn FnMut(Handle)) {
        for iterator in self.map_iterators.values() {
            iterator.object.trace(visit);
        }
        for handle in &self.locals {
            visit(*handle);
        }
        for address in &self.upvalues {
            visit(address.root);
        }
        for value in &self.stack {
            value.trace(visit);
        }
        self.popped_roots.trace(visit);
        if let Some(resume) = &self.resume {
            resume.trace(visit);
        }
        for value in self.returning.iter().flatten() {
            value.trace(visit);
        }
        for function in &self.defers {
            for address in &function.captures {
                visit(address.root);
            }
        }
        if let Some(value) = &self.panic {
            value.trace(visit);
        }
        if let Some(value) = &self.recovered {
            value.trace(visit);
        }
    }
}

#[derive(Default)]
pub(super) struct FramePool {
    frames: HashMap<(u64, usize), Vec<FrameStorage>>,
    operands: Vec<Vec<Value>>,
    bytes: usize,
}

#[derive(Default)]
pub(super) struct FrameStorage {
    pub locals: Vec<Handle>,
    pub globals: Vec<Handle>,
    pub escaped: Vec<bool>,
    pub stack: Vec<Value>,
    pub popped: memory::OperandRoots,
    pub defers: Vec<FunctionValue>,
}

impl FrameStorage {
    fn bytes(&self) -> usize {
        (self.locals.capacity() + self.globals.capacity()) * size_of::<Handle>()
            + self.escaped.capacity() * size_of::<bool>()
            + self.stack.capacity() * size_of::<Value>()
            + self.popped.storage_bytes()
            + self.defers.capacity() * size_of::<FunctionValue>()
            + self.escaped.iter().filter(|escaped| !**escaped).count() * (128 + size_of::<Value>())
    }
}

impl FramePool {
    pub fn take_operands(&mut self, count: usize) -> Vec<Value> {
        if let Some(index) = self
            .operands
            .iter()
            .position(|values| values.capacity() >= count)
        {
            let values = self.operands.swap_remove(index);
            self.bytes -= values.capacity() * size_of::<Value>();
            values
        } else {
            Vec::with_capacity(count)
        }
    }

    pub fn recycle_operands(&mut self, mut values: Vec<Value>, limit: usize) {
        values.clear();
        let bytes = values.capacity() * size_of::<Value>();
        if bytes != 0 && self.operands.len() < 8 && bytes <= limit.saturating_sub(self.bytes) {
            self.bytes += bytes;
            self.operands.push(values);
        }
    }

    pub fn take(&mut self, generation: u64, function: usize) -> FrameStorage {
        let key = (generation, function);
        let storage = self
            .frames
            .get_mut(&key)
            .and_then(Vec::pop)
            .unwrap_or_default();
        self.bytes -= storage.bytes();
        storage
    }
}

impl Trace for FramePool {
    fn trace(&self, visit: &mut dyn FnMut(Handle)) {
        for storage in self.frames.values().flatten() {
            for (handle, escaped) in storage.locals.iter().zip(&storage.escaped) {
                if !escaped {
                    visit(*handle);
                }
            }
        }
    }
}

impl Instance {
    pub(super) fn cache_frame(&mut self, frame: &mut Frame) -> Result<(), RuntimeError> {
        if frame.revision.generation != self.revision.generation
            || self.limits.max_frame_cache_bytes == 0
        {
            return Ok(());
        }
        for (handle, escaped) in frame.locals.iter().zip(&frame.escaped) {
            if !escaped {
                let typ = self.heap.get(*handle)?.typ.clone();
                let value = Value {
                    typ,
                    data: Data::Uninitialized,
                };
                let bytes = value.logical_bytes()? + 128;
                self.heap
                    .replace(*handle, value, bytes)
                    .map_err(|(error, _)| error)?;
            }
        }
        frame.stack.clear();
        frame.popped_roots.clear();
        frame.defers.clear();
        let storage = FrameStorage {
            locals: std::mem::take(&mut frame.locals),
            globals: std::mem::take(&mut frame.globals),
            escaped: std::mem::take(&mut frame.escaped),
            stack: std::mem::take(&mut frame.stack),
            popped: std::mem::take(&mut frame.popped_roots),
            defers: std::mem::take(&mut frame.defers),
        };
        let bytes = storage.bytes();
        let key = (frame.revision.generation, frame.prepared.index);
        if bytes
            <= self
                .limits
                .max_frame_cache_bytes
                .saturating_sub(self.frame_pool.bytes)
            && self.frame_pool.frames.get(&key).map_or(0, Vec::len) < 8
        {
            self.frame_pool.frames.entry(key).or_default().push(storage);
            self.frame_pool.bytes += bytes;
        }
        Ok(())
    }
}

impl Instance {
    pub(super) fn push_frame(
        &mut self,
        callee: FunctionValue,
        mut arguments: Vec<Value>,
        expected_results: usize,
        initializing: bool,
    ) -> Result<(), RuntimeError> {
        self.transient_roots
            .extend(callee.captures.iter().map(|address| address.root));
        for value in &arguments {
            value.trace(&mut |handle| self.transient_roots.push(handle));
        }
        if self.frames.len() >= self.limits.max_frames {
            return Err(RuntimeError::new(
                "frame_limit",
                &callee.function,
                "frame budget exhausted",
            ));
        }
        let revision = callee
            .revision
            .clone()
            .unwrap_or_else(|| self.revision.clone());
        self.types.use_context(revision.program.decoded.types());
        let function = match callee.index {
            Some(index) if callee.revision.is_some() => {
                revision.program.function_table[index].clone()
            }
            _ => revision
                .program
                .function(&callee.module, &callee.function)?
                .clone(),
        };
        if arguments.len() != function.declaration.signature.params.len()
            || expected_results != function.declaration.signature.results.len()
            || callee.captures.len() != function.upvalues.len()
        {
            return Err(RuntimeError::new(
                "invalid_call",
                &callee.function,
                "argument, result or capture count mismatch",
            ));
        }
        let stack_limit = function.stack_limit;
        let mut storage = self.frame_pool.take(revision.generation, function.index);
        for (handle, escaped) in storage.locals.iter().zip(&storage.escaped) {
            if !escaped {
                self.transient_roots.push(*handle);
            }
        }
        storage.locals.reserve(
            function
                .local_types
                .len()
                .saturating_sub(storage.locals.len()),
        );
        let mut incoming = arguments.drain(..);
        for (index, typ) in function.local_types.iter().enumerate() {
            let value = match incoming.next() {
                Some(value) => value,
                None => Value {
                    typ: typ.clone(),
                    data: Data::Uninitialized,
                },
            };
            let value = self.coerce(value, typ)?;
            if index == storage.locals.len() {
                storage.locals.push(self.allocate(value)?);
            } else if storage.escaped[index] {
                storage.locals[index] = self.allocate(value)?;
            } else {
                self.store_slot(storage.locals[index], value)?;
            }
        }
        drop(incoming);
        self.frame_pool
            .recycle_operands(arguments, self.limits.max_frame_cache_bytes);
        storage.escaped.resize(storage.locals.len(), false);
        storage.escaped.fill(false);
        storage.stack.reserve(stack_limit);
        if storage.globals.is_empty() {
            storage.globals.extend(
                function
                    .globals
                    .iter()
                    .map(|id| self.globals[&(callee.module.to_string(), id.clone())]),
            );
        }
        let result_slots = if self.frames.is_empty() {
            0
        } else {
            expected_results
        };
        let base_slots = storage.locals.len() + callee.captures.len() + stack_limit;
        let memory =
            match self
                .memory
                .take_frame(revision.generation, function.module_index, function.index)
            {
                Some(memory) => memory,
                None => {
                    let storage = memory::GuestFrameAccounting {
                        base_slots,
                        ..Default::default()
                    };
                    if let Err(error) =
                        self.charge_guest(128 + (base_slots + result_slots) as u64 * 16)
                    {
                        self.memory.recycle_frame_storage(
                            revision.generation,
                            function.module_index,
                            function.index,
                            storage,
                        );
                        return Err(error);
                    }
                    storage
                }
            };
        self.frames.push(Frame {
            globals: storage.globals,
            prepared: function,
            memory,
            revision,
            module: callee.module,
            function: callee.function,
            pc: 0,
            locals: storage.locals,
            escaped: storage.escaped,
            upvalues: callee.captures,
            stack: storage.stack,
            map_iterators: HashMap::new(),
            popped_roots: storage.popped,
            expected_results,
            initializing,
            defers: storage.defers,
            returning: None,
            panic: None,
            recovered: None,
            deferred: false,
            resume: None,
            tail_return: None,
            after_init: None,
        });
        Ok(())
    }

    pub(super) fn finish_frame(&mut self) -> Result<(), RuntimeError> {
        let frame = self.frames.last_mut().unwrap();
        if let Some(callee) = frame.defers.pop() {
            let index = self.frames.len();
            self.push_frame(callee, Vec::new(), 0, false)?;
            self.frames[index].deferred = true;
            return Ok(());
        }
        let function = &frame.prepared;
        if frame.panic.is_none() && !function.declaration.result_locals.is_empty() {
            let values = function
                .declaration
                .result_locals
                .iter()
                .map(|id| {
                    let value = self.heap.get(frame.locals[function.locals[id]])?;
                    if matches!(value.data, Data::Uninitialized) {
                        let mut remaining = self.limits.max_heap_bytes;
                        Value::zero_with_budget(
                            &self.types,
                            &value.typ,
                            0,
                            self.limits.max_value_depth,
                            self.limits.max_sequence_elements,
                            &mut remaining,
                        )
                    } else {
                        Ok(value.clone())
                    }
                })
                .collect::<Result<Vec<_>, _>>()?;
            frame.memory.returned =
                memory::grow_frame_buffer(frame.memory.returned, values.len(), false)?;
            frame.returning = Some(values);
        }
        let mut frame = self.frames.pop().unwrap();
        self.cache_frame(&mut frame)?;
        self.memory.recycle_frame_storage(
            frame.revision.generation,
            frame.prepared.module_index,
            frame.prepared.index,
            frame.memory,
        );
        let mut values = frame.returning.unwrap_or_default();
        if let Some(panic) = frame.panic {
            if let Some(owner) = self.frames.last_mut() {
                owner.returning = Some(Vec::new());
                owner.panic = Some(panic);
                return Ok(());
            }
            return Err(RuntimeError::new("panic", "guest", panic.to_string()));
        }
        if frame.deferred {
            let owner = self.frames.last().ok_or_else(|| {
                RuntimeError::new("invalid_defer", "frame", "deferred frame lost owner")
            })?;
            if frame
                .recovered
                .as_ref()
                .zip(owner.panic.as_ref())
                .is_some_and(|(recovered, panic)| Arc::ptr_eq(recovered, panic))
            {
                let function = owner
                    .revision
                    .program
                    .function(&owner.module, &owner.function)?;
                let values = function
                    .declaration
                    .signature
                    .results
                    .iter()
                    .map(|typ| self.types.resolve(&owner.module, typ))
                    .collect::<Result<Vec<_>, _>>()?
                    .iter()
                    .map(|typ| self.zero(typ, 0))
                    .collect::<Result<Vec<_>, _>>()?;
                let owner = self.frames.last_mut().unwrap();
                owner.panic = None;
                owner.returning = Some(values);
            }
            return Ok(());
        }
        if values.len() != frame.expected_results {
            return Err(RuntimeError::new(
                "invalid_return",
                &frame.function,
                "result count mismatch",
            ));
        }
        if frame.initializing {
            self.initializing.remove(frame.module.as_ref());
            self.initialized.insert(frame.module.to_string());
        }
        if let Some(caller) = self.frames.last_mut() {
            caller.stack.append(&mut values);
            self.frame_pool
                .recycle_operands(values, self.limits.max_frame_cache_bytes);
        } else if self.foreground == Some(self.current_task) {
            self.results = values;
            self.foreground = None;
        }
        if self.frames.is_empty() {
            self.scope_work.entry(self.current_scope).or_default().tasks -= 1;
            self.changed_scopes.insert(self.current_scope);
        }
        if frame.initializing
            && self
                .frames
                .last()
                .is_some_and(|caller| caller.after_init.is_some())
        {
            // Complete the suspended owner action after initialization, without
            // executing or charging its bytecode instruction a second time.
            return self.step();
        }
        Ok(())
    }

    pub(super) fn begin_return(&mut self, values: Vec<Value>) -> Result<(), RuntimeError> {
        let frame = self.frames.last().unwrap();
        let function = &frame.prepared;
        if values.len() != function.result_types.len() {
            return Err(RuntimeError::new(
                "invalid_return",
                &frame.function,
                "result count mismatch",
            ));
        }
        let values = values
            .into_iter()
            .zip(function.result_types.iter())
            .map(|(value, typ)| self.coerce(value, typ))
            .collect::<Result<Vec<_>, _>>()?;
        let frame = self.frames.last_mut().unwrap();
        frame.memory.returned =
            memory::grow_frame_buffer(frame.memory.returned, values.len(), false)?;
        frame.returning = Some(values);
        self.finish_frame()
    }
}
