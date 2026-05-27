package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestPointerSemantics(t *testing.T) {
	executor := engine.NewMiniExecutor()

	passCases := []struct {
		name string
		code string
	}{
		{
			name: "basic-dereference-and-assignment",
			code: `
package main

func main() {
	p := new(Int64)
	*p = 123
	if *p != 123 {
		panic("pointer assignment failed")
	}
}
`,
		},
		{
			name: "struct-pointer-member-access",
			code: `
package main

type Point struct {
	X Int64
	Y Int64
}

func main() {
	p := new(Point)
	p.X = 10
	p.Y = 20
	if p.X != 10 || p.Y != 20 {
		panic("struct pointer member assignment failed")
	}
	p2 := *p
	if p2.X != 10 {
		panic("struct dereference failed")
	}
}
`,
		},
		{
			name: "pointer-to-pointer",
			code: `
package main

func main() {
	val := 456
	p := new(Int64)
	*p = val
	p2 := new(*Int64)
	*p2 = p
	if **p2 != 456 {
		panic("nested dereference failed")
	}
}
`,
		},
		{
			name: "address-of-local-global-and-struct-literal",
			code: `
package main

var global = 3

type Point struct {
	X Int64
	Y Int64
}

func main() {
	n := 10
	pn := &n
	*pn = 12
	if n != 12 {
		panic("address-of local failed")
	}

	pg := &global
	*pg = 14
	if global != 14 {
		panic("address-of global failed")
	}

	p := &Point{X: 1, Y: 2}
	p.X = 7
	if (*p).X != 7 || p.Y != 2 {
		panic("address-of struct literal failed")
	}
}
`,
		},
		{
			name: "address-of-struct-field-and-deref",
			code: `
package main

type Point struct {
	X Int64
}

func main() {
	p := &Point{X: 5}
	field := &p.X
	*field = 8
	if p.X != 8 {
		panic("address-of field failed")
	}

	again := &*field
	*again = 9
	if p.X != 9 || *field != 9 {
		panic("address-of deref failed")
	}
}
`,
		},
		{
			name: "address-of-anonymous-struct-literal",
			code: `
package main

func main() {
	p := &struct {
		X int64
		Name string
	}{X: 7, Name: "mini"}
	p.X = 9
	if p.X != 9 || p.Name != "mini" {
		panic("anonymous struct pointer failed")
	}
}
`,
		},
	}
	for _, tc := range passCases {
		t.Run(tc.name, func(t *testing.T) {
			runPointerProgram(t, executor, tc.code)
		})
	}

	rejectCases := []struct {
		name string
		code string
	}{
		{
			name: "address-of-map-index",
			code: `
package main

func main() {
	m := map[string]int64{"x": 1}
	_ = &m["x"]
}
`,
		},
		{
			name: "address-of-slice-expression",
			code: `
package main

func main() {
	arr := []int64{1, 2, 3}
	_ = &arr[0:2]
}
`,
		},
		{
			name: "address-of-function-call",
			code: `
package main

func value() Int64 { return 1 }

func main() {
	_ = &value()
}
`,
		},
		{
			name: "pointer-does-not-implicitly-decay-to-value",
			code: `
package main

func takeValue(n Int64) {}

func main() {
	p := new(Int64)
	takeValue(p)
}
`,
		},
		{
			name: "value-does-not-implicitly-become-pointer",
			code: `
package main

func takePointer(p *Int64) {}

func main() {
	n := 1
	takePointer(n)
}
`,
		},
		{
			name: "pointer-cannot-enter-any",
			code: `
package main

func main() {
	raw := new(Int64)
	var boxed Any = raw
	_ = boxed
}
`,
		},
	}
	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			rejectPointerProgram(t, executor, tc.code)
		})
	}
}

func runPointerProgram(t *testing.T, executor *engine.MiniExecutor, code string) {
	t.Helper()
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func rejectPointerProgram(t *testing.T, executor *engine.MiniExecutor, code string) {
	t.Helper()
	if _, err := executor.NewRuntimeByGoCode(code); err == nil {
		t.Fatal("expected pointer program to be rejected")
	}
}
