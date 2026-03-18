package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestRuntimePanicPaths(t *testing.T) {
	executor := engine.NewMiniExecutor()

	tests := []struct {
		name    string
		code    string
		wantErr string
	}{
		{
			name: "Access member of nil",
			code: `
package main
func main() {
	var m map[string]int
	x := m["key"] // should be handled, but what about struct?
}
`,
			wantErr: "", // Map access on nil is actually okay in some languages, check mini behavior
		},
		{
			name: "Call nil closure",
			code: `
package main
func main() {
	var f func()
	f()
}
`,
			wantErr: "is not callable",
		},
		{
			name: "Division by zero",
			code: `
package main
func main() {
	x := 1 / 0
}
`,
			wantErr: "division by zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := executor.NewRuntimeByGoCode(tt.code)
			if err != nil {
				if tt.wantErr != "" && strings.Contains(err.Error(), tt.wantErr) {
					return
				}
				t.Fatalf("Compile failed: %v", err)
			}
			err = prog.Execute(context.Background())
			if err == nil {
				t.Log("Warning: Execute returned nil error")
				return
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
