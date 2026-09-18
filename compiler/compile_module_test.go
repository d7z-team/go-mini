package compiler

import (
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestCompilePackageMergesFilesWithStableDebugFiles(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: `
package main

func AddOne(v int64) int64 {
	return v + 1
}
`,
		}, {
			Path: "b.mgo",
			Text: `
package main

func Main() int64 {
	return AddOne(41)
}
`,
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	if len(result.Symbols.Files) != 2 {
		t.Fatalf("expected two symbol files, got %#v", result.Symbols.Files)
	}
	data, err := result.EncodeJSON()
	if err != nil {
		t.Fatalf("EncodeJSON failed: %v", err)
	}
	if _, err := ir.DecodeJSON(data); err != nil {
		t.Fatalf("DecodeJSON failed: %v", err)
	}
}

func TestCompilePackageReportsPackageMismatch(t *testing.T) {
	result, err := compileTestPackage(SourcePackage{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "a.mgo",
			Text: "package main\n",
		}, {
			Path: "b.mgo",
			Text: "package other\n",
		}},
	})
	if err != nil {
		t.Fatalf("compileTestPackage failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected diagnostics")
	}
	if result.Diagnostics[0].Code != "compiler.package.name.mismatch" {
		t.Fatalf("expected package mismatch diagnostic, got %#v", result.Diagnostics)
	}
}

func TestCompileWorkspaceFillsModuleRequirementHash(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `
package main

import "example/lib"

func Main() int64 {
	return lib.Add(20, 22)
}
`,
		}},
	}, {
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `
package lib

func Add(left int64, right int64) int64 {
	return left + right
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	mainArtifact, ok := result.Artifact("example/main")
	if !ok {
		t.Fatalf("expected main artifact")
	}
	libArtifact, ok := result.Artifact("example/lib")
	if !ok {
		t.Fatalf("expected lib artifact")
	}
	libHash, err := ir.Hash(&libArtifact)
	if err != nil {
		t.Fatalf("Hash lib failed: %v", err)
	}
	if len(mainArtifact.Requirements) != 1 || mainArtifact.Requirements[0].ModulePath != "example/lib" {
		t.Fatalf("expected lib requirement, got %#v", mainArtifact.Requirements)
	}
	if mainArtifact.Requirements[0].Hash != libHash {
		t.Fatalf("expected lib requirement hash %q, got %#v", libHash, mainArtifact.Requirements[0])
	}
}

func TestCompileWorkspaceKeepsImportAliasesFileScoped(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "left_use.mgo",
			Text: `
package main

import lib "example/left"

func LeftValue() int64 {
	return lib.Value()
}
`,
		}, {
			Path: "right_use.mgo",
			Text: `
package main

import lib "example/right"

func RightValue() int64 {
	return lib.Value()
}

func Main() int64 {
	return LeftValue()*10 + RightValue()
}
`,
		}},
	}, {
		ModulePath: "example/left",
		Files: []SourceFile{{
			Path: "left.mgo",
			Text: `
package left

func Value() int64 {
	return 4
}
`,
		}},
	}, {
		ModulePath: "example/right",
		Files: []SourceFile{{
			Path: "right.mgo",
			Text: `
package right

func Value() int64 {
	return 2
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	mainArtifact, ok := result.Artifact("example/main")
	if !ok {
		t.Fatalf("expected main artifact")
	}
	requirements := map[string][]string{}
	for _, requirement := range mainArtifact.Requirements {
		requirements[requirement.ModulePath] = append([]string(nil), requirement.Exports...)
	}
	for _, modulePath := range []string{"example/left", "example/right"} {
		exports := requirements[modulePath]
		if len(exports) != 1 || exports[0] != "Value" {
			t.Fatalf("expected %s.Value requirement, got %#v", modulePath, mainArtifact.Requirements)
		}
	}
}

func TestCompileWorkspaceUsesDependencyExportSignatures(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `
package main

import "example/text"

func Main() string {
	before, after, ok := text.Cut("compiler/runtime", "/")
	joined := text.Join([]string{before, after}, "-")
	sprinted := text.Sprint("mini", int64(42))
	if ok && sprinted == "mini42" {
		return joined
	}
	return ""
}
`,
		}},
	}, {
		ModulePath: "example/text",
		Files: []SourceFile{{
			Path: "text.mgo",
			Text: `
package text

func Cut(s string, sep string) (string, string, bool) {
	return s, sep, true
}

func Join(values []string, sep string) string {
	out := ""
	for i := 0; i < len(values); i++ {
		out += values[i]
	}
	return out
}

func Sprint(args ...any) string {
	out := ""
	for i := 0; i < len(args); i++ {
		switch value := args[i].(type) {
		case string:
			out += value
		case int64:
			out += "42"
		}
	}
	return out
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	mainArtifact, ok := result.Artifact("example/main")
	if !ok {
		t.Fatalf("expected main artifact")
	}
	if len(mainArtifact.Requirements) != 1 || mainArtifact.Requirements[0].ModulePath != "example/text" {
		t.Fatalf("expected text requirement, got %#v", mainArtifact.Requirements)
	}
	if len(mainArtifact.Requirements[0].Exports) != 3 || mainArtifact.Requirements[0].Exports[0] != "Cut" || mainArtifact.Requirements[0].Exports[1] != "Join" || mainArtifact.Requirements[0].Exports[2] != "Sprint" {
		t.Fatalf("expected Cut, Join, and Sprint requirements, got %#v", mainArtifact.Requirements[0].Exports)
	}
}

func TestCompileWorkspaceSupportsDotImports(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `
package main

import . "example/lib"

func Main() Score {
	value := Score(Answer)
	return Add(value, Default)
}

func Spawn() {
	go Add(Score(Answer), Default)
}
`,
		}},
	}, {
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `
package lib

const Answer = 40
var Default Score = 2
type Score int64

func Add(left Score, right Score) Score {
	return left + right
}
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	mainArtifact, ok := result.Artifact("example/main")
	if !ok {
		t.Fatalf("expected main artifact")
	}
	if len(mainArtifact.Requirements) != 1 || mainArtifact.Requirements[0].ModulePath != "example/lib" {
		t.Fatalf("expected dot import requirement, got %#v", mainArtifact.Requirements)
	}
	if len(mainArtifact.Requirements[0].Exports) != 4 {
		t.Fatalf("expected dot import exports to be tracked, got %#v", mainArtifact.Requirements[0].Exports)
	}
	if strings.Join(mainArtifact.Requirements[0].Exports, ",") != "Add,Answer,Default,Score" {
		t.Fatalf("expected stable dot import export set, got %#v", mainArtifact.Requirements[0].Exports)
	}
}
