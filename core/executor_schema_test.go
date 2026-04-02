package engine

import (
	"context"
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/bytecode"
)

func TestMiniExecutorExportsParsedSchema(t *testing.T) {
	exec := NewMiniExecutor()
	exec.RegisterFFI("demo.Call", nil, 1, "function(String, ...Any) tuple(Void, String)", "demo route")
	exec.RegisterStructSpec("demo.Payload", "struct { Msg String; Count Int64; }")

	schema := exec.GetExportedSchema()
	if schema == nil {
		t.Fatal("expected schema snapshot")
	}

	funcSig := schema.Funcs["demo.Call"]
	if funcSig == nil {
		t.Fatal("expected parsed function schema")
	}
	if !funcSig.Function.Variadic {
		t.Fatal("expected variadic function schema")
	}
	if got := string(funcSig.Function.Return); got != "tuple(Void, String)" {
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

	exportedSpecs := exec.GetExportedSpecs()
	if got := exportedSpecs["demo.Call"]; got != "function(String, ...Any) tuple(Void, String)" {
		t.Fatalf("unexpected exported function spec: %s", got)
	}
	if got := exportedSpecs["demo.Payload"]; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct spec: %s", got)
	}

	exportedStructs := exec.GetExportedStructs()
	if got := exportedStructs["demo.Payload"]; got != "struct { Msg String; Count Int64; }" {
		t.Fatalf("unexpected exported struct-only spec: %s", got)
	}
}

func TestExportMetadataIncludesRegisteredFFISignatures(t *testing.T) {
	exec := NewMiniExecutor()
	exec.RegisterFFI("demo.Call", nil, 1, "function(String, ...Any) tuple(Void, String)", "demo route")

	meta := exec.ExportMetadata()
	if !strings.Contains(meta, `"Call": "function(String, ...Any) tuple(Void, String) // demo route"`) {
		t.Fatalf("expected exported metadata to include parsed route signature, got:\n%s", meta)
	}
}

func TestCompiledBytecodeJSONRoundTripRemainsExecutable(t *testing.T) {
	exec := NewMiniExecutor()
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

	session := prog.LastSession()
	if session == nil {
		t.Fatal("expected last session")
	}
	counter, loadErr := session.Load("counter")
	if loadErr != nil {
		t.Fatalf("load counter failed: %v", loadErr)
	}
	if counter.I64 != 2 {
		t.Fatalf("unexpected counter value after bytecode roundtrip: %#v", counter)
	}
}

func TestNewRuntimeByBytecodeJSONRebuildsProgramBlueprint(t *testing.T) {
	exec := NewMiniExecutor()
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

	rebuilt := prog.GetAst()
	if rebuilt == nil {
		t.Fatal("expected rebuilt program")
	}
	if rebuilt.Package != "main" {
		t.Fatalf("unexpected package: %s", rebuilt.Package)
	}
	if rebuilt.Constants["Version"] != "v1" {
		t.Fatalf("unexpected constant map: %#v", rebuilt.Constants)
	}
	if rebuilt.Structs["Payload"] == nil {
		t.Fatalf("expected rebuilt struct metadata: %#v", rebuilt.Structs)
	}
	if rebuilt.Interfaces["Reader"] == nil {
		t.Fatalf("expected rebuilt interface metadata: %#v", rebuilt.Interfaces)
	}
	if rebuilt.Functions["main"] == nil {
		t.Fatalf("expected rebuilt function stubs: %#v", rebuilt.Functions)
	}
}

func TestMiniProgramMarshalJSONDefaultsToBytecode(t *testing.T) {
	exec := NewMiniExecutor()
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

func TestNewRuntimeByJSONAutoDetectsBytecode(t *testing.T) {
	exec := NewMiniExecutor()
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

	loaded, err := exec.NewRuntimeByJSON(payload)
	if err != nil {
		t.Fatalf("load by generic json failed: %v", err)
	}
	if err := loaded.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestNewRuntimeByJSONRejectsASTPayload(t *testing.T) {
	exec := NewMiniExecutor()
	astPayload := []byte(`{"meta":"boot","constants":{},"variables":{},"types":{},"structs":{},"functions":{},"main":[]}`)
	_, err := exec.NewRuntimeByJSON(astPayload)
	if err == nil {
		t.Fatal("expected ast payload rejection")
	}
	if !strings.Contains(err.Error(), "expected go-mini bytecode") {
		t.Fatalf("unexpected ast json load error: %v", err)
	}
}

func TestBytecodeUnmarshalRejectsInvalidExecutableTask(t *testing.T) {
	payload := []byte(`{"format":"go-mini-bytecode","version":1,"opcode_set":"runtime.opcode.v1","entry":[{"op":"PUSH","operand":"1"}],"executable":{"global_init_order":[],"globals":{},"functions":{},"main_tasks":[{"op":57,"data_kind":"literal_var","data":{"type":"Int64","vtype":0,"i64":1}},{"op":57,"data_kind":"literal_var","data":{"type":"Int64","vtype":0,"i64":2}},{"op":57,"data_kind":"literal_var","data":{"type":"Int64","vtype":0,"i64":3}},{"op":32}]}}`)
	_, err := bytecode.UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected executable task decode failure")
	}
	if !strings.Contains(err.Error(), "opcode") && !strings.Contains(err.Error(), "deserializable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRuntimeByCompiledRequiresExecutableBytecode(t *testing.T) {
	exec := NewMiniExecutor()
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
