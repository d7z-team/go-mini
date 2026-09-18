package cache

import (
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestSymbolActionIdentityIncludesProgramSourceAndOptimization(t *testing.T) {
	programHash := strings.Repeat("1", 64)
	sourceHash := strings.Repeat("2", 64)
	action := NewSymbolAction(ir.CompilerIdentity, programHash, sourceHash, 1)
	want, err := action.ID()
	if err != nil {
		t.Fatal(err)
	}
	changes := []SymbolAction{action, action, action, action}
	changes[0].Compiler = "other"
	changes[1].ProgramHash = strings.Repeat("3", 64)
	changes[2].SourceGraphHash = strings.Repeat("4", 64)
	changes[3].Optimization = 2
	for index, changed := range changes {
		got, err := changed.ID()
		if err != nil || got == want {
			t.Fatalf("symbol action change %d: id=%s err=%v", index, got, err)
		}
	}
}

func TestSymbolCacheOwnsSidecar(t *testing.T) {
	programHash := strings.Repeat("1", 64)
	action := NewSymbolAction(ir.CompilerIdentity, programHash, strings.Repeat("2", 64), 1)
	symbols := ir.ProgramSymbols{
		Format: ir.SymbolsFormat, Version: ir.SymbolsVersion, CompilerID: ir.CompilerIdentity, ContractID: ir.SymbolsContract,
		ProgramHash: programHash, Optimization: 1,
		Packages: map[string]ir.PackageSymbols{"example/main": {ModulePath: "example/main", CodeHash: strings.Repeat("3", 64)}},
	}
	var err error
	symbols.Hash, err = ir.HashProgramSymbols(symbols)
	if err != nil {
		t.Fatal(err)
	}
	store := New(NewMemoryBackend())
	if err := store.StoreSymbols(action, symbols); err != nil {
		t.Fatal(err)
	}
	pkg := symbols.Packages["example/main"]
	pkg.ModulePath = "changed"
	symbols.Packages["example/main"] = pkg
	lookup, err := store.LookupSymbols(action)
	if err != nil || !lookup.Hit || lookup.Symbols.Packages["example/main"].ModulePath != "example/main" {
		t.Fatalf("LookupSymbols = %#v, %v", lookup, err)
	}
	delete(lookup.Symbols.Packages, "example/main")
	again, err := store.LookupSymbols(action)
	if err != nil || !again.Hit || len(again.Symbols.Packages) != 1 {
		t.Fatalf("symbol cache shared lookup storage: %#v, %v", again, err)
	}
}

func TestSymbolCacheRejectsMismatchedActionBinding(t *testing.T) {
	programHash := strings.Repeat("1", 64)
	action := NewSymbolAction(ir.CompilerIdentity, programHash, strings.Repeat("2", 64), 1)
	other := NewSymbolAction(ir.CompilerIdentity, programHash, strings.Repeat("3", 64), 1)
	symbols := ir.ProgramSymbols{
		Format: ir.SymbolsFormat, Version: ir.SymbolsVersion, CompilerID: ir.CompilerIdentity, ContractID: ir.SymbolsContract,
		ProgramHash: programHash, Optimization: 1,
		Packages: map[string]ir.PackageSymbols{"example/main": {ModulePath: "example/main", CodeHash: strings.Repeat("4", 64)}},
	}
	var err error
	symbols.Hash, err = ir.HashProgramSymbols(symbols)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalJSON(symbols)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := other.ID()
	if err != nil {
		t.Fatal(err)
	}
	state, err := canonicalJSON(symbolState{Format: symbolStateFormat, Version: symbolStateVersion, ActionID: otherID.String(), Symbols: raw})
	if err != nil {
		t.Fatal(err)
	}
	actionID, err := action.ID()
	if err != nil {
		t.Fatal(err)
	}
	backend := NewMemoryBackend()
	outputID := OutputIDFor(state)
	if err := backend.PutOutput(outputID, state); err != nil {
		t.Fatal(err)
	}
	if err := backend.PutAction(actionID, Entry{Output: outputID, Size: int64(len(state))}); err != nil {
		t.Fatal(err)
	}
	lookup, err := New(backend).LookupSymbols(action)
	if err != nil || lookup.Hit || lookup.Reason != "symbol state action mismatch" {
		t.Fatalf("LookupSymbols = %#v, %v", lookup, err)
	}
}
