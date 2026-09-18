//! Immutable source sidecars bound to exact executable and package hashes.

use crate::{
    contract::canonical_hash, contract_generated as wire, error::RuntimeError, program::Program,
};
use std::collections::HashSet;

pub fn validate(program: &Program, symbols: &wire::ProgramSymbols) -> Result<(), RuntimeError> {
    let invalid = |message| RuntimeError::new("invalid_symbols", "symbols", message);
    let manifest: serde_json::Value = serde_json::from_str(wire::CONTRACT_JSON)?;
    let spec = &manifest["spec"];
    if symbols.format != spec["symbols_format"].as_str().unwrap()
        || symbols.version != spec["symbols_version"].as_i64().unwrap()
        || symbols.contract_id != spec["symbols_contract"].as_str().unwrap()
        || symbols.compiler_id != program.image().compiler_id
        || symbols.program_hash != program.image().hash
    {
        return Err(invalid("sidecar identity mismatch"));
    }
    let mut unhashed = symbols.clone();
    unhashed.hash.clear();
    if canonical_hash(&unhashed)? != symbols.hash {
        return Err(invalid("sidecar hash mismatch"));
    }
    let packages = symbols
        .packages
        .as_ref()
        .ok_or_else(|| invalid("missing package symbols"))?;
    if packages.len() != program.decoded.artifacts().len() {
        return Err(invalid("package closure mismatch"));
    }
    for (module, artifact) in program.decoded.artifacts() {
        let package = packages
            .get(module)
            .ok_or_else(|| invalid("missing package symbols"))?;
        let archive = &program.image().packages.as_ref().unwrap()[module];
        if package.module_path != *module
            || package.code_hash != archive.artifact_hash
            || !valid_hash(&package.code_hash)
            || !package.source_hash.is_empty() && !valid_hash(&package.source_hash)
        {
            return Err(invalid("package code or source identity mismatch"));
        }
        let mut files = HashSet::new();
        for file in package.files.iter() {
            let id = file.id.trim();
            let path = file.path.trim();
            if id.is_empty()
                || path.is_empty()
                || !file.hash.is_empty() && !valid_hash(&file.hash)
                || files.contains(id)
                || files.contains(path)
            {
                return Err(invalid("invalid source file identity"));
            }
            files.insert(id);
            files.insert(path);
        }
        let mut globals: HashSet<_> = artifact
            .globals
            .iter()
            .map(|global| global.id.as_str())
            .collect();
        if package.globals.len() != globals.len() {
            return Err(invalid("global symbol count mismatch"));
        }
        for global in package.globals.iter() {
            if !globals.remove(global.id.as_str()) || global.name.trim().is_empty() {
                return Err(invalid("invalid global symbol"));
            }
        }
        let mut functions: HashSet<_> = artifact
            .functions
            .iter()
            .map(|function| function.id.as_str())
            .collect();
        if package.functions.len() != functions.len() {
            return Err(invalid("function symbol count mismatch"));
        }
        for function in package.functions.iter() {
            if !functions.remove(function.id.as_str()) || function.name.trim().is_empty() {
                return Err(invalid("invalid function symbol"));
            }
            let code = program.function(module, &function.id)?;
            if let Some(location) = &function.declaration {
                validate_location(location, &files)?;
            }
            let mut scopes = HashSet::new();
            for scope in function.scopes.iter() {
                if scope.id <= 0
                    || scopes.contains(&scope.id)
                    || scope.parent != 0 && !scopes.contains(&scope.parent)
                {
                    return Err(invalid("invalid lexical scope"));
                }
                let mut previous = 0;
                for range in scope.ranges.iter() {
                    if range.start < previous
                        || range.start < 0
                        || range.start >= range.end
                        || range.end > code.code.len() as i64
                    {
                        return Err(invalid("invalid lexical scope range"));
                    }
                    previous = range.end;
                }
                scopes.insert(scope.id);
            }
            let mut locals: HashSet<_> = code.locals.keys().map(String::as_str).collect();
            if function.locals.len() != locals.len() {
                return Err(invalid("local symbol count mismatch"));
            }
            for local in function.locals.iter() {
                if !locals.remove(local.id.as_str())
                    || local.scope != 0 && !scopes.contains(&local.scope)
                {
                    return Err(invalid("invalid local symbol"));
                }
                if let Some(location) = &local.declaration {
                    validate_location(location, &files)?;
                }
            }
            let mut upvalues: HashSet<_> = code.upvalues.keys().map(String::as_str).collect();
            if function.upvalues.len() != upvalues.len() {
                return Err(invalid("upvalue symbol count mismatch"));
            }
            for upvalue in function.upvalues.iter() {
                if !upvalues.remove(upvalue.id.as_str()) {
                    return Err(invalid("invalid upvalue symbol"));
                }
            }
            let mut previous = -1;
            for location in function.locations.iter() {
                if location.pc <= previous
                    || location.pc < 0
                    || location.pc >= code.code.len() as i64
                    || location.points.is_empty()
                {
                    return Err(invalid("invalid instruction symbol"));
                }
                let mut points = HashSet::new();
                for point in location.points.iter() {
                    validate_location(point, &files)?;
                    if !points.insert((&point.file, point.line, point.column)) {
                        return Err(invalid("duplicate instruction source point"));
                    }
                }
                previous = location.pc;
            }
        }
    }
    Ok(())
}

fn valid_hash(value: &str) -> bool {
    value.len() == 64 && value.bytes().all(|byte| byte.is_ascii_hexdigit())
}

fn validate_location(location: &wire::Location, files: &HashSet<&str>) -> Result<(), RuntimeError> {
    if !files.contains(location.file.trim()) || location.line <= 0 || location.column < 0 {
        return Err(RuntimeError::new(
            "invalid_symbols",
            "location",
            "invalid source position",
        ));
    }
    Ok(())
}
