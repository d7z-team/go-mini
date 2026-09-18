package workspace

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

func standardSources(t *testing.T, files ...source.File) SourceSet {
	t.Helper()
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ID:         PackageID{Namespace: "std", Path: "fmt"},
		ModulePath: "fmt",
		Files:      files,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

func TestComposeStandardSourceSetsMergesPackageFiles(t *testing.T) {
	left := standardSources(t, source.NewFile("left", "left.mgo", "package fmt\nfunc Left() {}\n"))
	right := standardSources(t, source.NewFile("right", "right.mgo", "package fmt\nfunc Right() {}\n"))

	combined, err := ComposeStandardSourceSets(left, right)
	if err != nil {
		t.Fatal(err)
	}
	pkg, found, err := combined.Package("fmt")
	if err != nil || !found {
		t.Fatalf("Package failed: found=%t err=%v", found, err)
	}
	if len(pkg.Files) != 2 || pkg.Files[0].Path != "left.mgo" || pkg.Files[1].Path != "right.mgo" {
		t.Fatalf("merged files = %#v", pkg.Files)
	}
}

func TestComposeStandardSourceSetsRejectsConflictingFiles(t *testing.T) {
	left := standardSources(t, source.NewFile("left", "same.mgo", "package fmt\n"))
	right := standardSources(t, source.NewFile("right", "same.mgo", "package fmt\n"))
	combined, err := ComposeStandardSourceSets(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := combined.Package("fmt"); err == nil || !strings.Contains(err.Error(), "duplicate source file") {
		t.Fatalf("conflicting package error = %v", err)
	}
}

func TestComposeStandardSourceSetsRejectsModulePackage(t *testing.T) {
	sources, err := NewMemorySourceSet([]SourcePackage{{
		ID:         PackageID{Namespace: "module:example.com/fmt", Path: "fmt"},
		ModulePath: "fmt",
		Files:      []source.File{source.NewFile("fmt", "fmt.mgo", "package fmt\n")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	combined, err := ComposeStandardSourceSets(sources)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := combined.Package("fmt"); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("module package error = %v", err)
	}
}

func TestComposeStandardSourceSetsMergesPackageMaterial(t *testing.T) {
	left, err := NewMemorySourceSet([]SourcePackage{{
		ID:              PackageID{Namespace: "std", Path: "example"},
		ModulePath:      "example",
		Files:           []source.File{source.NewFile("left", "left.mgo", "package example\n")},
		TestFiles:       []source.File{source.NewFile("left-test", "left_test.mgo", "package example\n")},
		Resources:       []ResourceFile{{Path: "left.txt", Data: []byte("left")}},
		SelectionTarget: target.Target{Tags: []string{target.LanguageTag}},
		SourceCandidates: []SourceCandidate{{
			Path: "left.mgo", Selected: true,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewMemorySourceSet([]SourcePackage{{
		ID:              PackageID{Namespace: "std", Path: "example"},
		ModulePath:      "example",
		Files:           []source.File{source.NewFile("right", "right.mgo", "package example\n")},
		TestFiles:       []source.File{source.NewFile("right-test", "right_test.mgo", "package example\n")},
		Resources:       []ResourceFile{{Path: "right.txt", Data: []byte("right")}},
		SelectionTarget: target.Target{Tags: []string{target.LanguageTag}},
		SourceCandidates: []SourceCandidate{{
			Path: "right.mgo", Selected: true,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	combined, err := ComposeStandardSourceSets(left, right)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := combined.Package("example")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) != 2 || len(pkg.TestFiles) != 2 || len(pkg.Resources) != 2 || len(pkg.SourceCandidates) != 2 {
		t.Fatalf("merged package shape = files:%d tests:%d resources:%d candidates:%d", len(pkg.Files), len(pkg.TestFiles), len(pkg.Resources), len(pkg.SourceCandidates))
	}
}

type countingSourceSet struct {
	SourceSet
	loads atomic.Int32
}

func (s *countingSourceSet) Package(modulePath string) (SourcePackage, bool, error) {
	s.loads.Add(1)
	return s.SourceSet.Package(modulePath)
}

func TestComposeStandardSourceSetsLoadsPackagesLazilyOnce(t *testing.T) {
	sourceSet := &countingSourceSet{SourceSet: standardSources(t, source.NewFile("fmt", "fmt.mgo", "package fmt\n"))}
	combined, err := ComposeStandardSourceSets(sourceSet)
	if err != nil {
		t.Fatal(err)
	}
	if sourceSet.loads.Load() != 0 {
		t.Fatal("composition loaded package contents while indexing")
	}
	for range 2 {
		if _, found, err := combined.Package("fmt"); err != nil || !found {
			t.Fatalf("Package failed: found=%t err=%v", found, err)
		}
	}
	if loads := sourceSet.loads.Load(); loads != 1 {
		t.Fatalf("package load count = %d, want 1", loads)
	}
}

func TestComposeStandardSourceSetsRejectsResourceConflict(t *testing.T) {
	makeSources := func(data string) SourceSet {
		sources, err := NewMemorySourceSet([]SourcePackage{{
			ID:         PackageID{Namespace: "std", Path: "example"},
			ModulePath: "example",
			Files:      []source.File{source.NewFile(data, data+".mgo", "package example\n")},
			Resources:  []ResourceFile{{Path: "value.txt", Data: []byte(data)}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return sources
	}
	combined, err := ComposeStandardSourceSets(makeSources("left"), makeSources("right"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := combined.Package("example"); err == nil || !strings.Contains(err.Error(), "duplicate resource file") {
		t.Fatalf("resource conflict error = %v", err)
	}
}
