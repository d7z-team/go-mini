package compiler

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestCompilePreservesNoSwitchFunctionMetadata(t *testing.T) {
	result, err := compileTestSource("example/noswitch", "main.mgo", `package noswitch

//minigo:noswitch
func update(value *int) { *value = *value + 1 }

func Apply(value *int) { update(value) }
`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	found := false
	if function, _, ok := artifactFunctionByName(result.Artifact, result.Symbols, "update"); ok {
		found = function.NoSwitch
	}
	if !found {
		t.Fatal("compiled update function is not marked no-switch")
	}
}

func TestCompilePreservesNoSwitchOnGenericSpecialization(t *testing.T) {
	result, err := compileTestSource("example/noswitch", "main.mgo", `package noswitch

//minigo:noswitch
func identity[T any](value T) T { return value }

func Apply() int { return identity(1) }
`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	for _, symbol := range result.Symbols.Functions {
		if strings.HasPrefix(symbol.Name, "generic_identity_") {
			function, _, ok := artifactFunctionByName(result.Artifact, result.Symbols, symbol.Name)
			if !ok {
				t.Fatal("generic specialization symbols reference missing function")
			}
			if !function.NoSwitch {
				t.Fatal("generic specialization lost no-switch metadata")
			}
			return
		}
	}
	t.Fatal("generic identity specialization was not emitted")
}

func TestCompileRejectsSchedulingInsideNoSwitchFunction(t *testing.T) {
	result, err := compileTestSource("example/noswitch", "main.mgo", `package noswitch

func helper() {}

//minigo:noswitch
func update() { helper() }
`)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Fatal("no-switch function call compiled successfully")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "semantic.noswitch.operation" {
			return
		}
	}
	t.Fatalf("missing no-switch compiler diagnostic: %#v", result.Diagnostics)
}

func TestCheckRejectsSchedulingInsideNoSwitchFunction(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example/noswitch",
		Files: []source.File{{Path: "main.mgo", Text: `package noswitch

//minigo:noswitch
func update(ch chan int) { <-ch }
`}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Check(Request{Root: "example/noswitch", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Fatal("Check accepted a blocking no-switch function")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "semantic.noswitch.operation" {
			return
		}
	}
	t.Fatalf("missing no-switch Check diagnostic: %#v", result.Diagnostics)
}

func TestCheckRejectsGenericChannelRangeInsideNoSwitchFunction(t *testing.T) {
	result, err := compileTestSource("example/noswitch", "main.mgo", `package noswitch

//minigo:noswitch
func drain[C ~chan int](ch C) {
	for range ch {}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Fatal("no-switch function with a generic channel range compiled successfully")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "semantic.noswitch.operation" {
			return
		}
	}
	t.Fatalf("missing no-switch compiler diagnostic: %#v", result.Diagnostics)
}

func TestCheckRejectsCompoundOperatorOverloadInsideNoSwitchFunction(t *testing.T) {
	result, err := compileTestSource("example/noswitch", "main.mgo", `package noswitch

type Value struct { Number int }

func (v Value) OpAdd(other Value) Value {
	return Value{Number: v.Number + other.Number}
}

//minigo:noswitch
func add(left *Value, right Value) {
	*left += right
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Fatal("no-switch function with a compound operator overload compiled successfully")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "semantic.noswitch.operation" {
			return
		}
	}
	t.Fatalf("missing no-switch compiler diagnostic: %#v", result.Diagnostics)
}
