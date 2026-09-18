package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/types"
)

func TestCompileWorkspaceAllowsImportedDefinedTypeToUnnamedUnderlyingAssignment(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `
package main

import "example/lib"

func Main() int64 {
	var raw lib.Raw = []byte{40, 2}
	var kind lib.Kind = "ok"
	var out []byte
	out = raw
	if kind != "ok" {
		return 0
	}
	return int64(len(out))
}
`,
		}},
	}, {
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `
package lib

type Raw []byte
type Kind string
`,
		}},
	}})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileRejectsDistinctDefinedTypeAssignmentsWithIdenticalUnderlying(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{
			name: "scalar",
			source: `
package main
type Score int64
type Count int64
func Main() {
	var score Score = 1
	var count Count = score
	_ = count
}
`,
		},
		{
			name: "slice",
			source: `
package main
type Score int64
type Scores []Score
type MoreScores []Score
func Main() {
	var scores Scores = []Score{1}
	var more MoreScores = scores
	_ = more
}
`,
		},
		{
			name: "map",
			source: `
package main
type Score int64
type Lookup map[string]Score
type MoreLookup map[string]Score
func Main() {
	var lookup Lookup = map[string]Score{"x": 1}
	var more MoreLookup = lookup
	_ = more
}
`,
		},
		{
			name: "struct",
			source: `
package main
type Box struct { Value int64 }
type Crate struct { Value int64 }
func Main() {
	var box Box = Box{Value: 1}
	var crate Crate = box
	_ = crate
}
`,
		},
		{
			name: "array",
			source: `
package main
type Pair [2]int64
type OtherPair [2]int64
func Main() {
	var pair Pair = Pair{1, 2}
	var other OtherPair = pair
	_ = other
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, "semantic.assign.type")
		})
	}
}

func TestCompileWorkspaceQualifiesUnexportedDependencyResultTypes(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{{
		ModulePath: "example/main",
		Files: []SourceFile{{
			Path: "main.mgo",
			Text: `
package main

import "example/lib"

func Main() int64 {
	value := lib.New()
	if value == nil {
		return 0
	}
	return value.Read()
}

`,
		}},
	}, {
		ModulePath: "example/lib",
		Files: []SourceFile{{
			Path: "lib.mgo",
			Text: `
package lib

type hidden struct {
	Value int64
}

func New() *hidden {
	value := new(hidden)
	value.Value = 42
	return value
}

func (h *hidden) Read() int64 {
	return h.Value
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
	var sawQualifiedLocal bool
	if fn, functionSymbols, ok := artifactFunctionByName(mainArtifact, result.PackageSymbols["example/main"], "Main"); ok {
		localNames := functionLocalNames(functionSymbols)
		for _, local := range fn.Locals {
			if localNames[local.ID] == "value" && types.FormatWithTable(&mainArtifact.TypeTable, local.Type) == "Ptr<example/lib.hidden>" {
				sawQualifiedLocal = true
			}
		}
	}
	if !sawQualifiedLocal {
		t.Fatalf("expected local value to use qualified hidden pointer type")
	}
}

func TestCompileWorkspacePromotesNestedDependencyMethods(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/nodes"
func Empty(node nodes.Node) bool {
	switch node.(type) {
	case *nodes.IfNode:
		return false
	}
	return true
}

func Count(node *nodes.IfNode) int { return len(node.Pipe.Decl) }
`}}},
		{ModulePath: "example/nodes", Files: []SourceFile{{Path: "nodes.mgo", Text: `package nodes
type Position int
func (Position) Position() Position { return 0 }
type NodeType int
func (NodeType) Type() NodeType { return 0 }
type Node interface { Position() Position; Type() NodeType; tree() int }
type VariableNode struct{}
type PipeNode struct { Decl []*VariableNode }
type BranchNode struct { NodeType; Position; Pipe *PipeNode }
func (BranchNode) tree() int { return 0 }
type IfNode struct { BranchNode }
`}}},
	})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected nested promoted methods to satisfy the dependency interface, got %#v", result.Diagnostics)
	}
}

func TestCompileWorkspaceExportsEmbeddedInterfaceMethodSet(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{ModulePath: "example/main", Files: []SourceFile{{Path: "main.mgo", Text: `package main
import "example/io"
func Copy(reader io.Reader) {}
func Use(reader io.ReadCloser) { Copy(reader) }
`}}},
		{ModulePath: "example/io", Files: []SourceFile{{Path: "io.mgo", Text: `package io
type Reader interface { Read([]byte) (int, error) }
type Closer interface { Close() error }
type ReadCloser interface { Reader; Closer }
`}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("embedded dependency interface method set: %#v", result.Diagnostics)
	}
}
