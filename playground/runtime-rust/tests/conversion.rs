mod support;

use mini_go::{
    HostValue, LoadOptions, Program, SnapshotLimits, contract_generated as wire,
    instance::{ExecutionLimits, Instance, PollStatus},
    snapshot::HostData,
};
use serde_json::json;
use std::sync::Arc;

#[test]
fn byte_append_growth_keeps_the_old_backing_and_copies_only_visible_bytes() {
    let slice = json!({"kind":wire::Slice,"node":"bytes"});
    let image = support::image(json!({
        "type_table":{"nodes":[{"id":"bytes","kind":wire::Slice,"elem":{"kind":3,"primitive":9}}]},
        "constants":[{"id":"byte","type":{"kind":3,"primitive":9},"value":255}],
        "functions":[{"id":"fn.Main","signature":{"params":[{"type":slice}],"results":[slice,slice]},
            "locals":[{"id":"input","type":slice},{"id":"grown","type":slice}],"instructions":[
                {"op":"load_local","payload":{"local":"input"}},
                {"op":"const","payload":{"constant":"byte"}},
                {"op":"append","payload":{"count":1}},
                {"op":"store_local","payload":{"local":"grown"}},
                {"op":"load_local","payload":{"local":"grown"}},
                {"op":"zero","payload":{"type":{"kind":3,"primitive":3}}},
                {"op":"const","payload":{"constant":"byte"}},
                {"op":"store_index"},
                {"op":"load_local","payload":{"local":"input"}},
                {"op":"load_local","payload":{"local":"grown"}},
                {"op":"return","payload":{"result_count":2}}
            ]}]
    }));
    let program = Arc::new(Program::load(&image, LoadOptions::default()).unwrap());
    let mut instance = Instance::new(
        program,
        ExecutionLimits {
            max_heap_bytes: 20_000,
            ..ExecutionLimits::default()
        },
    )
    .unwrap();
    let input = vec![128; 4096];
    instance.start_bytes("default", &input).unwrap();
    assert_eq!(instance.poll_steps(11).unwrap(), PollStatus::Ready);
    let snapshot = instance
        .snapshot_results(SnapshotLimits::default())
        .unwrap();
    instance.close().unwrap();
    assert_eq!(snapshot.bytes(&snapshot.roots[0]).unwrap(), input);
    let grown = snapshot.bytes(&snapshot.roots[1]).unwrap();
    assert_eq!(grown.len(), 4097);
    assert_eq!(grown[0], 255);
    assert_eq!(grown[4096], 255);
    assert_eq!(&grown[1..4096], &input[1..]);
    assert_eq!(instance.heap_stats().live_bytes, 0);
}

#[test]
fn byte_backing_mutation_preserves_pointer_aliases_with_a_byte_sized_budget() {
    let slice = json!({"kind":wire::Slice,"node":"bytes"});
    let array = json!({"kind":wire::Array,"node":"pair"});
    let pointer = json!({"kind":wire::Pointer,"node":"pointer"});
    let image = support::image(json!({
        "type_table":{"nodes":[
            {"id":"bytes","kind":wire::Slice,"elem":{"kind":3,"primitive":9}},
            {"id":"pair","kind":wire::Array,"length":2,"elem":{"kind":3,"primitive":9}},
            {"id":"pointer","kind":wire::Pointer,"elem":array}
        ]},
        "constants":[{"id":"one","type":{"kind":3,"primitive":3},"value":1},
            {"id":"byte","type":{"kind":3,"primitive":9},"value":255}],
        "functions":[{"id":"fn.Main","signature":{"params":[{"type":slice}],"results":[slice,pointer]},
            "locals":[{"id":"input","type":slice},{"id":"pointer","type":pointer}],"instructions":[
                {"op":"load_local","payload":{"local":"input"}},
                {"op":"convert","payload":{"type":pointer}},
                {"op":"store_local","payload":{"local":"pointer"}},
                {"op":"load_local","payload":{"local":"pointer"}},
                {"op":"zero","payload":{"type":array}},
                {"op":"store_indirect"},
                {"op":"load_local","payload":{"local":"input"}},
                {"op":"const","payload":{"constant":"one"}},
                {"op":"const","payload":{"constant":"byte"}},
                {"op":"store_index"},
                {"op":"load_local","payload":{"local":"input"}},
                {"op":"load_local","payload":{"local":"pointer"}},
                {"op":"return","payload":{"result_count":2}}
            ]}]
    }));
    let program = Arc::new(Program::load(&image, LoadOptions::default()).unwrap());
    let mut instance = Instance::new(
        program,
        ExecutionLimits {
            max_heap_bytes: 5120,
            ..ExecutionLimits::default()
        },
    )
    .unwrap();
    let input = vec![128; 4096];
    instance.start_bytes("default", &input).unwrap();
    assert_eq!(instance.poll_steps(13).unwrap(), PollStatus::Ready);
    let snapshot = instance
        .snapshot_results(SnapshotLimits::default())
        .unwrap();
    instance.close().unwrap();
    assert_eq!(instance.heap_stats().live_bytes, 0);
    let bytes = snapshot.bytes(&snapshot.roots[0]).unwrap();
    assert_eq!(&bytes[..3], &[0, 255, 128]);
    assert_eq!(bytes.len(), input.len());
    assert_eq!(&input[..3], &[128; 3]);
    let HostData::Pointer(address) = &snapshot.roots[1].data else {
        panic!("expected pointer")
    };
    let array = snapshot.resolve_address(address).unwrap();
    let HostData::Array(values) = &array.data else {
        panic!("expected array view")
    };
    assert!(matches!(values[0].data, HostData::Unsigned(0)));
    assert!(matches!(values[1].data, HostData::Unsigned(255)));
}

#[test]
fn repeated_pointer_type_views_write_original_storage_and_survive_in_snapshots() {
    let a = json!({"kind":4,"node":"A","named":{"module_path":"test","decl_id":"A"}});
    let b = json!({"kind":4,"node":"B","named":{"module_path":"test","decl_id":"B"}});
    let pa = json!({"kind":8,"node":"pointer.A"});
    let pb = json!({"kind":8,"node":"pointer.B"});
    let mut code = vec![json!({"op":"address_of","payload":{"kind":"local","local":"value"}})];
    for index in 0..3001 {
        code.push(
            json!({"op":"convert","payload":{"type":if index % 2 == 0 { &pb } else { &pa }}}),
        );
    }
    code.extend([
        json!({"op":"store_local","payload":{"local":"pointer"}}),
        json!({"op":"load_local","payload":{"local":"pointer"}}),
        json!({"op":"const","payload":{"constant":"answer"}}),
        json!({"op":"store_indirect"}),
        json!({"op":"load_local","payload":{"local":"pointer"}}),
        json!({"op":"load_local","payload":{"local":"value"}}),
        json!({"op":"return","payload":{"result_count":2}}),
    ]);
    let image = support::image(json!({
        "type_table":{"nodes":[
            {"id":"A","kind":4,"identity":{"module_path":"test","decl_id":"A"},"underlying":{"kind":3,"primitive":3}},
            {"id":"B","kind":4,"identity":{"module_path":"test","decl_id":"B"},"underlying":{"kind":3,"primitive":3}},
            {"id":"pointer.A","kind":8,"elem":a}, {"id":"pointer.B","kind":8,"elem":b}
        ]},
        "constants":[{"id":"answer","type":b,"value":42}],
        "functions":[{"id":"fn.Main","signature":{"results":[pb,a]},
            "locals":[{"id":"value","type":a},{"id":"pointer","type":pb}],"instructions":code}]
    }));
    let program = Arc::new(Program::load(&image, LoadOptions::default()).unwrap());
    let expected = program
        .types()
        .resolve("test", &serde_json::from_value(b).unwrap())
        .unwrap();
    let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(4000).unwrap(), PollStatus::Ready);
    let snapshot = instance
        .snapshot_results(SnapshotLimits::default())
        .unwrap();
    let HostData::Pointer(address) = &snapshot.roots[0].data else {
        panic!("expected pointer")
    };
    assert_eq!(
        address.path.len(),
        1,
        "views retain the original storage without intermediate views"
    );
    instance.close().unwrap();
    assert_eq!(instance.heap_stats().live_objects, 0);
    let value = snapshot.resolve_address(address).unwrap();
    assert_eq!(value.typ, expected);
    assert!(matches!(value.data, HostData::Integer(42)));
    assert!(matches!(snapshot.roots[1].data, HostData::Integer(42)));
}

#[test]
fn array_pointer_conversion_preserves_snapshot_view_and_faults_on_short_input() {
    for target_kind in [wire::Array, wire::Pointer] {
        let slice = json!({"kind":wire::Slice,"node":"slice"});
        let array = json!({"kind":wire::Array,"node":"array"});
        let pointer = json!({"kind":wire::Pointer,"node":"pointer"});
        let target = if target_kind == wire::Array {
            &array
        } else {
            &pointer
        };
        let image = support::image(json!({
            "type_table":{"nodes":[
                {"id":"slice","kind":wire::Slice,"elem":{"kind":3,"primitive":3}},
                {"id":"array","kind":wire::Array,"length":2,"elem":{"kind":3,"primitive":3}},
                {"id":"pointer","kind":wire::Pointer,"elem":array}
            ]},
            "functions":[{"id":"fn.Main","signature":{"params":[{"type":slice}],"results":[target]},
                "locals":[{"id":"input","type":slice}],"instructions":[
                    {"op":"load_local","payload":{"local":"input"}},
                    {"op":"convert","payload":{"type":target}},
                    {"op":"return","payload":{"result_count":1}}
                ]}]
        }));
        let program = Arc::new(Program::load(&image, LoadOptions::default()).unwrap());
        let typ = program
            .types()
            .resolve("test", &serde_json::from_value(slice).unwrap())
            .unwrap();
        let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
        let mut input = HostValue {
            typ,
            data: HostData::Array(vec![
                HostValue::int(20),
                HostValue::int(22),
                HostValue::int(99),
            ]),
        };
        instance
            .start_host("default", std::slice::from_ref(&input))
            .unwrap();
        assert_eq!(instance.poll_steps(3).unwrap(), PollStatus::Ready);
        let snapshot = instance
            .snapshot_results(SnapshotLimits::default())
            .unwrap();
        let root = &snapshot.roots[0];
        let value = match &root.data {
            HostData::Pointer(address) => snapshot.resolve_address(address).unwrap(),
            _ => std::borrow::Cow::Borrowed(root),
        };
        let HostData::Array(values) = &value.data else {
            panic!("expected array view")
        };
        assert_eq!(values.len(), 2);
        assert!(matches!(values[0].data, HostData::Integer(20)));
        assert!(matches!(values[1].data, HostData::Integer(22)));
        input.data = HostData::Array(vec![HostValue::int(1)]);
        instance.start_host("default", &[input]).unwrap();
        assert_eq!(instance.poll_steps(1).unwrap(), PollStatus::Running);
        assert_eq!(instance.poll_steps(1).unwrap_err().code, "type_error");
        assert_eq!(
            instance.start("default", vec![]).unwrap_err().code,
            "faulted"
        );
        instance.close().unwrap();
        assert_eq!(instance.heap_stats().live_objects, 0);
        assert!(matches!(values[1].data, HostData::Integer(22)));
    }
}
