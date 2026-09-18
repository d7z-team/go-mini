//! Arena roots, collection boundaries, pressure thresholds and allocation recovery.

use super::*;

impl Instance {
    pub(super) fn collect_at_boundary(&mut self) -> Result<usize, RuntimeError> {
        self.transient_roots.clear();
        self.collect_rooted()
    }

    /// Reclaims unreachable arena objects at an explicit owner boundary.
    /// This does not change the Go guest allocation ledger.
    pub fn collect_garbage(&mut self) -> Result<HeapStats, RuntimeError> {
        self.frame_pool = frame::FramePool::default();
        self.collect_at_boundary()?;
        Ok(self.heap.stats())
    }

    pub(super) fn prune_revision_state(&mut self) {
        self.retired_revisions
            .retain(|_, revision| revision.strong_count() != 0);
        let current = self.revision.generation;
        let retired = &self.retired_revisions;
        self.memory.retain_revisions(|generation| {
            generation == current || retired.contains_key(&generation)
        });
        self.constant_values.retain(|(generation, _), _| {
            *generation == self.revision.generation
                || self.retired_revisions.contains_key(generation)
        });
        self.call_bindings.retain(|(generation, _, _), _| {
            *generation == current || retired.contains_key(generation)
        });
    }

    pub(super) fn collect_rooted(&mut self) -> Result<usize, RuntimeError> {
        self.prune_revision_state();
        let mut roots = std::mem::take(&mut self.collection_roots);
        roots.clear();
        roots.extend(self.globals.values().copied());
        self.frame_pool.trace(&mut |handle| roots.push(handle));
        roots.extend(self.transient_roots.iter().copied());
        if let Some(task) = &self.resuming_task {
            for frame in &task.frames {
                frame.trace(&mut |handle| roots.push(handle));
            }
            if let Some(operation) = &task.blocked {
                operation.trace(&mut |handle| roots.push(handle));
            }
        }
        for value in self.constant_values.values() {
            value.trace(&mut |handle| roots.push(handle));
        }
        for frame in self
            .frames
            .iter()
            .chain(&self.suspended_frames)
            .chain(self.runnable.iter().flat_map(|task| &task.frames))
            .chain(self.blocked.iter().flat_map(|task| &task.frames))
        {
            frame.trace(&mut |handle| roots.push(handle));
        }
        for value in &self.results {
            value.trace(&mut |handle| roots.push(handle));
        }
        for timer in &self.timers {
            timer.channel.trace(&mut |handle| roots.push(handle));
        }
        for task in self.blocked.iter() {
            if let Some(operation) = &task.blocked {
                operation.trace(&mut |handle| roots.push(handle));
            }
        }
        let outcome = self.heap.collect(roots.iter().copied());
        roots.clear();
        self.collection_roots = roots;
        let released = outcome?;
        let stats = self.heap.stats();
        self.collect_after_bytes = stats.total_allocated_bytes.saturating_add(
            (stats.live_bytes / 2)
                .max(256 << 10)
                .min(self.limits.max_heap_bytes.saturating_sub(stats.live_bytes))
                .max(1),
        );
        self.collect_after_objects = stats.live_objects.saturating_add(
            (stats.live_objects / 2)
                .max(256)
                .min(self.limits.max_objects.saturating_sub(stats.live_objects))
                .max(1),
        );
        self.prune_revision_state();
        Ok(released)
    }

    pub(super) fn allocate(&mut self, value: Value) -> Result<Handle, RuntimeError> {
        // Slot and node costs are independent of the host allocator layout.
        let bytes = value.logical_bytes()?.checked_add(128).ok_or_else(|| {
            RuntimeError::new("allocation_limit", "value", "logical size overflow")
        })?;
        value.trace(&mut |handle| self.transient_roots.push(handle));
        let stats = self.heap.stats();
        if stats.total_allocated_bytes >= self.collect_after_bytes
            || stats.live_objects >= self.collect_after_objects
        {
            self.collect_rooted()?;
        }
        let handle = match self.heap.allocate(value, bytes) {
            Ok(handle) => handle,
            Err((error, value)) if error.code == "allocation_limit" => {
                self.frame_pool = frame::FramePool::default();
                self.collect_rooted()?;
                self.heap
                    .allocate(value, bytes)
                    .map_err(|(error, _)| error)?
            }
            Err((error, _)) => return Err(error),
        };
        self.transient_roots.push(handle);
        Ok(handle)
    }

    pub(super) fn prepare_heap_replacements(
        &mut self,
        replacements: &[(Handle, u64)],
    ) -> Result<(), RuntimeError> {
        let fits = self.heap.replacements_fit(replacements)?;
        let stats = self.heap.stats();
        if !fits
            || stats.total_allocated_bytes >= self.collect_after_bytes
            || stats.live_objects >= self.collect_after_objects
        {
            if !fits {
                self.frame_pool = frame::FramePool::default();
            }
            self.transient_roots
                .extend(replacements.iter().map(|(handle, _)| *handle));
            self.collect_rooted()?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn program_with_constant(number: i64) -> Arc<Program> {
        super::super::test_helpers::program_with_artifact(|artifact| {
            artifact["constants"][0]["value"] = number.into();
        })
    }

    #[test]
    fn idle_patches_release_revision_indexes_without_collection() {
        let programs = [program_with_constant(10), program_with_constant(20)];
        let mut vm = Instance::new(programs[0].clone(), ExecutionLimits::default()).unwrap();
        let allocated = vm.heap_stats().total_allocated_bytes;
        for index in 0..10_000 {
            let old = vm.revision.generation;
            vm.constant_values.insert((old, 0), Value::int(42));
            vm.call_bindings.insert((old, 0, 0), (old, 0));
            vm.memory
                .recycle_frame_storage(old, 0, 0, memory::GuestFrameAccounting::default());
            let plan = vm.prepare_patch(programs[(index + 1) % 2].clone()).unwrap();
            vm.apply_patch(plan).unwrap();
            assert!(vm.retired_revisions.is_empty());
            assert!(vm.constant_values.is_empty());
            assert!(vm.call_bindings.is_empty());
            assert!(vm.memory.take_frame(old, 0, 0).is_none());
        }
        assert_eq!(vm.retained_revisions().len(), 1);
        assert_eq!(vm.heap_stats().total_allocated_bytes, allocated);
        vm.close().unwrap();
    }

    #[test]
    fn collection_releases_revision_caches_after_last_closure() {
        let mut vm = Instance::new(program_with_constant(10), ExecutionLimits::default()).unwrap();
        let old = vm.revision.generation;
        let closure = vm
            .allocate(Value {
                typ: TypeIdentity::Any,
                data: Data::Function(FunctionValue {
                    index: Some(0),
                    revision: Some(vm.revision.clone()),
                    module: "examples/arithmetic".into(),
                    function: "fn.Main".into(),
                    captures: vec![],
                }),
            })
            .unwrap();
        vm.globals
            .insert(("examples/arithmetic".into(), "retained".into()), closure);
        let plan = vm.prepare_patch(program_with_constant(20)).unwrap();
        vm.apply_patch(plan).unwrap();
        let current = vm.revision.generation;
        for generation in [old, current] {
            vm.constant_values.insert((generation, 0), Value::int(42));
            vm.call_bindings.insert((generation, 0, 0), (current, 0));
            vm.memory.recycle_frame_storage(
                generation,
                0,
                0,
                memory::GuestFrameAccounting::default(),
            );
        }
        vm.collect_garbage().unwrap();
        assert!(vm.retired_revisions.contains_key(&old));
        assert!(vm.constant_values.contains_key(&(old, 0)));
        assert!(vm.memory.take_frame(old, 0, 0).is_some());
        vm.memory
            .recycle_frame_storage(old, 0, 0, memory::GuestFrameAccounting::default());
        vm.globals
            .remove(&("examples/arithmetic".into(), "retained".into()));
        vm.collect_garbage().unwrap();
        assert!(vm.retired_revisions.is_empty());
        assert!(!vm.constant_values.contains_key(&(old, 0)));
        assert!(!vm.call_bindings.contains_key(&(old, 0, 0)));
        assert!(vm.memory.take_frame(old, 0, 0).is_none());
        assert!(vm.constant_values.contains_key(&(current, 0)));
        assert!(vm.call_bindings.contains_key(&(current, 0, 0)));
        assert!(vm.memory.take_frame(current, 0, 0).is_some());
        vm.close().unwrap();
    }
}
