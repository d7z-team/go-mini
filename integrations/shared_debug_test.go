package integrations_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/bootstrap/compilerentry"
	"github.com/d7z-team/mini-go/compiler/service"
	miniruntime "github.com/d7z-team/mini-go/runtime"
	"github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestSharedSourceBuildAndBreakpoint(t *testing.T) {
	data, err := os.ReadFile("../testdata/language/workspace.json")
	if err != nil {
		t.Fatal(err)
	}
	var workspace compilerentry.ToolsRequest
	if err = json.Unmarshal(data, &workspace); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("../testdata/debug/breakpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		EntryPoints []compiler.EntryPoint
		Source      string
		Line        int
		StopReason  string
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var tools compilerentry.ToolService
	workspace.Operation = "workspace/open"
	opened := tools.Execute(t.Context(), workspace)
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	defer tools.Execute(t.Context(), compilerentry.ToolsRequest{Operation: "workspace/close", Session: opened.Session})
	built := tools.Execute(t.Context(), compilerentry.ToolsRequest{Operation: "build/prepare", Session: opened.Session, Build: service.BuildOptions{Revision: opened.Revision, Symbols: true, EntryPoints: fixture.EntryPoints}})
	if built.Error != nil || built.ImageJSON == "" {
		t.Fatalf("build: %v %v", built.Error, built.Diagnostics)
	}
	var image bytecode.ExecutionImage
	var symbols bytecode.ProgramSymbols
	if err = json.Unmarshal([]byte(built.ImageJSON), &image); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(built.SymbolsJSON), &symbols); err != nil {
		t.Fatal(err)
	}
	program, err := miniruntime.LoadExecutionImage(image)
	if err != nil {
		t.Fatal(err)
	}
	program, err = program.WithSymbols(symbols)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := program.Instantiate(t.Context(), miniruntime.InstanceOptions{Debugger: miniruntime.NewDebugger()})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	bound := built.Sources[fixture.Source]
	breakpoints, err := instance.SetBreakpoints(bound.Module, bound.Path, []int{fixture.Line})
	if err != nil {
		t.Fatal(err)
	}
	if len(breakpoints) != 1 || !breakpoints[0].Verified {
		t.Fatalf("breakpoints: %+v", breakpoints)
	}
	execution, err := instance.Start("default")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		state, _, err := execution.PollSteps(100)
		if err != nil {
			t.Fatal(err)
		}
		if state == miniruntime.ExecutionPaused {
			break
		}
	}
	event, ok := execution.PauseEvent()
	if !ok {
		t.Fatal("expected breakpoint")
	}
	if string(event.Kind) != fixture.StopReason {
		t.Fatalf("stop reason: %s", event.Kind)
	}
	if _, err = execution.Continue(); err != nil {
		t.Fatal(err)
	}
	if _, err = execution.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
}
