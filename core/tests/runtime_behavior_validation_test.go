package engine_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestLowercaseAnyTypeSwitchStringBehavior(t *testing.T) {
	exec := engine.NewMiniExecutor()

	const code = `
package main

var result = ""

func format(v any) string {
	switch x := v.(type) {
	case string:
		return "str:" + x
	case int:
		return "int"
	default:
		return "other"
	}
}

func main() {
	result = format("abc")
}
`

	prog, err := exec.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	snapshot := prog.SharedState()
	result, ok := snapshot.LoadGlobal("result")
	if !ok || result == nil {
		t.Fatal("missing result global")
	}
	if result.Str != "str:abc" {
		t.Fatalf("unexpected lowercase any type-switch result: %q", result.Str)
	}
}

func TestNilSliceAppendBehavior(t *testing.T) {
	exec := engine.NewMiniExecutor()

	const code = `
package main

func main() {
	var values []int
	values = append(values, 1)
	if len(values) != 1 {
		panic("append should have produced one element")
	}
}
`

	prog, err := exec.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestUppercaseInt64ControlFlowAccumulationBehavior(t *testing.T) {
	exec := engine.NewMiniExecutor()

	const code = `
package main

var total Int64

func main() {
	local := Int64(0)
	for _, i := range []Int64{0, 1, 2, 3, 4, 5} {
		switch i {
		case 1:
			local = local + 1
			continue
		case 4:
			local = local + 4
			break
		default:
			local = local + 2
		}
		local = local + 10
	}
	total = total + local
}
`

	prog, err := exec.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	snapshot := prog.SharedState()
	total, ok := snapshot.LoadGlobal("total")
	if !ok || total == nil {
		t.Fatal("missing total global")
	}
	if total.I64 != 63 {
		t.Fatalf("unexpected Int64 control-flow accumulation result: %d", total.I64)
	}
}
