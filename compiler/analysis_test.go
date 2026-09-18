package compiler_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestAnalyzeTransitiveInterfaceMetadataAndInvalidation(t *testing.T) {
	sources := func(methodType, privateBody string) workspace.SourceSet {
		set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "contract", Files: []source.File{{Path: "contract.mgo", Text: "package contract\ntype Base interface { Write(" + methodType + ") }\ntype Writer interface { Base }\ntype Alias = Writer\nfunc private() int { return " + privateBody + " }"}}},
			{ModulePath: "api", Files: []source.File{{Path: "api.mgo", Text: "package api\nimport \"contract\"\nfunc Accept(value contract.Alias) {}"}}},
			{ModulePath: "main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"api\"\ntype Writer struct {}\nfunc (*Writer) Write(value int) {}\nfunc main() { var value Writer; api.Accept(&value) }"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	request := compiler.AnalysisRequest{Request: compiler.Request{Root: "main", Sources: sources("int", "1")}}
	first, err := compiler.Analyze(request)
	if err != nil || source.HasErrors(first.Diagnostics) {
		t.Fatalf("Analyze: %v, %v", err, first.Diagnostics)
	}
	checked, err := compiler.Check(request.Request)
	if err != nil || !checked.OK() {
		t.Fatalf("Check: %v, %v", err, checked.Diagnostics)
	}
	compiled, err := compiler.Compile(request.Request)
	if err != nil || !compiled.OK() {
		t.Fatalf("Compile: %v, %v", err, compiled.Diagnostics)
	}
	request.Previous = &first
	request.Request.Sources = sources("int", "2")
	private, err := compiler.Analyze(request)
	if err != nil || source.HasErrors(private.Diagnostics) || private.Packages["main"].Checked.Info != first.Packages["main"].Checked.Info {
		t.Fatalf("private implementation invalidated public facts: %v, %v", err, private.Diagnostics)
	}
	request.Request.Sources = sources("string", "2")
	changed, err := compiler.Analyze(request)
	if err != nil || !source.HasErrors(changed.Diagnostics) || changed.DependencyHashes["main"] == first.DependencyHashes["main"] {
		t.Fatalf("transitive interface change retained stale facts: %v, %v", err, changed.Diagnostics)
	}
	compiled, err = compiler.Compile(request.Request)
	if err != nil || !reflect.DeepEqual(compiled.Diagnostics, changed.Diagnostics) {
		t.Fatalf("diagnostics diverged: Compile=%v, Analyze=%v, err=%v", compiled.Diagnostics, changed.Diagnostics, err)
	}
}

func TestAnalyzeReusesUnchangedPackagesAndInvalidatesDependencies(t *testing.T) {
	sources := func(resultType string) workspace.SourceSet {
		set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Value() " + resultType + " { panic(0) }"}}},
			{ModulePath: "main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"lib\"\nfunc main() { var n int = lib.Value(); _ = n }"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	request := compiler.AnalysisRequest{Request: compiler.Request{Root: "main", Sources: sources("int")}}
	first, err := compiler.Analyze(request)
	if err != nil || source.HasErrors(first.Diagnostics) {
		t.Fatalf("first: %v, %v", err, first.Diagnostics)
	}
	request.Previous = &first
	warm, err := compiler.Analyze(request)
	if err != nil || warm.Packages["main"].Checked.Info != first.Packages["main"].Checked.Info || warm.Stats.PackagesAnalyzed != 0 {
		t.Fatalf("unchanged snapshot was not reused: %v, %+v", err, warm.Stats)
	}
	request.Request.Sources = sources("string")
	changed, err := compiler.Analyze(request)
	if err != nil || changed.DependencyHashes["main"] == first.DependencyHashes["main"] || changed.Packages["main"].Checked.Info == first.Packages["main"].Checked.Info {
		t.Fatalf("changed dependency retained old facts: %v, %v", err, changed.Diagnostics)
	}
	if !source.HasErrors(changed.Diagnostics) || changed.Diagnostics[0].Code != "semantic.assign.type" {
		t.Fatalf("changed result type was not checked: %v", changed.Diagnostics)
	}
	request.Request.Sources = sources("int")
	request.Request.Target = target.Target{Tags: []string{"custom"}}
	retargeted, err := compiler.Analyze(request)
	if err != nil || retargeted.Packages["main"].Checked.Info == first.Packages["main"].Checked.Info {
		t.Fatalf("target reuse: %v", err)
	}
	request.Request.Target = target.Target{}
	request.Request.Limits = compiler.Limits{MaxASTNodes: 2}
	limited, err := compiler.Analyze(request)
	if err != nil || !source.HasErrors(limited.Diagnostics) {
		t.Fatalf("limits were bypassed: %v, %v", err, limited.Diagnostics)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request.Request.Context = ctx
	if _, err := compiler.Analyze(request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestCheckAndCompileReportSourceDiagnostics(t *testing.T) {
	for _, statement := range []string{
		`import "lib"`,
		`import lib "lib"; var lib = 1`,
		`import lib "lib"; import lib "lib"`,
		`type A = []A`,
		`type A struct { Next A }`,
		`type A map[[]int]int`,
		`type A [...]int`,
		`type A interface { ~int }; var value A`,
		`type A interface { M() int }; type B interface { M() string }; type C interface { A; B }`,
		`type A = int; func (A) M() {}`,
		`func value() string { return "x" }; var n int = value()`,
		`func value() { n := 0; n := 1; _ = n }`,
	} {
		t.Run(statement, func(t *testing.T) {
			set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
				{ModulePath: "lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 1 }"}}},
				{ModulePath: "main", Files: []source.File{{Path: "main.mgo", Text: "package main\n" + statement + "\nfunc main() {}"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := compiler.Request{Root: "main", Sources: set, Cache: cache.New(cache.NewMemoryBackend())}
			checked, err := compiler.Check(request)
			if err != nil || checked.OK() {
				t.Fatalf("Check: %v, %v", err, checked.Diagnostics)
			}
			compiled, err := compiler.Compile(request)
			if err != nil || !reflect.DeepEqual(compiled.Diagnostics, checked.Diagnostics) {
				t.Fatalf("Compile: %v, %v; Check: %v", err, compiled.Diagnostics, checked.Diagnostics)
			}
		})
	}
}

func TestAnalyzeReusedDiagnosticsHonorRecoveryMode(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "a", Files: []source.File{{Path: "a.mgo", Text: "package a\nfunc A() { missing() }"}}},
		{ModulePath: "b", Files: []source.File{{Path: "b.mgo", Text: "package b\nfunc B() {}"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := compiler.AnalysisRequest{Request: compiler.Request{Sources: sources}, Roots: []string{"a", "b"}, ContinueAfterErrors: true}
	first, err := compiler.Analyze(request)
	if err != nil || !source.HasErrors(first.Diagnostics) || first.Packages["b"].Checked.Info == nil {
		t.Fatalf("recovery: %v, %+v", err, first)
	}
	request.Previous = &first
	warm, err := compiler.Analyze(request)
	if err != nil || !reflect.DeepEqual(warm.Diagnostics, first.Diagnostics) || warm.Packages["b"].Checked.Info != first.Packages["b"].Checked.Info {
		t.Fatalf("reused recovery: %v, %v", err, warm.Diagnostics)
	}
	request.ContinueAfterErrors = false
	stopped, err := compiler.Analyze(request)
	if err != nil || !source.HasErrors(stopped.Diagnostics) || stopped.Packages["b"].Checked.Info != nil {
		t.Fatalf("stop after reused error: %v, %+v", err, stopped)
	}
	request.Previous = nil
	coldStopped, err := compiler.Analyze(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Previous = &coldStopped
	request.ContinueAfterErrors = true
	resumed, err := compiler.Analyze(request)
	if err != nil || !reflect.DeepEqual(resumed.ExportHashes, first.ExportHashes) || !reflect.DeepEqual(resumed.Diagnostics, first.Diagnostics) {
		t.Fatalf("recovery after stopped snapshot: %v, %+v", err, resumed)
	}
}
