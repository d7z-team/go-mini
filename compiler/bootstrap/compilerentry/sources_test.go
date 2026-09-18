package compilerentry

import (
	"strings"
	"testing"
)

func TestSourceSetLoadsResourcesByPackage(t *testing.T) {
	sources, err := newSourceSet([]Package{{
		Namespace:  "module:example",
		ModulePath: "example/assets",
		Files:      []File{{Path: "assets.mgo", Text: "package assets\n"}},
		Resources:  []Resource{{Path: "value.txt", Data: []byte("value")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pkg, exists, err := sources.Package("example/assets")
	if err != nil || !exists {
		t.Fatalf("load package: exists=%v err=%v", exists, err)
	}
	if len(pkg.Resources) != 1 || pkg.Resources[0].Path != "value.txt" || string(pkg.Resources[0].Data) != "value" {
		t.Fatalf("resources = %#v", pkg.Resources)
	}
}

func TestSourceSetReconstructsSourceBackedResources(t *testing.T) {
	sources, err := newSourceSet([]Package{{
		Namespace:  "module:example",
		ModulePath: "example/assets",
		Files:      []File{{Path: "example/assets/data.mgo", Text: "package assets\nconst Value = 42\n"}},
		Resources:  []Resource{{Path: "data.mgo", SourcePath: "example/assets/data.mgo"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pkg, exists, err := sources.Package("example/assets")
	if err != nil || !exists {
		t.Fatalf("load package: exists=%v err=%v", exists, err)
	}
	if got := string(pkg.Resources[0].Data); got != "package assets\nconst Value = 42\n" {
		t.Fatalf("resource data = %q", got)
	}
}

func TestSourceSetRejectsInvalidSourceBackedResources(t *testing.T) {
	tests := []struct {
		name     string
		files    []File
		resource Resource
		want     string
	}{
		{name: "missing", resource: Resource{Path: "data.mgo", SourcePath: "missing.mgo"}, want: "references missing source"},
		{name: "source and data", files: []File{{Path: "data.mgo", Text: "package data\n"}}, resource: Resource{Path: "data.mgo", SourcePath: "data.mgo", Data: []byte{}}, want: "specifies source path and data"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sources, err := newSourceSet([]Package{{Namespace: "module:example", ModulePath: "example/assets", Files: test.files, Resources: []Resource{test.resource}}})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = sources.Package("example/assets")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Package() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestSourceSetRejectsAmbiguousCrossPackageSource(t *testing.T) {
	sources, err := newSourceSet([]Package{
		{Namespace: "module:example", ModulePath: "example/first", Files: []File{{Path: "shared.mgo", Text: "package first\n"}}},
		{Namespace: "module:example", ModulePath: "example/second", Files: []File{{Path: "shared.mgo", Text: "package second\n"}}},
		{Namespace: "module:example", ModulePath: "example/assets", Resources: []Resource{{Path: "shared.mgo", SourcePath: "shared.mgo"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = sources.Package("example/assets")
	if err == nil || !strings.Contains(err.Error(), "references ambiguous source") {
		t.Fatalf("Package() error = %v", err)
	}
}

func TestSourceSetRejectsDuplicatePackages(t *testing.T) {
	_, err := newSourceSet([]Package{{Namespace: "module:example", ModulePath: "example/value"}, {Namespace: "module:example", ModulePath: "example/value"}})
	if err == nil {
		t.Fatal("duplicate package was accepted")
	}
}

func TestSourceSetRejectsMissingPackageNamespace(t *testing.T) {
	_, err := newSourceSet([]Package{{ModulePath: "example/value"}})
	if err == nil || !strings.Contains(err.Error(), "empty namespace") {
		t.Fatalf("newSourceSet error = %v", err)
	}
}
