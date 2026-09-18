use mini_go::contract_generated;
use serde::Deserialize;
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, path::Path};

#[derive(Deserialize)]
struct Manifest {
    version: u64,
    files: BTreeMap<String, Entry>,
    #[serde(default)]
    inputs: BTreeMap<String, String>,
}
#[derive(Deserialize)]
struct Entry {
    sha256: String,
    kind: String,
    source: String,
    oracle: String,
    compiler_id: Option<String>,
}

fn read_tree(root: &Path, directory: &Path, files: &mut BTreeMap<String, Vec<u8>>) {
    for entry in std::fs::read_dir(directory).unwrap() {
        let path = entry.unwrap().path();
        if path.is_dir() {
            read_tree(root, &path, files);
            continue;
        }
        let name = path
            .strip_prefix(root)
            .unwrap()
            .to_str()
            .unwrap()
            .replace('\\', "/");
        if name == "manifest.json" || name.ends_with(".md") {
            continue;
        }
        files.insert(name, std::fs::read(path).unwrap());
    }
}

#[test]
fn distributed_inputs_match_complete_manifests_and_current_compiler() {
    let contract: serde_json::Value =
        serde_json::from_str(contract_generated::CONTRACT_JSON).unwrap();
    for corpus in ["rpc", "runtime", "stdlib-host"] {
        let root = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../../testdata")
            .join(corpus);
        let manifest: Manifest =
            serde_json::from_slice(&std::fs::read(root.join("manifest.json")).unwrap()).unwrap();
        assert_eq!(manifest.version, 2);
        for (name, hash) in manifest.inputs {
            assert!(
                Path::new(&name)
                    .components()
                    .all(|part| matches!(part, std::path::Component::Normal(_))),
                "invalid source path {name}"
            );
            let source = Path::new(env!("CARGO_MANIFEST_DIR"))
                .join("../..")
                .join(&name);
            assert_eq!(
                format!("{:x}", Sha256::digest(std::fs::read(source).unwrap())),
                hash,
                "source changed: {name}"
            );
        }
        let mut files = BTreeMap::new();
        read_tree(&root, &root, &mut files);
        for (name, entry) in manifest.files {
            assert!(
                !entry.kind.is_empty() && !entry.source.is_empty() && !entry.oracle.is_empty(),
                "{corpus}/{name}: incomplete provenance"
            );
            let bytes = files
                .remove(&name)
                .unwrap_or_else(|| panic!("{corpus}/{name}: missing input"));
            assert_eq!(
                format!("{:x}", Sha256::digest(bytes)),
                entry.sha256,
                "{corpus}/{name}: stale content"
            );
            if let Some(identity) = entry.compiler_id {
                assert_eq!(
                    identity,
                    contract["compiler_id"].as_str().unwrap(),
                    "{corpus}/{name}: stale image"
                );
            }
        }
        assert!(
            files.is_empty(),
            "{corpus}: unlisted inputs {:?}",
            files.keys()
        );
    }
}
