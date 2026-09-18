//! Fixed workloads; counts are observations, not machine-dependent pass limits.
use mini_go::{
    LoadOptions, Program,
    instance::{ExecutionLimits, Instance, PollStatus},
    value::Value,
};
use serde_json::json;
use std::{
    alloc::{GlobalAlloc, Layout, System},
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::Instant,
};

#[path = "../tests/support/mod.rs"]
mod support;

struct CountingAllocator;
static ALLOCATIONS: AtomicU64 = AtomicU64::new(0);
static BYTES: AtomicU64 = AtomicU64::new(0);

// This standalone benchmark is single-threaded. The allocator delegates storage
// and layout ownership unchanged to System; only successful requests are counted.
unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let pointer = unsafe { System.alloc(layout) };
        if !pointer.is_null() {
            ALLOCATIONS.fetch_add(1, Ordering::Relaxed);
            BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
        }
        pointer
    }
    unsafe fn dealloc(&self, pointer: *mut u8, layout: Layout) {
        unsafe { System.dealloc(pointer, layout) }
    }
    unsafe fn realloc(&self, pointer: *mut u8, layout: Layout, size: usize) -> *mut u8 {
        let next = unsafe { System.realloc(pointer, layout, size) };
        if !next.is_null() {
            ALLOCATIONS.fetch_add(1, Ordering::Relaxed);
            BYTES.fetch_add(size as u64, Ordering::Relaxed);
        }
        next
    }
}

#[global_allocator]
static ALLOCATOR: CountingAllocator = CountingAllocator;

fn run(name: &str, image: &[u8], expected: i64, profile: bool) {
    let program = Arc::new(Program::load(image, LoadOptions::default()).unwrap());
    let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
    if profile {
        instance.start_profile(4096, 128).unwrap();
    }
    for iteration in 0..2 {
        if iteration == 1 {
            ALLOCATIONS.store(0, Ordering::Relaxed);
            BYTES.store(0, Ordering::Relaxed);
        }
        let before = Instant::now();
        let heap = instance.heap_stats();
        // A separate warmed batch excludes loading and initial frame allocation.
        let repetitions = if iteration == 1 { 100 } else { 1 };
        for _ in 0..repetitions {
            instance.start("default", vec![Value::int(1000)]).unwrap();
            let mut budget = 100_000;
            loop {
                let quantum = budget.min(1024);
                assert_ne!(quantum, 0, "bounded benchmark work");
                budget -= quantum;
                match instance.poll_steps(quantum).unwrap() {
                    PollStatus::Running => {}
                    PollStatus::Ready => break,
                    status => panic!("unexpected benchmark state: {status:?}"),
                }
            }
            assert_eq!(instance.results()[0].integer().unwrap(), expected);
        }
        if iteration == 1 {
            let allocations = ALLOCATIONS.load(Ordering::Relaxed);
            let bytes = BYTES.load(Ordering::Relaxed);
            let after = instance.heap_stats();
            println!(
                "{name}: calls=100 elapsed={:?} allocations={allocations} requested_bytes={bytes} arena_allocated={} collections={}",
                before.elapsed(),
                after.total_allocated_bytes - heap.total_allocated_bytes,
                after.collections - heap.collections
            );
        }
    }
    instance.close().unwrap();
}

fn main() {
    let mut instructions = Vec::new();
    for _ in 0..1000 {
        instructions.extend([
            json!({"op":"load_local","payload":{"local":"n"}}),
            json!({"op":"store_local","payload":{"local":"n"}}),
        ]);
    }
    instructions.extend([
        json!({"op":"load_local","payload":{"local":"n"}}),
        json!({"op":"return","payload":{"result_count":1}}),
    ]);
    let scalar = support::image(json!({"functions":[{
        "id":"fn.Main",
        "signature":{"params":[{"type":{"kind":3,"primitive":3}}],"results":[{"kind":3,"primitive":3}]},
        "locals":[{"id":"n","type":{"kind":3,"primitive":3}}],
        "instructions":instructions
    }]}));
    run("scalar-slots", &scalar, 1000, false);
    run("sparse-profile", &scalar, 1000, true);
    run(
        "arithmetic",
        include_bytes!("../examples/blocks/arithmetic.json"),
        500500,
        false,
    );
    run(
        "closure",
        include_bytes!("../examples/blocks/closure.json"),
        2003,
        false,
    );
}
