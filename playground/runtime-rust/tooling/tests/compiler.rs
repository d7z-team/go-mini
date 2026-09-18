use mini_go::ffi::Cancellation;
use mini_go_tooling::session::CompilerSession;
use serde_json::json;

#[tokio::test]
async fn language_session_reuses_analysis_and_queries_shared_core() {
    let image =
        std::fs::read(std::env::var_os("MINIGO_TOOLS_IMAGE").unwrap_or_else(|| {
            concat!(env!("CARGO_MANIFEST_DIR"), "/assets/compiler.json.gz").into()
        }))
        .unwrap();
    let mut session = CompilerSession::new(&image).await.unwrap();
    let cancel = Cancellation::default();
    let mut workspace: serde_json::Value =
        serde_json::from_str(include_str!("../../../../testdata/language/workspace.json")).unwrap();
    workspace["Operation"] = "workspace/open".into();
    {
        use std::future::Future;
        let mut opening = Box::pin(session.call(workspace, &cancel));
        std::future::poll_fn(|context| {
            assert!(opening.as_mut().poll(context).is_pending());
            std::task::Poll::Ready(())
        })
        .await;
    }
    let canceled = Cancellation::default();
    canceled.cancel();
    assert_eq!(
        session
            .call(json!({"Operation":"workspace/analyze"}), &canceled)
            .await
            .unwrap_err()
            .code,
        "canceled"
    );
    let first = session
        .call(json!({"Operation":"workspace/analyze"}), &cancel)
        .await
        .unwrap();
    assert_eq!(first["Analysis"]["Revision"], "1");
    let queries: Vec<serde_json::Value> =
        serde_json::from_str(include_str!("../../../../testdata/language/queries.json")).unwrap();
    for mut query in queries {
        let operation = format!("language/{}", query["Operation"].as_str().unwrap());
        query["URI"] = "mini-go://sample/main.mgo".into();
        query["Snapshot"] = session.snapshot().into();
        session
            .call(json!({"Operation":operation,"Query":query}), &cancel)
            .await
            .unwrap_or_else(|error| panic!("{operation}: {error}"));
    }
    let hover=session.call(json!({"Operation":"language/hover","Query":{"Snapshot":session.snapshot(),"URI":"mini-go://sample/main.mgo","Position":{"line":2,"character":6}}}),&cancel).await.unwrap();
    assert!(
        hover["Value"]["contents"]["value"]
            .as_str()
            .unwrap()
            .contains("Answer")
    );
    let fixture: serde_json::Value =
        serde_json::from_str(include_str!("../../../../testdata/workspace/sources.json")).unwrap();
    let trees = serde_json::from_value::<Vec<mini_go_tooling::sources::SourceTree>>(
        fixture["Trees"].clone(),
    )
    .unwrap();
    let mut language = mini_go_tooling::language::LanguageService { session };
    let assembled = language.sources(&trees, &cancel).await.unwrap();
    session = language.session;
    let paths: Vec<_> = assembled
        .packages
        .iter()
        .map(|p| p.module_path.as_str())
        .collect();
    assert_eq!(serde_json::json!(paths), fixture["Paths"]);
    assert_eq!(
        assembled.packages[1]
            .resources
            .as_ref()
            .unwrap()
            .iter()
            .find(|r| r.path == "assets/data.bin")
            .unwrap()
            .data
            .as_deref(),
        Some("AP8B")
    );
    for failure in fixture["Failures"].as_array().unwrap() {
        let error = session
            .call(
                json!({"Operation":"workspace/sources","Trees":failure["Trees"]}),
                &cancel,
            )
            .await
            .unwrap_err();
        assert!(
            error
                .to_string()
                .contains(failure["Error"].as_str().unwrap()),
            "{error}"
        );
    }
    let debug_fixture: serde_json::Value =
        serde_json::from_str(include_str!("../../../../testdata/debug/breakpoint.json")).unwrap();
    let build=session.call(json!({"Operation":"build/prepare","Build":{"Revision":session.revision(),"Symbols":true,"EntryPoints":debug_fixture["EntryPoints"]}}),&cancel).await.unwrap();
    assert!(
        build["Diagnostics"]
            .as_array()
            .is_none_or(|items| items.is_empty()),
        "{build}"
    );
    let mut debugger = mini_go_tooling::dap::DebugSession::default();
    debugger
        .launch(
            build["ImageJSON"].as_str().unwrap().as_bytes(),
            build["SymbolsJSON"].as_str(),
            serde_json::from_value(build["Sources"].clone()).unwrap(),
            "default",
        )
        .unwrap();
    let breakpoints = debugger
        .request(
            "setBreakpoints",
            &json!({"source":{"path":debug_fixture["Source"]},"breakpoints":[{"line":debug_fixture["Line"]}]}),
        )
        .unwrap();
    assert_eq!(breakpoints["breakpoints"][0]["verified"], true);
    debugger.request("configurationDone", &json!({})).unwrap();
    let mut stopped = None;
    for _ in 0..1000 {
        debugger.poll().unwrap();
        if let Some(event) = debugger
            .events()
            .unwrap()
            .into_iter()
            .find(|event| event["event"] == "stopped")
        {
            stopped = Some(event);
            break;
        }
    }
    let thread = stopped.expect("breakpoint stop")["body"]["threadId"].clone();
    let stack = debugger
        .request("stackTrace", &json!({"threadId":thread}))
        .unwrap();
    assert!(!stack["stackFrames"].as_array().unwrap().is_empty());
    let source = debugger
        .request(
            "source",
            &json!({"sourceReference":stack["stackFrames"][0]["source"]["sourceReference"]}),
        )
        .unwrap();
    assert!(source["content"].as_str().unwrap().contains("Answer"));
    debugger
        .request("continue", &json!({"threadId":thread}))
        .unwrap();
    let mut terminated = false;
    for _ in 0..1000 {
        debugger.poll().unwrap();
        if debugger
            .events()
            .unwrap()
            .iter()
            .any(|event| event["event"] == "terminated")
        {
            terminated = true;
            break;
        }
    }
    assert!(terminated, "target termination");
    debugger.close();
    let previous_snapshot = session.snapshot().to_owned();
    assert!(session.upgrade(b"invalid image", &cancel).await.is_err());
    assert_eq!(session.snapshot(), previous_snapshot);
    session.upgrade(&image, &cancel).await.unwrap();
    assert_ne!(session.snapshot(), previous_snapshot);
    let previous_snapshot = session.snapshot().to_owned();
    {
        use std::future::Future;
        let mut interrupted =
            Box::pin(session.call(json!({"Operation":"workspace/analyze"}), &cancel));
        std::future::poll_fn(|context| {
            assert!(interrupted.as_mut().poll(context).is_pending());
            std::task::Poll::Ready(())
        })
        .await;
    }
    session
        .call(json!({"Operation":"workspace/analyze"}), &cancel)
        .await
        .unwrap();
    assert_ne!(session.snapshot(), previous_snapshot);
    let stale=session.call(json!({"Operation":"language/hover","Query":{"Snapshot":previous_snapshot,"URI":"mini-go://sample/main.mgo","Position":{"line":2,"character":6}}}),&cancel).await.unwrap_err();
    assert_eq!(stale.code, "stale");
    for _ in 0..20 {
        session
            .call(json!({"Operation":"workspace/analyze"}), &cancel)
            .await
            .unwrap();
    }
    let stats = session.stats().unwrap();
    assert_eq!(stats.active_scopes, 0);
    assert_eq!(stats.blocked_tasks, 0);
    assert_eq!(stats.runnable_tasks, 0);
    assert_eq!(stats.pending_ffi_calls, 0);
    session.close().await.unwrap();
}
