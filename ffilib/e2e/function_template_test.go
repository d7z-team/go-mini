package e2e_test

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/ffilib/fmtlib"
)

type templateOutputRecorder struct {
	out strings.Builder
}

func (r *templateOutputRecorder) Print(_ context.Context, s string) {
	r.out.WriteString(s)
}

func newFFILibTemplateExecutor() *engine.MiniExecutor {
	return newStdExecutor()
}

func registerTemplateModule(t *testing.T, executor *engine.MiniExecutor, path, source string) {
	t.Helper()
	compiled, err := executor.CompileGoCode(source)
	if err != nil {
		t.Fatalf("compile module %s failed: %v", path, err)
	}
	prog, err := executor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("load module %s failed: %v", path, err)
	}
	executor.SetModuleLoader(func(request string) (*ast.ProgramStmt, error) {
		if request == path {
			return compiled.Program, nil
		}
		return nil, runtime.ErrModuleNotFound
	})
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
	executor := newFFILibTemplateExecutor()
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
	executor := newFFILibTemplateExecutor()
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
	executor := newFFILibTemplateExecutor()
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
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "aaa.BBB",
		PackagePath: "aaa",
		Name:        "BBB",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
		Body:        `{{ pkg "fmt" }}.Println({{ args }})`,
	})
	if err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	compiled, err := executor.CompileGoCode(`
package main

import x "aaa"

func main() {
	x.BBB("virtual")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	prog, err := executor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("load runtime failed: %v", err)
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
	if imports := compiled.ImportedPrograms; len(imports) != 0 {
		t.Fatalf("compile-only import leaked into artifact imports: %#v", imports)
	}
	for key := range compiled.Program.ImportLocs {
		if key == "x" || strings.HasSuffix(key, "\x1fx") {
			t.Fatalf("compile-only import location leaked: %#v", compiled.Program.ImportLocs)
		}
	}
}

func TestStatementTemplateCanExpandToMultipleStatements(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
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

func TestTemplateBodyShapeIsInferredFromRenderedContext(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.twice",
		Name:      "twice",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecInt64),
		Body:      `{{ arg 0 }} + {{ arg 0 }}`,
	}); err != nil {
		t.Fatalf("register expression template failed: %v", err)
	}
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Store",
		PackagePath: "trace",
		Name:        "Store",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		Body: `{{ fresh "value" }} := {{ arg 0 }}
{{ pkg "fmt" }}.Println({{ fresh "value" }})`,
	}); err != nil {
		t.Fatalf("register statement template failed: %v", err)
	}
	prog, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

func main() {
	v := twice(21)
	println(v)
	trace.Store("stmt")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	recorder := &templateOutputRecorder{}
	if err := prog.Execute(fmtlib.WithOutputter(context.Background(), recorder)); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got, want := recorder.out.String(), "42\nstmt\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}

func TestTemplateStatementBodyRequiresVoidSignature(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.badStmt",
		Name:      "badStmt",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecInt64),
		Body:      `{{ fresh "value" }} := {{ arg 0 }}`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	_, err := executor.CompileGoCode(`
package main

func main() {
	badStmt(1)
}
`)
	if err == nil || !strings.Contains(err.Error(), "renders statements and requires Void source signature") {
		t.Fatalf("expected non-void statement template error, got %v", err)
	}
}

func TestTemplateExpressionContextRejectsStatementBody(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.valueFromStmt",
		Name:      "valueFromStmt",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecInt64),
		Body: `{{ fresh "value" }} := {{ arg 0 }}
{{ fresh "value" }}`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	_, err := executor.CompileGoCode(`
package main

func main() {
	v := valueFromStmt(1)
	_ = v
}
`)
	if err == nil || !strings.Contains(err.Error(), "expand call template custom.valueFromStmt as expression") {
		t.Fatalf("expected expression-context statement template error, got %v", err)
	}
}

func TestTemplateInGoDeferMustExpandToCallExpression(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.deferValue",
		Name:      "deferValue",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		Body:      `{{ arg 0 }}`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	_, err := executor.CompileGoCode(`
package main

func main() {
	defer deferValue(1)
}
`)
	if err == nil || !strings.Contains(err.Error(), "go/defer call template must expand to a call expression") {
		t.Fatalf("expected go/defer call-only template error, got %v", err)
	}
}

func TestTemplateExpansionIsFixedPoint(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.note",
		Name:      "note",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		Body:      `println({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register note template failed: %v", err)
	}
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Inner",
		PackagePath: "trace",
		Name:        "Inner",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		Body: `{{ pkg "fmt" }}.Println("inner")
{{ pkg "fmt" }}.Println({{ arg 0 }})`,
	}); err != nil {
		t.Fatalf("register inner template failed: %v", err)
	}
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Outer",
		PackagePath: "trace",
		Name:        "Outer",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
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
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.loop",
		Name:      "loop",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, true, runtime.SpecAny),
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
			executor := newFFILibTemplateExecutor()
			err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
				ID:        "bad." + name,
				Name:      name,
				SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecInt64, false, runtime.SpecAny),
				Body:      `0`,
			})
			if err == nil {
				t.Fatalf("expected %s registration conflict", name)
			}
		})
	}
}

func TestTemplatePlanRejectsMissingPackagesAndMembers(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.missing",
		Name:      "badMissing",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
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

	executor = newFFILibTemplateExecutor()
	registerTemplateModule(t, executor, "trace", `
package main

func Other() {}
`)
	err = executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		Body:        `{{ pkg "fmt" }}.Println()`,
	})
	if err != nil {
		t.Fatalf("register missing member template failed: %v", err)
	}
	_, err = executor.CompileGoCode(`
package main

import "trace"

func main() {
	trace.Line()
}
`)
	if err == nil || !strings.Contains(err.Error(), "references missing package member trace.Line") {
		t.Fatalf("expected missing package member compile error, got %v", err)
	}
}

func TestUnusedInvalidTemplateDoesNotAffectCompilation(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.unused",
		Name:      "badUnused",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		Body:      `{{ pkg "missing/pkg" }}.Do()`,
	})
	if err != nil {
		t.Fatalf("register unused template failed: %v", err)
	}
	if _, err := executor.CompileGoCode(`
package main

func main() {
	println("ok")
}
`); err != nil {
		t.Fatalf("unused invalid template should not fail compilation: %v", err)
	}
}

func TestTemplateRegistrationRejectsInvalidPkgReference(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.pkg.dynamic",
		Name:      "badPkgDynamic",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		Body:      `{{ pkg (print "fmt") }}.Println()`,
	})
	if err == nil {
		t.Fatal("expected dynamic pkg reference registration error")
	}
	err = executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.pkg.empty",
		Name:      "badPkgEmpty",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		Body:      `{{ pkg "" }}.Println()`,
	})
	if err == nil {
		t.Fatal("expected empty pkg reference registration error")
	}
}

func TestTemplateRegistrationChecksExistingPackageMemberSignature(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	registerTemplateModule(t, executor, "trace", `
package main

func Real(v int64) {}
`)
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Real",
		PackagePath: "trace",
		Name:        "Real",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
		Body:        `println({{ arg 0 }})`,
	})
	if err != nil {
		t.Fatalf("register package template failed: %v", err)
	}
	_, err = executor.CompileGoCode(`
package main

import "trace"

func main() {
	trace.Real(1)
}
`)
	if err == nil || !strings.Contains(err.Error(), "does not match existing package member trace.Real") {
		t.Fatalf("expected package member signature conflict, got %v", err)
	}
}

func TestCompileOnlyResidualPackageUsageIsRejected(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
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
	if err == nil {
		t.Fatalf("expected compile-only residual error, got %v", err)
	}
}

func TestCompileOnlyPackageTemplateDoesNotRejectShadowedLocalAlias(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
		Body:        `println("template", {{ arg 0 }})`,
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
	trace.Line("facade")
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
	if got, want := recorder.out.String(), "template facade\nlocal value\n"; got != want {
		t.Fatalf("unexpected output %q, want %q", got, want)
	}
}

func TestPackageTemplateDoesNotMatchShadowedAlias(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	registerTemplateModule(t, executor, "trace", `
package main

func Line(v string) {}
`)
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecString),
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
			executor := newFFILibTemplateExecutor()
			_, err := executor.NewRuntimeByGoCode(tc.code)
			if err == nil {
				t.Fatal("expected reserved template name error")
			}
		})
	}
}

func TestInternalTemplatePrefixCannotBeDeclared(t *testing.T) {
	executor := newFFILibTemplateExecutor()
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

func TestTemplateNameCannotBeUsedAsRuntimeValue(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"local assignment", `package main
func main() {
	f := println
	_ = f
}`},
		{"global assignment", `package main
var f = println
func main() {
	_ = f
}`},
		{"composite literal", `package main
func main() {
	_ = []any{println}
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := newFFILibTemplateExecutor()
			_, err := executor.NewRuntimeByGoCode(tc.code)
			if err == nil {
				t.Fatal("expected template value usage to be rejected")
			}
		})
	}
}

func TestPackageTemplateNameCannotBeUsedAsRuntimeValue(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:          "trace.Line",
		PackagePath: "trace",
		Name:        "Line",
		SourceSig:   runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
		Body:        `println()`,
	}); err != nil {
		t.Fatalf("register template failed: %v", err)
	}
	_, err := executor.NewRuntimeByGoCode(`
package main

import "trace"

func main() {
	f := trace.Line
	_ = f
}
`)
	if err == nil {
		t.Fatal("expected package template value usage to be rejected")
	}
}

func TestTemplateBodyRejectsDataObjectAccess(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "bad.data",
		Name:      "badData",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
		Body:      `println({{ .Args }})`,
	})
	if err == nil {
		t.Fatal("expected template data object access registration error")
	}
}

func TestCustomGlobalTemplateNameIsReserved(t *testing.T) {
	executor := newFFILibTemplateExecutor()
	err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.note",
		Name:      "note",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false, runtime.SpecAny),
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

	executor := newFFILibTemplateExecutor()
	if err := executor.RegisterFunctionTemplate(calltemplate.FunctionTemplate{
		ID:        "custom.later",
		Name:      "later",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
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
		Name:      "foo",
		SourceSig: runtime.MustRuntimeFuncSig(runtime.SpecVoid, false),
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
