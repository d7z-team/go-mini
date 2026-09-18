mod support;

use mini_go::{
    LoadOptions, Program,
    instance::{ExecutionLimits, Instance, PollStatus},
};
use serde_json::json;
use std::sync::Arc;

#[test]
fn tail_call_keeps_pending_defers_and_counts_only_bytecode_steps() {
    let image = support::image(json!({
        "constants":[{"id":"answer","type":{"kind":3,"primitive":3},"value":40},{"id":"delta","type":{"kind":3,"primitive":3},"value":2}],
        "globals":[{"id":"deferred","type":{"kind":3,"primitive":3}}],
        "functions":[
            {"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},"instructions":[
                {"op":"call_direct","payload":{"function":"wrapper","result_count":1}},
                {"op":"load_global","payload":{"global":"deferred"}},
                {"op":"binary","payload":{"operator":"+"}}, {"op":"return","payload":{"result_count":1}}
            ]},
            {"id":"wrapper","signature":{"results":[{"kind":3,"primitive":3}]},"instructions":[
                {"op":"make_closure","payload":{"function":"cleanup"}}, {"op":"defer_push"},
                {"op":"tail_call_direct","payload":{"function":"answer","result_count":1}}
            ]},
            {"id":"answer","signature":{"results":[{"kind":3,"primitive":3}]},"instructions":[
                {"op":"const","payload":{"constant":"answer"}}, {"op":"return","payload":{"result_count":1}}
            ]},
            {"id":"cleanup","instructions":[
                {"op":"const","payload":{"constant":"delta"}}, {"op":"store_global","payload":{"global":"deferred"}}, {"op":"return","payload":{}}
            ]}
        ]
    }));
    let program = Arc::new(Program::load(&image, LoadOptions::default()).unwrap());
    let mut instance = Instance::new(
        program,
        ExecutionLimits {
            max_frames: 3,
            ..Default::default()
        },
    )
    .unwrap();
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(6).unwrap(), PollStatus::Running);
    assert_eq!(instance.steps(), 6);
    assert_eq!(instance.poll_steps(6).unwrap(), PollStatus::Ready);
    assert_eq!(instance.steps(), 12);
    assert_eq!(instance.results()[0].integer().unwrap(), 42);
    instance.close().unwrap();
    assert_eq!(instance.heap_stats().live_objects, 0);
}
