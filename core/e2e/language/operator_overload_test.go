package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestOperatorOverloadExecutesAsMethodCall(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

type Vec struct {
	X int
	Y int
}

func (v Vec) OpAdd(other Vec) Vec {
	return Vec{X: v.X + other.X, Y: v.Y + other.Y}
}

func (v Vec) OpNeg() Vec {
	return Vec{X: -v.X, Y: -v.Y}
}

func (v Vec) OpLt(other Vec) bool {
	return v.X + v.Y < other.X + other.Y
}

func main() {
	a := Vec{X: 2, Y: 4}
	b := Vec{X: 3, Y: 5}
	c := a + b
	if c.X != 5 || c.Y != 9 {
		panic("OpAdd result mismatch")
	}

	n := -c
	if n.X != -5 || n.Y != -9 {
		panic("OpNeg result mismatch")
	}

	if !(a < b) {
		panic("OpLt result mismatch")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestOperatorOverloadInterfaceReceiver(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

type Addable interface {
	OpAdd(Vec) Vec
}

type Vec struct {
	X int
}

func (v Vec) OpAdd(other Vec) Vec {
	return Vec{X: v.X + other.X}
}

func main() {
	var a Addable = Vec{X: 1}
	b := Vec{X: 2}
	c := a + b
	if c.X != 3 {
		panic("interface OpAdd result mismatch")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestLogicalOperatorsDoNotUseOverload(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

type Flag struct {
	Value bool
}

func (f Flag) OpAnd(other Flag) bool {
	return f.Value && other.Value
}

func main() {
	a := Flag{Value: true}
	b := Flag{Value: false}
	_ = a && b
}
`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected compile error for logical operator overload")
	}
	if !strings.Contains(err.Error(), "And operator expects Bool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOperatorOverloadRejectsReceiverMismatch(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "pointer operand value receiver",
			code: `
package main

type Vec struct {
	X int
}

func (v Vec) OpAdd(other Vec) Vec {
	return other
}

func main() {
	p := &Vec{X: 1}
	b := Vec{X: 2}
	_ = p + b
}
`,
			want: "OpAdd method receiver type mismatch: expected Vec, got Ptr<Vec>",
		},
		{
			name: "value operand pointer receiver",
			code: `
package main

type Vec struct {
	X int
}

func (v *Vec) OpAdd(other Vec) Vec {
	return other
}

func main() {
	a := Vec{X: 1}
	b := Vec{X: 2}
	_ = a + b
}
`,
			want: "OpAdd method receiver type mismatch: expected Ptr<Vec>, got Vec",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectCompileErrorContains(t, tc.code, tc.want)
		})
	}
}

func TestOperatorOverloadRejectsInvalidReturnShape(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "void",
			code: `
package main

type Vec struct {
	X int
}

func (v Vec) OpAdd(other Vec) {
}

func main() {
	a := Vec{X: 1}
	b := Vec{X: 2}
	a + b
}
`,
			want: "OpAdd method must return a single non-Void value, got Void",
		},
		{
			name: "tuple",
			code: `
package main

type Vec struct {
	X int
}

func (v Vec) OpAdd(other Vec) (Vec, Vec) {
	return v, other
}

func main() {
	a := Vec{X: 1}
	b := Vec{X: 2}
	_ = a + b
}
`,
			want: "OpAdd method must return a single non-Void value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectCompileErrorContains(t, tc.code, tc.want)
		})
	}
}

func TestOperatorOverloadUsesTypedConstantOperand(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

const one = 1

type Vec struct {
	X int
}

func (v Vec) OpAdd(delta int) Vec {
	return Vec{X: v.X + delta}
}

func main() {
	v := Vec{X: 1}
	c := v + one
	if c.X != 2 {
		panic("constant operand OpAdd result mismatch")
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestComparisonOverloadMustReturnBool(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

type Vec struct {
	X int
}

func (v Vec) OpLt(other Vec) Vec {
	return v
}

func main() {
	a := Vec{X: 1}
	b := Vec{X: 2}
	_ = a < b
}
`
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected compile error for invalid comparison overload")
	}
	if !strings.Contains(err.Error(), "OpLt method must return Bool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func expectCompileErrorContains(t *testing.T, code, want string) {
	t.Helper()
	executor := engine.MustNewMiniExecutor()
	_, err := executor.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatalf("expected compile error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %v", want, err)
	}
}
