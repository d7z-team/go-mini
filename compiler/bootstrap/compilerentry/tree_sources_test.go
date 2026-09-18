package compilerentry

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/service"
)

func TestSourceTreesPreservePackagesResourcesAndWorkspace(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/workspace/sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Trees    []SourceTree
		Paths    []string
		Failures []struct {
			Name  string
			Trees []SourceTree
			Error string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var tools ToolService
	reply := tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/sources", Trees: fixture.Trees})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	packages := reply.Value.(SourcePackages).Packages
	var paths []string
	for _, pkg := range packages {
		paths = append(paths, pkg.ModulePath)
	}
	if !reflect.DeepEqual(paths, fixture.Paths) {
		t.Fatalf("paths: %v", paths)
	}
	if packages[0].Files[0].URI != "file:///scripts/main.mgo" {
		t.Fatal("source URI lost")
	}
	found := false
	for _, resource := range packages[1].Resources {
		if resource.Path == "assets/data.bin" {
			found = true
			if !reflect.DeepEqual(resource.Data, []byte{0, 255, 1}) {
				t.Fatal("binary resource changed")
			}
		}
	}
	if !found {
		t.Fatal("binary resource missing")
	}
	opened := tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/open", Root: "app", Packages: packages})
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	defer tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/close", Session: opened.Session})
	for _, failure := range fixture.Failures {
		reply := tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/sources", Trees: failure.Trees})
		if reply.Error == nil || !strings.Contains(reply.Error.Message, failure.Error) {
			t.Fatalf("%s: %+v", failure.Name, reply.Error)
		}
	}
	analyzed := tools.Execute(t.Context(), ToolsRequest{Operation: "workspace/analyze", Session: opened.Session, Revision: opened.Revision})
	if analyzed.Error != nil {
		t.Fatal(analyzed.Error)
	}
	build := tools.Execute(t.Context(), ToolsRequest{Operation: "build/prepare", Session: opened.Session, Build: service.BuildOptions{Revision: opened.Revision}})
	if build.Error != nil || len(build.Diagnostics) != 0 || build.ImageJSON == "" {
		t.Fatalf("build: %+v", build)
	}
}
