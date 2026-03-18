package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestSemanticValidationErrors(t *testing.T) {
	executor := engine.NewMiniExecutor()

	tests := []struct {
		name    string
		code    string
		wantErr string
	}{
		{
			name: "Missing return in all paths",
			code: `
package main
func test() int {
	x := 1
}
`,
			wantErr: "缺少返回语句",
		},
		{
			name: "Missing return in one branch",
			code: `
package main
func test(b bool) int {
	if b {
		return 1
	}
	// missing return here
}
`,
			wantErr: "缺少返回语句",
		},
		{
			name: "Break outside loop",
			code: `
package main
func main() {
	break
}
`,
			wantErr: "break 语句只能在循环中使用",
		},
		{
			name: "Continue outside loop",
			code: `
package main
func main() {
	continue
}
`,
			wantErr: "continue 语句只能在循环中使用",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.NewRuntimeByGoCode(tt.code)
			if err == nil {
				t.Error("Expected validation error but got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestRuntimeLogicErrors(t *testing.T) {
	executor := engine.NewMiniExecutor()

	tests := []struct {
		name    string
		code    string
		wantErr string
	}{
		{
			name: "Bitwise on non-int",
			code: `
package main
func main() {
	x := 1.5 & 2
}
`,
			wantErr: "expected Int64",
		},
		{
			name: "Arithmetic on strings",
			code: `
package main
func main() {
	x := "a" - "b"
}
`,
			wantErr: "arithmetic operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				// Validation might catch some, but we want to test runtime if possible
				if strings.Contains(err.Error(), tt.wantErr) {
					return
				}
				t.Fatalf("Compile failed unexpectedly: %v", err)
			}
			err = prog.Execute(context.Background())
			if err == nil {
				t.Error("Expected runtime error but got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
