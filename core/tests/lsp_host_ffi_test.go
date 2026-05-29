package engine_test

import (
	"errors"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

func TestLSPHostFFICompletion(t *testing.T) {
	testExecutor := engine.MustNewMiniExecutor()
	schema := runtime.NewFFISurfaceSchema()
	if err := schema.AddStruct("hostfs", "File", runtime.MustParseRuntimeStructSpec("hostfs.File", runtime.StructOwnershipHostOpaque, "struct { Read function(HostRef<hostfs.File>, TypeBytes) tuple(Int64, Error); Close function(HostRef<hostfs.File>) Error; }")); err != nil {
		t.Fatal(err)
	}
	if err := schema.AddRouteDecls([]runtime.FFIRouteDecl{
		testsurface.Route("hostfs.Open", 1, runtime.MustParseRuntimeFuncSig("function(String) tuple(HostRef<hostfs.File>, Error)"), ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := testExecutor.UseSurface(testsurface.SchemaBundle(schema, nil)); err != nil {
		t.Fatal(err)
	}

	sourceSnippet := `package main
import "hostfs"

func main() {
    f, err := hostfs.Open("test.txt")
    if err == nil {
        f.Read(make([]byte, 1024))
        f.Close()
    }
}`

	testProgram, parseErrs := testExecutor.AnalyzeGoCodeTolerant(sourceSnippet)
	if testProgram == nil {
		t.Fatal("failed to get program")
	}

	// 验证是否有语义错误 (排除语法错误)
	for _, parseErr := range parseErrs {
		var astErr *ast.MiniAstError
		if errors.As(parseErr, &astErr) {
			t.Errorf("Unexpected semantic error: %v", parseErr)
		}
	}

	// 2. 验证包成员补全 (hostfs.)
	t.Run("OS_PackageCompletion", func(t *testing.T) {
		// 光标在 hostfs.Open 的点号之后
		completionItems := testProgram.GetCompletionsAt(5, 18)
		foundOpen := false
		for _, item := range completionItems {
			if item.Label == "Open" && item.Kind == "func" {
				foundOpen = true
				break
			}
		}
		if !foundOpen {
			t.Error("missing hostfs.Open completion")
			for _, item := range completionItems {
				t.Logf("- %s (%s)", item.Label, item.Kind)
			}
		}
	})

	// 3. 验证 FFI 结构体成员补全 (f.)
	t.Run("FFI_StructCompletion", func(t *testing.T) {
		// 光标在 f.Read 的点号之后 (7, 11)
		completionItems := testProgram.GetCompletionsAt(7, 11)
		foundRead := false
		foundClose := false
		for _, item := range completionItems {
			if item.Label == "Read" && item.Kind == "method" {
				foundRead = true
			}
			if item.Label == "Close" && item.Kind == "method" {
				foundClose = true
			}
		}
		if !foundRead {
			t.Error("missing f.Read completion")
		}
		if !foundClose {
			t.Error("missing f.Close completion")
		}
		if !foundRead || !foundClose {
			for _, item := range completionItems {
				t.Logf("- %s (%s) Type: %s", item.Label, item.Kind, item.Type)
			}
		}
	})
}
