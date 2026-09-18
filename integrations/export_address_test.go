package integrations_test

import (
	"fmt"
	"os"
	"testing"

	minigo "github.com/d7z-team/mini-go"
	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestExportVariableAddresses(t *testing.T) {
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS("testdata/execution/export_address"), ".", "example")
	if err != nil {
		t.Fatal(err)
	}
	root, err := cache.ResolveDiskRoot("")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		want int64
	}{
		{"Assign", 14},
		{"Address", 9},
		{"Fields", 13},
		{"ArrayIndex", 17},
		{"Copies", 58},
		{"ArrayAssignment", 79},
		{"SliceAssignment", 87},
		{"MapAssignment", 87},
		{"PointerAssignment", 87},
		{"Named", 6},
		{"Shadow", 12},
		{"Dot", 24},
	}
	entries := make([]minigo.EntryPoint, len(cases))
	for i, test := range cases {
		entries[i] = minigo.EntryPoint{Name: test.name, Function: test.name}
	}
	for _, level := range []compiler.OptimizationLevel{compiler.OptimizationNone, compiler.OptimizationDefault, compiler.OptimizationFull} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			engine, err := minigo.New(minigo.Config{Sources: sources, Cache: cache.NewDiskBackend(root), Optimization: level, Symbols: true})
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			checked, err := engine.Check("example")
			if err != nil || !checked.OK() {
				t.Fatalf("check: %v %+v", err, checked)
			}
			var hash string
			for pass := 0; pass < 2; pass++ {
				program, result, err := engine.Compile("example", entries...)
				if err != nil || !result.OK() {
					t.Fatalf("compile: %v %+v", err, result)
				}
				if pass == 0 {
					hash = program.Hash()
				} else if program.Hash() != hash || result.Stats.PackagesCompiled != 0 {
					t.Fatalf("cache changed image or compiled packages: %+v", result.Stats)
				}
				for _, test := range cases {
					t.Run(fmt.Sprintf("%s/%d", test.name, pass), func(t *testing.T) {
						requireIntegrationInt(t, callIntegrationEntry(t, program, test.name), test.want)
					})
				}
			}
		})
	}
}

func FuzzExportVariableAddresses(f *testing.F) {
	f.Add(int16(3), int16(-7), byte(0), false)
	f.Add(int16(32767), int16(-32768), byte(2), true)
	f.Fuzz(func(t *testing.T, initial, delta int16, optimization byte, dot bool) {
		importDeclaration, prefix := `import "example/state"`, "state."
		if dot {
			importDeclaration, prefix = `import . "example/state"`, ""
		}
		text := fmt.Sprintf(`package main
%s
func Result() int {
    %sValue = %d
    pointer := &%sValue
    *pointer += %d
    %sArray, %sArray[0] = [2]int{8, 9}, *pointer
    return %sArray[0] + %sArray[1]
}`, importDeclaration, prefix, initial, prefix, delta, prefix, prefix, prefix, prefix)
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "example/state", Files: []source.File{{Path: "state.mgo", Text: "package state\nvar Value int\nvar Array [2]int"}}},
			{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: text}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		engine, err := minigo.New(minigo.Config{Sources: sources, Cache: cache.NewMemoryBackend(), Optimization: compiler.OptimizationLevel(optimization % 3), Symbols: true})
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		var hash string
		for pass := 0; pass < 2; pass++ {
			program, result, err := engine.Compile("example", minigo.EntryPoint{Name: "result", Function: "Result"})
			if err != nil || !result.OK() {
				t.Fatalf("compile: %v, %v", err, result.Diagnostics)
			}
			if pass == 0 {
				hash = program.Hash()
			} else if program.Hash() != hash || result.Stats.PackagesCompiled != 0 {
				t.Fatalf("cache changed artifact: %+v", result.Stats)
			}
			requireIntegrationInt(t, callIntegrationEntry(t, program, "result"), int64(initial)+int64(delta)+9)
		}
	})
}
