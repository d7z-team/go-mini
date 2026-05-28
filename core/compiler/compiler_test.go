package compiler

import (
	"os"
	"path/filepath"
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
		FuncSchemas: map[ast.Ident]*runtime.RuntimeFuncSig{
			"fmt.Sprintf": runtime.MustParseRuntimeFuncSig("function(String, ...Any) String"),
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
		"section .data",
		"section .text",
		"global.base:",
		"global.result:",
		"fn.add: ; signature",
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

func TestCompileSourceRewritesOperatorOverloadBeforeBytecode(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

type Vec struct {
	X int
}

var a = Vec{X: 1}
var b = Vec{X: 2}
var result = a + b

func (v Vec) OpAdd(other Vec) Vec {
	return other
}
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if artifact.Bytecode == nil {
		t.Fatal("expected bytecode artifact, got nil")
	}
	asm := artifact.Bytecode.Disassemble()
	if strings.Contains(asm, runtime.OpApplyBinary.String()) {
		t.Fatalf("overloaded operator should be rewritten before bytecode:\n%s", asm)
	}
	if !strings.Contains(asm, "OpAdd") {
		t.Fatalf("expected rewritten method call in disassembly:\n%s", asm)
	}
}

func TestCompileSourceRewritesNestedOperatorOverloads(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

type Vec struct {
	X int
}

var a = Vec{X: 1}
var b = Vec{X: 2}

func (v Vec) OpAdd(other Vec) Vec {
	return other
}

func main() {
	items := []Vec{a + b}
	_ = func() Vec {
		return items[0] + b
	}()
	switch (a + b).X {
	case (b + a).X:
	}
}
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	asm := artifact.Bytecode.Disassemble()
	if strings.Contains(asm, runtime.OpApplyBinary.String()) {
		t.Fatalf("nested overloaded operators should be rewritten before bytecode:\n%s", asm)
	}
}

func TestCompileSourcePushesNamedConstants(t *testing.T) {
	c := New(Config{})
	artifact, _, _, err := c.CompileSource("snippet", `
package main

const one = 1

var result = one
`, false)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	asm := artifact.Bytecode.Disassemble()
	if strings.Contains(asm, runtime.OpLoadVar.String()) && strings.Contains(asm, "one") {
		t.Fatalf("named constant should be pushed, not loaded as a variable:\n%s", asm)
	}
}

func TestCompileSourceAcceptsParsedSchema(t *testing.T) {
	funcSig, err := runtime.ParseRuntimeFuncSig("function(String, ...Any) String")
	if err != nil {
		t.Fatalf("parse func schema failed: %v", err)
	}
	structSig, err := runtime.ParseRuntimeStructSpec("demo.Payload", runtime.StructOwnershipVMValue, "struct { Msg String; Count Int64; }")
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

func TestCompileSourceRejectsVMCreatedHostOpaqueTypes(t *testing.T) {
	hostSpec := runtime.MustParseRuntimeStructSpec("demo.Handle", runtime.StructOwnershipHostOpaque, "struct { Ping function(HostRef<demo.Handle>) Void; }")
	c := New(Config{
		StructSchemas: map[ast.Ident]*runtime.RuntimeStructSpec{
			"demo.Handle": hostSpec,
		},
	})

	cases := []struct {
		name string
		src  string
	}{
		{
			name: "var value",
			src: `package main
import "demo"
var h demo.Handle
`,
		},
		{
			name: "function param value",
			src: `package main
import "demo"
func use(h demo.Handle) {}
`,
		},
		{
			name: "composite literal",
			src: `package main
import "demo"
func main() { _ = demo.Handle{} }
`,
		},
		{
			name: "new value",
			src: `package main
import "demo"
func main() { _ = new(demo.Handle) }
`,
		},
		{
			name: "slice of values",
			src: `package main
import "demo"
func main() { _ = make([]demo.Handle, 1) }
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := c.CompileSource("snippet", tc.src, false)
			if err == nil {
				t.Fatalf("expected compile error for %s", tc.name)
			}
		})
	}
}

func TestCompileSourceAllowsHostRefFromFactory(t *testing.T) {
	hostSpec := runtime.MustParseRuntimeStructSpec("demo.Handle", runtime.StructOwnershipHostOpaque, "struct { Ping function(HostRef<demo.Handle>) Void; }")
	c := New(Config{
		FuncSchemas: map[ast.Ident]*runtime.RuntimeFuncSig{
			"demo.NewHandle": runtime.MustParseRuntimeFuncSig("function() HostRef<demo.Handle>"),
		},
		StructSchemas: map[ast.Ident]*runtime.RuntimeStructSpec{
			"demo.Handle": hostSpec,
		},
	})

	_, _, _, err := c.CompileSource("snippet", `package main
import "demo"
var h *demo.Handle
func main() {
	h = demo.NewHandle()
	h.Ping()
}
`, false)
	if err != nil {
		t.Fatalf("expected HostRef factory usage to compile: %v", err)
	}
}

func TestCompileSourceKeepsExecutableBytecode(t *testing.T) {
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

func TestCompileFilesMergesTypesAndDedupesImports(t *testing.T) {
	c := New(Config{
		FuncSchemas: map[ast.Ident]*runtime.RuntimeFuncSig{
			"fmt.Sprintf": runtime.MustParseRuntimeFuncSig("function(String, ...Any) String"),
		},
	})

	artifact, _, _, err := c.CompileFiles([]SourceFile{
		{
			Filename: "a.go",
			Code: `package main

import "fmt"

type Alias int
`,
		},
		{
			Filename: "b.go",
			Code: `package main

import "fmt"

var msg = fmt.Sprintf("%s", "hi")
func main() {
	var _ Alias = 1
}
`,
		},
	}, false)
	if err != nil {
		t.Fatalf("compile files failed: %v", err)
	}
	if _, ok := artifact.Program.Types["Alias"]; !ok {
		t.Fatalf("expected merged type alias, got %+v", artifact.Program.Types)
	}
	if len(artifact.Program.Imports) != 1 {
		t.Fatalf("expected deduped imports, got %+v", artifact.Program.Imports)
	}
}

func TestCompileFilesRejectsPackageMismatch(t *testing.T) {
	c := New(Config{})
	_, _, _, err := c.CompileFiles([]SourceFile{
		{Filename: "a.go", Code: "package main\n"},
		{Filename: "b.go", Code: "package other\n"},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "package mismatch") {
		t.Fatalf("expected package mismatch error, got %v", err)
	}
}

func TestCompileFilesRejectsDuplicateDefinitions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "function function",
			a:    "package main\nfunc helper() {}\n",
			b:    "package main\nfunc helper() {}\n",
			want: "duplicate function definition",
		},
		{
			name: "variable variable",
			a:    "package main\nvar helper Int64\n",
			b:    "package main\nvar helper Int64\n",
			want: "duplicate variable definition",
		},
		{
			name: "variable function",
			a:    "package main\nvar helper Int64\n",
			b:    "package main\nfunc helper() {}\n",
			want: "duplicate top-level symbol helper",
		},
		{
			name: "type function",
			a:    "package main\ntype helper Int64\n",
			b:    "package main\nfunc helper() {}\n",
			want: "duplicate top-level symbol helper",
		},
		{
			name: "import alias conflict",
			a:    "package main\nimport f \"fmt\"\n",
			b:    "package main\nimport f \"strings\"\n",
			want: "duplicate import alias f",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := New(Config{}).CompileFiles([]SourceFile{
				{Filename: "a.go", Code: tc.a},
				{Filename: "b.go", Code: tc.b},
			}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestCompileSourceRejectsSingleFileDuplicateTopLevelSymbols(t *testing.T) {
	cases := []struct {
		name string
		code string
		want string
	}{
		{
			name: "function function",
			code: "package main\nfunc helper() {}\nfunc helper() {}\n",
			want: "duplicate top-level function helper",
		},
		{
			name: "variable function",
			code: "package main\nvar helper Int64\nfunc helper() {}\n",
			want: "duplicate top-level function helper",
		},
		{
			name: "type type",
			code: "package main\ntype helper Int64\ntype helper String\n",
			want: "duplicate top-level type helper",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := New(Config{}).CompileSource("snippet", tc.code, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestCompileDirLoadsOnlyMGOFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mgo"), []byte("package main\n\ntype Alias int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.mgo"), []byte("package main\n\nfunc main() { var _ Alias = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.go"), []byte("package main\n\nfunc ignored() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(Config{})
	artifact, _, _, err := c.CompileDir(dir, false)
	if err != nil {
		t.Fatalf("compile dir failed: %v", err)
	}
	if _, ok := artifact.Program.Types["Alias"]; !ok {
		t.Fatalf("expected type from .mgo files, got %+v", artifact.Program.Types)
	}
	if _, ok := artifact.Program.Functions["ignored"]; ok {
		t.Fatalf("did not expect .go file to be loaded in directory mode")
	}
}

func TestCompileDirRejectsMissingMGOFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "only.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New(Config{})
	_, _, _, err := c.CompileDir(dir, false)
	if err == nil || !strings.Contains(err.Error(), ScriptFileExt) {
		t.Fatalf("expected missing .mgo error, got %v", err)
	}
}
