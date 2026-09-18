package runtime

import (
	"context"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestCodeOnlyBreakpointsActivateWhenSymbolsAreAttached(t *testing.T) {
	artifact := patchGlobalArtifact(1)
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.entry", pc: 0, line: 1, column: 1})
	symbolized := patchTestProgram(t, artifact, "no-symbols")
	program := symbolized.WithoutSymbols()
	if program == nil || program.code != symbolized.code || program.SymbolsHash() != "" || symbolized.SymbolsHash() == "" {
		t.Fatalf("detached program identity: code=%p/%p symbols=%q/%q", program.code, symbolized.code, program.SymbolsHash(), symbolized.SymbolsHash())
	}
	instance, err := program.Instantiate(nil, InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	resolved, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{1})
	if err != nil || len(resolved) != 1 || resolved[0].Verified || resolved[0].RequestedLine != 1 {
		t.Fatalf("code-only breakpoints = %#v, %v", resolved, err)
	}
	plan, err := instance.PreparePatch(nil, symbolized)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if !instance.vm.debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 1) {
		t.Fatal("attaching symbols did not activate the requested breakpoint")
	}
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != ExecutionPaused {
		t.Fatalf("breakpoint poll = %s, %v", state, err)
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatal(err)
	}
	result, err := execution.Wait(nil)
	if err != nil || len(result.Values) != 1 {
		t.Fatalf("code-only program execution = %#v, %v", result, err)
	}
}

func TestSymbolOnlyPatchRefreshesBreakpointsWithoutChangingCode(t *testing.T) {
	program := patchTestProgram(t, patchGlobalArtifact(1), "symbol-only-code")
	oldPackage := program.symbols.packages["patch/global"]
	oldFunction := oldPackage.functions["fn.entry"]
	oldFunction.Locations = []ir.InstructionSymbol{{PC: 0, Points: []ir.Location{{File: "main.mgo", Line: 4, Column: 1}}}}
	oldPackage.functions["fn.entry"] = oldFunction
	program.symbols.hash = "symbols-old"

	newFunction := oldFunction
	newFunction.Locations = []ir.InstructionSymbol{{PC: 0, Points: []ir.Location{{File: "main.mgo", Line: 8, Column: 1}}}}
	newPackage := packageSymbolIndex{functions: map[string]ir.FunctionSymbols{"fn.entry": newFunction}, globals: oldPackage.globals}
	updated := &Program{code: program.code, symbols: &symbolIndex{
		hash: "symbols-new", packages: map[string]packageSymbolIndex{"patch/global": newPackage},
	}}

	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	resolved, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{4})
	if err != nil || len(resolved) != 1 || resolved[0].Line != 4 {
		t.Fatalf("old symbols breakpoint = %#v, %v", resolved, err)
	}
	plan, err := instance.PreparePatch(context.Background(), updated)
	if err != nil {
		t.Fatal(err)
	}
	result, err := instance.ApplyPatch(plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Previous.Hash != result.Current.Hash || result.Previous.SymbolsHash != "symbols-old" || result.Current.SymbolsHash != "symbols-new" || len(result.ChangedModules) != 0 {
		t.Fatalf("symbol-only patch result = %#v", result)
	}
	if instance.Revision().Generation != 2 || !instance.vm.debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 8) {
		t.Fatalf("symbol-only revision or breakpoint was not committed: %#v", instance.Revision())
	}
}

func TestSymbolOnlyPatchKeepsOldFrameSymbols(t *testing.T) {
	program := patchTestProgram(t, patchCallArtifact(10, 1), "symbol-pin-code")
	oldPackage := program.symbols.packages["patch/main"]
	oldFunction := oldPackage.functions["fn.entry"]
	oldFunction.Locations = []ir.InstructionSymbol{{PC: 0, Points: []ir.Location{{File: "old.mgo", Line: 4, Column: 1}}}}
	oldPackage.functions["fn.entry"] = oldFunction
	program.symbols.hash = "symbol-pin-old"

	newFunction := oldFunction
	newFunction.Locations = []ir.InstructionSymbol{{PC: 0, Points: []ir.Location{{File: "new.mgo", Line: 8, Column: 1}}}}
	updated := &Program{code: program.code, symbols: &symbolIndex{
		hash: "symbol-pin-new",
		packages: map[string]packageSymbolIndex{"patch/main": {
			functions: map[string]ir.FunctionSymbols{"fn.entry": newFunction}, globals: oldPackage.globals,
		}},
	}}
	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if state, _, err := execution.PollSteps(1); err != nil || state != ExecutionRunning {
		t.Fatalf("old execution first poll = %s, %v", state, err)
	}
	plan, err := instance.PreparePatch(context.Background(), updated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if err := execution.RequestPause(); err != nil {
		t.Fatal(err)
	}
	state, err := execution.Poll()
	if err != nil || state != ExecutionPaused {
		t.Fatalf("old execution pause = %s, %v", state, err)
	}
	event, ok := execution.PauseEvent()
	if !ok || event.Generation != 1 || event.Frame.Loc.File != "old.mgo" || event.Frame.Loc.Line != 4 {
		t.Fatalf("old frame did not retain old symbols: %#v", event)
	}
}

func TestSymbolDetachAndReattachRefreshesRequestedBreakpoints(t *testing.T) {
	artifact := patchGlobalArtifact(1)
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.entry", pc: 0, line: 4, column: 1})
	program := patchTestProgram(t, artifact, "symbol-detach-code")
	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	if _, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{4}); err != nil {
		t.Fatal(err)
	}
	detached := program.WithoutSymbols()
	plan, err := instance.PreparePatch(context.Background(), detached)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if instance.Revision().Generation != 2 || instance.Revision().SymbolsHash != "" || instance.vm.debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 4) {
		t.Fatalf("detached revision or breakpoint state = %#v", instance.Revision())
	}
	source := breakpointSource{ModulePath: "patch/global", File: "main.mgo"}
	instance.vm.debugger.mu.RLock()
	requested := append([]int(nil), instance.vm.debugger.breakpoints[source].requested...)
	instance.vm.debugger.mu.RUnlock()
	if len(requested) != 1 || requested[0] != 4 {
		t.Fatalf("detaching symbols discarded requested breakpoints: %#v", requested)
	}
	plan, err = instance.PreparePatch(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if instance.Revision().Generation != 3 || instance.Revision().SymbolsHash == "" || !instance.vm.debugger.hasBreakpoint(instance.Revision().Generation, "patch/global", "main.mgo", 4) {
		t.Fatalf("reattached revision or breakpoint state = %#v", instance.Revision())
	}
}

func TestPausedFrameRetainsSymbolsAcrossDetachPatch(t *testing.T) {
	artifact := patchGlobalArtifact(1)
	setTestInstructionLocations(t, &artifact, testInstructionLocation{function: "fn.entry", pc: 0, line: 4, column: 1})
	program := patchTestProgram(t, artifact, "paused-symbol-pin")
	instance, err := program.Instantiate(context.Background(), InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupTestInstance(t, instance)
	if _, err := instance.SetBreakpoints("patch/global", "main.mgo", []int{4}); err != nil {
		t.Fatal(err)
	}
	execution, err := instance.Start("run")
	if err != nil {
		t.Fatal(err)
	}
	if state, err := execution.Poll(); err != nil || state != ExecutionPaused {
		t.Fatalf("breakpoint poll = %s, %v", state, err)
	}
	snapshot, err := execution.DebugSnapshot()
	if err != nil || len(snapshot.Frames) == 0 || !snapshot.Frames[0].HasSymbols {
		t.Fatalf("initial snapshot = %#v, %v", snapshot, err)
	}
	scopes, err := execution.DebugScopes(snapshot.Frames[0].ID)
	if err != nil || len(scopes) == 0 {
		t.Fatalf("initial scopes = %#v, %v", scopes, err)
	}
	variableReference := scopes[len(scopes)-1].VariablesReference
	if _, err := execution.DebugVariables(variableReference, 0, 0); err != nil {
		t.Fatal(err)
	}

	plan, err := instance.PreparePatch(context.Background(), program.WithoutSymbols())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	if instance.Revision().SymbolsHash != "" {
		t.Fatalf("current revision retained symbols: %#v", instance.Revision())
	}
	if retained, err := instance.RetainedRevisions(context.Background()); err != nil || len(retained) != 2 {
		t.Fatalf("retained revisions while paused = %#v, %v", retained, err)
	}
	if variables, err := execution.DebugVariables(variableReference, 0, 0); err != nil || len(variables) == 0 {
		t.Fatalf("old debug reference after detach = %#v, %v", variables, err)
	}
	if _, err := execution.Continue(); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if retained, err := instance.RetainedRevisions(context.Background()); err != nil || len(retained) != 1 || retained[0].Generation != 2 {
		t.Fatalf("retained revisions after resume = %#v, %v", retained, err)
	}
	if _, err := execution.DebugVariables(variableReference, 0, 0); err == nil {
		t.Fatal("debug reference remained valid after resume")
	}
}
