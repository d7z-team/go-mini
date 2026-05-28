package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTypeGateRejectsInvalidOperations(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	requireCompileErrorContains(t, executor, `
package main
func main() {
	if 10 == "10" {
		panic("bad")
	}
}`, "cannot compare Int64 and String")

	requireCompileErrorContains(t, executor, `
package main
const S = "10"
func main() {
	if S == 10 {
		panic("bad")
	}
}`, "cannot compare String and Int64")

	requireCompileErrorContains(t, executor, `
package main
func main() {
	_ = "x" + 1
}`, "Plus operator does not support String and Int64")

	requireCompileErrorContains(t, executor, `
package main
func main() {
	m := map[int64]string{1: "one"}
	delete(m, "1")
}`, "delete: key type mismatch")

	requireCompileErrorContains(t, executor, `
package main
func main() {
	arr := []int64{1}
	arr = append(arr, "x")
}`, "append: argument 2 type mismatch")

	requireCompileErrorContains(t, executor, `
package main
func main() {
	var x int64 = 1
	var y any = &x
	_ = y
}`, "cannot assign Ptr<Int64> to y (Any)")

	requireCompileErrorContains(t, executor, `
package main
func main() {
	arr := []int64{1, 2}
	_ = arr[0.0:1]
}`, "slice low index must be Int64")
}

func TestTypeGateAllowsValidAnyPureValues(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main
func main() {
	values := []any{int64(10), "10", true, []any{int64(1), "x"}}
	if values[0] != int64(10) {
		panic("bad int")
	}
	if values[1] != "10" {
		panic("bad string")
	}
	if values[2] != true {
		panic("bad bool")
	}
}`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestRuntimeTypeGateRejectsDynamicAnyMismatch(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main
func main() {
	values := []any{"10"}
	if values[0] == int64(10) {
		panic("bad")
	}
}`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected dynamic Any comparison mismatch to fail at runtime")
	}
	if !strings.Contains(err.Error(), "unsupported equality comparison") {
		t.Fatalf("unexpected runtime error: %v", err)
	}
}

func TestAnyWrappedScalarCanCompareWithNil(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main
func main() {
	var key any = "k"
	if key == nil {
		panic("any scalar should not be nil")
	}
	if !(key != nil) {
		panic("any scalar nil inequality failed")
	}
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
