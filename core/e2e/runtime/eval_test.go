package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestExecutorEvalExpressions(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	tests := []struct {
		expr    string
		env     map[string]interface{}
		want    interface{}
		wantErr bool
	}{
		{
			expr: "1 + 2 * 3",
			want: int64(7),
		},
		{
			expr: "a + b",
			env: map[string]interface{}{
				"a": int64(10),
				"b": int64(20),
			},
			want: int64(30),
		},
		{
			expr: "s + \" world\"",
			env: map[string]interface{}{
				"s": "hello",
			},
			want: "hello world",
		},
		{
			expr: "a > 10 && b",
			env: map[string]interface{}{
				"a": int64(15),
				"b": true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			results, err := executor.Eval(context.Background(), tt.expr, tt.env)
			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if len(results) != 1 {
					t.Fatalf("Eval() returned %d values, want 1", len(results))
				}
				got := results[0]
				gotVal := got.Interface()
				if gotVal != tt.want {
					t.Errorf("Eval() = %v, want %v", gotVal, tt.want)
				}
			}
		})
	}
}

func TestEvalByteCopy(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	original := []byte("hello")
	env := map[string]interface{}{
		"b": original,
	}

	results, err := executor.Eval(context.Background(), "b", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Eval() returned %d values, want 1", len(results))
	}
	res := results[0]

	original[0] = 'H'

	items, ok := res.Interface().([]interface{})
	if !ok {
		t.Fatalf("Eval() returned %T, want byte array projection", res.Interface())
	}
	got := make([]byte, len(items))
	for i, item := range items {
		n, ok := item.(int64)
		if !ok {
			t.Fatalf("Eval() byte item %d = %T, want int64", i, item)
		}
		got[i] = byte(n)
	}
	if string(got) != "hello" {
		t.Errorf("Eval result aliased host byte slice: got %#v", got)
	}
}

func TestEvalAfterExecute(t *testing.T) {
	e := engine.MustNewMiniExecutor()

	code := `
package main

var globalVar = "initialized"

func test() string {
	return globalVar + ":ok"
}
`
	prog, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	results, err := prog.Eval(context.Background(), "test()", nil)
	if err != nil {
		t.Errorf("Eval after Execute failed: %v", err)
		return
	}
	if len(results) != 1 {
		t.Fatalf("Eval() returned %d values, want 1", len(results))
	}
	res := results[0]

	if res.Str != "initialized:ok" {
		t.Errorf("Expected 'initialized:ok', got %q", res.Str)
	}
}

func TestExecutableEvalUsesCompiledProgramSymbols(t *testing.T) {
	e := engine.MustNewMiniExecutor()
	if err := e.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bump",
		Name:      "bump",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecInt64),
		Body:      `{{ arg 0 }} + 1`,
	}); err != nil {
		t.Fatal(err)
	}

	prog, err := e.NewRuntimeByGoCode(`
package main

import "strings"

const Base = 10

type Box struct {
	V int
}

func (b Box) OpAdd(other Box) Box {
	return Box{V: b.V + other.V}
}

func MakeBox(v int) Box {
	return Box{V: v}
}

func Factorial(n int) int {
	if n <= 1 {
		return 1
	}
	return n * Factorial(n-1)
}

func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		expr string
		env  map[string]interface{}
		want interface{}
	}{
		{name: "operator_overload", expr: "(MakeBox(2) + MakeBox(3)).V", want: int64(5)},
		{name: "env", expr: "MakeBox(x).V", env: map[string]interface{}{"x": int64(7)}, want: int64(7)},
		{name: "recursive_function", expr: "Factorial(5)", want: int64(120)},
		{name: "template", expr: "bump(41)", want: int64(42)},
		{name: "ffi_import", expr: `strings.ToUpper("go")`, want: "GO"},
		{name: "source_constant", expr: "Base + 5", want: int64(15)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := prog.Eval(context.Background(), tt.expr, tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 {
				t.Fatalf("Eval() returned %d values, want 1", len(results))
			}
			if got := results[0].Interface(); got != tt.want {
				t.Fatalf("Eval() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestExecutableEvalUsesRegisteredModuleAfterBytecodeLoad(t *testing.T) {
	mathxSource := `
	package mathx

	func Double(v int) int {
		return v * 2
	}
	`
	compilerExec := engine.MustNewMiniExecutor()
	if err := compilerExec.UseSurface(surface.Library("mathx", surface.GoFile("mathx.mgo", mathxSource))); err != nil {
		t.Fatal(err)
	}
	payload, err := compilerExec.CompileGoCodeToBytecodeJSON(`
package main

import "mathx"

func Use(v int) int {
	return mathx.Double(v)
}

func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}

	loader := engine.MustNewMiniExecutor()
	if err := loader.UseSurface(surface.Library("mathx", surface.GoFile("mathx.mgo", mathxSource))); err != nil {
		t.Fatal(err)
	}
	prog, err := loader.NewRuntimeByBytecodeJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	results, err := prog.Eval(context.Background(), "mathx.Double(21) + Use(1)", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Eval() returned %d values, want 1", len(results))
	}
	if got := results[0].I64; got != 44 {
		t.Fatalf("Eval() = %d, want 44", got)
	}
}

func TestEvalRejectsImplicitHostStructConversion(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	_, err := executor.Eval(context.Background(), "u", map[string]interface{}{"u": struct{ Name string }{Name: "Bob"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported host value") {
		t.Fatalf("expected unsupported host value error, got %v", err)
	}
}

func TestEvalRuntimeFailureEscapesAsAPIError(t *testing.T) {
	e := engine.MustNewMiniExecutor()

	prog, err := e.NewRuntimeByGoCode(`
package main

func boom() Int64 {
	panic("boom")
}
`)
	if err != nil {
		t.Fatal(err)
	}

	results, err := prog.Eval(context.Background(), "boom()", nil)
	if err == nil {
		t.Fatal("expected Eval to return API error")
	}
	if len(results) != 0 {
		t.Fatalf("expected no result values on runtime failure, got %d", len(results))
	}
}
