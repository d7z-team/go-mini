mod support;

use mini_go::{
    instance::{ExecutionLimits, Instance, PollStatus},
    loader::LoadLimits,
    program::Program,
};
use serde_json::json;
use std::sync::Arc;

#[test]
fn unbuffered_try_send_commits_to_a_registered_receiver_without_changing_length() {
    let image = support::image(json!({
        "type_table":{"nodes":[{"id":"channel","kind":9,"direction":1,"elem":{"kind":3,"primitive":3}}]},
        "constants":[{"id":"answer","type":{"kind":3,"primitive":3},"value":42}],
        "functions":[{"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3},{"kind":3,"primitive":3},{"kind":3,"primitive":1}]},"locals":[
            {"id":"channel","type":{"kind":9,"node":"channel"}},
            {"id":"token","type":{"kind":3,"primitive":20}},
            {"id":"sent","type":{"kind":3,"primitive":1}}
        ],"instructions":[
            {"op":"zero","payload":{"type":{"kind":3,"primitive":3}}},
            {"op":"make_waitable","payload":{"type":{"kind":9,"node":"channel"}}},
            {"op":"store_local","payload":{"local":"channel"}},
            {"op":"make_wait_token"},{"op":"store_local","payload":{"local":"token"}},
            {"op":"load_local","payload":{"local":"channel"}},{"op":"load_local","payload":{"local":"token"}},{"op":"waitable_subscribe_recv"},
            {"op":"load_local","payload":{"local":"channel"}},{"op":"const","payload":{"constant":"answer"}},{"op":"waitable_try_send"},
            {"op":"store_local","payload":{"local":"sent"}},
            {"op":"load_local","payload":{"local":"channel"}},{"op":"len"},
            {"op":"load_local","payload":{"local":"channel"}},{"op":"waitable_recv"},
            {"op":"load_local","payload":{"local":"sent"}},{"op":"return","payload":{"result_count":3}}
        ]}]
    }));
    let program = Arc::new(Program::load(&image, LoadLimits::default()).unwrap());
    let mut instance = Instance::new(
        program,
        ExecutionLimits {
            max_allocated_bytes: 1024,
            ..ExecutionLimits::default()
        },
    )
    .unwrap();
    instance.start("default", Vec::new()).unwrap();
    assert_eq!(instance.poll_steps(8).unwrap(), PollStatus::Running);
    assert_eq!(instance.steps(), 8);
    assert_eq!(instance.memory_stats().total_allocated_bytes, 640);
    assert_eq!(instance.poll_steps(3).unwrap(), PollStatus::Running);
    assert_eq!(instance.memory_stats().total_allocated_bytes, 656);
    assert_eq!(instance.poll_steps(7).unwrap(), PollStatus::Ready);
    assert_eq!(instance.results()[0].integer().unwrap(), 0);
    assert_eq!(instance.results()[1].integer().unwrap(), 42);
    assert!(matches!(
        instance.results()[2].data(),
        mini_go::value::Data::Bool(true)
    ));
    instance.collect_garbage().unwrap();
    assert_eq!(instance.heap_stats().live_objects, 0);
    instance.start("default", Vec::new()).unwrap();
    assert_eq!(instance.poll_steps(18).unwrap(), PollStatus::Ready);
    let stats = instance.memory_stats();
    assert_eq!(
        [
            stats.live_bytes,
            stats.allocated_since_sweep,
            stats.total_allocated_bytes,
            stats.peak_bytes
        ],
        [624, 176, 1088, 912]
    );
    instance.close().unwrap();
}
