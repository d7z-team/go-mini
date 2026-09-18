package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompileCacheActionUsesStructuredPackageIdentity(t *testing.T) {
	actionFor := func(namespace string) (string, string) {
		header := workspace.PackageHeader{
			Source: workspace.SourcePackage{
				ID:         workspace.PackageID{Namespace: namespace, Path: "main"},
				ModulePath: "example/main",
			},
			Package: "main",
		}
		action, key, err := workspaceCacheAction(header, nil, OptimizationDefault)
		if err != nil {
			t.Fatal(err)
		}
		return action.PackageID, key
	}

	firstID, firstKey := actionFor("workspace:first")
	secondID, secondKey := actionFor("workspace:second")
	if firstID != "workspace:first::main" || secondID != "workspace:second::main" {
		t.Fatalf("compile action package identities = %q, %q", firstID, secondID)
	}
	if firstKey == secondKey {
		t.Fatal("distinct package namespaces produced the same compile action")
	}
}
