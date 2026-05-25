package engine_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestSurfaceLibraryRunCases(t *testing.T) {
	bundle := surface.Merge(mathxLibrary(2), labelsLibrary(), multiFileLibrary(), leakyLibrary())
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "call exported function",
			Imports: []string{"mathx"},
			Expr:    "mathx.Double(21)",
			Output:  testutil.OutputInt,
			Want:    "42",
		},
		{
			Name:    "library imports another library",
			Imports: []string{"labels"},
			Expr:    "labels.Score(20)",
			Output:  testutil.OutputInt,
			Want:    "41",
		},
		{
			Name:    "multi file library exports struct values",
			Imports: []string{"bundle"},
			Body: `
	c := bundle.NewCounter(10)
	test.OutInt(bundle.Add(c, 5))
`,
			Want: "15",
		},
		{
			Name:           "package member rejects builtin leak",
			Imports:        []string{"mathx"},
			Expr:           "mathx.len(1)",
			WantCompileErr: "package mathx has no member len",
		},
		{
			Name:           "library cannot read main scope",
			Imports:        []string{"leaky"},
			Decls:          "var secret = 7",
			Expr:           "leaky.Value()",
			WantCompileErr: "variable secret does not exist",
		},
	}, testutil.WithSurface(bundle))
}

func TestSurfaceLibraryBytecodeLoadRequiresMatchingSourceHash(t *testing.T) {
	source := `
package main

import "labels"

func main() {
	if labels.Score(20) != 41 {
		panic("library score mismatch")
	}
	}
`
	compilerExec := engine.NewMiniExecutor()
	if err := compilerExec.UseSurface(labelsLibrary()); err != nil {
		t.Fatal(err)
	}
	if err := compilerExec.UseSurface(mathxLibrary(2)); err != nil {
		t.Fatal(err)
	}
	payload, err := compilerExec.CompileGoCodeToBytecodeJSON(source)
	if err != nil {
		t.Fatalf("compile bytecode: %v", err)
	}

	missing := engine.NewMiniExecutor()
	if _, err := missing.NewRuntimeByBytecodeJSON(payload); err == nil || !strings.Contains(err.Error(), "missing external VM module labels") {
		t.Fatalf("expected missing module hash error, got %v", err)
	}

	mismatch := engine.NewMiniExecutor()
	if err := mismatch.UseSurface(labelsLibrary()); err != nil {
		t.Fatal(err)
	}
	if err := mismatch.UseSurface(mathxLibrary(3)); err != nil {
		t.Fatal(err)
	}
	if _, err := mismatch.NewRuntimeByBytecodeJSON(payload); err == nil || !strings.Contains(err.Error(), "external VM module labels schema mismatch") {
		t.Fatalf("expected module hash mismatch, got %v", err)
	}

	loader := engine.NewMiniExecutor()
	if err := loader.UseSurface(labelsLibrary()); err != nil {
		t.Fatal(err)
	}
	if err := loader.UseSurface(mathxLibrary(2)); err != nil {
		t.Fatal(err)
	}
	prog, err := loader.NewRuntimeByBytecodeJSON(payload)
	if err != nil {
		t.Fatalf("load bytecode: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute bytecode: %v", err)
	}
}

func TestSurfaceLibraryMergeAndValidation(t *testing.T) {
	if merged := surface.Merge(mathxLibrary(2), mathxLibrary(2)); merged.Err != nil {
		t.Fatalf("same library should merge: %v", merged.Err)
	}
	if merged := surface.Merge(mathxLibrary(2), mathxLibrary(3)); merged.Err == nil {
		t.Fatalf("different library source should conflict")
	}

	exec := engine.NewMiniExecutor()
	if err := exec.UseSurface(surface.Library("empty")); err == nil || !strings.Contains(err.Error(), "has no source files") {
		t.Fatalf("expected no source files error, got %v", err)
	}
	if err := exec.UseSurface(surface.Libraries(surface.LibraryModule{
		Path: "badlang",
		Files: []surface.LibraryFile{{
			Filename: "badlang.mgo",
			Language: "python",
			Code:     "package badlang",
		}},
	})); err == nil || !strings.Contains(err.Error(), "unsupported language") {
		t.Fatalf("expected unsupported language error, got %v", err)
	}
	if err := exec.UseSurface(surface.Library("badsyntax", surface.GoFile("badsyntax.mgo", `package badsyntax
func Broken(
`))); err == nil || !strings.Contains(err.Error(), "parse surface library badsyntax") {
		t.Fatalf("expected parse error, got %v", err)
	}

	circular := surface.Merge(
		surface.Library("a", surface.GoFile("a.mgo", `package a
import "b"
func Value() int { return b.Value() }
`)),
		surface.Library("b", surface.GoFile("b.mgo", `package b
import "a"
func Value() int { return a.Value() }
`)),
	)
	if err := exec.UseSurface(circular); err == nil || !strings.Contains(err.Error(), "circular import dependency") {
		t.Fatalf("expected circular library dependency error, got %v", err)
	}
}

func TestSurfaceLibraryDoesNotPolluteBytecodeWhenUnused(t *testing.T) {
	compilerExec := engine.NewMiniExecutor()
	if err := compilerExec.UseSurface(mathxLibrary(2)); err != nil {
		t.Fatal(err)
	}
	payload, err := compilerExec.CompileGoCodeToBytecodeJSON(`
package main

func main() {}
`)
	if err != nil {
		t.Fatalf("compile bytecode: %v", err)
	}
	loader := engine.NewMiniExecutor()
	if _, err := loader.NewRuntimeByBytecodeJSON(payload); err != nil {
		t.Fatalf("unused surface library should not be required by bytecode: %v", err)
	}
}

func TestSurfaceLibraryConflictsWithRegisteredModule(t *testing.T) {
	exec := engine.NewMiniExecutor()
	compiled, err := exec.CompileGoCode(`
package mathx

func Double(v int) int { return v * 2 }
`)
	if err != nil {
		t.Fatalf("compile registered module: %v", err)
	}
	prog, err := exec.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("load registered module: %v", err)
	}
	exec.RegisterModule("mathx", prog)
	if err := exec.UseSurface(mathxLibrary(2)); err == nil || !strings.Contains(err.Error(), "conflicts with registered bytecode module") {
		t.Fatalf("expected registered module conflict, got %v", err)
	}
}

func mathxLibrary(multiplier int) *surface.Bundle {
	return surface.Library("mathx", surface.GoFile("mathx.mgo", `
package mathx

func Double(v int) int {
	return v * `+strconv.Itoa(multiplier)+`
}
`))
}

func labelsLibrary() *surface.Bundle {
	return surface.Library("labels", surface.GoFile("labels.mgo", `
package labels

import "mathx"

func Score(v int) int {
	return mathx.Double(v) + 1
}
`))
}

func multiFileLibrary() *surface.Bundle {
	return surface.Library("bundle",
		surface.GoFile("bundle_counter.mgo", `
package bundle

type Counter struct {
	Value int
}

func NewCounter(v int) Counter {
	return Counter{Value: v}
}
`),
		surface.GoFile("bundle_ops.mgo", `
package bundle

func Add(c Counter, delta int) int {
	return c.Value + delta
}
`),
	)
}

func leakyLibrary() *surface.Bundle {
	return surface.Library("leaky", surface.GoFile("leaky.mgo", `
package leaky

func Value() int {
	return secret
}
`))
}
