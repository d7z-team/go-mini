package engine_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
)

const fullPipelineHelperModule = `
package helper

import "strings"

var BootTrace = boot()
var Counter = 1

func boot() string {
	return "helper-boot"
}

func MakeAdder(seed int) func(int) int {
	current := seed
	return func(delta int) int {
		current = current + delta
		return current
	}
}

func Tag(parts []string) string {
	return strings.Join(parts, "/")
}

func Next() int {
	Counter = Counter + 1
	return Counter
}
`

const fullPipelineMainProgram = `
package main

import "helper"
import "encoding/json"
import "time"

var trace = boot()
var payload any
var childResult = 0

func mark(s string) {
	trace = trace + "|" + s
}

func boot() string {
	if helper.BootTrace != "helper-boot" {
		panic("helper boot mismatch")
	}
	return "main-boot"
}

func risky(label string) string {
	defer mark("defer:" + label)
	defer func() {
		if r := recover(); r != nil {
			mark("recover:" + string(r))
		}
	}()
	if label == "panic" {
		panic("boom")
	}
	mark("body:" + label)
	return label + ":ok"
}

func main() {
	mark("start")
	if helper.Counter != 1 {
		panic("helper init order mismatch")
	}

	adder := helper.MakeAdder(10)
	if adder(5) != 15 {
		panic("adder step 1")
	}
	if adder(7) != 22 {
		panic("adder step 2")
	}
	mark("closure")

	if risky("safe") != "safe:ok" {
		panic("safe call mismatch")
	}
	risky("panic")

	go func(base int) {
		defer mark("child.defer")
		childResult = helper.Next() + base
	}(5)
	time.Sleep(1)
	mark("go")

	raw, err := json.Marshal(map[string]any{
		"trace": trace,
		"tag": helper.Tag([]string{"go", "mini", "vm"}),
		"sum": childResult,
		"counter": helper.Counter,
	})
	if err != nil {
		panic(err)
	}
	obj, err := json.Unmarshal(raw)
	if err != nil {
		panic(err)
	}
	payload = obj
}
`

const fullPipelineExpectedTrace = "main-boot|start|closure|body:safe|defer:safe|recover:boom|defer:panic|child.defer|go"

func TestFullPipelineIntegrity(t *testing.T) {
	t.Run("source_runtime", func(t *testing.T) {
		exec, _ := buildFullPipelineFixture(t)
		prog, err := exec.NewRuntimeByGoCode(fullPipelineMainProgram)
		if err != nil {
			t.Fatalf("new runtime by source failed: %v", err)
		}
		assertFullPipelineExecution(t, prog)
	})

	t.Run("compiled_prepared_only", func(t *testing.T) {
		exec, compiled := buildFullPipelineFixture(t)
		prog, err := exec.NewRuntimeByCompiled(compiled)
		if err != nil {
			t.Fatalf("new runtime by compiled failed: %v", err)
		}
		assertFullPipelineExecution(t, prog)
	})

	t.Run("bytecode_json_roundtrip", func(t *testing.T) {
		exec, compiled := buildFullPipelineFixture(t)
		payload, err := compiled.MarshalBytecodeJSON()
		if err != nil {
			t.Fatalf("marshal bytecode failed: %v", err)
		}
		prog, err := exec.NewRuntimeByBytecodeJSON(payload)
		if err != nil {
			t.Fatalf("new runtime by bytecode json failed: %v", err)
		}
		assertFullPipelineExecution(t, prog)
	})
}

func buildFullPipelineFixture(t *testing.T) (*engine.MiniExecutor, *compiler.Artifact) {
	t.Helper()

	exec := engine.NewMiniExecutor()
	helperProg, err := exec.NewRuntimeByGoCode(fullPipelineHelperModule)
	if err != nil {
		t.Fatalf("compile helper module failed: %v", err)
	}
	exec.RegisterModule("helper", helperProg)

	compiled, err := exec.CompileGoCode(fullPipelineMainProgram)
	if err != nil {
		t.Fatalf("compile main program failed: %v", err)
	}
	return exec, compiled
}

func assertFullPipelineExecution(t *testing.T, prog *engine.MiniProgram) {
	t.Helper()

	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	snapshot := prog.SharedState()
	if snapshot == nil {
		t.Fatal("expected shared state snapshot")
	}
	if !snapshot.HasModule("helper") {
		t.Fatal("expected helper module to be loaded into module cache")
	}

	traceVar, ok := snapshot.LoadGlobal("trace")
	if !ok || traceVar == nil {
		t.Fatal("missing trace global")
	}
	if traceVar.Str != fullPipelineExpectedTrace {
		t.Fatalf("unexpected trace order: got %q want %q", traceVar.Str, fullPipelineExpectedTrace)
	}

	childResult, ok := snapshot.LoadGlobal("childResult")
	if !ok || childResult == nil {
		t.Fatal("missing childResult global")
	}
	if childResult.I64 != 7 {
		t.Fatalf("unexpected childResult: %#v", childResult)
	}

	payloadVar, ok := snapshot.LoadGlobal("payload")
	if !ok || payloadVar == nil {
		t.Fatal("missing payload global")
	}

	obj, ok := runtimeVarToInterface(payloadVar).(map[string]interface{})
	if !ok {
		t.Fatalf("payload should decode to map[string]interface{}, got %T", runtimeVarToInterface(payloadVar))
	}
	if got, _ := obj["trace"].(string); got != fullPipelineExpectedTrace {
		t.Fatalf("unexpected payload trace: %#v", obj["trace"])
	}
	if got, _ := obj["tag"].(string); got != "go/mini/vm" {
		t.Fatalf("unexpected payload tag: %#v", obj["tag"])
	}
	assertPayloadNumber(t, obj["sum"], 7)
	assertPayloadNumber(t, obj["counter"], 2)
}

func assertPayloadNumber(t *testing.T, value interface{}, want int64) {
	t.Helper()

	switch got := value.(type) {
	case int64:
		if got != want {
			t.Fatalf("unexpected numeric payload value: got %d want %d", got, want)
		}
	case float64:
		if int64(got) != want {
			t.Fatalf("unexpected numeric payload value: got %v want %d", got, want)
		}
	default:
		t.Fatalf("unexpected numeric payload type %T (%#v)", value, value)
	}
}

func runtimeVarToInterface(v *runtime.Var) interface{} {
	if v == nil {
		return nil
	}
	switch v.VType {
	case runtime.TypeAny:
		switch inner := v.Ref.(type) {
		case *runtime.Var:
			return runtimeVarToInterface(inner)
		case *runtime.VMMap:
			out := make(map[string]interface{}, inner.Len())
			for k, item := range inner.Snapshot() {
				out[k] = runtimeVarToInterface(item)
			}
			return out
		case *runtime.VMArray:
			items := inner.Snapshot()
			out := make([]interface{}, len(items))
			for i, item := range items {
				out[i] = runtimeVarToInterface(item)
			}
			return out
		default:
			return inner
		}
	case runtime.TypeMap:
		if m, ok := v.Ref.(*runtime.VMMap); ok {
			out := make(map[string]interface{}, m.Len())
			for k, item := range m.Snapshot() {
				out[k] = runtimeVarToInterface(item)
			}
			return out
		}
	case runtime.TypeArray:
		if arr, ok := v.Ref.(*runtime.VMArray); ok {
			items := arr.Snapshot()
			out := make([]interface{}, len(items))
			for i, item := range items {
				out[i] = runtimeVarToInterface(item)
			}
			return out
		}
	}
	return v.Interface()
}
