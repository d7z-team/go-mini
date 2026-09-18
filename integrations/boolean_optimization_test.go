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

func TestBooleanOptimizationPreservesSourceBehavior(t *testing.T) {
	const module = "example/booleans"
	sources, err := workspace.DiscoverIndexedSourceTree(os.DirFS("testdata/boolean_optimization"), ".", module)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		want int64
	}{{"Loop", 505500}, {"Shadow", 12}, {"Captures", 1}, {"Named", 3}, {"Mutation", 7}, {"Multiple", 9}, {"Recovered", 42}}
	var entries []compiler.EntryPoint
	for _, check := range checks {
		entries = append(entries, compiler.EntryPoint{Name: check.name, Function: check.name})
	}
	entries = append(entries, compiler.EntryPoint{Name: "Panic", Function: "Panic"})
	text, err := os.ReadFile("testdata/boolean_optimization/main.mgo")
	if err != nil {
		t.Fatal(err)
	}
	var breakpointLine int
	for index, line := range strings.Split(string(text), "\n") {
		if strings.Contains(line, "// breakpoint") {
			breakpointLine = index + 1
		}
	}
	backend := cache.NewMemoryBackend()
	for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			result, err := compiler.Prepare(compiler.Request{Root: module, Sources: sources, Cache: cache.New(backend), EntryPoints: entries, Optimization: level, Symbols: true})
			if err != nil || result.Image == nil || result.Symbols == nil || !result.Checked.OK() {
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
			for _, check := range checks {
				t.Run(check.name, func(t *testing.T) { requireIntegrationInt(t, callIntegrationEntry(t, program, check.name), check.want) })
			}
			instance, err := program.Instantiate(t.Context(), miniruntime.InstanceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if _, err = instance.Call(t.Context(), "Panic"); err == nil || !strings.Contains(err.Error(), "boolean panic") {
				t.Fatalf("panic: %v", err)
			}
			instance.Close()
			instance, err = program.Instantiate(t.Context(), miniruntime.InstanceOptions{Debugger: miniruntime.NewDebugger()})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			breakpoints, err := instance.SetBreakpoints(module, "main.mgo", []int{breakpointLine})
			if err != nil || len(breakpoints) != 1 || !breakpoints[0].Verified {
				t.Fatalf("breakpoint: %+v %v", breakpoints, err)
			}
			execution, err := instance.Start("Loop")
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 100; i++ {
				state, _, err := execution.PollSteps(1000)
				if err != nil {
					t.Fatal(err)
				}
				if state == miniruntime.ExecutionPaused {
					break
				}
			}
			event, ok := execution.PauseEvent()
			if !ok || event.Kind != miniruntime.EventBreakpoint || event.Frame.Loc.Line != breakpointLine {
				t.Fatalf("pause: %+v %v", event, ok)
			}
			if _, err = execution.Continue(); err != nil {
				t.Fatal(err)
			}
			completed, err := execution.Wait(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			requireIntegrationInt(t, completed.Values[0], 505500)
		})
	}
}
