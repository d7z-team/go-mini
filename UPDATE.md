# ffilib Migration Update

## Resume Rule

Every resumed or compacted context must read this file before continuing work.
Update this file after each completed migration block.

## Goal

Move `core/ffilib` to top-level `ffilib` as a separate standard-library FFI package.
`core` must become only the core engine/compiler/runtime surface and must not use
or depend on any `ffilib` functionality. No compatibility shims are kept.

## Final Boundaries

- `core` contains engine, parser/frontend, lowering, compiler, bytecode, runtime,
  debugger, LSP server core, FFI wire helpers in `core/ffigo`, and registration
  surfaces.
- `core` must not import or reference `ffilib`.
- `core` must not expose `InjectStandardLibraries`.
- `core.NewMiniExecutor` registers only language builtins.
- `ffilib` owns all standard-library FFI packages and exposes only one top-level
  composition entry: `ffilib.RegisterAll(executor)`.
- Non-`ffilib` tests must not depend on `ffilib`, directly or through helpers.
- `core/e2e` must contain only core language/runtime/module/FFI mechanism tests.
  Tests of standard library behavior must live under `ffilib`.
- Product assembly code may call `ffilib.RegisterAll`, but tests outside `ffilib`
  should use core-only mocks or language state assertions.

## Migration Steps

1. Move `core/ffilib` to top-level `ffilib`.
2. Rewrite import paths from `gopkg.d7z.net/go-mini/core/ffilib...` to
   `gopkg.d7z.net/go-mini/ffilib...`.
3. Add `ffilib.RegisterAll` as the single top-level standard-library registration
   entry, moving all standard library registration out of `core`.
4. Remove all `ffilib` imports and default standard library registration from
   `core.NewMiniExecutor`.
5. Delete `core.InjectStandardLibraries`.
6. Update product assembly (`cmd/exec`, `cmd/lsp-server`) to call
   `ffilib.RegisterAll`.
7. Move standard-library behavior tests out of `core` and into `ffilib`.
8. Rewrite remaining core tests to use shared-state assertions, script modules,
   or local mock FFI/templates instead of `ffilib`.
9. Delete obsolete tests/helpers that only verified the old mixed layout.
10. Update docs/checklists (`TODO.md`, `AGENTS.md`, `DOCS.md` as needed).
11. Verify:
    - `rg 'ffilib|InjectStandardLibraries' core --glob '*.go'` has no production
      dependency hits; the target is no hits in `core` after test migration too.
    - `go list -deps ./core | rg '/ffilib'` has no output.
    - `go test ./core/...`
    - `go test ./ffilib/...`
    - `go test ./cmd/...`
    - final broad `go test ./...`.

## Progress

- [x] Migration plan recorded.
- [x] Directory moved to top-level `ffilib`.
- [x] `ffilib.RegisterAll` added.
- [x] `core` production code no longer references `ffilib`.
- [x] `core.InjectStandardLibraries` removed.
- [x] Product assembly updated.
- [x] Non-`ffilib` tests no longer depend on `ffilib`.
- [x] `core/e2e` split by real ownership.
- [x] Obsolete tests/helpers removed.
- [x] Docs updated.
- [x] Verification complete.
- [x] Final rescan cleanup complete.

## Verification Log

- `go test ./core/...` passed.
- `go test ./ffilib/...` passed.
- `go test ./cmd/...` passed.
- `go test ./...` passed.
- `make test` passed.
- `make examples` passed.
- `go list -deps ./core | rg '/ffilib'` produced no output.
- `rg -n 'core/ffilib|InjectStandardLibraries|gopkg\.d7z\.net/go-mini/ffilib' core --glob '*.go'` produced no output.

## Final Rescan Notes

- A final full scan found no remaining `core/ffilib`, `InjectStandardLibraries`,
  or `core` import/dependency path to `ffilib`.
- Product assembly references to `ffilib.RegisterAll` remain only in
  `cmd/exec` and `cmd/lsp-server`.
- The ffigen migration regression test now points at `ffilib/iolib/io_ffigen.go`.
- Remaining standard-library-looking names in `core` tests are Go test imports,
  AST/LSP syntax samples, schema/compiler samples, or local mock FFI mechanism
  tests; they do not depend on the migrated `ffilib` package.
- Cleaned migration leftovers: removed empty `ffilib/common.go` and empty
  `core/debugger/tests`.
- Fixed `core/tests/canonical_type_construction_test.go` to tolerate transient
  files disappearing during parallel package tests while `core/ffigen` creates
  and removes temporary directories.
- Fixed final lint fallout from the split: `core/benchmark` now uses a local
  `bench` mock FFI without `ffilib`, and exported `ffilib` mirror constants keep
  their standard-library names with local lint explanations.

## Final Rescan Verification

- `make lint` passed.
- `make test` passed after final lint cleanup.
- `make examples` passed after final lint cleanup.
- `go test ./core/...` passed.
- `go test ./ffilib/...` passed.
- `go test ./cmd/...` passed.
- `go test ./...` passed after final lint cleanup.
- `rg -n 'core/ffilib|InjectStandardLibraries' . --glob '!UPDATE.md'`
  produced no output.
- `rg -n 'gopkg\.d7z\.net/go-mini/ffilib' core --glob '*.go'` produced no
  output.
- `go list -deps ./core | rg '/ffilib'` produced no output.
- `find core -type d -empty -print` produced no output.
