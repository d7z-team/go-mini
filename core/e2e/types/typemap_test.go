package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
)

type typemapOutputRecorder struct {
	sb strings.Builder
}

func (o *typemapOutputRecorder) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

func TestTypeMapRobustness(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	tests := []struct {
		name string
		code string
	}{
		{
			name: "NestedMapAccess",
			code: `
			package main
			import "fmt"
			func main() {
				data := map[string]any{
					"user": map[string]any{
						"profile": map[string]any{
							"name": "dragon",
						},
					},
				}
				if data.user.profile.name != "dragon" {
					panic("nested access failed")
				}
				fmt.Println("Nested access OK")
			}
			`,
		},
		{
			name: "MissingFieldReturnsNil",
			code: `
			package main
			import "fmt"
			func main() {
				data := map[string]any{"a": 1}
				if data.b != nil {
					panic("missing field should be nil")
				}
				fmt.Println("Missing field OK")
			}
			`,
		},
		{
			name: "ResultMapAccess",
			code: `
			package main
			import "encoding/json"
			import "fmt"
			func main() {
				val, err := json.Unmarshal([]byte(` + "`" + `{"meta":{"code":200}}` + "`" + `))
				if err != nil { panic(err) }
				if val.meta.code != 200 {
					panic("result map access failed")
				}
				fmt.Println("Result map access OK")
			}
			`,
		},
		{
			name: "MixedAccess",
			code: `
			package main
			import "fmt"
			func main() {
				data := map[string]any{
					"list": []any{
						map[string]any{"id": 1},
						map[string]any{"id": 2},
					},
				}
				if data.list[1].id != 2 {
					panic("mixed access failed")
				}
				fmt.Println("Mixed access OK")
			}
			`,
		},
		{
			name: "AnyWrappedScalarMemberAccess",
			code: `
			package main
			import "encoding/json"
			import "fmt"
			func main() {
				// 通过 json.Unmarshal 获得一个真正的 Any 类型标量
				a, err := json.Unmarshal([]byte("123"))
				if err != nil { panic(err) }
				if a.something != nil {
					panic("scalar member access should be nil")
				}
				fmt.Println("Scalar member access OK")
			}
			`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				t.Fatalf("failed to create runtime: %v", err)
			}
			recorder := &typemapOutputRecorder{}
			ctx := fmtlib.WithOutputter(context.Background(), recorder)
			err = prog.Execute(ctx)
			if tt.name == "AnyWrappedScalarMemberAccess" {
				if err == nil {
					t.Fatal("expected scalar Any member access to fail")
				}
				if !strings.Contains(err.Error(), "does not support member access") {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("execution failed: %v", err)
				return
			}
			want := map[string]string{
				"NestedMapAccess":         "Nested access OK\n",
				"MissingFieldReturnsNil":  "Missing field OK\n",
				"ResultMapAccess":         "Result map access OK\n",
				"MixedAccess":             "Mixed access OK\n",
			}
			if marker, ok := want[tt.name]; ok && !strings.Contains(recorder.sb.String(), marker) {
				t.Fatalf("expected output marker %q, got %q", marker, recorder.sb.String())
			}
		})
	}
}
