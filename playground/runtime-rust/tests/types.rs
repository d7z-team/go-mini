use mini_go::{
    contract_generated as wire,
    types::{ConstructedType, TypeIdentity, TypeRegistry},
};

#[test]
fn dynamic_type_construction_is_structural_bounded_and_transactional() {
    let artifact: wire::Artifact = serde_json::from_str(r#"{
        "module":{"path":"example"}, "type_table":{"nodes":[{"id":"array","kind":6,"length":2,"elem":{"kind":3,"primitive":3}}]}
    }"#).unwrap();
    let mut registry = TypeRegistry::new([&artifact]).unwrap();
    let shape = || ConstructedType::Array {
        length: 2,
        element: TypeIdentity::Primitive(wire::PrimitiveInt),
    };
    let array = registry.construct(shape(), 1, 16).unwrap();
    let compiled = registry
        .resolve(
            "example",
            &wire::TypeRef {
                kind: wire::Array,
                node: "array".to_owned(),
                ..Default::default()
            },
        )
        .unwrap();
    assert!(registry.identical(&array, &compiled).unwrap());
    assert_eq!(registry.construct(shape(), 1, 16).unwrap(), array);
    let invalid = ConstructedType::Function {
        params: vec![TypeIdentity::Primitive(wire::PrimitiveInt)],
        results: vec![],
        variadic: true,
    };
    assert_eq!(
        registry.construct(invalid, 10, 16).unwrap_err().code,
        "invalid_type"
    );
    assert_eq!(registry.construct(shape(), 1, 16).unwrap(), array);
    let excessive = ConstructedType::Array {
        length: 3,
        element: TypeIdentity::Primitive(wire::PrimitiveInt),
    };
    assert_eq!(
        registry.construct(excessive, 1, 16).unwrap_err().code,
        "type_limit"
    );
    assert!(registry.identical(&array, &compiled).unwrap());
}

#[test]
fn malformed_type_references_do_not_resolve_by_node_text_alone() {
    let artifact: wire::Artifact = serde_json::from_str(
        r#"{
        "module":{"path":"example"},
        "type_table":{"nodes":[{"id":"slice","kind":5,"elem":{"kind":3,"primitive":3}}]}
    }"#,
    )
    .unwrap();
    let registry = TypeRegistry::new([&artifact]).unwrap();
    let reference = wire::TypeRef {
        kind: wire::Slice,
        node: "slice".to_owned(),
        ..Default::default()
    };
    let resolved = registry.resolve("example", &reference).unwrap();
    let (module, node) = registry.node(&resolved).unwrap().unwrap();
    assert_eq!(module, "example");
    assert_eq!(node.kind, wire::Slice);
    assert_eq!(
        registry.resolve(module, &node.elem).unwrap(),
        TypeIdentity::Primitive(wire::PrimitiveInt)
    );
    assert!(registry.resolve("other", &reference).is_err());
    let wrong = wire::TypeRef {
        kind: wire::Map,
        ..reference.clone()
    };
    assert!(registry.resolve("example", &wrong).is_err());
    let wrong = wire::TypeRef {
        primitive: wire::PrimitiveInt,
        ..reference
    };
    assert!(registry.resolve("example", &wrong).is_err());
}

#[test]
fn named_underlying_cycles_are_reported_without_recursive_stack_growth() {
    let artifact: wire::Artifact = serde_json::from_str(r#"{
        "module":{"path":"example"},
        "type_table":{"nodes":[
          {"id":"a","kind":4,"identity":{"module_path":"example","decl_id":"A"},"underlying":{"kind":4,"named":{"module_path":"example","decl_id":"B"}}},
          {"id":"b","kind":4,"identity":{"module_path":"example","decl_id":"B"},"underlying":{"kind":4,"named":{"module_path":"example","decl_id":"A"}}}
        ]}
    }"#).unwrap();
    assert_eq!(
        TypeRegistry::new([&artifact]).err().unwrap().code,
        "type_cycle"
    );
}
