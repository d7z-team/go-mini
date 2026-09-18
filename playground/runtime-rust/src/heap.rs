//! Owner-local, generational object storage with transactional tracing.

use crate::error::RuntimeError;
use std::sync::atomic::{AtomicU64, Ordering};

static NEXT_HEAP: AtomicU64 = AtomicU64::new(1);

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct Handle {
    owner: u64,
    index: usize,
    generation: u64,
}

/// Reports every strong guest reference held by an object.
pub trait Trace {
    fn trace(&self, visit: &mut dyn FnMut(Handle));
    /// True only when edges cannot change without a mutable heap borrow or
    /// replacement. Interior-mutable values must retain the default.
    fn stable_edges(&self) -> bool {
        false
    }
}

#[derive(Clone)]
struct Slot<T> {
    generation: u64,
    value: Option<T>,
    bytes: u64,
    next_free: Option<usize>,
    edges: Option<Vec<Handle>>,
    marked: u64,
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct HeapStats {
    pub collections: u64,
    pub live_objects: usize,
    pub live_bytes: u64,
    pub total_allocated_bytes: u64,
    pub peak_bytes: u64,
}

/// Mutated only by the VM owner. Logical bytes come from the contract's cost
/// model, independently of Rust allocation sizes.
pub struct Heap<T> {
    id: u64,
    slots: Vec<Slot<T>>,
    free: Option<usize>,
    max_objects: usize,
    max_bytes: u64,
    stats: HeapStats,
    external_bytes: u64,
    occupied: Vec<usize>,
    mark_epoch: u64,
    trace_pending: Vec<Handle>,
}

impl<T: Trace> Heap<T> {
    pub(crate) fn owner_id(&self) -> u64 {
        self.id
    }

    pub fn new(max_objects: usize, max_bytes: u64) -> Result<Self, RuntimeError> {
        let id = NEXT_HEAP
            .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |next| {
                next.checked_add(1)
            })
            .map_err(|_| {
                RuntimeError::new("allocation_limit", "heap", "heap identity exhausted")
            })?;
        Ok(Self {
            id,
            slots: Vec::new(),
            free: None,
            max_objects,
            max_bytes,
            stats: HeapStats::default(),
            external_bytes: 0,
            occupied: Vec::new(),
            mark_epoch: 0,
            trace_pending: Vec::new(),
        })
    }

    pub fn stats(&self) -> HeapStats {
        self.stats
    }

    pub(crate) fn replacements_fit(
        &self,
        replacements: &[(Handle, u64)],
    ) -> Result<bool, RuntimeError> {
        let mut bytes = self.stats.live_bytes;
        for (handle, _) in replacements {
            bytes -= self.allocation_bytes(*handle)?;
        }
        for (_, replacement) in replacements {
            let Some(total) = bytes.checked_add(*replacement) else {
                return Ok(false);
            };
            bytes = total;
        }
        Ok(bytes <= self.max_bytes)
    }

    /// Reserves logical storage owned by the instance outside arena slots.
    pub(crate) fn set_external_bytes(&mut self, bytes: u64) -> Result<(), RuntimeError> {
        let live = self
            .stats
            .live_bytes
            .checked_sub(self.external_bytes)
            .and_then(|live| live.checked_add(bytes))
            .filter(|live| *live <= self.max_bytes)
            .ok_or_else(|| {
                RuntimeError::new("allocation_limit", "heap", "logical byte limit exceeded")
            })?;
        self.stats.total_allocated_bytes = self
            .stats
            .total_allocated_bytes
            .saturating_add(bytes.saturating_sub(self.external_bytes));
        self.stats.live_bytes = live;
        self.stats.peak_bytes = self.stats.peak_bytes.max(live);
        self.external_bytes = bytes;
        Ok(())
    }

    /// On failure, returns the object to the caller without changing the heap.
    pub fn allocate(&mut self, value: T, bytes: u64) -> Result<Handle, (RuntimeError, T)> {
        let Some(live_bytes) = self.stats.live_bytes.checked_add(bytes) else {
            return Err((
                RuntimeError::new("allocation_limit", "heap", "logical byte overflow"),
                value,
            ));
        };
        if self.stats.live_objects >= self.max_objects || live_bytes > self.max_bytes {
            return Err((
                RuntimeError::new(
                    "allocation_limit",
                    "heap",
                    "object or logical byte limit exceeded",
                ),
                value,
            ));
        }
        if self.occupied.try_reserve(1).is_err() {
            return Err((
                RuntimeError::new("allocation_limit", "heap", "occupied storage exhausted"),
                value,
            ));
        }
        let index = if let Some(index) = self.free {
            let slot = &mut self.slots[index];
            self.free = slot.next_free.take();
            slot.value = Some(value);
            slot.bytes = bytes;
            slot.edges = None;
            index
        } else {
            // Exhausted generation slots remain retired; metadata is bounded too.
            let target = self
                .slots
                .capacity()
                .saturating_mul(2)
                .max(1)
                .min(self.max_objects);
            let allocation_failed = self.slots.len() == self.slots.capacity()
                && self
                    .slots
                    .try_reserve_exact(target.saturating_sub(self.slots.len()))
                    .is_err();
            if self.slots.len() >= self.max_objects || allocation_failed {
                return Err((
                    RuntimeError::new("allocation_limit", "heap", "object storage exhausted"),
                    value,
                ));
            }
            let index = self.slots.len();
            self.slots.push(Slot {
                generation: 1,
                value: Some(value),
                bytes,
                next_free: None,
                edges: None,
                marked: 0,
            });
            index
        };
        self.occupied.push(index);
        self.stats.live_objects += 1;
        self.stats.live_bytes = live_bytes;
        self.stats.total_allocated_bytes = self.stats.total_allocated_bytes.saturating_add(bytes);
        self.stats.peak_bytes = self.stats.peak_bytes.max(live_bytes);
        Ok(Handle {
            owner: self.id,
            index,
            generation: self.slots[index].generation,
        })
    }

    pub fn get(&self, handle: Handle) -> Result<&T, RuntimeError> {
        self.slots
            .get(handle.index)
            .filter(|slot| handle.owner == self.id && slot.generation == handle.generation)
            .and_then(|slot| slot.value.as_ref())
            .ok_or_else(|| {
                RuntimeError::new("stale_reference", "heap", "object handle is not live")
            })
    }

    pub fn get_mut(&mut self, handle: Handle) -> Result<&mut T, RuntimeError> {
        self.get(handle)?;
        self.slots
            .get_mut(handle.index)
            .filter(|slot| handle.owner == self.id && slot.generation == handle.generation)
            .and_then(|slot| {
                slot.edges = None;
                slot.value.as_mut()
            })
            .ok_or_else(|| {
                RuntimeError::new("stale_reference", "heap", "object handle is not live")
            })
    }

    /// Replaces an object without changing its identity. Quota failure leaves
    /// both the old object and its accounting intact.
    pub fn replace(
        &mut self,
        handle: Handle,
        value: T,
        bytes: u64,
    ) -> Result<(), (RuntimeError, T)> {
        if let Err(error) = self.get(handle) {
            return Err((error, value));
        }
        let old_bytes = self.slots[handle.index].bytes;
        let Some(live_bytes) = (self.stats.live_bytes - old_bytes)
            .checked_add(bytes)
            .filter(|total| *total <= self.max_bytes)
        else {
            return Err((
                RuntimeError::new("allocation_limit", "heap", "logical byte limit exceeded"),
                value,
            ));
        };
        self.slots[handle.index].value = Some(value);
        self.slots[handle.index].edges = None;
        self.slots[handle.index].bytes = bytes;
        self.stats.live_bytes = live_bytes;
        self.stats.total_allocated_bytes = self
            .stats
            .total_allocated_bytes
            .saturating_add(bytes.saturating_sub(old_bytes));
        self.stats.peak_bytes = self.stats.peak_bytes.max(live_bytes);
        Ok(())
    }

    pub(crate) fn allocation_bytes(&self, handle: Handle) -> Result<u64, RuntimeError> {
        self.get(handle)?;
        Ok(self.slots[handle.index].bytes)
    }

    /// Commits both sides of a resource registration after validating their
    /// combined growth. Failure leaves the original graph and accounting intact.
    pub(crate) fn replace_pair(
        &mut self,
        replacements: [(Handle, T, u64); 2],
    ) -> Result<(), RuntimeError> {
        let checked = (|| {
            if replacements[0].0 == replacements[1].0 {
                return Err(RuntimeError::new(
                    "invalid_reference",
                    "heap",
                    "replacement handles must differ",
                ));
            }
            let mut bytes = self.stats.live_bytes;
            for (handle, _, _) in &replacements {
                bytes -= self.allocation_bytes(*handle)?;
            }
            for (_, _, size) in &replacements {
                bytes = bytes
                    .checked_add(*size)
                    .filter(|bytes| *bytes <= self.max_bytes)
                    .ok_or_else(|| {
                        RuntimeError::new("allocation_limit", "heap", "logical byte limit exceeded")
                    })?;
            }
            Ok(bytes)
        })();
        let live_bytes = checked?;
        for (handle, value, bytes) in replacements {
            let slot = &mut self.slots[handle.index];
            self.stats.total_allocated_bytes = self
                .stats
                .total_allocated_bytes
                .saturating_add(bytes.saturating_sub(slot.bytes));
            slot.value = Some(value);
            slot.bytes = bytes;
            slot.edges = None;
        }
        self.stats.live_bytes = live_bytes;
        self.stats.peak_bytes = self.stats.peak_bytes.max(live_bytes);
        Ok(())
    }

    /// The owner validates a path and its replacement before entering this
    /// infallible mutation. Unchanged outgoing edges may retain the trace cache.
    pub(crate) fn update(
        &mut self,
        handle: Handle,
        bytes: u64,
        edges_unchanged: bool,
        update: impl FnOnce(&mut T),
    ) -> Result<(), RuntimeError> {
        let previous = self.allocation_bytes(handle)?;
        let live = (self.stats.live_bytes - previous)
            .checked_add(bytes)
            .filter(|live| *live <= self.max_bytes)
            .ok_or_else(|| {
                RuntimeError::new("allocation_limit", "heap", "logical byte limit exceeded")
            })?;
        let slot = &mut self.slots[handle.index];
        update(slot.value.as_mut().unwrap());
        if !edges_unchanged {
            slot.edges = None;
        }
        slot.bytes = bytes;
        self.stats.live_bytes = live;
        self.stats.peak_bytes = self.stats.peak_bytes.max(live);
        self.stats.total_allocated_bytes = self
            .stats
            .total_allocated_bytes
            .saturating_add(bytes.saturating_sub(previous));
        Ok(())
    }

    /// Marks the complete graph before releasing anything. A stale root or edge
    /// aborts collection without changing the graph or accounting.
    pub fn collect(
        &mut self,
        roots: impl IntoIterator<Item = Handle>,
    ) -> Result<usize, RuntimeError> {
        self.collect_with_persistent_roots(&[], roots)
    }

    pub(crate) fn collect_with_persistent_roots(
        &mut self,
        persistent: &[Handle],
        roots: impl IntoIterator<Item = Handle>,
    ) -> Result<usize, RuntimeError> {
        self.mark_epoch = match self.mark_epoch.checked_add(1) {
            Some(epoch) => epoch,
            None => {
                for slot in &mut self.slots {
                    slot.marked = 0;
                }
                1
            }
        };
        let epoch = self.mark_epoch;
        let mut pending = std::mem::take(&mut self.trace_pending);
        pending.clear();
        let marked = (|| {
            for root in persistent.iter().copied().chain(roots) {
                self.get(root)?;
                if self.slots[root.index].marked != epoch {
                    pending.try_reserve(1).map_err(|_| {
                        RuntimeError::new("allocation_limit", "heap", "trace storage exhausted")
                    })?;
                    self.slots[root.index].marked = epoch;
                    pending.push(root);
                }
            }
            while let Some(handle) = pending.pop() {
                let slot = &mut self.slots[handle.index];
                let mut edges = slot.edges.take().unwrap_or_default();
                let cacheable = slot.value.as_ref().unwrap().stable_edges();
                if edges.is_empty() || !cacheable {
                    edges.clear();
                    slot.value
                        .as_ref()
                        .unwrap()
                        .trace(&mut |child| edges.push(child));
                }
                let traced = (|| {
                    for child in &edges {
                        self.get(*child)?;
                        if self.slots[child.index].marked != epoch {
                            pending.try_reserve(1).map_err(|_| {
                                RuntimeError::new(
                                    "allocation_limit",
                                    "heap",
                                    "trace storage exhausted",
                                )
                            })?;
                            self.slots[child.index].marked = epoch;
                            pending.push(*child);
                        }
                    }
                    Ok::<_, RuntimeError>(())
                })();
                if cacheable {
                    self.slots[handle.index].edges = Some(edges);
                }
                traced?;
            }
            Ok::<_, RuntimeError>(())
        })();
        pending.clear();
        self.trace_pending = pending;
        marked?;
        let mut released = 0;
        let mut position = 0;
        while position < self.occupied.len() {
            let index = self.occupied[position];
            let slot = &mut self.slots[index];
            if slot.marked == epoch {
                position += 1;
                continue;
            }
            slot.value = None;
            slot.edges = None;
            self.stats.live_bytes -= slot.bytes;
            self.stats.live_objects -= 1;
            slot.bytes = 0;
            released += 1;
            if let Some(generation) = slot.generation.checked_add(1) {
                slot.generation = generation;
                slot.next_free = self.free;
                self.free = Some(index);
            }
            self.occupied.swap_remove(position);
        }
        self.stats.collections = self.stats.collections.saturating_add(1);
        Ok(released)
    }

    /// Reserves only new objects. Existing values and handles are never copied.
    pub(crate) fn prepare_allocations(
        &mut self,
        values: Vec<(T, u64)>,
    ) -> Result<AllocationBatch<'_, T>, RuntimeError> {
        let failure = || {
            RuntimeError::new(
                "allocation_limit",
                "heap",
                "allocation batch exceeds storage limits",
            )
        };
        let count = values.len();
        if count > self.max_objects.saturating_sub(self.stats.live_objects) {
            return Err(failure());
        }
        let bytes = values
            .iter()
            .try_fold(0u64, |total, (_, bytes)| total.checked_add(*bytes))
            .ok_or_else(failure)?;
        let live = self
            .stats
            .live_bytes
            .checked_add(bytes)
            .filter(|live| *live <= self.max_bytes)
            .ok_or_else(failure)?;
        let mut handles = Vec::new();
        handles.try_reserve_exact(count).map_err(|_| failure())?;
        let mut free = self.free;
        let mut additional = 0;
        for _ in 0..count {
            let (index, generation) = if let Some(index) = free {
                let slot = &self.slots[index];
                free = slot.next_free;
                (index, slot.generation)
            } else {
                let index = self
                    .slots
                    .len()
                    .checked_add(additional)
                    .filter(|index| *index < self.max_objects)
                    .ok_or_else(failure)?;
                additional += 1;
                (index, 1)
            };
            handles.push(Handle {
                owner: self.id,
                index,
                generation,
            });
        }
        self.slots
            .try_reserve_exact(additional)
            .map_err(|_| failure())?;
        self.occupied.try_reserve(count).map_err(|_| failure())?;
        Ok(AllocationBatch {
            heap: self,
            values,
            handles,
            next_free: free,
            live,
            bytes,
        })
    }
}

pub(crate) struct AllocationBatch<'a, T: Trace> {
    heap: &'a mut Heap<T>,
    values: Vec<(T, u64)>,
    handles: Vec<Handle>,
    next_free: Option<usize>,
    live: u64,
    bytes: u64,
}
impl<T: Trace> AllocationBatch<'_, T> {
    pub(crate) fn handles(&self) -> &[Handle] {
        &self.handles
    }
    /// All capacity and quota checks precede this infallible publication.
    pub(crate) fn commit(self) {
        let count = self.values.len();
        for ((value, bytes), handle) in self.values.into_iter().zip(self.handles) {
            let slot = Slot {
                generation: handle.generation,
                value: Some(value),
                bytes,
                next_free: None,
                edges: None,
                marked: 0,
            };
            if handle.index == self.heap.slots.len() {
                self.heap.slots.push(slot);
            } else {
                self.heap.slots[handle.index] = slot;
            }
            self.heap.occupied.push(handle.index);
        }
        self.heap.free = self.next_free;
        self.heap.stats.live_objects += count;
        self.heap.stats.live_bytes = self.live;
        self.heap.stats.total_allocated_bytes = self
            .heap
            .stats
            .total_allocated_bytes
            .saturating_add(self.bytes);
        self.heap.stats.peak_bytes = self.heap.stats.peak_bytes.max(self.live);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        types::TypeIdentity,
        value::{Address, Data, Value},
    };

    #[test]
    fn allocation_batch_preserves_payload_and_publishes_only_on_commit() {
        #[derive(Debug)]
        struct Bytes(Vec<u8>);
        impl Trace for Bytes {
            fn trace(&self, _: &mut dyn FnMut(Handle)) {}
        }
        let mut heap = Heap::new(4, 1024).unwrap();
        let root = heap.allocate(Bytes(vec![42; 256]), 256).unwrap();
        let payload = heap.get(root).unwrap().0.as_ptr();
        let before = heap.stats();
        assert!(
            heap.prepare_allocations(vec![(Bytes(vec![1]), 1024)])
                .is_err()
        );
        assert_eq!(heap.stats(), before);
        let candidate = heap
            .prepare_allocations(vec![(Bytes(vec![2]), 64)])
            .unwrap();
        let unpublished = candidate.handles()[0];
        drop(candidate);
        assert!(heap.get(unpublished).is_err());
        assert_eq!(heap.stats(), before);
        let candidate = heap
            .prepare_allocations(vec![(Bytes(vec![3]), 64)])
            .unwrap();
        let published = candidate.handles()[0];
        candidate.commit();
        assert_eq!(heap.get(published).unwrap().0, [3]);
        assert_eq!(heap.get(root).unwrap().0.as_ptr(), payload);
        assert_eq!(heap.stats().live_bytes, 320);
        heap.collect([root]).unwrap();
        let candidate = heap
            .prepare_allocations(vec![(Bytes(vec![4]), 64)])
            .unwrap();
        let reused = candidate.handles()[0];
        candidate.commit();
        assert!(heap.get(published).is_err());
        assert_eq!(heap.get(reused).unwrap().0, [4]);
    }

    #[test]
    fn failed_mark_keeps_objects_and_next_epoch_can_recover() {
        #[derive(Debug)]
        struct Node(Option<Handle>);
        impl Trace for Node {
            fn trace(&self, visit: &mut dyn FnMut(Handle)) {
                if let Some(child) = self.0 {
                    visit(child);
                }
            }
        }
        let mut heap = Heap::new(4, 128).unwrap();
        let stale = heap.allocate(Node(None), 16).unwrap();
        heap.collect([]).unwrap();
        let root = heap.allocate(Node(Some(stale)), 16).unwrap();
        let other = heap.allocate(Node(None), 16).unwrap();
        let before = heap.stats();
        assert_eq!(heap.collect([root]).unwrap_err().code, "stale_reference");
        assert_eq!(heap.stats(), before);
        assert!(heap.get(other).is_ok());
        heap.get_mut(root).unwrap().0 = None;
        heap.mark_epoch = u64::MAX;
        heap.collect([root]).unwrap();
        assert!(heap.get(other).is_err());
        assert!(heap.get(root).is_ok());
    }

    #[test]
    fn external_metadata_reservation_is_atomic_and_survives_arena_collection() {
        let mut heap = Heap::new(4, 256).unwrap();
        let root = heap.allocate(Value::int(42), 128).unwrap();
        heap.set_external_bytes(128).unwrap();
        let before = heap.stats();
        assert_eq!(
            heap.set_external_bytes(129).unwrap_err().code,
            "allocation_limit"
        );
        assert_eq!(heap.stats(), before);
        assert_eq!(heap.get(root).unwrap().integer().unwrap(), 42);
        heap.collect([]).unwrap();
        assert_eq!(heap.stats().live_objects, 0);
        assert_eq!(heap.stats().live_bytes, 128);
        heap.set_external_bytes(0).unwrap();
        assert_eq!(heap.stats().live_bytes, 0);
        assert_eq!(heap.stats().total_allocated_bytes, 256);
    }

    #[test]
    fn reciprocal_edges_commit_atomically_with_the_combined_budget() {
        let mut heap = Heap::new(4, 100).unwrap();
        let first = heap.allocate(Value::int(1), 32).unwrap();
        let second = heap.allocate(Value::int(2), 32).unwrap();
        let pointer = |root| Value {
            typ: TypeIdentity::Any,
            data: Data::Pointer(Address {
                identity: std::sync::Arc::default(),
                root,
                path: vec![],
            }),
        };
        heap.collect_with_persistent_roots(&[first], [second])
            .unwrap();
        let before = heap.stats();
        assert_eq!(
            heap.replace_pair([(first, pointer(second), 48), (second, pointer(first), 64)])
                .unwrap_err()
                .code,
            "allocation_limit"
        );
        assert_eq!(heap.stats(), before);
        assert_eq!(heap.get(first).unwrap().integer().unwrap(), 1);
        assert_eq!(heap.get(second).unwrap().integer().unwrap(), 2);
        heap.replace_pair([(first, pointer(second), 48), (second, pointer(first), 48)])
            .unwrap();
        heap.collect_with_persistent_roots(&[first], []).unwrap();
        assert!(heap.get(second).is_ok());
        heap.collect([]).unwrap();
        assert_eq!(heap.stats().live_objects, 0);
    }

    #[test]
    fn reachable_graph_tracks_mutation_cycles_and_quota_failure() {
        let mut heap = Heap::new(8, 512).unwrap();
        let first = heap.allocate(Value::int(1), 32).unwrap();
        let second = heap.allocate(Value::int(2), 32).unwrap();
        let pointer = |root| Value {
            typ: TypeIdentity::Any,
            data: Data::Pointer(Address {
                identity: std::sync::Arc::default(),
                root,
                path: vec![],
            }),
        };
        let root = heap.allocate(pointer(first), 32).unwrap();
        heap.collect_with_persistent_roots(&[root], [second])
            .unwrap();
        let before = heap.stats();
        assert_eq!(
            heap.update(root, 1024, false, |_| panic!("quota must precede mutation"))
                .unwrap_err()
                .code,
            "allocation_limit"
        );
        assert_eq!(heap.stats(), before);
        heap.collect_with_persistent_roots(&[root], [second])
            .unwrap();
        assert!(heap.get(first).is_ok());
        *heap.get_mut(root).unwrap() = pointer(second);
        heap.collect_with_persistent_roots(&[root], []).unwrap();
        assert!(heap.get(first).is_err());
        assert!(heap.get(second).is_ok());
        let third = heap.allocate(pointer(root), 32).unwrap();
        heap.update(root, 32, false, |value| *value = pointer(third))
            .unwrap();
        heap.collect_with_persistent_roots(&[root], []).unwrap();
        assert!(heap.get(second).is_err());
        assert!(heap.get(third).is_ok());
        heap.collect_with_persistent_roots(&[], []).unwrap();
        assert_eq!(heap.stats().live_objects, 0);
        assert_eq!(heap.stats().live_bytes, 0);
    }

    #[test]
    fn persistent_roots_with_interior_mutability_are_retraced() {
        #[derive(Debug)]
        struct Node(bool, std::cell::RefCell<Vec<Handle>>);
        impl Trace for Node {
            fn stable_edges(&self) -> bool {
                self.0
            }
            fn trace(&self, visit: &mut dyn FnMut(Handle)) {
                for handle in self.1.borrow().iter() {
                    visit(*handle);
                }
            }
        }
        let mut heap = Heap::new(4, 128).unwrap();
        let root = heap.allocate(Node(true, Default::default()), 16).unwrap();
        let child = heap.allocate(Node(true, Default::default()), 16).unwrap();
        heap.collect_with_persistent_roots(&[root], [child])
            .unwrap();
        heap.get_mut(root).unwrap().0 = false;
        heap.get(root).unwrap().1.borrow_mut().push(child);
        heap.collect_with_persistent_roots(&[root], []).unwrap();
        assert!(heap.get(child).is_ok());
        heap.get(root).unwrap().1.borrow_mut().clear();
        heap.collect_with_persistent_roots(&[root], []).unwrap();
        assert!(heap.get(child).is_err());
    }
}
