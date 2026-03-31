package compiler

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

func TestCompileSourceResolvesGlobalInitDependencies(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

var b = a + 1
var a = 1
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	if len(artifact.GlobalInitOrder) != 2 {
		t.Fatalf("unexpected global init order size: %d", len(artifact.GlobalInitOrder))
	}
	if artifact.GlobalInitOrder[0] != "a" || artifact.GlobalInitOrder[1] != "b" {
		t.Fatalf("unexpected global init order: %v", artifact.GlobalInitOrder)
	}
}

func TestCompileSourcePreservesDeclaredImportsBeforeGlobals(t *testing.T) {
	c := New(Config{
		Specs: map[ast.Ident]ast.GoMiniType{
			"fmt.Sprintf": "function(String, ...Any) String",
		},
	})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

import "fmt"

var msg = fmt.Sprintf("%s", "hi")
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	if len(artifact.GlobalInitOrder) < 2 {
		t.Fatalf("unexpected global init order: %v", artifact.GlobalInitOrder)
	}
	if artifact.GlobalInitOrder[0] != "fmt" {
		t.Fatalf("import alias should be initialized first: %v", artifact.GlobalInitOrder)
	}
}

func TestCompileSourceBuildsBytecodeWhenSupported(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

var base = 1
var result = add(2)

func add(v int) int {
	return v + base
}

func main() {}
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if artifact.Bytecode == nil {
		t.Fatal("expected bytecode artifact, got nil")
	}

	asm := artifact.Bytecode.Disassemble()
	expected := []string{
		"section .data:",
		"section .text:",
		"global base",
		"global result",
		"add(",
		"CALL",
	}
	for _, sym := range expected {
		if !strings.Contains(asm, sym) {
			t.Fatalf("expected %q in disassembly, got:\n%s", sym, asm)
		}
	}

	payload, err := artifact.MarshalBytecodeJSON()
	if err != nil {
		t.Fatalf("marshal bytecode failed: %v", err)
	}
	if !strings.Contains(string(payload), "\"globals\"") {
		t.Fatalf("unexpected bytecode payload: %s", payload)
	}
}
