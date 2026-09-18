package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompilerCloseReleasesSessionState(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "test/main",
		Files:      []source.File{source.NewFile("main", "main.mgo", "package main\nfunc main() {}\n")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	frontend, err := New(Options{
		Sources:          sources,
		TraceCache:       func(cache.Event) {},
		HostCapabilities: map[string][]string{"test/main": {"console"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := frontend.Close(); err != nil {
		t.Fatal(err)
	}
	if frontend.options.Sources != nil || frontend.options.Cache != nil || frontend.options.TraceCache != nil ||
		frontend.options.HostCapabilities != nil || frontend.options.Target.Tags != nil {
		t.Fatal("closed compiler retains configuration references")
	}
	if err := frontend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := frontend.Check("test/main"); err == nil || err.Error() != "compiler session is closed" {
		t.Fatalf("check after close = %v", err)
	}
}
