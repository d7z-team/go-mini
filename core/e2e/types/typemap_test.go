package tests

import (
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTypeMapRobustness(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	tests := []struct {
		name string
		code string
	}{
		{
			name: "NestedMapAccess",
			code: `
			package main
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
			}
			`,
		},
		{
			name: "MissingFieldReturnsNil",
			code: `
			package main
			func main() {
				data := map[string]any{"a": 1}
				if data.b != nil {
					panic("missing field should be nil")
				}
			}
			`,
		},
		{
			name: "ResultMapAccess",
			code: `
			package main
			func main() {
				val := map[string]any{"meta": map[string]any{"code": 200}}
				if val.meta.code != 200 {
					panic("result map access failed")
				}
			}
			`,
		},
		{
			name: "MixedAccess",
			code: `
			package main
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
			}
			`,
		},
		{
			name: "AnyWrappedScalarMemberAccess",
			code: `
			package main
			func main() {
				var a any = 123
				if a.something != nil {
					panic("scalar member access should be nil")
				}
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
			err = prog.Execute(t.Context())
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
		})
	}
}
