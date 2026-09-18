package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestGeneratedBlockScopeReturnAfterSiblingBlocks(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main

func f() Int64 {
	{ return 1 }
	{ _ = 2 }
	{ _ = 3 }
	{ _ = 4 }
	{ _ = 5 }
	return 0
}

func main() { _ = f() }
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generated block scope return should not host-panic: %v", r)
		}
	}()
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestGeneratedBlockScopePanicAfterSiblingBlocksReturnsVMError(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main

func f() {
	{ panic("boom") }
	{ _ = 2 }
	{ _ = 3 }
	{ _ = 4 }
}

func main() { f() }
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generated block scope panic should return VM error, got host panic: %v", r)
		}
	}()
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected VM panic error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected VM error: %v", err)
	}
}

func TestSwitchInitExecutesOnce(t *testing.T) {
	exec := engine.MustNewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main

var Count Int64

func bump() Int64 {
	Count = Count + 1
	return Count
}

func main() {
	switch x := bump(); x {
	case 1:
	default:
		panic("switch init ran more than once")
	}
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	count, ok := prog.SharedState().LoadGlobal("Count")
	if !ok || count == nil || count.I64 != 1 {
		t.Fatalf("switch init should run once, got %#v", count)
	}
}

func TestBytecodeJSONRejectsOrphanScopeExitExecutable(t *testing.T) {
	program := bytecode.NewProgram()
	program.Executable = &runtime.PreparedProgram{
		Globals:   map[string]*runtime.PreparedGlobal{},
		Functions: map[string]*runtime.PreparedFunction{},
		MainTasks: []runtime.Task{{Op: runtime.OpScopeExit}},
	}
	payload, err := json.Marshal(program)
	if err != nil {
		t.Fatalf("marshal bytecode failed: %v", err)
	}

	_, err = bytecode.UnmarshalJSON(payload)
	if err == nil {
		t.Fatal("expected invalid executable bytecode error")
	}
	if !strings.Contains(err.Error(), "invalid executable bytecode") {
		t.Fatalf("unexpected error: %v", err)
	}
}
