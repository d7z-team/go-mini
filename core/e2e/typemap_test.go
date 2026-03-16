package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

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
			import "json"
			import "fmt"
			func main() {
				res := json.Unmarshal([]byte(` + "`" + `{"meta":{"code":200}}` + "`" + `))
				if res.err != nil { panic(res.err) }
				if res.val.meta.code != 200 {
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
			import "json"
			import "fmt"
			func main() {
				// 通过 json.Unmarshal 获得一个真正的 Any 类型标量
				res := json.Unmarshal([]byte("123"))
				if res.err != nil { panic(res.err) }
				a := res.val
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
			err = prog.Execute(context.Background())
			if err != nil {
				t.Errorf("execution failed: %v", err)
			}
		})
	}
}
