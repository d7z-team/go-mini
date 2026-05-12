package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestForLoopClosureSeesPostLoopOuterMutation(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

func main() {
	x := 1
	var f Any
	for i := 0; i < 1; i++ {
		f = func() Int64 { return x }
		x = 5
	}
	x = 9
	if f() != 9 {
		panic("closure should observe outer mutation after loop")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestForLoopClosureSeesOuterMutationAfterBreak(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

func main() {
	x := 1
	var f Any
	for i := 0; i < 3; i++ {
		f = func() Int64 { return x }
		x = 5
		break
	}
	x = 9
	if f() != 9 {
		panic("closure should observe outer mutation after break")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestRangeLoopClosureSeesPostLoopOuterMutation(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

func main() {
	x := 1
	var f Any
	for _, i := range []Int64{0} {
		_ = i
		f = func() Int64 { return x }
		x = 5
	}
	x = 9
	if f() != 9 {
		panic("range-loop closure should observe outer mutation after loop")
	}
}
`

	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
