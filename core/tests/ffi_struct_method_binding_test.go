package engine_test

import (
	"errors"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestFFIMethodBindingKeepsFirstParam(t *testing.T) {
	exec := engine.NewMiniExecutor()
	schema := runtime.NewFFISurfaceSchema()
	schema.AddStruct("demo.Table", runtime.MustParseRuntimeStructSpec("demo.Table", runtime.StructOwnershipHostOpaque, "struct { SetString function(HostRef<demo.Table>, Int64, Int64, String) Void; }"))
	schema.AddRouteDecls([]runtime.FFIRouteDecl{
		testsurface.Route("demo.NewTable", 1, runtime.MustParseRuntimeFuncSig("function() HostRef<demo.Table>"), ""),
	})
	if err := exec.UseSurface(testsurface.SchemaBundle(schema, nil)); err != nil {
		t.Fatal(err)
	}

	source := `package main
import "demo"

func main() {
	tbl := demo.NewTable()
	tbl.SetString(1, 2, "x")
}`

	prog, errs := exec.AnalyzeGoCodeTolerant(source)
	if prog == nil {
		t.Fatal("expected program")
	}
	for _, err := range errs {
		var astErr *ast.MiniAstError
		if errors.As(err, &astErr) {
			t.Fatalf("unexpected semantic error: %v", err)
		}
	}
}
