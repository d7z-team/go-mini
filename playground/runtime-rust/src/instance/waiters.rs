//! Stable wait registrations and deduplicated owner-ready events.
use super::*;
use scheduler::{Blocked, Task};
use std::collections::BTreeSet;

pub(super) struct WaitingTask {
    pub task: Task,
    pub(super) dependencies: Vec<Handle>,
}

#[derive(Default)]
pub(super) struct Waiters {
    entries: BTreeMap<u64, WaitingTask>,
    resources: HashMap<Handle, BTreeSet<u64>>,
    calls: HashMap<u64, u64>,
    ready: BTreeSet<u64>,
    next: u64,
}

impl Waiters {
    pub fn len(&self) -> usize {
        self.entries.len()
    }
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }
    pub fn iter(&self) -> impl Iterator<Item = &Task> {
        self.entries.values().map(|waiting| &waiting.task)
    }
    pub fn iter_mut(&mut self) -> impl Iterator<Item = &mut Task> {
        self.entries.values_mut().map(|waiting| &mut waiting.task)
    }
    pub fn clear(&mut self) {
        self.entries.clear();
        self.resources.clear();
        self.calls.clear();
        self.ready.clear();
    }
    pub fn push(&mut self, task: Task, dependencies: Vec<Handle>) {
        self.next += 1;
        let key = self.next;
        self.insert(key, WaitingTask { task, dependencies });
        self.ready.insert(key);
    }
    pub fn insert(&mut self, key: u64, waiting: WaitingTask) {
        for handle in &waiting.dependencies {
            self.resources.entry(*handle).or_default().insert(key);
        }
        if let Some(Blocked::Ffi(call)) = waiting.task.blocked {
            self.calls.insert(call, key);
        }
        self.entries.insert(key, waiting);
    }
    pub fn remove(&mut self, key: u64) -> WaitingTask {
        self.ready.remove(&key);
        let waiting = self.entries.remove(&key).expect("live wait registration");
        for handle in &waiting.dependencies {
            let keys = self.resources.get_mut(handle).unwrap();
            keys.remove(&key);
            if keys.is_empty() {
                self.resources.remove(handle);
            }
        }
        if let Some(Blocked::Ffi(call)) = waiting.task.blocked {
            self.calls.remove(&call);
        }
        waiting
    }
    pub fn next_ready(&mut self) -> Option<u64> {
        self.ready.pop_first()
    }
    pub fn notify_resource(&mut self, handle: Handle) {
        if let Some(keys) = self.resources.get(&handle) {
            self.ready.extend(keys);
        }
    }
    pub fn notify_call(&mut self, call: u64) {
        if let Some(key) = self.calls.get(&call) {
            self.ready.insert(*key);
        }
    }
    pub fn first(&self, handle: Handle, send: bool) -> Option<u64> {
        self.resources.get(&handle)?.iter().copied().find(|key| {
            matches!(
                (&self.entries[key].task.blocked, send),
                (Some(Blocked::Send { .. }), true) | (Some(Blocked::Receive { .. }), false)
            )
        })
    }
    pub fn send_count(&self, handle: Handle) -> usize {
        self.resources
            .get(&handle)
            .into_iter()
            .flatten()
            .filter(|key| matches!(self.entries[key].task.blocked, Some(Blocked::Send { .. })))
            .count()
    }
    pub fn remove_scope(&mut self, scope: u64) -> Vec<Task> {
        let keys: Vec<_> = self
            .entries
            .iter()
            .filter(|(_, entry)| entry.task.scope == scope)
            .map(|(key, _)| *key)
            .collect();
        keys.into_iter().map(|key| self.remove(key).task).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ready_events_are_deduplicated_ordered_and_detached_on_cancel() {
        let mut heap = Heap::new(4, 1024).unwrap();
        let resource = heap.allocate(Value::int(0), 16).unwrap();
        let unrelated = heap.allocate(Value::int(0), 16).unwrap();
        let mut waiters = Waiters::default();
        for id in 1..=128 {
            waiters.push(
                Task {
                    id,
                    scope: id,
                    frames: Vec::new(),
                    blocked: Some(Blocked::Ffi(id)),
                },
                vec![if id <= 2 { resource } else { unrelated }],
            );
        }
        while waiters.next_ready().is_some() {}
        // Idle owner boundaries consume no registrations, regardless of table size.
        for _ in 0..128 {
            assert!(waiters.next_ready().is_none());
        }
        waiters.notify_call(2);
        waiters.notify_resource(resource);
        waiters.notify_resource(resource);
        let first = waiters.next_ready().unwrap();
        assert_eq!(waiters.remove(first).task.id, 1);
        let second = waiters.next_ready().unwrap();
        assert_eq!(waiters.remove(second).task.id, 2);
        assert!(waiters.next_ready().is_none());
        waiters.notify_call(1);
        waiters.notify_resource(resource);
        assert!(waiters.next_ready().is_none());
        assert_eq!(waiters.remove_scope(3)[0].id, 3);
        waiters.notify_call(3);
        assert!(waiters.next_ready().is_none());
        waiters.clear();
        assert!(waiters.resources.is_empty());
        assert!(waiters.calls.is_empty());
    }
}
