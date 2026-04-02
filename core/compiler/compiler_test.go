package compiler

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
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
	if err := artifact.Bytecode.Validate(); err != nil {
		t.Fatalf("expected valid bytecode artifact: %v", err)
	}

	asm := artifact.Bytecode.Disassemble()
	expected := []string{
		"go-mini-bytecode",
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

func TestCompileSourceBytecodeUsesRuntimeOpcodeNames(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

var current = 1

func main() {
	current = current + 1
}
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if artifact.Bytecode == nil {
		t.Fatal("expected bytecode artifact, got nil")
	}

	asm := artifact.Bytecode.Disassemble()
	expected := []string{
		runtime.OpPush.String(),
		runtime.OpEvalLHS.String(),
		runtime.OpLoadVar.String(),
		runtime.OpAssign.String(),
		runtime.OpApplyBinary.String(),
	}
	for _, sym := range expected {
		if !strings.Contains(asm, sym) {
			t.Fatalf("expected %q in disassembly, got:\n%s", sym, asm)
		}
	}
}

func TestCompileSourceAcceptsParsedSchema(t *testing.T) {
	funcSig, err := runtime.ParseRuntimeFuncSig("function(String, ...Any) String")
	if err != nil {
		t.Fatalf("parse func schema failed: %v", err)
	}
	structSig, err := runtime.ParseRuntimeStructSpec("demo.Payload", "struct { Msg String; Count Int64; }")
	if err != nil {
		t.Fatalf("parse struct schema failed: %v", err)
	}

	c := New(Config{
		FuncSchemas: map[ast.Ident]*runtime.RuntimeFuncSig{
			"fmt.Sprintf": funcSig,
		},
		StructSchemas: map[ast.Ident]*runtime.RuntimeStructSpec{
			"demo.Payload": structSig,
		},
	})

	artifact, _, _, err := c.CompileSource("snippet", `
package main

import "fmt"

type Payload struct {
	Msg string
	Count int
}

var msg = fmt.Sprintf("%s", "hi")
`, false)
	if err != nil {
		t.Fatalf("compile failed with parsed schema: %v", err)
	}
	if artifact == nil || artifact.Program == nil {
		t.Fatal("expected compiled artifact")
	}
}

func TestCompileSourceKeepsExecutableBytecodeWhenDisplayBytecodeUnsupported(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

func cleanup() {}

func main() {
	defer cleanup()
}
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if artifact.Bytecode == nil {
		t.Fatal("expected bytecode artifact")
	}
	if artifact.Bytecode.Executable == nil {
		t.Fatal("expected executable bytecode payload")
	}
}
