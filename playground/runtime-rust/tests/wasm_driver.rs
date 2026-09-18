mod support;
use mini_go::{
    Instance, Limits, LoadOptions, Program, RuntimeError,
    ffi::{self, Bridge, Call, Cancellation, Completion, Reply, Session},
    instance::{Instance as Machine, PollStatus},
};
use serde_json::json;
use std::{
    future::Future,
    pin::Pin,
    sync::{Arc, Mutex},
    task::{Poll, Waker},
};

#[derive(Clone, Default)]
struct Host {
    completion: Arc<Mutex<Option<Completion>>>,
    cleanup: Arc<Mutex<(bool, Option<Waker>)>>,
}
impl Call for Host {
    fn cancel(&self) {}
}
impl Bridge for Host {
    fn open(&self, _: Cancellation) -> Result<Box<dyn Session>, RuntimeError> {
        Ok(Box::new(self.clone()))
    }
}
impl Session for Host {
    fn start(
        &self,
        _: Cancellation,
        _: ffi::Request,
        completion: Completion,
    ) -> Result<Box<dyn Call>, RuntimeError> {
        *self.completion.lock().unwrap() = Some(completion);
        Ok(Box::<Host>::default())
    }
    fn shutdown(&self, _: Cancellation) -> Result<(), RuntimeError> {
        panic!("async cleanup must be polled")
    }
    fn shutdown_async(
        &self,
    ) -> Pin<Box<dyn Future<Output = Result<(), RuntimeError>> + Send + '_>> {
        Box::pin(std::future::poll_fn(|cx| {
            let mut cleanup = self.cleanup.lock().unwrap();
            if cleanup.0 {
                Poll::Ready(Ok(()))
            } else {
                cleanup.1 = Some(cx.waker().clone());
                Poll::Pending
            }
        }))
    }
}

fn fixture(name: &str) -> Vec<u8> {
    let patch = name.ends_with("-patch");
    let name = name.trim_end_matches("-patch");
    let ffi = json!([
        {"op":"const","payload":{"constant":"route"}}, {"op":"zero","payload":{"type":{"kind":5,"node":"bytes"}}},
        {"op":"call_ffi","payload":{"arg_count":2,"result_count":3}}, {"op":"pop"},{"op":"pop"},{"op":"pop"}, {"op":"return","payload":{}}
    ]);
    let mut artifact = json!({
        "type_table":{"nodes":[{"id":"bytes","kind":5,"elem":{"kind":3,"primitive":9}}]},
        "constants":[{"id":"route","type":{"kind":3,"primitive":2},"value":"wasm.test"},{"id":"answer","type":{"kind":3,"primitive":3},"value":42}],
        "functions":[{"id":"fn.Main","signature":{"results":[{"kind":3,"primitive":3}]},"instructions":[{"op":"const","payload":{"constant":"answer"}},{"op":"return","payload":{"result_count":1}}]}]
    });
    if name == "init" {
        artifact["functions"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":"fn.init","instructions":ffi}));
    }
    if name == "host" {
        artifact["functions"][0] = json!({"id":"fn.Main","instructions":ffi});
    }
    if name == "host-result" {
        let mut instructions = ffi.as_array().unwrap()[..3].to_vec();
        instructions.push(json!({"op":"return","payload":{"result_count":3}}));
        artifact["functions"][0] = json!({"id":"fn.Main","signature":{"results":[{"kind":5,"node":"bytes"},{"kind":3,"primitive":2},{"kind":3,"primitive":3}]},"instructions":instructions});
    }
    if name == "background" {
        artifact["functions"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":"worker","instructions":ffi}));
        artifact["functions"][0]["instructions"]
            .as_array_mut()
            .unwrap()
            .splice(
                0..0,
                [
                    json!({"op":"make_closure","payload":{"function":"worker"}}),
                    json!({"op":"spawn","payload":{"arg_count":0}}),
                ],
            );
    }
    if name == "echo" || name == "string" {
        let typ = json!({"kind":3,"primitive":if name == "echo" {3} else {2}});
        artifact["functions"][0] = json!({"id":"fn.Main","signature":{"params":[{"type":typ}],"results":[typ]},"locals":[{"id":"input","type":typ}],"instructions":[{"op":"load_local","payload":{"local":"input"}},{"op":"return","payload":{"result_count":1}}]});
    }
    if name == "timer" {
        artifact["type_table"]["nodes"]
            .as_array_mut()
            .unwrap()
            .push(json!({"id":"channel","kind":9,"direction":1,"elem":{"kind":3,"primitive":1}}));
        artifact["constants"].as_array_mut().unwrap().extend([
            json!({"id":"capacity","type":{"kind":3,"primitive":3},"value":1}),
            json!({"id":"delay","type":{"kind":3,"primitive":7},"value":20000000}),
        ]);
        artifact["functions"][0]["locals"] =
            json!([{"id":"channel","type":{"kind":9,"node":"channel"}}]);
        artifact["functions"][0]["instructions"].as_array_mut().unwrap().splice(0..0, serde_json::from_value::<Vec<serde_json::Value>>(json!([
            {"op":"const","payload":{"constant":"capacity"}}, {"op":"make_waitable","payload":{"type":{"kind":9,"node":"channel"}}},
            {"op":"store_local","payload":{"local":"channel"}}, {"op":"load_local","payload":{"local":"channel"}},
            {"op":"const","payload":{"constant":"delay"}}, {"op":"zero","payload":{"type":{"kind":3,"primitive":7}}},
            {"op":"call_intrinsic","payload":{"id":"time.timer_start","arg_count":3}},
            {"op":"load_local","payload":{"local":"channel"}}, {"op":"waitable_recv"}, {"op":"pop"}
        ])).unwrap());
    }
    if name == "loop" {
        artifact["functions"][0] = json!({"id":"fn.Main","instructions":[{"op":"label","payload":{"label":"loop"}},{"op":"jump","payload":{"label":"loop"}}]});
    }
    if patch {
        artifact["constants"][1]["value"] = json!(43);
    }
    support::image(artifact)
}

#[test]
fn external_driver_initializes_asynchronously_and_retains_cleanup_until_complete() {
    let host = Host::default();
    let program = Arc::new(Program::load(&fixture("init"), LoadOptions::default()).unwrap());
    let mut machine = Machine::with_bridge(program, Limits::default(), &host).unwrap();
    assert_eq!(
        machine
            .poll_initialize(&Cancellation::default(), 256)
            .unwrap(),
        PollStatus::Pending
    );
    assert!(!machine.root_ready());
    host.completion
        .lock()
        .unwrap()
        .take()
        .unwrap()
        .complete(Reply::new(vec![], None, None));
    assert_eq!(
        machine
            .poll_initialize(&Cancellation::default(), 256)
            .unwrap(),
        PollStatus::Ready
    );
    let instance = Instance::externally_driven(machine).unwrap();
    let execution = instance.start("default", vec![]).unwrap();
    instance.drive(256).unwrap();
    assert!(execution.scope_settled());
    assert!(execution.result().is_ok());
    instance.begin_shutdown();
    instance.drive(256).unwrap();
    assert!(instance.shutdown_result().is_none());
    let observed = instance.wake().epoch();
    for _ in 0..3 {
        instance.begin_shutdown();
        assert!(!instance.drive(256).unwrap());
    }
    assert_eq!(
        instance.wake().epoch(),
        observed,
        "pending cleanup must sleep until a real notification"
    );
    let wake = {
        let mut cleanup = host.cleanup.lock().unwrap();
        cleanup.0 = true;
        cleanup.1.take().unwrap()
    };
    wake.wake();
    instance.drive(256).unwrap();
    assert!(instance.shutdown_result().unwrap().is_ok());
}

#[test]
fn external_driver_cancels_running_work_and_wasm_fixtures_validate() {
    for name in [
        "answer",
        "answer-patch",
        "init",
        "host",
        "host-result",
        "loop",
        "background",
        "echo",
        "string",
        "timer",
    ] {
        let bytes = fixture(name);
        let program = Arc::new(Program::load(&bytes, LoadOptions::default()).unwrap());
        if let Some(directory) = std::env::var_os("MINIGO_WASM_FIXTURES") {
            std::fs::create_dir_all(&directory).unwrap();
            std::fs::write(
                std::path::Path::new(&directory).join(format!("{name}.json")),
                &bytes,
            )
            .unwrap();
        }
        if name != "loop" {
            continue;
        }
        let mut machine = Machine::new(program, Limits::default()).unwrap();
        machine.initialize_root(&Cancellation::default()).unwrap();
        let instance = Instance::externally_driven(machine).unwrap();
        let execution = instance.start("default", vec![]).unwrap();
        assert!(instance.drive(8).unwrap());
        execution.cancel();
        instance.drive(8).unwrap();
        assert!(execution.scope_settled());
        assert_eq!(execution.result().unwrap_err().code, "canceled");
        instance.begin_shutdown();
        instance.drive(8).unwrap();
        assert!(instance.shutdown_result().unwrap().is_ok());
    }
}
