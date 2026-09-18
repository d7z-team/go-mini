package minigo_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
)

func TestEngineAcceptsCustomLibraryAndReportsHostCapabilities(t *testing.T) {
	library := fstest.MapFS{
		"custom/value.mgo": {Data: []byte("package custom\nfunc Value() int { return 42 }\n")},
	}
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/custom",
		Files: []source.File{{Path: "main.mgo", Text: `package main
import "custom"
func Value() int { return custom.Value() }
`}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := minigo.NewStandardLibrary("custom", library, "custom-host")
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := minigo.NewLibrarySet(registered)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := minigo.New(minigo.Config{Sources: sources, Libraries: libraries})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := engine.AvailableHostCapabilities()
	want := append(stdlib.HostCapabilities(), "custom-host")
	slices.Sort(want)
	if !reflect.DeepEqual(capabilities, want) {
		t.Fatalf("host capabilities = %v, want %v", capabilities, want)
	}
	capabilities[0] = "changed"
	if engine.AvailableHostCapabilities()[0] == "changed" {
		t.Fatal("AvailableHostCapabilities exposed mutable engine state")
	}
	result, err := engine.Check("example/custom")
	if err != nil || !result.OK() {
		t.Fatalf("custom library check: result=%#v err=%v", result, err)
	}
	program, result, err := engine.Compile("example/custom", minigo.EntryPoint{Name: "value", Function: "Value"})
	if err != nil || !result.OK() {
		t.Fatalf("custom library compile: result=%#v err=%v", result, err)
	}
	if got := program.RequiredHostCapabilities(); !reflect.DeepEqual(got, []string{"custom-host"}) {
		t.Fatalf("program capabilities = %v", got)
	}
}

func TestLibrarySetUnifiesSharedHostCapabilities(t *testing.T) {
	first, err := minigo.NewStandardLibrary("first", fstest.MapFS{
		"first/first.mgo": {Data: []byte("package first\n")},
	}, "shared")
	if err != nil {
		t.Fatal(err)
	}
	second, err := minigo.NewStandardLibrary("second", fstest.MapFS{
		"second/second.mgo": {Data: []byte("package second\n")},
	}, "shared")
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := minigo.NewLibrarySet(first, second)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example.com/application",
		Files:      []source.File{{Path: "main.mgo", Text: "package application\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := minigo.New(minigo.Config{Sources: sources, Libraries: libraries})
	if err != nil {
		t.Fatal(err)
	}
	want := append(stdlib.HostCapabilities(), "shared")
	slices.Sort(want)
	if got := engine.AvailableHostCapabilities(); !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities = %v, want %v", got, want)
	}
}

func TestEngineAcceptsModuleLibrary(t *testing.T) {
	module, err := minigo.NewModuleLibrary(
		"embedded-dependency", "example.com/dependency",
		fstest.MapFS{
			"value.mgo": {Data: []byte("package dependency\nfunc Value() int { return 42 }\n")},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := minigo.NewLibrarySet(module)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example.com/application",
		Files: []source.File{{Path: "main.mgo", Text: `package application
import "example.com/dependency"
func Value() int { return dependency.Value() }
`}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := minigo.New(minigo.Config{Sources: sources, Libraries: libraries})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check("example.com/application")
	if err != nil || !result.OK() {
		t.Fatalf("module library check: result=%#v err=%v", result, err)
	}
}

func TestEngineCompilesWithoutAdditionalLibraries(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example.com/application",
		Files:      []source.File{{Path: "main.mgo", Text: "package application\nfunc Value() int { return 42 }\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := minigo.New(minigo.Config{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Check("example.com/application")
	if err != nil || !result.OK() {
		t.Fatalf("standalone check: result=%#v err=%v", result, err)
	}
}

func TestEngineRejectsRegisteredPackageOwnershipConflict(t *testing.T) {
	module, err := minigo.NewModuleLibrary("duplicate", "example.com/application", fstest.MapFS{
		"main.mgo": {Data: []byte("package application\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := minigo.NewLibrarySet(module)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example.com/application", Files: []source.File{{Path: "main.mgo", Text: "package application\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := minigo.New(minigo.Config{Sources: sources, Libraries: libraries}); err == nil {
		t.Fatal("engine accepted two owners for one package")
	}
}
