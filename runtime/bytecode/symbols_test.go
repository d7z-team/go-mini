package bytecode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateProgramSymbolsAcceptsBoundSidecar(t *testing.T) {
	image, symbols := testProgramSymbols(t)
	if err := ValidateProgramSymbols(&image, &symbols); err != nil {
		t.Fatalf("ValidateProgramSymbols failed: %v", err)
	}

	imageHash := image.Hash
	symbols.Packages[image.Root] = PackageSymbols{}
	if image.Hash != imageHash {
		t.Fatal("symbol mutation changed execution image identity")
	}
}

func TestValidateProgramSymbolsRejectsBrokenBindings(t *testing.T) {
	image, valid := testProgramSymbols(t)
	tests := []struct {
		name   string
		mutate func(*ProgramSymbols)
	}{
		{name: "program hash", mutate: func(symbols *ProgramSymbols) { symbols.ProgramHash = strings.Repeat("0", 64) }},
		{name: "package code hash", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.CodeHash = strings.Repeat("0", 64)
			symbols.Packages[image.Root] = pkg
		}},
		{name: "source hash", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.SourceHash = "bad"
			symbols.Packages[image.Root] = pkg
		}},
		{name: "function id", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].ID = "fn.missing"
			symbols.Packages[image.Root] = pkg
		}},
		{name: "local id", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].Locals[0].ID = "local.missing"
			symbols.Packages[image.Root] = pkg
		}},
		{name: "upvalue id", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].Upvalues[0].ID = "upvalue.missing"
			symbols.Packages[image.Root] = pkg
		}},
		{name: "scope range", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].Scopes[0].Ranges[0].End = 2
			symbols.Packages[image.Root] = pkg
		}},
		{name: "instruction pc", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].Locations[0].PC = 1
			symbols.Packages[image.Root] = pkg
		}},
		{name: "source file", mutate: func(symbols *ProgramSymbols) {
			pkg := symbols.Packages[image.Root]
			pkg.Functions[0].Locations[0].Points[0].File = "missing.mgo"
			symbols.Packages[image.Root] = pkg
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symbols := CloneProgramSymbols(valid)
			test.mutate(&symbols)
			symbols.Hash, _ = HashProgramSymbols(symbols)
			if err := ValidateProgramSymbols(&image, &symbols); err == nil {
				t.Fatal("broken program symbols passed validation")
			}
		})
	}
	t.Run("image content hash", func(t *testing.T) {
		changed := image
		changed.Root = "changed"
		if err := ValidateProgramSymbols(&changed, &valid); err == nil {
			t.Fatal("program symbols accepted an image with a stale content hash")
		}
	})
	t.Run("package content hash", func(t *testing.T) {
		changed := image
		changed.Packages = make(map[string]PackageArchive, len(image.Packages))
		for path, archive := range image.Packages {
			archive.Artifact = append(json.RawMessage(nil), archive.Artifact...)
			changed.Packages[path] = archive
		}
		archive := changed.Packages[image.Root]
		artifact, err := DecodeJSON(archive.Artifact)
		if err != nil {
			t.Fatal(err)
		}
		artifact.Module.Package = "changed"
		archive.Artifact, err = EncodeJSON(&artifact)
		if err != nil {
			t.Fatal(err)
		}
		changed.Packages[image.Root] = archive
		changed.Hash, _ = HashExecutionImage(changed)
		mutated := CloneProgramSymbols(valid)
		mutated.ProgramHash = changed.Hash
		mutated.Hash, _ = HashProgramSymbols(mutated)
		if err := ValidateProgramSymbols(&changed, &mutated); err == nil {
			t.Fatal("program symbols accepted a stale package code hash")
		}
	})
}

func TestCloneProgramSymbolsOwnsNestedData(t *testing.T) {
	_, symbols := testProgramSymbols(t)
	clone := CloneProgramSymbols(symbols)
	pkg := clone.Packages["example/main"]
	pkg.Files[0].Path = "changed.mgo"
	pkg.Functions[0].Locals[0].Name = "changed"
	pkg.Functions[0].Scopes[0].Ranges[0].End = 0
	pkg.Functions[0].Locations[0].Points[0].Line = 99
	clone.Packages["example/main"] = pkg

	original := symbols.Packages["example/main"]
	if original.Files[0].Path != "main.mgo" || original.Functions[0].Locals[0].Name != "value" || original.Functions[0].Scopes[0].Ranges[0].End != 1 || original.Functions[0].Locations[0].Points[0].Line != 1 {
		t.Fatalf("CloneProgramSymbols shared nested storage: %#v", original)
	}
}

func FuzzValidateProgramSymbols(f *testing.F) {
	image, symbols := testProgramSymbols(f)
	seed, err := json.Marshal(symbols)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"format":"mini-go-program-symbols"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		var candidate ProgramSymbols
		if json.Unmarshal(data, &candidate) != nil {
			return
		}
		_ = ValidateProgramSymbols(&image, &candidate)
	})
}

func testProgramSymbols(t testing.TB) (ExecutionImage, ProgramSymbols) {
	t.Helper()
	artifact := NewArtifact("example/main", "main")
	artifact.Functions = []Function{{
		ID: "fn.main", Signature: testSignature("function() Void"),
		Locals:       []Local{{ID: "local.value", Type: testType("Int64")}},
		Upvalues:     []Upvalue{{ID: "upvalue.name", Type: testType("String")}},
		Instructions: []Instruction{{Op: string(OpReturn), Payload: json.RawMessage(`{"result_count":0}`)}},
	}}
	attachTestTypeNodes(&artifact)
	artifactJSON, artifactHash, err := EncodeJSONAndHash(&artifact)
	if err != nil {
		t.Fatal(err)
	}
	image := ExecutionImage{
		Format: ExecutionFormat, Version: ExecutionVersion, CompilerID: CompilerIdentity, ContractID: ExecutionContract,
		Root:     artifact.Module.Path,
		Entries:  []Entry{{Name: DefaultEntryName, ModulePath: artifact.Module.Path, FunctionID: "fn.main"}},
		Packages: map[string]PackageArchive{artifact.Module.Path: {Artifact: artifactJSON, ArtifactHash: artifactHash}},
	}
	image.Hash, err = HashExecutionImage(image)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := strings.Repeat("1", 64)
	symbols := ProgramSymbols{
		Format: SymbolsFormat, Version: SymbolsVersion, CompilerID: CompilerIdentity, ContractID: SymbolsContract,
		ProgramHash: image.Hash,
		Packages: map[string]PackageSymbols{artifact.Module.Path: {
			ModulePath: artifact.Module.Path, CodeHash: artifactHash, SourceHash: sourceHash,
			Files: []SourceFile{{ID: "file.main", Path: "main.mgo", Hash: sourceHash}},
			Functions: []FunctionSymbols{{
				ID: "fn.main", Name: "main", Declaration: &Location{File: "main.mgo", Line: 1, Column: 1},
				Locals:    []LocalSymbol{{ID: "local.value", Name: "value", Scope: 1, Declaration: &Location{File: "main.mgo", Line: 1, Column: 13}}},
				Upvalues:  []UpvalueSymbol{{ID: "upvalue.name", Name: "name"}},
				Scopes:    []DebugScope{{ID: 1, Ranges: []PCRange{{Start: 0, End: 1}}}},
				Locations: []InstructionSymbol{{PC: 0, Points: []Location{{File: "main.mgo", Line: 1, Column: 22}}}},
			}},
		}},
	}
	symbols.Hash, err = HashProgramSymbols(symbols)
	if err != nil {
		t.Fatal(err)
	}
	return image, symbols
}
