package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestFFIStructMethodBindingDoesNotStripNonReceiverFirstParam(t *testing.T) {
	exec := engine.NewMiniExecutor()
	exec.DeclareStructSchema("demo.Table", runtime.MustParseRuntimeStructSpec("demo.Table", "struct { SetString function(Int64, Int64, String) Void; }"))
	exec.DeclareFuncSchema("demo.NewTable", runtime.MustParseRuntimeFuncSig("function() demo.Table"))

	source := `package main
import "demo"

func main() {
	tbl := demo.NewTable()
	tbl.SetString(1, 2, "x")
}`

	prog, errs := exec.NewMiniProgramByGoCodeTolerant(source)
	if prog == nil {
		t.Fatal("expected program")
	}
	for _, err := range errs {
		if _, ok := err.(*ast.MiniAstError); ok {
			t.Fatalf("unexpected semantic error: %v", err)
		}
	}
}
