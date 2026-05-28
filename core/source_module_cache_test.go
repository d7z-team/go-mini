package engine

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
)

func TestPrepareArtifactModulesDoesNotCommitOnDependencyLoweringFailure(t *testing.T) {
	exec := MustNewMiniExecutor()
	modules := map[string]*ast.ProgramStmt{
		"a": parseModuleForCacheTest(t, "a.mgo", `
package a

import "b"

func Value() int {
	return b.Value()
}
`),
		"b": badLoweringModuleForCacheTest(t),
	}
	for path, module := range modules {
		exec.moduleSources[path] = module
	}

	_, err := exec.CompileGoCode(`
package main

import "a"

func main() {
	if a.Value() != 2 {
		panic("bad value")
	}
}
`)
	if err == nil || !strings.Contains(err.Error(), "compile module b") {
		t.Fatalf("expected dependency compile failure, got %v", err)
	}

	exec.mu.RLock()
	cachedA := exec.modules["a"]
	cachedB := exec.modules["b"]
	exec.mu.RUnlock()
	if cachedA != nil || cachedB != nil {
		t.Fatalf("failed dependency preparation must not commit module cache: a=%v b=%v", cachedA != nil, cachedB != nil)
	}

	modules["b"] = parseModuleForCacheTest(t, "b.mgo", `
package b

func Value() int {
	return 2
}
`)
	exec.moduleSources["b"] = modules["b"]
	if _, err := exec.CompileGoCode(`
package main

import "a"

func main() {
	if a.Value() != 2 {
		panic("bad value")
	}
}
`); err != nil {
		t.Fatalf("compile after fixing dependency: %v", err)
	}

	exec.mu.RLock()
	cachedA = exec.modules["a"]
	cachedB = exec.modules["b"]
	exec.mu.RUnlock()
	if cachedA == nil || cachedB == nil {
		t.Fatalf("successful dependency preparation should commit both modules: a=%v b=%v", cachedA != nil, cachedB != nil)
	}
}

func parseModuleForCacheTest(t *testing.T, filename, code string) *ast.ProgramStmt {
	t.Helper()
	programs, _, err := compiler.ParseSourceFiles([]compiler.SourceFile{{
		Filename: filename,
		Language: (compiler.GoFrontend{}).Language(),
		Code:     code,
	}}, false)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	if len(programs) != 1 || programs[0] == nil {
		t.Fatalf("parse %s produced invalid program", filename)
	}
	return programs[0]
}

func badLoweringModuleForCacheTest(t *testing.T) *ast.ProgramStmt {
	t.Helper()
	program := parseModuleForCacheTest(t, "b.mgo", `
package b

func Value() int {
	return 2
}
`)
	fn := program.Functions["Value"]
	fn.Body.Children = append(fn.Body.Children, &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{Meta: "program"},
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
	})
	return program
}
