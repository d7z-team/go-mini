package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestRangeOuterLocalCounterAcrossAllLoaders(t *testing.T) {
	const code = `
package main
import "fmt"

var trace = ""

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	rowScan := 0
	kept := 0

	for _, day := range []Int64{12, 12, 9} {
		rowScan++
		mark("row-" + string(rowScan))
		if day > 9 {
			mark("continue-" + string(rowScan))
			continue
		}
		kept = kept + 1
		mark("keep-" + string(rowScan))
	}
	fmt.Println("rowScan=", rowScan)
	fmt.Println("kept=", kept)
	fmt.Println(trace)
}
`

	loaders := []struct {
		name string
		load func(*engine.MiniExecutor) (*engine.MiniProgram, error)
	}{
		{
			name: "source",
			load: func(exec *engine.MiniExecutor) (*engine.MiniProgram, error) {
				return exec.NewRuntimeByGoCode(code)
			},
		},
		{
			name: "compiled",
			load: func(exec *engine.MiniExecutor) (*engine.MiniProgram, error) {
				compiled, err := exec.CompileGoCode(code)
				if err != nil {
					return nil, err
				}
				return exec.NewRuntimeByCompiled(compiled)
			},
		},
		{
			name: "bytecode_json",
			load: func(exec *engine.MiniExecutor) (*engine.MiniProgram, error) {
				compiled, err := exec.CompileGoCode(code)
				if err != nil {
					return nil, err
				}
				payload, err := compiled.MarshalBytecodeJSON()
				if err != nil {
					return nil, err
				}
				return exec.NewRuntimeByBytecodeJSON(payload)
			},
		},
	}

	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			exec := engine.NewMiniExecutor()
			prog, err := loader.load(exec)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			output := executeWithCapturedOutput(t, prog)
			expected := "rowScan= 3\nkept= 1\nrow-1|continue-1|row-2|continue-2|row-3|keep-3|\n"
			if output != expected {
				t.Fatalf("unexpected output: %q", output)
			}
		})
	}
}
