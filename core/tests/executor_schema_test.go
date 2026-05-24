package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

type packageValueProviderFunc func(runtime.FFIBindContext) (*runtime.Var, error)

func (f packageValueProviderFunc) Bind(ctx runtime.FFIBindContext) (*runtime.Var, error) {
	return f(ctx)
}

func TestMiniExecutorExportsParsedSchema(t *testing.T) {
	exec := engine.NewMiniExecutor()
	exec.RegisterFFISchema("demo.Call", nil, 1, runtime.MustParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)"), "demo route")
	exec.RegisterStructSchema("demo.Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; Count Int64; }"))
	exec.RegisterInterfaceSchema("demo.Reader", runtime.MustParseRuntimeInterfaceSpec("interface{Read(TypeBytes) tuple(Int64, Error);}"))

	schema := exec.ExportedSchema()
	if schema == nil {
		t.Fatal("expected schema snapshot")
	}

	funcSig := schema.Funcs["demo.Call"]
	if funcSig == nil {
		t.Fatal("expected parsed function schema")
	}
	if !funcSig.Variadic {
		t.Fatal("expected variadic function schema")
	}
	if got := string(funcSig.ReturnType.Raw); got != "tuple(Void, String)" {
		t.Fatalf("unexpected return type: %s", got)
	}

	structSpec := schema.Structs["demo.Payload"]
	if structSpec == nil {
		t.Fatal("expected parsed struct schema")
	}
	if len(structSpec.Fields) != 2 {
		t.Fatalf("unexpected struct field count: %d", len(structSpec.Fields))
	}
	if structSpec.Fields[0].Name != "Msg" || structSpec.Fields[1].Name != "Count" {
		t.Fatalf("unexpected struct field order: %+v", structSpec.Fields)
	}

	if got := schema.Funcs["demo.Call"].Spec; got != "function(String, ...Any) tuple(Void, String)" {
		t.Fatalf("unexpected exported function spec: %s", got)
	}
	if got := schema.Structs["demo.Payload"].Spec; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct spec: %s", got)
	}
	if got := schema.Interfaces["demo.Reader"].Spec; got != "interface{Read(TypeBytes) tuple(Int64, Error);}" {
		t.Fatalf("unexpected exported interface spec: %s", got)
	}
}

func TestMiniExecutorRuntimeExecutorReturnsErrorAPI(t *testing.T) {
	exec := engine.NewMiniExecutor()
	runtimeExec, err := exec.RuntimeExecutor()
	if err != nil {
		t.Fatalf("runtime executor failed: %v", err)
	}
	if runtimeExec == nil {
		t.Fatal("expected runtime executor")
	}
}

func TestExportMetadataIncludesRegisteredFFISignatures(t *testing.T) {
	exec := engine.NewMiniExecutor()
	exec.RegisterFFISchema("demo.Call", nil, 1, runtime.MustParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)"), "demo route")

	meta := exec.ExportMetadata()
	if !strings.Contains(meta, `"Call": "function(String, ...Any) tuple(Void, String) // demo route"`) {
		t.Fatalf("expected exported metadata to include parsed route signature, got:\n%s", meta)
	}
}

func TestCompiledBytecodeJSONRoundTripRemainsExecutable(t *testing.T) {
	exec := engine.NewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package main

var counter = 1

func inc(v int) int {
	return v + counter
}

func main() {
	counter = inc(1)
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		t.Fatal("expected executable bytecode")
	}

	payload, err := compiled.MarshalBytecodeJSON()
	if err != nil {
		t.Fatalf("marshal bytecode failed: %v", err)
	}
	decoded, err := bytecode.UnmarshalJSON(payload)
	if err != nil {
		t.Fatalf("unmarshal bytecode failed: %v", err)
	}

	compiled.Bytecode = decoded
	compiled.Program.Variables["counter"] = nil
	compiled.Program.Functions["inc"].Body = nil
	compiled.Program.Main = nil

	prog, err := exec.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("new runtime by compiled failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	shared := prog.SharedState()
	if shared == nil {
		t.Fatal("expected shared state")
	}
	counter, ok := shared.LoadGlobal("counter")
	if !ok {
		t.Fatal("load counter failed: missing global")
	}
	if counter.I64 != 2 {
		t.Fatalf("unexpected counter value after bytecode roundtrip: %#v", counter)
	}
}

func TestPreparedProgramJSONRoundTripExecutes(t *testing.T) {
	exec := engine.NewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package main
var counter = 1
func main() { counter = counter + 41 }
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		t.Fatal("expected executable bytecode")
	}
	payload, err := json.Marshal(compiled.Bytecode.Executable)
	if err != nil {
		t.Fatalf("marshal prepared program failed: %v", err)
	}
	var prepared runtime.PreparedProgram
	if err := json.Unmarshal(payload, &prepared); err != nil {
		t.Fatalf("unmarshal prepared program failed: %v", err)
	}
	runtimeExec, err := runtime.NewExecutorFromPrepared(&prepared)
	if err != nil {
		t.Fatalf("new executor from prepared failed: %v", err)
	}
	if err := runtimeExec.Execute(context.Background()); err != nil {
		t.Fatalf("execute prepared roundtrip failed: %v", err)
	}
	counter, ok := runtimeExec.SharedStateSnapshot().LoadGlobal("counter")
	if !ok || counter == nil || counter.I64 != 42 {
		t.Fatalf("unexpected counter global: %#v", counter)
	}
}

func TestNewRuntimeByBytecodeJSONUsesExecutableMetadataOnly(t *testing.T) {
	exec := engine.NewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package main

type Payload struct {
	Msg string
}

type Reader interface {
	Read() string
}

const Version = "v1"

var counter = 1

func main() {
	counter = counter + 1
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if compiled.Bytecode == nil || compiled.Bytecode.Executable == nil {
		t.Fatal("expected executable bytecode")
	}
	executable := compiled.Bytecode.Executable
	if executable.Constants["Version"] != "v1" {
		t.Fatalf("unexpected executable constants: %#v", executable.Constants)
	}
	if executable.StructSchemas["Payload"] == nil {
		t.Fatalf("expected executable struct schema: %#v", executable.StructSchemas)
	}
	if executable.InterfaceSchemas["Reader"] == nil {
		t.Fatalf("expected executable interface schema: %#v", executable.InterfaceSchemas)
	}
	payload, err := compiled.MarshalBytecodeJSON()
	if err != nil {
		t.Fatalf("marshal bytecode failed: %v", err)
	}

	prog, err := exec.NewRuntimeByBytecodeJSON(payload)
	if err != nil {
		t.Fatalf("load by bytecode json failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if prog.Compilation().Program != nil {
		t.Fatal("bytecode-loaded executable should not retain analysis AST")
	}
	counter, ok := prog.SharedState().LoadGlobal("counter")
	if !ok || counter == nil || counter.I64 != 2 {
		t.Fatalf("unexpected counter global: %#v", counter)
	}
}

func TestExecutableProgramMarshalJSONDefaultsToBytecode(t *testing.T) {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main
func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	payload, err := prog.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}
	if !strings.Contains(string(payload), `"format":"go-mini-bytecode"`) {
		t.Fatalf("expected bytecode json, got: %s", payload)
	}
}

func TestNewRuntimeByBytecodeJSONLoadsBytecode(t *testing.T) {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main
var counter = 1
func main() { counter = counter + 1 }
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	payload, err := prog.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal json failed: %v", err)
	}

	loaded, err := exec.NewRuntimeByBytecodeJSON(payload)
	if err != nil {
		t.Fatalf("load bytecode json failed: %v", err)
	}
	if err := loaded.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestMiniExecutorRejectsConflictingFFIRouteRegistration(t *testing.T) {
	exec := engine.NewMiniExecutor()
	exec.RegisterFFISchema("demo.Call", nil, 1, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "ffi route conflict") {
			t.Fatalf("expected ffi route conflict panic, got %v", r)
		}
	}()

	exec.RegisterFFISchema("demo.Call", nil, 2, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")
}

func TestMiniExecutorTryRegisterReportsSchemaConflict(t *testing.T) {
	exec := engine.NewMiniExecutor()
	if err := exec.TryRegisterFFISchema("demo.Mutate", nil, 1,
		runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", runtime.FFIParamInOutBytes), ""); err != nil {
		t.Fatalf("register schema failed: %v", err)
	}

	err := exec.TryRegisterFFISchema("demo.Mutate", nil, 1,
		runtime.MustParseRuntimeFuncSigWithModes("function(TypeBytes) Void", runtime.FFIParamIn), "")
	var conflict *runtime.SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "route" {
		t.Fatalf("expected route schema conflict error, got %T %v", err, err)
	}
}

func TestTryRegisterFFISchemaConflictDoesNotPolluteRoutes(t *testing.T) {
	exec := engine.NewMiniExecutor()
	sigA := runtime.MustParseRuntimeFuncSig("function(String) Void")
	sigB := runtime.MustParseRuntimeFuncSig("function(Int64) Void")
	exec.DeclareFuncSchema("demo.Call", sigA)

	err := exec.TryRegisterFFISchema("demo.Call", nil, 7, sigB, "")
	var conflict *runtime.SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "schema" {
		t.Fatalf("expected schema conflict error, got %T %v", err, err)
	}

	schema := exec.ExportedSchema()
	if schema.RegisteredFuncs["demo.Call"] {
		t.Fatalf("failed registration polluted registered route state: %+v", schema.RegisteredFuncs)
	}
	if got := schema.Funcs["demo.Call"]; got == nil || !runtime.SameRuntimeFuncSchema(got, sigA) {
		t.Fatalf("declared schema should remain unchanged, got %#v", got)
	}

	if err := exec.TryRegisterFFISchema("demo.Call", nil, 1, sigA, ""); err != nil {
		t.Fatalf("valid registration after failed conflict should succeed: %v", err)
	}
}

func TestUseSurfaceConflictAfterBindRollsBackPinnedHandles(t *testing.T) {
	exec := engine.NewMiniExecutor()
	stringSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("String"), ReadOnly: true}
	intSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("Int64"), ReadOnly: true}
	if err := exec.TryRegisterPackageValue("demo.Value", stringSpec, packageValueProviderFunc(func(runtime.FFIBindContext) (*runtime.Var, error) {
		return runtime.NewString("old"), nil
	})); err != nil {
		t.Fatalf("register existing package value failed: %v", err)
	}

	schema := runtime.NewFFISurfaceSchema()
	schema.AddValue("demo", "Value", intSpec)
	var handle uint32
	err := exec.UseSurface(surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		if ctx.PinnedRegistry == nil {
			return nil, errors.New("missing pinned registry")
		}
		handle = ctx.PinnedRegistry.RegisterPinnedTyped(&struct{}{}, "demo.Handle")
		bound := runtime.NewBoundFFISurface(schema)
		bound.AddPackageValue("demo", "Value", intSpec, runtime.NewInt(1))
		return bound, nil
	}))
	var conflict *runtime.SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "package value" {
		t.Fatalf("expected package value conflict, got %T %v", err, err)
	}
	if handle == 0 {
		t.Fatal("expected bind to allocate a pinned handle")
	}
	if _, ok := exec.HandleRegistry().Get(handle); ok {
		t.Fatalf("failed UseSurface polluted pinned handle %d", handle)
	}
	if got := exec.ExportedSchema().Values["demo.Value"]; got == nil || got.Type.Raw != "String" {
		t.Fatalf("existing package value schema should remain unchanged, got %#v", got)
	}
}

func TestUseSurfaceBindErrorRollsBackPinnedHandles(t *testing.T) {
	exec := engine.NewMiniExecutor()
	var handle uint32
	var directHandle uint32
	err := exec.UseSurface(surface.New(runtime.NewFFISurfaceSchema(), func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		if ctx.PinnedRegistry == nil {
			return nil, errors.New("missing pinned registry")
		}
		handle = ctx.PinnedRegistry.RegisterPinnedTyped(&struct{}{}, "demo.Handle")
		directHandle = ctx.Registry.RegisterTyped(&struct{}{}, "demo.Direct")
		return nil, errors.New("bind failed")
	}))
	if err == nil || !strings.Contains(err.Error(), "bind failed") {
		t.Fatalf("expected bind error, got %v", err)
	}
	if handle == 0 {
		t.Fatal("expected bind to allocate a pinned handle")
	}
	if _, ok := exec.HandleRegistry().Get(handle); ok {
		t.Fatalf("failed UseSurface bind error polluted pinned handle %d", handle)
	}
	if _, ok := exec.HandleRegistry().Get(directHandle); ok {
		t.Fatalf("failed UseSurface bind error polluted direct handle %d", directHandle)
	}
}

func TestTryRegisterPackageValueConflictDoesNotBindProvider(t *testing.T) {
	exec := engine.NewMiniExecutor()
	stringSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("String"), ReadOnly: true}
	intSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("Int64"), ReadOnly: true}
	if err := exec.TryRegisterPackageValue("demo.Value", stringSpec, packageValueProviderFunc(func(runtime.FFIBindContext) (*runtime.Var, error) {
		return runtime.NewString("old"), nil
	})); err != nil {
		t.Fatalf("register existing package value failed: %v", err)
	}

	called := false
	err := exec.TryRegisterPackageValue("demo.Value", intSpec, packageValueProviderFunc(func(runtime.FFIBindContext) (*runtime.Var, error) {
		called = true
		return runtime.NewInt(1), nil
	}))
	var conflict *runtime.SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != "package value" {
		t.Fatalf("expected package value conflict, got %T %v", err, err)
	}
	if called {
		t.Fatal("conflicting package value provider should not be bound")
	}
}

func TestTryRegisterPackageValueBindErrorRollsBackPinnedHandles(t *testing.T) {
	exec := engine.NewMiniExecutor()
	spec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("String"), ReadOnly: true}
	var handle uint32
	err := exec.TryRegisterPackageValue("demo.Value", spec, packageValueProviderFunc(func(ctx runtime.FFIBindContext) (*runtime.Var, error) {
		if ctx.PinnedRegistry == nil {
			return nil, errors.New("missing pinned registry")
		}
		handle = ctx.PinnedRegistry.RegisterPinnedTyped(&struct{}{}, "demo.Handle")
		return nil, errors.New("bind failed")
	}))
	if err == nil || !strings.Contains(err.Error(), "bind failed") {
		t.Fatalf("expected bind error, got %v", err)
	}
	if handle == 0 {
		t.Fatal("expected bind to allocate a pinned handle")
	}
	if _, ok := exec.HandleRegistry().Get(handle); ok {
		t.Fatalf("failed package value bind error polluted pinned handle %d", handle)
	}
	if got := exec.ExportedSchema().Values["demo.Value"]; got != nil {
		t.Fatalf("failed package value registration polluted schema: %#v", got)
	}
}

func TestMiniExecutorRejectsStructSchemaConflict(t *testing.T) {
	exec := engine.NewMiniExecutor()
	exec.RegisterStructSchema("demo.Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; }"))

	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "ffi struct schema conflict") {
			t.Fatalf("expected ffi struct schema conflict panic, got %v", r)
		}
	}()

	exec.RegisterStructSchema("demo.Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; Count Int64; }"))
}

func TestBytecodeUnmarshalRejectsInvalidExecutableTask(t *testing.T) {
	payload := []byte(fmt.Sprintf(`{"format":"go-mini-bytecode","version":%d,"opcode_set":"runtime.opcode.v4","entry":[{"op":"PUSH","operand":"1"}],"executable":{"global_init_order":[],"globals":{},"functions":{},"main_tasks":[{"op":5}]}}`, bytecode.CurrentVersion))
	_, err := bytecode.UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected executable task decode failure")
	}
	if !strings.Contains(err.Error(), "invalid executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRuntimeByCompiledRequiresExecutableBytecode(t *testing.T) {
	exec := engine.NewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package main
func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	compiled.Bytecode = nil

	_, err = exec.NewRuntimeByCompiled(compiled)
	if err == nil {
		t.Fatal("expected missing executable bytecode error")
	}
	if !strings.Contains(err.Error(), "missing executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRuntimeByBytecodeRejectsMissingExecutable(t *testing.T) {
	exec := engine.NewMiniExecutor()
	program := bytecode.NewProgram()
	program.Entry = []bytecode.Instruction{{Op: "PUSH", Operand: "1"}}

	_, err := exec.NewRuntimeByBytecode(program)
	if err == nil {
		t.Fatal("expected missing executable bytecode error")
	}
	if !strings.Contains(err.Error(), "missing executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileGoCodeToBytecodeReturnsExecutableProgram(t *testing.T) {
	exec := engine.NewMiniExecutor()
	program, err := exec.CompileGoCodeToBytecode(`
package main
func main() {}
`)
	if err != nil {
		t.Fatalf("compile to bytecode failed: %v", err)
	}
	if program == nil {
		t.Fatal("expected bytecode program")
	}
	if err := program.Validate(); err != nil {
		t.Fatalf("unexpected invalid bytecode: %v", err)
	}
	if program.Executable == nil {
		t.Fatal("expected executable bytecode payload")
	}
}

func TestPreparedOnlyBytecodeLoadsAndExecutes(t *testing.T) {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main
var Result Int64 = 1
func main() { Result = Result + 41 }
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	program, err := prog.Bytecode()
	if err != nil {
		t.Fatalf("bytecode accessor failed: %v", err)
	}

	preparedOnly := *program
	preparedOnly.Globals = nil
	preparedOnly.Entry = nil
	preparedOnly.Functions = nil

	loaded, err := exec.NewRuntimeByBytecode(&preparedOnly)
	if err != nil {
		t.Fatalf("load prepared-only bytecode failed: %v", err)
	}
	if loaded.Compilation().Program != nil {
		t.Fatal("prepared-only bytecode should not retain analysis AST")
	}
	if err := loaded.Execute(context.Background()); err != nil {
		t.Fatalf("execute prepared-only bytecode failed: %v", err)
	}
	result, ok := loaded.SharedState().LoadGlobal("Result")
	if !ok || result == nil || result.I64 != 42 {
		t.Fatalf("unexpected Result global: %#v", result)
	}
}

func TestModuleImportUsesPreparedExecutable(t *testing.T) {
	exec := engine.NewMiniExecutor()
	helperSource := `
package helper
func Answer() Int64 { return 40 }
`
	helperCompiled, err := exec.CompileGoCode(helperSource)
	if err != nil {
		t.Fatalf("compile helper failed: %v", err)
	}
	helperProg, err := exec.NewRuntimeByCompiled(helperCompiled)
	if err != nil {
		t.Fatalf("load helper failed: %v", err)
	}
	helperBytecode, err := helperProg.Bytecode()
	if err != nil {
		t.Fatalf("helper bytecode accessor failed: %v", err)
	}
	helperPreparedOnly := *helperBytecode
	helperPreparedOnly.Globals = nil
	helperPreparedOnly.Entry = nil
	helperPreparedOnly.Functions = nil

	helperRuntime, err := exec.NewRuntimeByBytecode(&helperPreparedOnly)
	if err != nil {
		t.Fatalf("load prepared-only helper failed: %v", err)
	}
	if helperRuntime.Compilation().Program != nil {
		t.Fatal("prepared helper runtime should not retain analysis AST")
	}

	exec.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		if path == "helper" {
			return helperCompiled.Program, nil
		}
		return nil, fmt.Errorf("module not found: %s", path)
	})
	exec.RegisterModule("helper", helperRuntime)
	metadata := exec.ExportMetadata()
	if !strings.Contains(metadata, `"helper"`) || !strings.Contains(metadata, `"Answer": "function() Int64"`) {
		t.Fatalf("expected prepared module metadata, got: %s", metadata)
	}

	mainProg, err := exec.NewRuntimeByGoCode(`
package main
import "helper"
var Result Int64
func main() { Result = helper.Answer() + 2 }
`)
	if err != nil {
		t.Fatalf("compile main failed: %v", err)
	}
	if err := mainProg.Execute(context.Background()); err != nil {
		t.Fatalf("execute main failed: %v", err)
	}
	result, ok := mainProg.SharedState().LoadGlobal("Result")
	if !ok || result == nil || result.I64 != 42 {
		t.Fatalf("unexpected Result global: %#v", result)
	}
	if !mainProg.SharedState().HasModule("helper") {
		t.Fatal("expected helper module loaded from prepared executable")
	}
}

func TestExecutableProgramBytecodeAccessor(t *testing.T) {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main
func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	bytecodeProgram, err := prog.Bytecode()
	if err != nil {
		t.Fatalf("bytecode accessor failed: %v", err)
	}
	if bytecodeProgram == nil || bytecodeProgram.Executable == nil {
		t.Fatal("expected executable bytecode program")
	}
}

func TestArtifactFromBytecodeJSONRoundTrip(t *testing.T) {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main
const Version = "v1"
func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	payload, err := prog.MarshalBytecodeJSON()
	if err != nil {
		t.Fatalf("marshal bytecode failed: %v", err)
	}
	artifact, err := exec.ArtifactFromBytecodeJSON(payload)
	if err != nil {
		t.Fatalf("artifact from bytecode json failed: %v", err)
	}
	if artifact == nil || artifact.Bytecode == nil || artifact.Bytecode.Executable == nil {
		t.Fatal("expected executable artifact")
	}
	if artifact.Program != nil {
		t.Fatal("bytecode artifact should not contain analysis AST")
	}
	if artifact.Bytecode.Executable.Constants["Version"] != "v1" {
		t.Fatalf("unexpected executable constants: %#v", artifact.Bytecode.Executable.Constants)
	}
}
