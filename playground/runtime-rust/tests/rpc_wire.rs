#![cfg(feature = "rpc")]
use mini_go::rpc::*;
use serde::Deserialize;

#[derive(Deserialize)]
struct Fixture {
    name: String,
    hex: String,
    #[serde(default)]
    invalid: bool,
    #[serde(default)]
    values: Vec<Input>,
}
#[derive(Deserialize)]
struct Input {
    #[serde(rename = "type")]
    typ: String,
    kind: String,
    #[serde(default)]
    value: String,
}

fn unhex(text: &str) -> Vec<u8> {
    text.as_bytes()
        .as_chunks::<2>()
        .0
        .iter()
        .map(|pair| u8::from_str_radix(std::str::from_utf8(pair).unwrap(), 16).unwrap())
        .collect()
}

#[test]
fn bounded_wire_mutations_preserve_decoder_state() {
    use mini_go::rpc::protocol::*;
    let limits = Limits {
        max_message_bytes: 1024,
        max_frame_bytes: 256,
        max_value_elements: 64,
        max_value_depth: 8,
        ..Limits::default()
    };
    let frame = Frame {
        kind: RENEW,
        values: vec![0],
        origin: "fuzz".into(),
        id: 1,
        ..Frame::default()
    }
    .encode(limits.max_message_bytes)
    .unwrap();
    let values =
        encode_values(&[Value::new("[]uint8", Data::Bytes(vec![0, 255]))], &limits).unwrap();
    for original in [&frame, &values] {
        for index in 0..original.len() {
            for replacement in [0, 1, 127, 128, 255] {
                let mut mutated = original.clone();
                mutated[index] = replacement;
                if let Ok(decoded) = decode_values(&mutated, &limits) {
                    let encoded = encode_values(&decoded, &limits).unwrap();
                    assert!(decode_values(&encoded, &limits).is_ok());
                }
                if let Ok(decoded) = Frame::decode(&mutated, &limits) {
                    assert!(
                        Frame::decode(&decoded.encode(limits.max_message_bytes).unwrap(), &limits)
                            .is_ok()
                    );
                }
                let _ = FfiRequest::decode(&mutated, &limits);
                let _ = FfiResponse::decode(&mutated, &limits);
                assert_eq!(Frame::decode(&frame, &limits).unwrap().id, 1);
                assert_eq!(
                    encode_values(&decode_values(&values, &limits).unwrap(), &limits).unwrap(),
                    values
                );
            }
        }
    }
}

#[test]
fn shared_value_wire() {
    let fixtures: Vec<Fixture> =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/values.json")).unwrap();
    for fixture in fixtures {
        let wire = unhex(&fixture.hex);
        let decoded = decode_values(&wire, &Limits::default());
        if fixture.invalid {
            assert!(decoded.is_err(), "{}", fixture.name);
            continue;
        }
        let decoded = decoded.unwrap_or_else(|error| panic!("{}: {error}", fixture.name));
        let values: Vec<_> = fixture
            .values
            .into_iter()
            .map(|input| {
                Value::new(
                    input.typ,
                    match input.kind.as_str() {
                        "nil" => Data::Nil,
                        "bool" => Data::Bool(input.value.parse().unwrap()),
                        "int" => Data::Int(input.value.parse().unwrap()),
                        "uint" => Data::Uint(input.value.parse().unwrap()),
                        "float-bits" => Data::Float(f64::from_bits(
                            u64::from_str_radix(&input.value, 16).unwrap(),
                        )),
                        "bytes" => Data::Bytes(unhex(&input.value)),
                        "string" => Data::String(input.value),
                        kind => panic!("unknown fixture kind {kind}"),
                    },
                )
            })
            .collect();
        for input in [values, decoded] {
            assert_eq!(
                encode_values(&input, &Limits::default()).unwrap(),
                wire,
                "{}",
                fixture.name
            );
        }
    }
}

#[test]
fn aggregate_limits_and_nested_shapes() {
    let values = vec![Value::new(
        "[]int64",
        Data::Slice(vec![
            Value::new("int64", Data::Int(1)),
            Value::new("int64", Data::Int(2)),
        ]),
    )];
    let payload = encode_values(&values, &Limits::default()).unwrap();
    let limits = Limits {
        max_value_elements: 2,
        ..Limits::default()
    };
    assert!(decode_values(&payload, &limits).is_err());
    assert!(encode_values(&values, &limits).is_err());
    assert_eq!(decode_values(&payload, &Limits::default()).unwrap(), values);
    let invalid = vec![Value::new(
        "[]string",
        Data::Slice(vec![Value::new("bool", Data::Bool(true))]),
    )];
    assert!(encode_values(&invalid, &Limits::default()).is_err());
}

#[test]
fn shared_envelope_wire() {
    use mini_go::rpc::protocol::*;
    #[derive(Deserialize)]
    struct Envelope {
        name: String,
        kind: String,
        hex: String,
        #[serde(default)]
        invalid: bool,
    }
    let fixtures: Vec<Envelope> =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/envelopes.json")).unwrap();
    for fixture in fixtures {
        let bytes = unhex(&fixture.hex);
        let limits = Limits::default();
        let encoded = match fixture.kind.as_str() {
            "ffi-request" => FfiRequest::decode(&bytes, &limits).map(|value| {
                assert_eq!(value.operation, "close");
                assert_eq!(value.lease, 7);
                value.encode()
            }),
            "ffi-response" => FfiResponse::decode(&bytes, &limits).map(|value| {
                assert_eq!(value.lease, 7);
                value.encode()
            }),
            "endpoint" => Frame::decode(&bytes, &limits).and_then(|value| {
                assert_eq!(value.origin, "p");
                assert_eq!(value.kind, READY);
                value.encode(limits.max_message_bytes)
            }),
            "fragment" => Assembly::default().push(&bytes, &limits).and_then(|value| {
                assert_eq!(value, Some(b"abc".to_vec()));
                encode_fragment(1, 3, 0, b"abc", 256)
            }),
            kind => panic!("unknown envelope {kind}"),
        };
        if fixture.invalid {
            assert!(encoded.is_err(), "{}", fixture.name);
        } else {
            assert_eq!(
                encoded.unwrap_or_else(|error| panic!("{}: {error}", fixture.name)),
                bytes,
                "{}",
                fixture.name
            );
        }
    }
}

#[test]
fn shared_control_frames_and_fragment_sequences() {
    use mini_go::rpc::protocol::*;
    #[derive(Deserialize)]
    struct FrameCase {
        name: String,
        kind: String,
        hex: String,
        id: u64,
        target: u64,
        binding: u64,
        reply: bool,
        #[serde(default)]
        code: String,
    }
    let cases: Vec<FrameCase> =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/frames.json")).unwrap();
    let names = [
        "",
        "hello",
        "ready",
        "bind",
        "call",
        "decision",
        "drop",
        "close",
        "cancel",
        "",
        "renew",
        "renew_ack",
        "accepted",
        "offer",
        "done",
    ];
    for case in cases {
        let bytes = unhex(&case.hex);
        let frame = Frame::decode(&bytes, &Limits::default())
            .unwrap_or_else(|error| panic!("{}: {error}", case.name));
        assert_eq!(names[frame.kind as usize], case.kind, "{}", case.name);
        assert_eq!(
            (
                frame.id,
                frame.target_id,
                frame.binding,
                frame.reply,
                frame.code.as_str()
            ),
            (
                case.id,
                case.target,
                case.binding,
                case.reply,
                case.code.as_str()
            ),
            "{}",
            case.name
        );
        assert_eq!(
            frame.encode(Limits::default().max_message_bytes).unwrap(),
            bytes,
            "{}",
            case.name
        );
    }
    #[derive(Deserialize)]
    struct Fragments {
        name: String,
        fragments: Vec<String>,
        #[serde(default)]
        messages: Vec<String>,
        error_at: Option<usize>,
    }
    let cases: Vec<Fragments> =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/fragments.json")).unwrap();
    for case in cases {
        let mut assembly = Assembly::default();
        for (index, frame) in case.fragments.iter().enumerate() {
            let result = assembly.push(&unhex(frame), &Limits::default());
            if case.error_at == Some(index) {
                assert!(result.is_err(), "{}", case.name);
                break;
            }
            let expected = case
                .messages
                .get(index)
                .and_then(|message| (!message.is_empty()).then(|| unhex(message)));
            assert_eq!(result.unwrap(), expected, "{}", case.name);
        }
    }
}

#[test]
fn control_fragments_can_interleave_with_data() {
    use mini_go::rpc::protocol::*;
    let limits = Limits::default();
    let mut assembly = Assembly::default();
    assert_eq!(
        assembly
            .push(&encode_fragment(1, 6, 0, b"abc", 256).unwrap(), &limits)
            .unwrap(),
        None
    );
    assert_eq!(
        assembly
            .push(&encode_control_fragment(b"OK", 256).unwrap(), &limits)
            .unwrap(),
        Some(b"OK".to_vec())
    );
    assert_eq!(
        assembly
            .push(&encode_fragment(1, 6, 3, b"def", 256).unwrap(), &limits)
            .unwrap(),
        Some(b"abcdef".to_vec())
    );
}

#[test]
fn lease_targets_round_trip_and_bound_count() {
    use mini_go::rpc::protocol::*;
    #[derive(Deserialize)]
    struct LeaseCase {
        name: String,
        targets: String,
        #[serde(default)]
        bindings: Vec<u64>,
        #[serde(default)]
        operations: Vec<u64>,
        #[serde(default)]
        invalid: bool,
    }
    let cases: Vec<LeaseCase> =
        serde_json::from_str(include_str!("../../../testdata/rpc/wire/leases.json")).unwrap();
    for case in cases {
        let result = decode_lease_targets(&unhex(&case.targets), &Limits::default());
        if case.invalid {
            assert!(result.is_err(), "{}", case.name);
        } else {
            assert_eq!(
                result.unwrap(),
                (case.bindings, case.operations),
                "{}",
                case.name
            );
        }
    }
    let limits = Limits::default();
    let payload = encode_lease_targets(&[3, 5], &[9]);
    assert_eq!(
        decode_lease_targets(&payload, &limits).unwrap(),
        (vec![3, 5], vec![9])
    );
    let invalid = encode_lease_targets(&[0], &[]);
    assert!(decode_lease_targets(&invalid, &limits).is_err());
    assert!(
        decode_lease_targets(
            &payload,
            &Limits {
                max_pending_controls: 1,
                ..limits
            }
        )
        .is_err()
    );
}
