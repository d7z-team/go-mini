package tests

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type templateOutputRecorder struct {
	out strings.Builder
}

func (r *templateOutputRecorder) Print(_ context.Context, s string) {
	r.out.WriteString(s)
}

func registerTemplateModule(t *testing.T, executor *engine.MiniExecutor, path, source string) {
	t.Helper()
	prog, err := executor.NewRuntimeByGoCode(source)
	if err != nil {
		t.Fatalf("compile module %s failed: %v", path, err)
	}
	executor.RegisterModule(path, prog)
}

func hasImportAlias(aliases map[string]string, path, prefix string) bool {
	for alias, gotPath := range aliases {
		if gotPath == path && strings.HasPrefix(alias, prefix) {
			return true
		}
	}
	return false
}

func TestDefaultPrintlnTemplate(t *testing.T) {
	executor := engine.NewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

func main() {
	println("hello", 7)
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "hello 7\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
	if aliases := prog.Compilation().Bytecode.Executable.ImportAliases; !hasImportAlias(aliases, "fmt", calltemplate.InternalNamePrefix+"pkg_fmt") {
		t.Fatalf("expected generated fmt import alias, got %#v", aliases)
	}
}

func TestTemplateUsesHygienicImportAlias(t *testing.T) {
	executor := engine.NewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

import f "fmt"

func main() {
	f := "local"
	_ = f
	print("hi")
	println("!")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "hi!\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
	aliases := prog.Compilation().Bytecode.Executable.ImportAliases
	if aliases["f"] != "fmt" || !hasImportAlias(aliases, "fmt", calltemplate.InternalNamePrefix+"pkg_fmt") {
		t.Fatalf("expected user fmt alias plus generated hygienic fmt alias, got %#v", aliases)
	}
}

func TestTemplateArgumentsAreASTPlaceholders(t *testing.T) {
	executor := engine.NewMiniExecutor()
	if _, err := executor.CompileGoCode(`
package main

func main() {
	println([]int64{1, 2})
	println(func() {})
}
`); err != nil {
		t.Fatalf("compile with composite and function literal args failed: %v", err)
	}
}

func TestCompileOnlyPackageTemplateIsRemovedBeforeBytecode(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "aaa.BBB",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "aaa",
		Member:      "BBB",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
		BodyKind:    calltemplate.TemplateExpr,
		Imports:     []calltemplate.TemplateImport{{Path: "fmt", AliasHint: "fmt"}},
		Body:        `{{ pkg "fmt" }}.Println({{ args }})`,
	})
	if err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	prog, err := executor.NewRuntimeByGoCode(`
package main

import x "aaa"

func main() {
	x.BBB("virtual")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "virtual\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
	aliases := prog.Compilation().Bytecode.Executable.ImportAliases
	if _, ok := aliases["x"]; ok {
		t.Fatalf("compile-only import leaked into bytecode aliases: %#v", aliases)
	}
	if hasImportAlias(aliases, "aaa", calltemplate.InternalNamePrefix) {
		t.Fatalf("synthetic compile-only import leaked into bytecode aliases: %#v", aliases)
	}
	if imports := prog.Compilation().ImportedPrograms; len(imports) != 0 {
		t.Fatalf("compile-only import leaked into artifact imports: %#v", imports)
	}
	for key := range prog.Compilation().Program.ImportLocs {
		if key == "x" || strings.HasSuffix(key, "\x1fx") {
			t.Fatalf("compile-only import location leaked: %#v", prog.Compilation().Program.ImportLocs)
		}
	}
}

func TestStatementTemplateCanExpandToMultipleStatements(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Line",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:    calltemplate.TemplateStmt,
		Imports:     []calltemplate.TemplateImport{{Path: "fmt", AliasHint: "fmt"}},
		Body: `{{ pkg "fmt" }}.Println("trace")
{{ pkg "fmt" }}.Println({{ arg 0 }})`,
	})
	if err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

func main() {
	trace.Line("value")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "trace\nvalue\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}

func TestTemplateExpansionIsFixedPoint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.note",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "note",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:  calltemplate.TemplateExpr,
		Body:      `println({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register note template failed: %v", err)
	}
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Inner",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Inner",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:    calltemplate.TemplateStmt,
		Imports:     []calltemplate.TemplateImport{{Path: "fmt", AliasHint: "fmt"}},
		Body: `{{ pkg "fmt" }}.Println("inner")
{{ pkg "fmt" }}.Println({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register inner template failed: %v", err)
	}
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Outer",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Outer",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:    calltemplate.TemplateStmt,
		Imports:     []calltemplate.TemplateImport{{Path: "trace", AliasHint: "trace"}},
		Body:        `{{ pkg "trace" }}.Inner({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register outer template failed: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

func main() {
	note("from global")
	trace.Outer("from stmt")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "from global\ninner\nfrom stmt\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}

func TestRecursiveTemplateExpansionIsRejected(t *testing.T) {
	executor := engine.NewMiniExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.loop",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "loop",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
		BodyKind:  calltemplate.TemplateExpr,
		Body:      `loop({{ args }})`,
	}); err != nil {
		t.Fatalf("register loop template failed: %v", err)
	}
	_, err := executor.NewRuntimeByGoCode(`
package main

func main() {
	loop("x")
}
`)
	if err == nil || !strings.Contains(err.Error(), "recursive call template expansion") {
		t.Fatalf("expected recursive template expansion error, got %v", err)
	}
}

func TestTemplateRegistrationRejectsRealSymbolConflicts(t *testing.T) {
	for _, name := range []string{"len", "panic", "append"} {
		t.Run(name, func(t *testing.T) {
			executor := engine.NewMiniExecutor()
			err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
				ID:        "bad." + name,
				Kind:      calltemplate.TemplateGlobalFunc,
				Name:      name,
				SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny),
				BodyKind:  calltemplate.TemplateExpr,
				Body:      `0`,
			})
			if err == nil {
				t.Fatalf("expected %s registration conflict", name)
			}
		})
	}
}

func TestTemplatePlanRejectsMissingRuntimePackages(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.missing",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "badMissing",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:  calltemplate.TemplateExpr,
		Imports:   []calltemplate.TemplateImport{{Path: "missing/pkg", AliasHint: "missing"}},
		Body:      `{{ pkg "missing/pkg" }}.Do()`,
	})
	if err != nil {
		t.Fatalf("register missing import template failed: %v", err)
	}
	_, err = executor.CompileGoCode(`
package main

func main() {
	badMissing()
}
`)
	if err == nil || !strings.Contains(err.Error(), "references missing package missing/pkg") {
		t.Fatalf("expected missing runtime import compile error, got %v", err)
	}

	err = executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "missing.Line",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "missing",
		Member:      "Line",
		PackageMode: calltemplate.RuntimePackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:    calltemplate.TemplateExpr,
		Body:        `println()`,
	})
	if err != nil {
		t.Fatalf("register missing runtime package template failed: %v", err)
	}
	_, err = executor.CompileGoCode(`
package main

import "missing"

func main() {
	missing.Line()
}
`)
	if err == nil || !strings.Contains(err.Error(), "references missing package missing") {
		t.Fatalf("expected missing runtime package compile error, got %v", err)
	}
}

func TestTemplateRegistrationRejectsInvalidAliasHint(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.alias",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "badAlias",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:  calltemplate.TemplateExpr,
		Imports:   []calltemplate.TemplateImport{{Path: "fmt", AliasHint: "for"}},
		Body:      `{{ pkg "fmt" }}.Println()`,
	})
	if err == nil {
		t.Fatal("expected invalid alias hint registration error")
	}
}

func TestTemplateRegistrationRejectsMixedPackageModes(t *testing.T) {
	executor := engine.NewMiniExecutor()
	registerTemplateModule(t, executor, "trace", `package trace`)
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Runtime",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Runtime",
		PackageMode: calltemplate.RuntimePackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:    calltemplate.TemplateExpr,
		Body:        `println()`,
	}); err != nil {
		t.Fatalf("register runtime package template failed: %v", err)
	}
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.CompileOnly",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "CompileOnly",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:    calltemplate.TemplateExpr,
		Body:        `println()`,
	})
	if err == nil {
		t.Fatal("expected mixed package mode registration error")
	}
}

func TestTemplateRegistrationChecksExistingPackageMemberSignature(t *testing.T) {
	executor := engine.NewMiniExecutor()
	registerTemplateModule(t, executor, "trace", `
package trace

func Real(v int64) {}
`)
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Real",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Real",
		PackageMode: calltemplate.RuntimePackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
		BodyKind:    calltemplate.TemplateExpr,
		Body:        `println({{ arg 0 }})`,
	})
	if err != nil {
		t.Fatalf("register runtime package template failed: %v", err)
	}
	_, err = executor.CompileGoCode(`
package main

import "trace"

func main() {
	trace.Real("bad")
}
`)
	if err == nil || !strings.Contains(err.Error(), "does not match existing package member trace.Real") {
		t.Fatalf("expected package member signature conflict, got %v", err)
	}
}

func TestCompileOnlyResidualPackageUsageIsRejected(t *testing.T) {
	executor := engine.NewMiniExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Line",
		PackageMode: calltemplate.CompileOnlyPackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:    calltemplate.TemplateExpr,
		Imports:     []calltemplate.TemplateImport{{Path: "trace", AliasHint: "trace"}},
		Body:        `{{ pkg "trace" }}.Other({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register compile-only template failed: %v", err)
	}
	_, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

func main() {
	trace.Line("x")
}
`)
	if err == nil || !strings.Contains(err.Error(), "compile-only template package alias") {
		t.Fatalf("expected compile-only residual error, got %v", err)
	}
}

func TestPackageTemplateDoesNotMatchShadowedAlias(t *testing.T) {
	executor := engine.NewMiniExecutor()
	registerTemplateModule(t, executor, "trace", `
package trace

func Line(v string) {}
`)
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		Kind:        calltemplate.TemplatePackageFunc,
		PackagePath: "trace",
		Member:      "Line",
		PackageMode: calltemplate.RuntimePackage,
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
		BodyKind:    calltemplate.TemplateExpr,
		Body:        `println("template")`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

type Local struct{}

func (l Local) Line(v string) {
	println("local", v)
}

func main() {
	trace := Local{}
	trace.Line("value")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "local value\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}

func TestReservedTemplateNameCannotBeDeclared(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"function", `package main
func println() {}`},
		{"constant", `package main
const println = "x"`},
		{"type", `package main
type println int64`},
		{"struct", `package main
type println struct{ V int64 }`},
		{"interface", `package main
type println interface{ Do() }`},
		{"local", `package main
func main() {
	println := 1
	_ = println
}`},
		{"parameter", `package main
func main() {
	f := func(println int64) {}
	_ = f
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := engine.NewMiniExecutor()
			_, err := executor.NewRuntimeByGoCode(tc.code)
			if err == nil {
				t.Fatal("expected reserved template name error")
			}
		})
	}
}

func TestInternalTemplatePrefixCannotBeDeclared(t *testing.T) {
	executor := engine.NewMiniExecutor()
	_, err := executor.NewRuntimeByGoCode(`
package main

func main() {
	__gomini_tpl_value := 1
	_ = __gomini_tpl_value
}
`)
	if err == nil {
		t.Fatal("expected internal template prefix declaration error")
	}
}

func TestTemplateBodyRejectsDataObjectAccess(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.data",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "badData",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:  calltemplate.TemplateExpr,
		Body:      `println({{ .Args }})`,
	})
	if err == nil {
		t.Fatal("expected template data object access registration error")
	}
}

func TestCustomGlobalTemplateNameIsReserved(t *testing.T) {
	executor := engine.NewMiniExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.note",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "note",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		BodyKind:  calltemplate.TemplateExpr,
		Imports:   []calltemplate.TemplateImport{{Path: "fmt", AliasHint: "fmt"}},
		Body:      `{{ pkg "fmt" }}.Println({{ arg 0 }})`,
	})
	if err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	_, err = executor.NewRuntimeByGoCode(`
package main

func main() {
	note := 1
	_ = note
}
`)
	if err == nil {
		t.Fatal("expected custom global template name conflict")
	}
}

func TestRealSymbolCannotBeRegisteredAfterGlobalTemplate(t *testing.T) {
	expectPanic := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatal("expected template conflict panic")
			}
		}()
		fn()
	}

	executor := engine.NewMiniExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.later",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "later",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:  calltemplate.TemplateExpr,
		Body:      `println()`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}

	expectPanic(t, func() {
		executor.DeclareFuncSchema("later", runtime.MustRuntimeFuncSig(runtime.SpecVoid, false))
	})
	expectPanic(t, func() {
		executor.RegisterConstant("later", "1")
	})
	expectPanic(t, func() {
		executor.RegisterStructSchema("later", runtime.MustParseRuntimeStructSpec("later", runtime.StructOwnershipVMValue, "struct { V Int64; }"))
	})
	expectPanic(t, func() {
		executor.RegisterInterfaceSchema("later", runtime.MustParseRuntimeInterfaceSpec("interface{Do() Void;}"))
	})
}

func TestCompilerRejectsDirectTemplateSchemaConflict(t *testing.T) {
	registry := calltemplate.NewRegistry()
	if err := registry.Register(calltemplate.FunctionTemplate{
		ID:        "custom.foo",
		Kind:      calltemplate.TemplateGlobalFunc,
		Name:      "foo",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		BodyKind:  calltemplate.TemplateExpr,
		Body:      `println()`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	c := compiler.New(compiler.Config{
		FuncSchemas: map[ast.Ident]*runtime.RuntimeFuncSig{
			"foo": runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		},
		Templates: registry,
	})
	_, _, _, err := c.CompileSource("snippet", `
package main

func main() {
	foo()
}
`, false)
	if err == nil || !strings.Contains(err.Error(), "conflicts with existing symbol foo") {
		t.Fatalf("expected direct compiler template conflict, got %v", err)
	}
}
