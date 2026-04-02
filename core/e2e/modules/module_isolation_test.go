package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

func TestModuleInitFailureDoesNotPolluteParentSession(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		switch path {
		case "broken":
			code := `
			package broken

			var Exported = "partial"
			var Trigger = 1 / 0
			`
			converter := ffigo.NewGoToASTConverter()
			node, err := converter.ConvertSource("snippet", code)
			if err != nil {
				return nil, err
			}
			return node.(*ast.ProgramStmt), nil
		default:
			return nil, fmt.Errorf("module not found: %s", path)
		}
	})

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "broken"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected broken module init to fail")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	session := runtime.LastSession()
	if session == nil {
		t.Fatal("expected last session")
	}
	if _, ok := session.ModuleCache["broken"]; ok {
		t.Fatalf("broken module should not be committed into cache: %#v", session.ModuleCache["broken"])
	}
	if session.LoadingModules["broken"] {
		t.Fatal("broken module should not remain in loading set")
	}
}

func TestModuleInitPanicFunctionDoesNotPolluteParentSession(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		switch path {
		case "panicmod":
			code := `
			package panicmod

			func fail() string {
				panic("boom")
			}

			var Exported = "partial"
			var Trigger = fail()
			`
			converter := ffigo.NewGoToASTConverter()
			node, err := converter.ConvertSource("snippet", code)
			if err != nil {
				return nil, err
			}
			return node.(*ast.ProgramStmt), nil
		default:
			return nil, fmt.Errorf("module not found: %s", path)
		}
	})

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "panicmod"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected panicing module init to fail")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	session := runtime.LastSession()
	if session == nil {
		t.Fatal("expected last session")
	}
	if _, ok := session.ModuleCache["panicmod"]; ok {
		t.Fatalf("panicmod should not be committed into cache: %#v", session.ModuleCache["panicmod"])
	}
	if session.LoadingModules["panicmod"] {
		t.Fatal("panicmod should not remain in loading set")
	}
}

func TestTransitivePartialInitDoesNotPolluteImporterChain(t *testing.T) {
	executor := engine.NewMiniExecutor()

	executor.SetModuleLoader(func(path string) (*ast.ProgramStmt, error) {
		var code string
		switch path {
		case "childbroken":
			code = `
			package childbroken

			var Exported = "child-partial"
			var Trigger = 1 / 0
			`
		case "parentbroken":
			code = `
			package parentbroken
			import "childbroken"

			var ParentExported = "parent-partial"
			var ChildValue = childbroken.Exported
			`
		default:
			return nil, fmt.Errorf("module not found: %s", path)
		}
		converter := ffigo.NewGoToASTConverter()
		node, err := converter.ConvertSource("snippet", code)
		if err != nil {
			return nil, err
		}
		return node.(*ast.ProgramStmt), nil
	})

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "parentbroken"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected transitive partial-init module chain to fail")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	session := runtime.LastSession()
	if session == nil {
		t.Fatal("expected last session")
	}
	if _, ok := session.ModuleCache["childbroken"]; ok {
		t.Fatalf("childbroken should not be committed into cache: %#v", session.ModuleCache["childbroken"])
	}
	if _, ok := session.ModuleCache["parentbroken"]; ok {
		t.Fatalf("parentbroken should not be committed into cache: %#v", session.ModuleCache["parentbroken"])
	}
	if session.LoadingModules["childbroken"] {
		t.Fatal("childbroken should not remain in loading set")
	}
	if session.LoadingModules["parentbroken"] {
		t.Fatal("parentbroken should not remain in loading set")
	}
}
