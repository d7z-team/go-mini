package compiler_test

import (
	"encoding/json"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type countingCacheBackend struct {
	inner                    cache.Backend
	actionReads, outputReads int
}

func (b *countingCacheBackend) GetAction(id cache.ActionID) (cache.Entry, bool, error) {
	b.actionReads++
	return b.inner.GetAction(id)
}

func (b *countingCacheBackend) GetOutput(id cache.OutputID) ([]byte, bool, error) {
	b.outputReads++
	return b.inner.GetOutput(id)
}

func (b *countingCacheBackend) PutOutput(id cache.OutputID, data []byte) error {
	return b.inner.PutOutput(id, data)
}

func (b *countingCacheBackend) PutAction(id cache.ActionID, entry cache.Entry) error {
	return b.inner.PutAction(id, entry)
}

type countingSourceSet struct {
	workspace.SourceSet
	pathLoads int
}

func (s *countingSourceSet) PackagePaths() ([]string, error) {
	s.pathLoads++
	return s.SourceSet.PackagePaths()
}

func TestBuildSessionPreparesTestRootsFromOneSnapshot(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/a", Files: []source.File{{Path: "a.mgo", Text: "package a\n"}}, TestFiles: []source.File{{Path: "a_test.mgo", Text: "package a\nimport \"testing\"\nfunc TestA(t *testing.T) {}\n"}}},
		{ModulePath: "example/b", Files: []source.File{{Path: "b.mgo", Text: "package b\n"}}, TestFiles: []source.File{{Path: "b_test.mgo", Text: "package b\nimport \"testing\"\nfunc TestB(t *testing.T) {}\n"}}},
		{ModulePath: "testing", Files: []source.File{{Path: "testing.mgo", Text: testingPackageSource}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	counted := &countingSourceSet{SourceSet: sources}
	backend := &countingCacheBackend{inner: cache.NewMemoryBackend()}
	session, err := compiler.New(compiler.Options{Sources: counted, Cache: cache.New(backend)})
	if err != nil {
		t.Fatal(err)
	}
	results, err := session.PrepareTests([]string{"example/b", "example/a"})
	if err != nil {
		t.Fatal(err)
	}
	if counted.pathLoads != 1 {
		t.Fatalf("source paths loaded %d times", counted.pathLoads)
	}
	for root, testName := range map[string]string{"example/a": "TestA", "example/b": "TestB"} {
		result := results[root]
		if result.Image == nil || !result.Checked.OK() || len(result.TestManifest) != 1 || result.TestManifest[0].Name != testName {
			t.Fatalf("test result for %s = %#v", root, result)
		}
	}
	if results["example/b"].Checked.Stats.PackageCacheHits == 0 {
		t.Fatalf("second test root did not reuse session packages: %#v", results["example/b"].Checked.Stats)
	}
	if backend.outputReads != 0 {
		t.Fatalf("shared package outputs were reread instead of using L1: %d", backend.outputReads)
	}
	if _, err := session.PrepareTests([]string{"example/a", "example/a"}); err == nil {
		t.Fatal("duplicate test root was accepted")
	}
}

func TestBuildSessionKeepsDecodedL1WithSharedCache(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example/generic", Files: []source.File{{Path: "generic.mgo", Text: "package generic\nfunc Double(value int) int { return value * 2 }\nfunc Apply[T ~int](value T) T { return T(Double(int(value))) }\n"}}},
		{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/generic\"\nfunc main() { _ = generic.Apply(21) }\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend := &countingCacheBackend{inner: cache.NewMemoryBackend()}
	session, err := compiler.New(compiler.Options{Sources: sources, Cache: cache.New(backend), Symbols: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, err := session.Prepare("example/main", nil)
	if err != nil || first.Image == nil || !first.Checked.OK() {
		t.Fatalf("first Prepare = %#v, %v", first, err)
	}
	backend.actionReads, backend.outputReads = 0, 0
	second, err := session.Prepare("example/main", nil)
	if err != nil || second.Image == nil || !second.Checked.OK() {
		t.Fatalf("second Prepare = %#v, %v", second, err)
	}
	if backend.actionReads != 0 || backend.outputReads != 0 {
		t.Fatalf("session L1 read shared cache: actions=%d outputs=%d", backend.actionReads, backend.outputReads)
	}
	if second.Checked.Stats.ImagesLinked != 0 {
		t.Fatalf("second Prepare did not reuse decoded image: %#v", second.Checked.Stats)
	}
	want, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	for path, archive := range second.Image.Packages {
		archive.Artifact[0] = '!'
		delete(second.Image.Packages, path)
	}
	second.Image.Entries[0].Name = "modified"
	second.Symbols.Hash = "modified"
	third, err := session.Prepare("example/main", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(third)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("caller mutation changed session-owned prepared state")
	}
}

func TestBuildSessionCanPrepareAfterBuildWithSharedCache(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := compiler.New(compiler.Options{Sources: sources, Cache: cache.New(cache.NewMemoryBackend())})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := session.Compile("example/main"); err != nil || !result.OK() {
		t.Fatalf("Compile = %#v, %v", result.Diagnostics, err)
	}
	prepared, err := session.Prepare("example/main", nil)
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("Prepare = %#v, %v", prepared.Checked.Diagnostics, err)
	}
}

func TestBuildSessionRejectsInvalidRequests(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/main",
		Files:      []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := compiler.New(compiler.Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Check(" "); err == nil {
		t.Fatal("empty root was accepted")
	}
	if _, err := session.PrepareTests(nil); err == nil {
		t.Fatal("empty test root set was accepted")
	}
}
