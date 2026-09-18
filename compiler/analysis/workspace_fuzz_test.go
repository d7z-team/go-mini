package analysis

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func FuzzWorkspaceIncrementalReuse(f *testing.F) {
	f.Add(int16(1), int16(2), false, []byte("base"))
	f.Add(int16(1), int16(1), false, []byte("base"))
	f.Add(int16(1), int16(1), true, []byte("changed"))
	f.Fuzz(func(t *testing.T, before, after int16, exportString bool, resource []byte) {
		if len(resource) > 4<<10 {
			return
		}
		buildSources := func(returnType, value string, data []byte) workspace.MemorySourceSet {
			set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
				{
					ModulePath: "fuzz/dep",
					Files: []source.File{{Path: "dep.mgo", Text: fmt.Sprintf(
						"package dep\nfunc Value() %s { return %s }\n", returnType, value,
					)}},
					Resources: []workspace.ResourceFile{{Path: "value.bin", Data: data}},
				},
				{
					ModulePath: "fuzz/main",
					Files:      []source.File{{Path: "main.mgo", Text: "package main\nimport \"fuzz/dep\"\nfunc main() { _ = dep.Value() }\n"}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			return set
		}
		first, err := CheckWorkspace(WorkspaceRequest{
			Root: "fuzz/main", Sources: buildSources("int", strconv.FormatInt(int64(before), 10), []byte("base")),
		})
		if err != nil {
			t.Fatal(err)
		}
		returnType, value := "int", strconv.FormatInt(int64(after), 10)
		if exportString {
			returnType, value = "string", strconv.Quote(value)
		}
		second, err := CheckWorkspace(WorkspaceRequest{
			Root: "fuzz/main", Sources: buildSources(returnType, value, resource), Previous: &first,
		})
		if err != nil {
			t.Fatal(err)
		}
		dependencyChanged := before != after || exportString || string(resource) != "base"
		wantMisses := 0
		if dependencyChanged {
			wantMisses = 1
		}
		if exportString {
			wantMisses = 2
		}
		if second.Stats.PackageCacheMisses != wantMisses || second.Stats.PackageCacheHits != 2-wantMisses {
			t.Fatalf("reuse = %d hits/%d misses, want %d/%d", second.Stats.PackageCacheHits, second.Stats.PackageCacheMisses, 2-wantMisses, wantMisses)
		}
		cold, err := CheckWorkspace(WorkspaceRequest{
			Root: "fuzz/main", Sources: buildSources(returnType, value, resource),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(second.ExportHashes, cold.ExportHashes) || !reflect.DeepEqual(second.Diagnostics, cold.Diagnostics) {
			t.Fatalf("incremental analysis differs from cold analysis: exports=%v/%v diagnostics=%v/%v", second.ExportHashes, cold.ExportHashes, second.Diagnostics, cold.Diagnostics)
		}
	})
}
