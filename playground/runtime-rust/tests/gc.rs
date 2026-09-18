mod support;

use mini_go::{
    instance::{ExecutionLimits, Instance, PollStatus},
    loader::LoadLimits,
    program::Program,
};
use serde_json::json;
use std::sync::Arc;

#[test]
fn frame_owned_map_iterator_is_traced_and_released_on_completion_cancel_and_close() {
    let program = Arc::new(Program::load(&support::image(json!({
        "type_table":{"nodes":[{"id":"map","kind":7,"key":{"kind":3,"primitive":3},"elem":{"kind":3,"primitive":3}}]},
        "constants":[{"id":"key","type":{"kind":3,"primitive":3},"value":1},{"id":"answer","type":{"kind":3,"primitive":3},"value":42}],
        "functions":[{"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},
            "locals":[{"id":"cursor","type":{"kind":7,"node":"map"}},{"id":"answer","type":{"kind":3,"primitive":3}}],
            "instructions":[
                {"op":"const","payload":{"constant":"key"}},
                {"op":"const","payload":{"constant":"answer"}},
                {"op":"make_map","payload":{"type":{"kind":7,"node":"map"},"entry_count":1}},
                {"op":"map_iter_init","payload":{"local":"cursor"}},
                {"op":"map_iter_next","payload":{"local":"cursor"}},
                {"op":"pop"},
                {"op":"store_local","payload":{"local":"answer"}},
                {"op":"pop"},
                {"op":"map_iter_close","payload":{"local":"cursor"}},
                {"op":"load_local","payload":{"local":"answer"}},
                {"op":"return","payload":{"result_count":1}}
            ]}]
    })), LoadLimits::default()).unwrap());
    for action in ["complete", "cancel", "close"] {
        let mut instance = Instance::new(program.clone(), ExecutionLimits::default()).unwrap();
        instance.start("default", vec![]).unwrap();
        assert_eq!(instance.poll_steps(4).unwrap(), PollStatus::Running);
        instance.collect_garbage().unwrap();
        match action {
            "cancel" => instance.cancel().unwrap(),
            "close" => instance.close().unwrap(),
            _ => {
                while instance.poll_steps(1).unwrap() == PollStatus::Running {
                    instance.collect_garbage().unwrap();
                }
                assert_eq!(instance.results()[0].integer().unwrap(), 42);
            }
        }
        instance.collect_garbage().unwrap();
        assert_eq!(instance.heap_stats().live_objects, 0, "{action}");
        instance.close().unwrap();
    }
}

#[test]
fn allocation_pressure_preserves_popped_arguments_and_unpublished_frame_slots() {
    let mut code = Vec::new();
    for _ in 0..200 {
        code.push(json!({"op":"make_sequence","payload":{"type":{"kind":5,"node":"slice"},"element_count":0}}));
        code.push(json!({"op":"pop"}));
    }
    code.extend([
        json!({"op":"const","payload":{"constant":"answer"}}),
        json!({"op":"make_sequence","payload":{"type":{"kind":5,"node":"slice"},"element_count":1}}),
        json!({"op":"call_direct","payload":{"function":"read","arg_count":1,"result_count":1}}),
        json!({"op":"return","payload":{"result_count":1}}),
    ]);
    let program = Arc::new(Program::load(&support::image(json!({
        "type_table":{"nodes":[{"id":"slice","kind":5,"elem":{"kind":3,"primitive":3}}]},
        "constants":[{"id":"answer","type":{"kind":3,"primitive":3},"value":42},{"id":"index","type":{"kind":3,"primitive":3},"value":0}],
        "functions":[
            {"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},"locals":[{"id":"parent","type":{"kind":3,"primitive":3}}],"instructions":code},
            {"id":"read","signature":{"params":[{"type":{"kind":5,"node":"slice"}}],"results":[{"kind":3,"primitive":3}]},
                "locals":[{"id":"input","type":{"kind":5,"node":"slice"}},{"id":"temporary","type":{"kind":3,"primitive":3}}],"instructions":[
                    {"op":"load_local","payload":{"local":"input"}},{"op":"const","payload":{"constant":"index"}},
                    {"op":"load_index"},{"op":"return","payload":{"result_count":1}}
                ]}
        ]
    })), LoadLimits::default()).unwrap());
    let mut observations = Vec::new();
    for quantum in [1, 4096] {
        let mut instance = Instance::new(
            program.clone(),
            ExecutionLimits {
                max_objects: 4,
                max_heap_bytes: 4096,
                ..Default::default()
            },
        )
        .unwrap();
        instance.start("default", vec![]).unwrap();
        while instance.poll_steps(quantum).unwrap() == PollStatus::Running {}
        assert_eq!(instance.results()[0].integer().unwrap(), 42);
        instance.collect_garbage().unwrap();
        assert_eq!(instance.heap_stats().live_objects, 0);
        observations.push((
            instance.steps(),
            instance.heap_stats().total_allocated_bytes,
        ));
        instance.close().unwrap();
        assert_eq!(instance.heap_stats().live_bytes, 0);
    }
    assert_eq!(observations[0], observations[1]);
}
