use mini_go::{
    contract::canonical_hash, contract_generated as wire, loader::LoadLimits, program::Program,
    symbols,
};
use serde::Deserialize;

#[path = "support/execution_vectors.rs"]
mod execution_vectors;

#[derive(Deserialize)]
struct Vector {
    name: String,
    image: Box<serde_json::value::RawValue>,
    symbols: wire::ProgramSymbols,
}

#[test]
fn compiler_sidecars_bind_exact_code_and_preserve_executable_identity() {
    let vectors: Vec<Vector> = execution_vectors::load();
    for vector in vectors
        .into_iter()
        .filter(|vector| vector.name == "arithmetic" || vector.name == "imported_type")
    {
        let program = Program::load(vector.image.get().as_bytes(), LoadLimits::default()).unwrap();
        let identity = program.image().hash.clone();
        let program = program.with_symbols(vector.symbols).unwrap();
        assert_eq!(program.image().hash, identity);
        let valid = program.symbols().unwrap();
        let mut broken = valid.clone();
        broken.program_hash = "0".repeat(64);
        assert_eq!(
            symbols::validate(&program, &broken).unwrap_err().code,
            "invalid_symbols"
        );
        for mutation in 0..4 {
            let mut broken = valid.clone();
            let package = broken
                .packages
                .as_mut()
                .unwrap()
                .get_mut("runtime/oracle")
                .unwrap();
            let function = package
                .functions
                .iter_mut()
                .find(|function| function.id == "fn.Main")
                .unwrap();
            match mutation {
                0 => function.id = "missing".to_owned(),
                1 => function.locations[0].pc = i64::MAX,
                2 => function.locations[0].points[0].file = "missing.mgo".to_owned(),
                3 => package.code_hash = "f".repeat(64),
                _ => unreachable!(),
            }
            broken.hash.clear();
            broken.hash = canonical_hash(&broken).unwrap();
            assert_eq!(
                symbols::validate(&program, &broken).unwrap_err().code,
                "invalid_symbols"
            );
        }
        symbols::validate(&program, valid).unwrap();
    }
}
