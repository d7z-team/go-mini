package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

const testingPackageSource = `package testing
type T struct{}
type InternalTest struct { Name string; F func(*T) }
type Report struct { Passed bool }
func Main(name string, tests []InternalTest) Report { return Report{Passed: true} }
func (t *T) Fatalf(format string, args ...any) {}
`

func TestPrepareTestSelectsInternalAndExternalTestsByBuildTags(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\n"}},
			TestFiles: []source.File{
				{Path: "debug_test.mgo", Text: "//go:build debug\n\npackage value\nimport \"testing\"\nfunc TestDebug(t *testing.T) {}\n"},
				{Path: "release_test.mgo", Text: "//go:build !debug\n\npackage value\nimport \"testing\"\nfunc TestRelease(t *testing.T) {}\n"},
				{Path: "external_test.mgo", Text: "//go:build debug\n\npackage value_test\nimport \"testing\"\nfunc TestExternal(t *testing.T) {}\n"},
			},
		},
		{
			ModulePath: "testing",
			Files:      []source.File{{Path: "testing.mgo", Text: testingPackageSource}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Target: target.Target{Tags: []string{"debug"}}, Sources: sources})
	if err != nil || !result.Checked.OK() || result.Image == nil {
		t.Fatalf("prepare tagged tests: result=%#v err=%v", result, err)
	}
	if len(result.TestManifest) != 2 || result.TestManifest[0].Name != "TestDebug" || result.TestManifest[1].Name != "TestExternal" {
		t.Fatalf("test manifest = %#v", result.TestManifest)
	}
	if got := result.Image.Target.Tags; len(got) != 2 || got[0] != "debug" || got[1] != "minigo" {
		t.Fatalf("image target = %#v", got)
	}
}

func TestPrepareTestPreservesBuildConstraintDiagnostic(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "//go:build (\n\npackage value\n"}},
			TestFiles:  []source.File{{Path: "value_test.mgo", Text: "package value\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\n"}},
		},
		{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingPackageSource}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked.OK() || len(result.Checked.Diagnostics) == 0 || result.Checked.Diagnostics[0].Code != "compiler.target.constraint" {
		t.Fatalf("build constraint diagnostics = %#v", result.Checked.Diagnostics)
	}
}

func TestPrepareTestBuildsWorkspaceTestVariant(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\nfunc Value() int { return 1 }\n"}},
			TestFiles: []source.File{
				{Path: "value_test.mgo", Text: "package value\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 1 { t.Fatalf(\"bad value\") } }\n"},
				{Path: "external_test.mgo", Text: "package value_test\nimport (\n\t\"example/value\"\n\t\"testing\"\n)\nfunc TestExternal(t *testing.T) { if value.Value() != 1 { t.Fatalf(\"bad value\") } }\n"},
			},
		},
		{
			ModulePath: "testing",
			Files:      []source.File{{Path: "testing.mgo", Text: testingPackageSource}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := cache.NewMemoryBackend()
	request := compiler.Request{Root: "example/value", Sources: sources, Cache: cache.New(backend)}
	result, err := compiler.PrepareTest(request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Checked.OK() || result.Image == nil {
		t.Fatalf("prepare test result = %#v", result)
	}
	entry, ok := result.Image.DefaultEntry()
	if !ok || entry.FunctionID != "fn.MiniGoTestMain" {
		t.Fatalf("test entry = %#v", entry)
	}
	if len(result.TestManifest) != 2 || result.TestManifest[0].Name != "TestValue" || result.TestManifest[0].Index != 0 || result.TestManifest[1].Name != "TestExternal" || result.TestManifest[1].Index != 1 {
		t.Fatalf("test manifest = %#v", result.TestManifest)
	}
	if result.Checked.Stats.TestsDiscovered != len(result.TestManifest) {
		t.Fatalf("test discovery stats = %#v", result.Checked.Stats)
	}
	foundSelection := false
	for _, candidate := range result.Image.Entries {
		foundSelection = foundSelection || candidate.Name == compiler.TestSelectionEntry
	}
	if !foundSelection {
		t.Fatalf("test image entries = %#v", result.Image.Entries)
	}
	warm, err := compiler.PrepareTest(request)
	if err != nil || warm.Image == nil || warm.Image.Hash != result.Image.Hash {
		t.Fatalf("warm prepare test: result=%#v err=%v", warm, err)
	}
	if warm.Checked.Stats.PackagesParsed != 0 || warm.Checked.Stats.PackagesAnalyzed != 0 || warm.Checked.Stats.PackagesLowered != 0 || warm.Checked.Stats.PackagesCompiled != 0 {
		t.Fatalf("warm test build did unnecessary front-end work: %#v", warm.Checked.Stats)
	}
	if warm.Checked.Stats.PrepareCacheHits != 1 || warm.Checked.Stats.ImagesLinked != 0 {
		t.Fatalf("warm test build relinked image: %#v", warm.Checked.Stats)
	}
}

func TestPrepareTestSeparatesInternalVariantFromProductionImports(t *testing.T) {
	testingSource := strings.Replace(testingPackageSource, "package testing\n", "package testing\nimport _ \"example/helper\"\n", 1)
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\nfunc Value() int { return 1 }\n"}},
			TestFiles:  []source.File{{Path: "value_test.mgo", Text: "package value\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 1 { t.Fatalf(\"bad value\") } }\n"}},
		},
		{ModulePath: "example/helper", Files: []source.File{{Path: "helper.mgo", Text: "package helper\nimport \"example/value\"\nfunc Read() int { return value.Value() }\n"}}},
		{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingSource}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Sources: sources})
	if err != nil || !result.Checked.OK() || result.Image == nil {
		t.Fatalf("prepare test variant: result=%#v err=%v", result, err)
	}
}

func TestPrepareTestProvidesResourcesToExternalPackage(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\n"}},
			TestFiles: []source.File{{Path: "external_test.mgo", Text: `package value_test
import (
	_ "embed"
	"testing"
)
//go:embed data.txt
var data string
func TestExternal(t *testing.T) { if data != "value" { t.Fatalf("bad resource") } }
`}},
			Resources: []workspace.ResourceFile{{Path: "data.txt", Data: []byte("value")}},
		},
		{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingPackageSource}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Sources: sources})
	if err != nil || !result.Checked.OK() || result.Image == nil {
		t.Fatalf("prepare external embed test: result=%#v err=%v", result, err)
	}
}

func TestPrepareTestKeepsProductionPackageCacheSeparate(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{
			ModulePath: "example/value",
			Files:      []source.File{{Path: "value.mgo", Text: "package value\nfunc Value() int { return 1 }\n"}},
			TestFiles:  []source.File{{Path: "value_test.mgo", Text: "package value\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\n"}},
		},
		{
			ModulePath: "example/main",
			Files:      []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/value\"\nfunc main() { _ = value.Value() }\n"}},
		},
		{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingPackageSource}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := cache.New(cache.NewMemoryBackend())
	prepared, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Sources: sources, Cache: store})
	if err != nil || prepared.Image == nil || !prepared.Checked.OK() {
		t.Fatalf("prepare tests: result=%#v err=%v", prepared, err)
	}
	built, err := compiler.Compile(compiler.Request{Root: "example/main", Sources: sources, Cache: store})
	if err != nil || !built.OK() {
		t.Fatalf("compile production package: result=%#v err=%v", built, err)
	}
	if _, ok := built.Artifact("example/value"); !ok {
		t.Fatal("production dependency artifact is missing")
	}
	for _, function := range built.PackageSymbols["example/value"].Functions {
		if function.Name == "TestValue" {
			t.Fatal("test variant was reused as the production package artifact")
		}
	}
}

func TestPrepareTestKeepsGeneratedModuleIdentityStable(t *testing.T) {
	backend := cache.NewMemoryBackend()
	build := func(value int) compiler.PrepareResult {
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "example/value", Files: []source.File{{Path: "value.mgo", Text: "package value\n"}}, TestFiles: []source.File{{Path: "value_test.mgo", Text: fmt.Sprintf("package value\nimport \"testing\"\nfunc TestValue(t *testing.T) { _ = %d }\n", value)}}},
			{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingPackageSource}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := compiler.PrepareTest(compiler.Request{Root: "example/value", Sources: sources, Cache: cache.New(backend)})
		if err != nil || !result.Checked.OK() || result.Image == nil {
			t.Fatalf("prepare tests: result=%#v err=%v", result, err)
		}
		return result
	}
	first, second := build(1), build(2)
	if first.Image.Root != second.Image.Root {
		t.Fatalf("test source edit changed generated module identity: %q != %q", first.Image.Root, second.Image.Root)
	}
	if first.Image.Hash == second.Image.Hash {
		t.Fatal("test source edit did not change execution image")
	}
	if second.Checked.Stats.PackagesParsed != 1 || second.Checked.Stats.PackagesAnalyzed != 1 || second.Checked.Stats.PackagesLowered != 1 || second.Checked.Stats.PackageCacheHits < 2 || second.Checked.Stats.PackageCacheMisses != 1 {
		t.Fatalf("test-only edit rebuilt the wrong packages: %#v", second.Checked.Stats)
	}
}
