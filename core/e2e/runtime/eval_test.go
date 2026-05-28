package tests

import (
	"context"
	"reflect"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
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
				if !reflect.DeepEqual(gotVal, tt.want) {
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

	// 修改原始数据
	original[0] = 'H'

	// 验证 VM 内部数据未变
	got := res.Interface().([]byte)
	if string(got) != "hello" {
		t.Errorf("Data leaked! Expected 'hello', got %q", string(got))
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

	// 1. Execute the program to initialize globals and imports
	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 2. Eval a function that uses those globals and imports
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
