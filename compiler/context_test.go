package compiler

import (
	"context"
	"errors"
	"testing"

	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompileContextDoesNotChangeCacheIdentity(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(cache.NewMemoryBackend())
	first, err := Compile(Request{Context: context.Background(), Root: "example/main", Sources: sources, Cache: store})
	if err != nil || !first.OK() {
		t.Fatalf("first compile = %#v, %v", first.Diagnostics, err)
	}
	secondContext := context.WithValue(context.Background(), struct{}{}, "different transient context")
	second, err := Compile(Request{Context: secondContext, Root: "example/main", Sources: sources, Cache: store})
	if err != nil || !second.OK() {
		t.Fatalf("second compile = %#v, %v", second.Diagnostics, err)
	}
	if second.Stats.PackageCacheHits != 1 || second.ArtifactHashes["example/main"] != first.ArtifactHashes["example/main"] {
		t.Fatalf("second compile did not reuse the package cache: %#v", second.Stats)
	}
}

func TestCompilerContextStopsBeforeBuild(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(Request{Context: ctx, Root: "example/main", Sources: sources}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Compile canceled context = %v", err)
	}
	session, err := New(Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.PrepareContext(ctx, "example/main", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareContext canceled context = %v", err)
	}
}
