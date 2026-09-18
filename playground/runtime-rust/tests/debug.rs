use mini_go::{
    contract::canonical_hash,
    contract_generated as wire,
    execution::{ExecutionState, SharedInstance},
    ffi::Cancellation,
    instance::{
        ExecutionLimits, Instance, PollStatus,
        debug::{EventKind, StepMode},
    },
    loader::LoadLimits,
    program::Program,
    snapshot::{HostData, SnapshotLimits},
};
use serde_json::json;
use std::sync::Arc;

mod support;

#[test]
fn selected_task_steps_after_its_own_instruction() {
    let image = support::image(json!({
        "constants":[{"id":"answer","type":{"kind":3,"primitive":3},"value":42}],
        "functions":[
            {"id":"fn.Main","instructions":[{"op":"make_closure","payload":{"function":"worker"}},{"op":"spawn","payload":{"arg_count":0}},{"op":"label","payload":{"label":"loop"}},{"op":"jump","payload":{"label":"loop"}}]},
            {"id":"worker","instructions":[{"op":"const","payload":{"constant":"answer"}},{"op":"pop"},{"op":"label","payload":{"label":"loop"}},{"op":"jump","payload":{"label":"loop"}}]}
        ]
    }));
    let program = Arc::new(Program::load(&image, LoadLimits::default()).unwrap());
    let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(2).unwrap(), PollStatus::Running);
    instance.request_pause();
    assert_eq!(instance.poll_steps(1).unwrap(), PollStatus::Paused);
    let stack = instance.debug_stack().unwrap();
    let worker = stack
        .iter()
        .find(|frame| frame.function == "worker")
        .unwrap();
    let task = worker.reference.task;
    assert_eq!(instance.debug_threads().len(), 2);
    assert_eq!(
        instance
            .debug_resume_task(StepMode::Instruction, u64::MAX)
            .unwrap_err()
            .code,
        "invalid_task"
    );
    instance
        .debug_resume_task(StepMode::Instruction, task)
        .unwrap();
    assert_eq!(instance.poll_steps(4096).unwrap(), PollStatus::Paused);
    let event = instance.debug_events().pop().unwrap();
    assert_eq!(event.frame.reference.task, task);
    assert_eq!(event.frame.pc, 1);
    instance.close().unwrap();
}

#[test]
fn panic_stop_preserves_the_frame_before_resuming_unwinding() {
    let image = support::image(serde_json::json!({
        "constants":[{"id":"message","type":{"kind":3,"primitive":2},"value":"boom"}],
        "functions":[{"id":"fn.Main","instructions":[
            {"op":"const","payload":{"constant":"message"}}, {"op":"panic"}
        ]}]
    }));
    let program = Arc::new(Program::load(&image, LoadLimits::default()).unwrap());
    let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
    instance.set_break_on_panic(true).unwrap();
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(2).unwrap(), PollStatus::Paused);
    assert_eq!(instance.steps(), 2);
    let event = instance.debug_events().pop().unwrap();
    assert_eq!(event.kind, EventKind::Panic);
    assert_eq!(event.frame.pc, 1);
    assert_eq!(instance.debug_stack().unwrap()[0].function, "fn.Main");
    instance.debug_resume(StepMode::Continue).unwrap();
    assert_eq!(instance.poll_steps(1).unwrap_err().code, "panic");
    instance.close().unwrap();
    assert_eq!(instance.heap_stats().live_objects, 0);
}

#[test]
fn paged_variables_borrow_only_requested_children_and_expire_on_resume() {
    let image = support::image(serde_json::json!({
        "type_table":{"nodes":[{"id":"array","kind":6,"length":1024,"elem":{"kind":3,"primitive":3}}]},
        "functions":[{"id":"fn.Main","locals":[{"id":"items","type":{"kind":6,"node":"array"}}],"instructions":[
            {"op":"return","payload":{}}
        ]}]
    }));
    let program = Arc::new(Program::load(&image, LoadLimits::default()).unwrap());
    let mut instance = Instance::new(program, ExecutionLimits::default()).unwrap();
    instance.start("default", vec![]).unwrap();
    instance.request_pause();
    assert_eq!(instance.poll_steps(1).unwrap(), PollStatus::Paused);
    let frame = instance.debug_stack().unwrap().remove(0).reference;
    let before = instance.heap_stats();
    assert!(
        instance
            .debug_bindings(
                &frame,
                SnapshotLimits {
                    max_bytes: 64,
                    ..Default::default()
                }
            )
            .is_err()
    );
    let variables = instance.debug_variables(&frame, 0, 1).unwrap();
    assert_eq!(variables[0].name, "items");
    assert_eq!(variables[0].children, 1024);
    let reference = variables[0].reference.clone().unwrap();
    let children = instance.debug_children(&reference, 1022, 20).unwrap();
    assert_eq!(
        children
            .iter()
            .map(|child| child.name.as_str())
            .collect::<Vec<_>>(),
        ["[1022]", "[1023]"]
    );
    assert!(
        children
            .iter()
            .all(|child| child.summary == "0" && child.reference.is_none())
    );
    assert!(
        instance
            .debug_children(&reference, 1024, 10)
            .unwrap()
            .is_empty()
    );
    assert_eq!(instance.heap_stats(), before);
    instance
        .debug_resume(mini_go::instance::debug::StepMode::Continue)
        .unwrap();
    assert_eq!(
        instance.debug_children(&reference, 0, 1).unwrap_err().code,
        "stale_reference"
    );
    instance.close().unwrap();
    assert_eq!(
        instance.debug_children(&reference, 0, 1).unwrap_err().code,
        "stale_reference"
    );
}

fn program(base: i64) -> Arc<Program> {
    let image = support::image(json!({
        "constants":[{"id":"base","type":{"kind":3,"primitive":3},"value":base},{"id":"delta","type":{"kind":3,"primitive":3},"value":22}],
        "functions":[
            {"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},"locals":[{"id":"n","type":{"kind":3,"primitive":3}}],"instructions":[
                {"op":"const","payload":{"constant":"base"}},{"op":"store_local","payload":{"local":"n"}},{"op":"load_local","payload":{"local":"n"}},
                {"op":"call_direct","payload":{"function":"add","arg_count":1,"result_count":1}},{"op":"return","payload":{"result_count":1}}
            ]},
            {"id":"add","signature":{"params":[{"type":{"kind":3,"primitive":3}}],"results":[{"kind":3,"primitive":3}]},"locals":[{"id":"x","type":{"kind":3,"primitive":3}}],"instructions":[
                {"op":"load_local","payload":{"local":"x"}},{"op":"const","payload":{"constant":"delta"}},{"op":"binary","payload":{"operator":"+"}},{"op":"return","payload":{"result_count":1}}
            ]}
        ]
    }));
    let program = Program::load(&image, LoadLimits::default()).unwrap();
    let contract: serde_json::Value = serde_json::from_str(wire::CONTRACT_JSON).unwrap();
    let mut symbols: wire::ProgramSymbols = serde_json::from_value(json!({
        "format":contract["spec"]["symbols_format"],"version":contract["spec"]["symbols_version"],"contract_id":contract["spec"]["symbols_contract"],
        "compiler_id":program.image().compiler_id,"program_hash":program.image().hash,
        "packages":{"test":{"module_path":"test","code_hash":program.image().packages.as_ref().unwrap()["test"].artifact_hash,
            "files":[{"id":"source","path":"main.mgo"}],"functions":[
                {"id":"fn.Main","name":"Main","locals":[{"id":"n","name":"number","scope":1}],"scopes":[{"id":1,"ranges":[{"start":0,"end":5}]}],
                 "locations":(0..5).map(|pc| json!({"pc":pc,"points":[{"file":"source","line":pc+1,"column":1}]})).collect::<Vec<_>>()},
                {"id":"add","name":"add","locals":[{"id":"x","name":"input"}],
                 "locations":(0..4).map(|pc| json!({"pc":pc,"points":[{"file":"source","line":pc+10,"column":1}]})).collect::<Vec<_>>()}
            ]}}
    })).unwrap();
    symbols.hash = canonical_hash(&symbols).unwrap();
    Arc::new(program.with_symbols(symbols).unwrap())
}

#[test]
fn source_stops_and_steps_preserve_budget_and_expire_frame_references() {
    let mut instance = Instance::new(program(20), ExecutionLimits::default()).unwrap();
    assert_eq!(
        instance.set_breakpoints("test", "main.mgo", &[3]).unwrap(),
        vec![3]
    );
    instance.start_profile(1, 2).unwrap();
    instance.start("default", Vec::new()).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Paused);
    assert_eq!(instance.steps(), 2);
    let frame = instance.debug_stack().unwrap().remove(0);
    assert_eq!(frame.pc, 2);
    assert_eq!(instance.debug_events()[0].kind, EventKind::Breakpoint);
    let values = instance
        .debug_bindings(&frame.reference, SnapshotLimits::default())
        .unwrap();
    assert_eq!(values.names, vec!["number"]);
    assert!(matches!(values.values.roots[0].data, HostData::Integer(20)));
    instance.debug_resume(StepMode::Into).unwrap();
    assert_eq!(
        instance
            .debug_bindings(&frame.reference, SnapshotLimits::default())
            .unwrap_err()
            .code,
        "stale_reference"
    );
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Paused);
    assert_eq!(instance.debug_stack().unwrap()[0].pc, 3);
    instance.debug_resume(StepMode::Over).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Paused);
    let frame = &instance.debug_stack().unwrap()[0];
    assert_eq!((&*frame.function, frame.pc), ("fn.Main", 4));
    instance.debug_resume(StepMode::Continue).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Ready);
    assert_eq!(instance.results()[0].integer().unwrap(), 42);
    let profile = instance.profile();
    assert_eq!(
        profile
            .samples
            .iter()
            .map(|sample| sample.count)
            .sum::<u64>()
            + profile.dropped,
        instance.steps()
    );
    assert!(profile.dropped > 0);
    instance.close().unwrap();
    assert!(matches!(values.values.roots[0].data, HostData::Integer(20)));
}

#[test]
fn shared_pause_is_observed_before_an_instruction_and_cancellation_releases_it() {
    let instance = SharedInstance::new(program(20), ExecutionLimits::default()).unwrap();
    let debugger = instance.debugger();
    debugger.pause();
    let execution = loop {
        match instance.start("default", Vec::new()) {
            Ok(execution) => break execution,
            Err(error) if error.code == "busy" => std::thread::yield_now(),
            Err(error) => panic!("{error}"),
        }
    };
    loop {
        match execution.poll_steps(1) {
            Ok(result) => {
                assert_eq!(result, (ExecutionState::Paused, 0));
                break;
            }
            Err(error) if error.code == "busy" => std::thread::yield_now(),
            Err(error) => panic!("{error}"),
        }
    }
    execution.cancel();
    assert!(execution.wait_scope(&Cancellation::default()).is_err());
    assert_eq!(execution.state(), ExecutionState::Canceled);
    instance.shutdown(&Cancellation::default()).unwrap();
    assert_eq!(instance.heap_stats().live_objects, 0);
}

#[test]
fn patch_rebinds_breakpoints_and_expires_inspection_without_rewriting_old_frames() {
    let mut instance = Instance::new(program(20), ExecutionLimits::default()).unwrap();
    instance.set_breakpoints("test", "main.mgo", &[3]).unwrap();
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Paused);
    let frame = instance.debug_stack().unwrap().remove(0);
    let plan = instance.prepare_patch(program(40)).unwrap();
    instance.apply_patch(plan).unwrap();
    assert_eq!(
        instance
            .debug_bindings(&frame.reference, Default::default())
            .unwrap_err()
            .code,
        "stale_reference"
    );
    let retained = instance.debug_stack().unwrap().remove(0);
    assert_eq!(retained.generation, 1);
    assert_eq!(retained.program_hash, frame.program_hash);
    instance.debug_resume(StepMode::Continue).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Ready);
    assert_eq!(instance.results()[0].integer().unwrap(), 42);
    instance.start("default", vec![]).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Paused);
    let current = instance.debug_stack().unwrap().remove(0);
    assert_eq!(current.generation, 2);
    assert_ne!(current.program_hash, retained.program_hash);
    let values = instance
        .debug_bindings(&current.reference, Default::default())
        .unwrap();
    assert!(matches!(values.values.roots[0].data, HostData::Integer(40)));
    instance.debug_resume(StepMode::Continue).unwrap();
    assert_eq!(instance.poll_steps(100).unwrap(), PollStatus::Ready);
    assert_eq!(instance.results()[0].integer().unwrap(), 62);
    instance.close().unwrap();
}
