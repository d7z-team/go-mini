package tests

import (
	"context"
	"reflect"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestEval(t *testing.T) {
	executor := engine.NewMiniExecutor()

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
			got, err := executor.Eval(context.Background(), tt.expr, tt.env)
			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				gotVal := got.Interface()
				if !reflect.DeepEqual(gotVal, tt.want) {
					t.Errorf("Eval() = %v, want %v", gotVal, tt.want)
				}
			}
		})
	}
}

func TestEvalByteCopy(t *testing.T) {
	executor := engine.NewMiniExecutor()

	original := []byte("hello")
	env := map[string]interface{}{
		"b": original,
	}

	res, err := executor.Eval(context.Background(), "b", env)
	if err != nil {
		t.Fatal(err)
	}

	// 修改原始数据
	original[0] = 'H'

	// 验证 VM 内部数据未变
	got := res.Interface().([]byte)
	if string(got) != "hello" {
		t.Errorf("Data leaked! Expected 'hello', got %q", string(got))
	}
}

func TestEvalStructInjection(t *testing.T) {
	executor := engine.NewMiniExecutor()

	type Metadata struct {
		ID int `json:"id"`
	}
	type User struct {
		Name     string   `json:"user_name"`
		Age      int      `json:"age"`
		Tags     []string `json:"tags"`
		Meta     Metadata `json:"meta"`
		Internal string   // 这种私有字段应该被忽略
	}

	u := User{
		Name:     "Bob",
		Age:      25,
		Tags:     []string{"dev", "go"},
		Meta:     Metadata{ID: 999},
		Internal: "secret",
	}

	env := map[string]interface{}{
		"u": u,
	}

	t.Run("Access basic field", func(t *testing.T) {
		res, err := executor.Eval(context.Background(), "u.user_name", env)
		if err != nil {
			t.Fatal(err)
		}
		if res.Str != "Bob" {
			t.Errorf("Expected 'Bob', got %q", res.Str)
		}
	})

	t.Run("Access nested field", func(t *testing.T) {
		res, err := executor.Eval(context.Background(), "u.meta.id", env)
		if err != nil {
			t.Fatal(err)
		}
		if res.I64 != 999 {
			t.Errorf("Expected 999, got %d", res.I64)
		}
	})

	t.Run("Access slice in struct", func(t *testing.T) {
		res, err := executor.Eval(context.Background(), "u.tags[0]", env)
		if err != nil {
			t.Fatal(err)
		}
		if res.Str != "dev" {
			t.Errorf("Expected 'dev', got %q", res.Str)
		}
	})
}

func TestEvalAfterExecute(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.InjectStandardLibraries()

	code := `
package main
import "strings"

var globalVar = "initialized"

func test() string {
	return strings.ToUpper(globalVar)
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
	res, err := prog.Eval(context.Background(), "test()", nil)
	if err != nil {
		t.Errorf("Eval after Execute failed: %v", err)
		return
	}

	if res.Str != "INITIALIZED" {
		t.Errorf("Expected 'INITIALIZED', got %q", res.Str)
	}
}

func TestNormalizeFixedSizeByteArray(t *testing.T) {
	e := engine.NewMiniExecutor()
	prog, _ := e.NewRuntimeByGoCode("package main")

	data := [4]byte{1, 2, 3, 4}
	res, err := prog.Eval(context.Background(), "d", map[string]interface{}{"d": data})
	if err != nil {
		t.Fatal(err)
	}

	got := res.Interface().([]byte)
	if len(got) != 4 || got[0] != 1 || got[3] != 4 {
		t.Errorf("Expected []byte{1,2,3,4}, got %v", got)
	}
}
