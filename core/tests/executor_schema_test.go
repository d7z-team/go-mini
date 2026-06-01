package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func requireSchemaConflict(t *testing.T, err error, kind string) {
	t.Helper()
	var conflict *runtime.SchemaConflictError
	if !errors.As(err, &conflict) || conflict.Kind != kind {
		t.Fatalf("expected %s conflict, got %T %v", kind, err, err)
	}
}

func TestMiniExecutorExportsParsedSchema(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	ffiSchema := runtime.NewFFISurfaceSchema()
	if err := ffiSchema.AddRouteDecls([]runtime.FFIRouteDecl{
		testsurface.Route("demo.Call", 1, runtime.MustParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)"), "demo route"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := ffiSchema.AddStruct("demo", "Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; Count Int64; }")); err != nil {
		t.Fatal(err)
	}
	if err := ffiSchema.AddInterface("demo", "Reader", runtime.MustParseRuntimeInterfaceSpec("interface{Read(Array<Byte>) tuple(Int64, Error);}")); err != nil {
		t.Fatal(err)
	}
	if err := exec.UseSurface(testsurface.SchemaBundle(ffiSchema, nil)); err != nil {
		t.Fatal(err)
	}

	snapshot := exec.ExportedSchema()
	if snapshot == nil {
		t.Fatal("expected schema snapshot")
	}

	funcSig := snapshot.Funcs["demo.Call"]
	if funcSig == nil {
		t.Fatal("expected parsed function schema")
	}
	if !funcSig.Variadic {
		t.Fatal("expected variadic function schema")
	}
	if got := string(funcSig.ReturnType.Raw); got != "tuple(Void, String)" {
		t.Fatalf("unexpected return type: %s", got)
	}

	structSpec := snapshot.Structs["demo.Payload"]
	if structSpec == nil {
		t.Fatal("expected parsed struct schema")
	}
	if len(structSpec.Fields) != 2 {
		t.Fatalf("unexpected struct field count: %d", len(structSpec.Fields))
	}
	if structSpec.Fields[0].Name != "Msg" || structSpec.Fields[1].Name != "Count" {
		t.Fatalf("unexpected struct field order: %+v", structSpec.Fields)
	}

	if got := snapshot.Funcs["demo.Call"].Spec; got != "function(String, ...Any) tuple(Void, String)" {
		t.Fatalf("unexpected exported function spec: %s", got)
	}
	if got := snapshot.Structs["demo.Payload"].Spec; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct spec: %s", got)
	}
	if got := snapshot.Interfaces["demo.Reader"].Spec; got != "interface{Read(Array<Byte>) tuple(Int64, Error);}" {
		t.Fatalf("unexpected exported interface spec: %s", got)
	}
}

func TestExportMetadataIncludesRegisteredFFISignatures(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	testsurface.UseRoute(t, exec, "demo.Call", nil, 1, runtime.MustParseRuntimeFuncSig("function(String, ...Any) tuple(Void, String)"), "demo route")

	meta := exec.ExportMetadata()
	if !strings.Contains(meta, `"Call": "function(String, ...Any) tuple(Void, String) // demo route"`) {
		t.Fatalf("expected exported metadata to include parsed route signature, got:\n%s", meta)
	}
}

func TestCompiledBytecodeJSONRoundTripRemainsExecutable(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
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

	prog, err := exec.NewRuntimeByArtifact(compiled)
	if err != nil {
		t.Fatalf("new runtime by executable artifact failed: %v", err)
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
	exec := engine.MustNewMiniExecutor()
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
	exec := engine.MustNewMiniExecutor()
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
	if executable.Constants["Version"].DisplayString() != "v1" {
		t.Fatalf("unexpected executable constants: %#v", executable.Constants)
	}
	if executable.StructSchemas["main.Payload"] == nil {
		t.Fatalf("expected executable struct schema: %#v", executable.StructSchemas)
	}
	if executable.InterfaceSchemas["main.Reader"] == nil {
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
	if _, err := prog.Bytecode(); err != nil {
		t.Fatalf("bytecode-loaded executable should expose bytecode only: %v", err)
	}
	counter, ok := prog.SharedState().LoadGlobal("counter")
	if !ok || counter == nil || counter.I64 != 2 {
		t.Fatalf("unexpected counter global: %#v", counter)
	}
}

func TestExecutableProgramMarshalJSONDefaultsToBytecode(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
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

func TestNewRuntimeByBytecodeJSONExecutesLoadedProgram(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
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
	exec := engine.MustNewMiniExecutor()
	testsurface.UseRoute(t, exec, "demo.Call", nil, 1, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")

	requireSchemaConflict(t, exec.UseSurface(testsurface.RouteBundle("demo.Call", nil, 2, runtime.MustParseRuntimeFuncSig("function(String) Void"), "")), "surface member")
}

func TestMiniExecutorUseSurfaceReportsSchemaConflict(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	testsurface.UseRoute(t, exec, "demo.Mutate", nil, 1, runtime.MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Void", runtime.FFIParamInOutBytes), "")

	requireSchemaConflict(t, exec.UseSurface(testsurface.RouteBundle("demo.Mutate", nil, 1, runtime.MustParseRuntimeFuncSigWithModes("function(Array<Byte>) Void", runtime.FFIParamIn), "")), "surface member")
}

func TestUseSurfaceRouteConflictDoesNotPolluteRoutes(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	sigA := runtime.MustParseRuntimeFuncSig("function(String) Void")
	sigB := runtime.MustParseRuntimeFuncSig("function(Int64) Void")
	testsurface.UseRoute(t, exec, "demo.Call", nil, 1, sigA, "")

	requireSchemaConflict(t, exec.UseSurface(testsurface.RouteBundle("demo.Call", nil, 7, sigB, "")), "surface member")

	schema := exec.ExportedSchema()
	if got := schema.Funcs["demo.Call"]; got == nil || !runtime.SameRuntimeFuncSchema(got, sigA) {
		t.Fatalf("schema should remain unchanged, got %#v", got)
	}
}

func TestUseSurfaceConflictAfterBindRollsBackPinnedHandles(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	stringSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("String"), ReadOnly: true}
	intSpec := &runtime.ValueSpec{Type: runtime.MustParseRuntimeType("Int64"), ReadOnly: true}
	schema := runtime.NewFFISurfaceSchema()
	if err := schema.AddValue("demo", "Value", stringSpec); err != nil {
		t.Fatal(err)
	}
	if err := exec.UseSurface(surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurfaceFromSchema(schema)
		bound.AddPackageValue("demo", "Value", stringSpec, runtime.NewString("old"))
		return bound, nil
	})); err != nil {
		t.Fatalf("register existing package value failed: %v", err)
	}

	var handle uint32
	var registry *ffigo.HandleRegistry
	err := exec.UseSurface(surface.New(schema, func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		if ctx.PinnedRegistry == nil {
			return nil, errors.New("missing pinned registry")
		}
		registry = ctx.Registry
		handle = ctx.PinnedRegistry.RegisterPinnedTyped(&struct{}{}, "demo.Handle")
		bound := runtime.NewBoundFFISurfaceFromSchema(schema)
		bound.AddPackageValue("demo", "Value", intSpec, runtime.NewInt(1))
		return bound, nil
	}))
	requireSchemaConflict(t, err, "package value")
	if handle == 0 {
		t.Fatal("expected bind to allocate a pinned handle")
	}
	if _, ok := registry.Get(handle); ok {
		t.Fatalf("failed UseSurface polluted pinned handle %d", handle)
	}
	if got := exec.ExportedSchema().Values["demo.Value"]; got == nil || got.Type.Raw != "String" {
		t.Fatalf("existing package value schema should remain unchanged, got %#v", got)
	}
}

func TestUseSurfaceBindErrorRollsBackPinnedHandles(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	var handle uint32
	var directHandle uint32
	var registry *ffigo.HandleRegistry
	err := exec.UseSurface(surface.New(runtime.NewFFISurfaceSchema(), func(ctx runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		if ctx.PinnedRegistry == nil {
			return nil, errors.New("missing pinned registry")
		}
		registry = ctx.Registry
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
	if _, ok := registry.Get(handle); ok {
		t.Fatalf("failed UseSurface bind error polluted pinned handle %d", handle)
	}
	if _, ok := registry.Get(directHandle); ok {
		t.Fatalf("failed UseSurface bind error polluted direct handle %d", directHandle)
	}
}

func TestMiniExecutorRejectsStructSchemaConflict(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	left := runtime.NewFFISurfaceSchema()
	if err := left.AddStruct("demo", "Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; }")); err != nil {
		t.Fatal(err)
	}
	if err := exec.UseSurface(surface.Router(left, nil)); err != nil {
		t.Fatal(err)
	}

	right := runtime.NewFFISurfaceSchema()
	if err := right.AddStruct("demo", "Payload", runtime.MustParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; Count Int64; }")); err != nil {
		t.Fatal(err)
	}
	requireSchemaConflict(t, exec.UseSurface(surface.Router(right, nil)), "surface type")
}

func TestBytecodeUnmarshalRejectsInvalidExecutableTask(t *testing.T) {
	payload := []byte(fmt.Sprintf(`{"format":"go-mini-bytecode","version":%d,"opcode_set":"runtime.opcode.v5","entry":[{"op":"PUSH","operand":"1"}],"executable":{"global_init_order":[],"globals":{},"functions":{},"main_tasks":[{"op":5}]}}`, bytecode.CurrentVersion))
	_, err := bytecode.UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected executable task decode failure")
	}
	if !strings.Contains(err.Error(), "invalid executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRuntimeByArtifactRequiresExecutableBytecode(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package main
func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	compiled.Bytecode = nil

	_, err = exec.NewRuntimeByArtifact(compiled)
	if err == nil {
		t.Fatal("expected missing executable bytecode error")
	}
	if !strings.Contains(err.Error(), "missing executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRuntimeByBytecodeRejectsMissingExecutable(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
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
	exec := engine.MustNewMiniExecutor()
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
	exec := engine.MustNewMiniExecutor()
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
	if _, err := loaded.Bytecode(); err != nil {
		t.Fatalf("prepared-only bytecode should expose bytecode only: %v", err)
	}
	if err := loaded.Execute(context.Background()); err != nil {
		t.Fatalf("execute prepared-only bytecode failed: %v", err)
	}
	result, ok := loaded.SharedState().LoadGlobal("Result")
	if !ok || result == nil || result.I64 != 42 {
		t.Fatalf("unexpected Result global: %#v", result)
	}
}

func TestExecutableProgramBytecodeAccessor(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
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
	exec := engine.MustNewMiniExecutor()
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
	if artifact.Bytecode.Executable.Constants["Version"].DisplayString() != "v1" {
		t.Fatalf("unexpected executable constants: %#v", artifact.Bytecode.Executable.Constants)
	}
}
