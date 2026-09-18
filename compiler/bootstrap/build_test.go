package bootstrap

import (
	"testing"
	"testing/fstest"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type bootstrapTestBuild struct {
	compiled compiler.Result
	graph    workspace.HeaderGraph
}

func compileBootstrapTest(t *testing.T, filesystem fstest.MapFS, root string) bootstrapTestBuild {
	t.Helper()
	sources, session, err := newCompilerSession(BuildOptions{Filesystem: filesystem})
	if err != nil {
		t.Fatal(err)
	}
	graph, err := workspace.LoadHeaders(root, sources, target.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Diagnostics) != 0 {
		return bootstrapTestBuild{
			compiled: compiler.Result{Target: graph.Target, Diagnostics: graph.Diagnostics, GraphHash: graph.Hash},
			graph:    graph,
		}
	}
	compiled, err := session.Compile(root)
	if err != nil {
		t.Fatal(err)
	}
	return bootstrapTestBuild{compiled: compiled, graph: graph}
}

func TestBootstrapDiscoversReachableClosure(t *testing.T) {
	filesystem := fstest.MapFS{
		"compiler/entry/entry.go":      {Data: []byte("package entry\nimport \"github.com/d7z-team/mini-go/compiler/library\"\nfunc Execute() int { return library.Value() }\n")},
		"compiler/library/library.go":  {Data: []byte("package library\nfunc Value() int { return 1 }\n")},
		"compiler/unreachable/host.go": {Data: []byte("package unreachable\nimport \"unsafe\"\n")},
		"runtime/bytecode/doc.go":      {Data: []byte("package bytecode\n")},
		"tooling/doc.go":               {Data: []byte("package tooling\n")},
	}
	result := compileBootstrapTest(t, filesystem, compilerModulePath+"/entry")
	if !result.compiled.OK() {
		t.Fatalf("bootstrap diagnostics = %#v", result.compiled.Diagnostics)
	}
	if _, ok := result.graph.Packages[compilerModulePath+"/library"]; !ok {
		t.Fatal("reachable package was not discovered")
	}
	if _, ok := result.graph.Packages[compilerModulePath+"/unreachable"]; ok {
		t.Fatal("unreachable package entered bootstrap closure")
	}
}

func TestBootstrapReportsReachableHostImport(t *testing.T) {
	filesystem := fstest.MapFS{
		"compiler/entry/entry.go": {Data: []byte("package entry\nimport _ \"unsafe\"\n")},
		"runtime/bytecode/doc.go": {Data: []byte("package bytecode\n")},
		"tooling/doc.go":          {Data: []byte("package tooling\n")},
	}
	result := compileBootstrapTest(t, filesystem, compilerModulePath+"/entry")
	if result.compiled.OK() {
		t.Fatal("reachable unresolved import has no diagnostic")
	}
}

func TestBootstrapAppliesMiniGoBuildConstraint(t *testing.T) {
	filesystem := fstest.MapFS{
		"compiler/entry/entry.go":      {Data: []byte("//go:build minigo\n\npackage entry\nfunc Execute() int { return 1 }\n")},
		"compiler/entry/entry_host.go": {Data: []byte("//go:build !minigo\n\npackage entry\nimport \"unsafe\"\n")},
		"runtime/bytecode/doc.go":      {Data: []byte("package bytecode\n")},
		"tooling/doc.go":               {Data: []byte("package tooling\n")},
	}
	result := compileBootstrapTest(t, filesystem, compilerModulePath+"/entry")
	if !result.compiled.OK() {
		t.Fatalf("bootstrap diagnostics = %#v", result.compiled.Diagnostics)
	}
}
