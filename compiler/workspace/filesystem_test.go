package workspace

import (
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
)

type countingFS struct {
	fs.FS
	reads map[string]int
}

func (s *countingFS) ReadFile(name string) ([]byte, error) {
	s.reads[name]++
	return fs.ReadFile(s.FS, name)
}

func TestDiscoverSourceTreeSnapshotsSourceTree(t *testing.T) {
	tree := &countingFS{FS: fstest.MapFS{
		"project/main.mgo":          {Data: []byte("package main\n")},
		"project/main_test.mgo":     {Data: []byte("package main\n")},
		"project/lib/value.mgo":     {Data: []byte("package lib\n")},
		"project/.cache/hidden.mgo": {Data: []byte("package hidden\n")},
		"project/README.md":         {Data: []byte("ignored")},
	}, reads: map[string]int{}}

	snapshot, err := DiscoverSourceTree(tree, "project", "example.com/project")
	if err != nil {
		t.Fatalf("DiscoverSourceTree failed: %v", err)
	}
	paths, err := snapshot.PackagePaths()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/project", "example.com/project/lib"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("package paths = %v, want %v", paths, want)
	}
	for _, name := range []string{"project/main.mgo", "project/main_test.mgo", "project/lib/value.mgo"} {
		if tree.reads[name] != 1 {
			t.Fatalf("%s read %d times, want once", name, tree.reads[name])
		}
	}
}

func TestIndexedPackageTreeLoadsOnlyRequestedPackageOnce(t *testing.T) {
	tree := &countingFS{FS: fstest.MapFS{
		"alpha/alpha.mgo":         {Data: []byte("package alpha\n")},
		"alpha/data.txt":          {Data: []byte("alpha")},
		"alpha/assets/.hidden":    {Data: []byte("hidden")},
		"alpha/_ignored/code.mgo": {Data: []byte("package ignored\n")},
		"beta/beta.mgo":           {Data: []byte("package beta\n")},
	}, reads: map[string]int{}}
	sources, err := DiscoverIndexedPackageTree(tree, ".")
	if err != nil {
		t.Fatal(err)
	}
	if tree.reads["alpha/alpha.mgo"] != 0 || tree.reads["beta/beta.mgo"] != 0 {
		t.Fatalf("indexing read source contents: %v", tree.reads)
	}
	pkg, ok, err := sources.Package("alpha")
	if err != nil || !ok {
		t.Fatalf("load alpha: ok=%t err=%v", ok, err)
	}
	foundHidden := false
	for _, resource := range pkg.Resources {
		foundHidden = foundHidden || resource.Path == "assets/.hidden"
	}
	if !foundHidden {
		t.Fatalf("hidden embed resource missing from indexed package: %#v", pkg.Resources)
	}
	if _, ok, err := sources.Package("alpha/_ignored"); err != nil || ok {
		t.Fatalf("hidden source directory became a package: ok=%t err=%v", ok, err)
	}
	if tree.reads["alpha/alpha.mgo"] != 1 || tree.reads["alpha/data.txt"] != 1 || tree.reads["beta/beta.mgo"] != 0 {
		t.Fatalf("unexpected package reads after alpha: %v", tree.reads)
	}
	if _, ok, err := sources.Package("alpha"); err != nil || !ok {
		t.Fatalf("reload alpha: ok=%t err=%v", ok, err)
	}
	if tree.reads["alpha/alpha.mgo"] != 1 || tree.reads["alpha/data.txt"] != 1 {
		t.Fatalf("cached package was read again: %v", tree.reads)
	}
}

func TestFilesystemDiscoverySharesFileBoundaries(t *testing.T) {
	for _, discover := range []struct {
		name string
		load func(fs.FS, string, string) (SourceSet, error)
	}{{"eager", DiscoverSourceTree}, {"indexed", DiscoverIndexedSourceTree}} {
		t.Run(discover.name, func(t *testing.T) {
			snapshot, err := discover.load(fstest.MapFS{
				"main.mgo":        {Mode: fs.ModeSymlink},
				"pipe.mgo":        {Mode: fs.ModeNamedPipe},
				".git/hidden.mgo": {Data: []byte("package hidden\n")},
				".cache/data.bin": {Data: []byte{0, 255}},
				"real.mgo":        {Data: []byte("package main\n")},
			}, ".", "example.com/project")
			if err != nil {
				t.Fatal(err)
			}
			pkg, ok, err := snapshot.Package("example.com/project")
			if err != nil || !ok || len(pkg.Files) != 1 || pkg.Files[0].Path != "real.mgo" {
				t.Fatalf("source boundaries: ok=%t err=%v package=%#v", ok, err, pkg)
			}
			var resources []string
			for _, resource := range pkg.Resources {
				resources = append(resources, resource.Path)
			}
			if !reflect.DeepEqual(resources, []string{".cache/data.bin", "real.mgo"}) {
				t.Fatalf("resources: %v", resources)
			}
		})
	}
}
