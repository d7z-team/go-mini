package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestParseOptimizationLevel(t *testing.T) {
	for value := 0; value <= 2; value++ {
		level, err := ParseOptimizationLevel(value)
		if err != nil || int(level) != value {
			t.Fatalf("ParseOptimizationLevel(%d) = %d, %v", value, level, err)
		}
	}
	for _, value := range []int{-1, 3} {
		if _, err := ParseOptimizationLevel(value); err == nil {
			t.Fatalf("ParseOptimizationLevel(%d) succeeded", value)
		}
	}
}

func TestCheckDoesNotDependOnOptimizationLevel(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Root: "example/main", Sources: sources, Optimization: OptimizationFull + 1}
	checked, err := Check(request)
	if err != nil || !checked.OK() {
		t.Fatalf("Check = %#v, %v", checked.Diagnostics, err)
	}
	if _, err := Compile(request); err == nil {
		t.Fatal("Compile accepted an unknown optimization level")
	}
}
