mod support;

use mini_go::{
    instance::{ExecutionLimits, Instance, PollStatus},
    loader::LoadLimits,
    program::Program,
    value::Data,
};
use serde_json::json;
use std::sync::Arc;

#[test]
fn nil_slice_header_mutations_preserve_nil_and_reject_out_of_bounds_growth() {
    for intrinsic in ["reflect.value_set_len", "reflect.value_set_cap"] {
        for (count, named) in [(0, true), (1, true), (-1, true), (0, false)] {
            let image = support::image(json!({
                "module":{"path":"reflect","package":"reflect"},
                "type_table":{"nodes":[
                    {"id":"slice","kind":5,"elem":{"kind":3,"primitive":3}},
                    {"id":"interface","kind":13},
                    {"id":"Value","kind":4,"identity":{"module_path":"reflect","decl_id":"Value"},"underlying":{"kind":12,"node":"view"}},
                    {"id":"view","kind":12,"fields":[{"name":"valid","type":{"kind":3,"primitive":1}},{"name":"settable","type":{"kind":3,"primitive":1}},{"name":"target","type":{"kind":2}},{"name":"data","type":{"kind":2}},{"name":"valueType","type":{"kind":13,"node":"interface"}}]}
                ]},
                "constants":[{"id":"true","type":{"kind":3,"primitive":1},"value":true},{"id":"count","type":{"kind":3,"primitive":3},"value":count}],
                "functions":[{"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":2},{"kind":3,"primitive":1},{"kind":5,"node":"slice"}]},
                    "locals":[{"id":"slice","type":{"kind":5,"node":"slice"}}],"instructions":[
                        {"op":"const","payload":{"constant":"true"}},{"op":"const","payload":{"constant":"true"}},
                        {"op":"address_of","payload":{"kind":"local","local":"slice"}},
                        {"op":"zero","payload":{"type":{"kind":2}}},
                        {"op":"zero","payload":{"type":{"kind":13,"node":"interface"}}},
                        {"op":"make_struct","payload":{"type":if named { json!({"kind":4,"named":{"module_path":"reflect","decl_id":"Value"}}) } else { json!({"kind":12,"node":"view"}) },"fields":["valid","settable","target","data","valueType"]}},
                        {"op":"const","payload":{"constant":"count"}},
                        {"op":"call_intrinsic","payload":{"id":intrinsic,"arg_count":2,"result_count":2}},
                        {"op":"load_local","payload":{"local":"slice"}},{"op":"return","payload":{"result_count":3}}
                    ]}]
            }));
            let program = Arc::new(Program::load(&image, LoadLimits::default()).unwrap());
            let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
            instance.start("default", vec![]).unwrap();
            assert_eq!(instance.poll_steps(10).unwrap(), PollStatus::Ready);
            assert!(
                matches!(instance.results()[1].data(), Data::Bool(success) if *success == (count == 0 && named)),
                "{intrinsic}({count}): {}",
                instance.results()[0]
            );
            assert!(matches!(instance.results()[2].data(), Data::Nil));
            instance.close().unwrap();
            assert_eq!(instance.heap_stats().live_bytes, 0);
        }
    }
}
