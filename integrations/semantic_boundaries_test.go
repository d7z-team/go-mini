package integrations_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/workspace"
	miniruntime "github.com/d7z-team/mini-go/runtime"
)

func TestSemanticBoundariesAcrossOptimizationAndCache(t *testing.T) {
	const module = "test/boundaries"
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS("../testdata/runtime/source/semantic_boundaries"), ".", module)
	if err != nil {
		t.Fatal(err)
	}
	for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			request := compiler.Request{
				Root: module, Sources: sources, Optimization: level, Cache: cache.New(cache.NewMemoryBackend()), Symbols: true,
				EntryPoints: []compiler.EntryPoint{{Name: "default", Function: "Main"}},
			}
			var hash string
			for pass := 0; pass < 2; pass++ {
				result, err := compiler.Prepare(request)
				if err != nil || result.Image == nil || result.Symbols == nil {
					t.Fatalf("prepare: %v %+v", err, result.Checked.Diagnostics)
				}
				if pass > 0 && result.Image.Hash != hash {
					t.Fatal("warm image identity changed")
				}
				hash = result.Image.Hash
				program, err := miniruntime.LoadExecutionImage(*result.Image)
				if err != nil {
					t.Fatal(err)
				}
				program, err = program.WithSymbols(*result.Symbols)
				if err != nil {
					t.Fatal(err)
				}
				requireIntegrationInt(t, callIntegrationEntry(t, program, "default"), 42)
			}
		})
	}
}

func TestMapIteratorPausedLifecycle(t *testing.T) {
	const module = "test/iterator"
	text, err := os.ReadFile("testdata/iterator_lifecycle/main.mgo")
	if err != nil {
		t.Fatal(err)
	}
	var programs []*miniruntime.Program
	for _, offset := range []string{"1", "2"} {
		sources, err := workspace.NewTreeSourceSet(module, []workspace.TreeFile{{Path: "main.mgo", Text: strings.Replace(string(text), "return sum + 1", "return sum + "+offset, 1)}})
		if err != nil {
			t.Fatal(err)
		}
		result, err := compiler.Prepare(compiler.Request{Root: module, Sources: sources, Symbols: true, Optimization: compiler.OptimizationFull, EntryPoints: []compiler.EntryPoint{{Name: "default", Function: "Main"}}})
		if err != nil || result.Image == nil {
			t.Fatalf("prepare: %v %+v", err, result.Checked.Diagnostics)
		}
		program, err := miniruntime.LoadExecutionImage(*result.Image)
		if err != nil {
			t.Fatal(err)
		}
		program, err = program.WithSymbols(*result.Symbols)
		if err != nil {
			t.Fatal(err)
		}
		programs = append(programs, program)
	}
	line := 0
	for i, text := range strings.Split(string(text), "\n") {
		if strings.Contains(text, "// pause") {
			line = i + 1
		}
	}
	for _, action := range []string{"continue", "cancel", "close", "patch"} {
		t.Run(action, func(t *testing.T) {
			instance, err := programs[0].Instantiate(t.Context(), miniruntime.InstanceOptions{Debugger: miniruntime.NewDebugger()})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			points, err := instance.SetBreakpoints(module, "main.mgo", []int{line})
			if err != nil || len(points) != 1 || !points[0].Verified {
				t.Fatalf("breakpoint: %v %+v", err, points)
			}
			execution, err := instance.Start("default")
			if err != nil {
				t.Fatal(err)
			}
			state, _, err := execution.PollSteps(1000)
			if err != nil || state != miniruntime.ExecutionPaused {
				t.Fatalf("pause: %v %v", state, err)
			}
			if action == "patch" {
				plan, err := instance.PreparePatch(t.Context(), programs[1])
				if err != nil {
					t.Fatal(err)
				}
				defer plan.Close()
				if _, err = instance.ApplyPatch(plan); err != nil {
					t.Fatal(err)
				}
			}
			switch action {
			case "cancel":
				execution.Cancel()
			case "close":
				instance.Close()
			default:
				if _, err = instance.SetBreakpoints(module, "main.mgo", nil); err != nil {
					t.Fatal(err)
				}
				if _, err = execution.Continue(); err != nil {
					t.Fatal(err)
				}
			}
			result, err := execution.Wait(t.Context())
			if action == "cancel" || action == "close" {
				if err == nil {
					t.Fatal("terminated execution returned success")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			requireIntegrationInt(t, result.Values[0], 42)
			if action == "patch" {
				result, err = instance.Call(t.Context(), "default")
				if err != nil {
					t.Fatal(err)
				}
				requireIntegrationInt(t, result.Values[0], 43)
			}
		})
	}
}
