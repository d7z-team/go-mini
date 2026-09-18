package compiler_test

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestImportedMembersRequireVisibleDeclarations(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"call", "func main() { lib.Missing() }", "semantic.import.member.missing"},
		{"unused", "func unused() { lib.Missing() }; func main() {}", "semantic.import.member.missing"},
		{"value", "var value = lib.Missing; func main() {}", "semantic.import.member.missing"},
		{"type", "var value lib.Missing; func main() {}", "semantic.import.member.missing"},
		{"private-function", "func main() { lib.hidden() }", "semantic.import.member.unexported"},
		{"private-type", "var value lib.hiddenType; func main() {}", "semantic.import.member.unexported"},
		{"not-type", "var value lib.Present; func main() {}", "semantic.import.member.not_type"},
		{"generic", "var value lib.Missing[int]; func main() {}", "semantic.import.member.missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
				{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Present() {}\nfunc hidden() {}\ntype hiddenType int\n"}}},
				{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/lib\"\n" + tc.body + "\n"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			check := func(diagnostics []source.Diagnostic) {
				t.Helper()
				for _, d := range diagnostics {
					if string(d.Code) == tc.code && d.Primary.Start.File == "main.mgo" && d.Primary.Start.Line == 3 {
						return
					}
				}
				t.Fatalf("missing positioned %s diagnostic: %#v", tc.code, diagnostics)
			}
			for level := compiler.OptimizationNone; level <= compiler.OptimizationFull; level++ {
				req := compiler.Request{Root: "example/main", Sources: sources, Optimization: level, Cache: cache.New(cache.NewMemoryBackend())}
				checked, err := compiler.Check(req)
				if err != nil {
					t.Fatal(err)
				}
				check(checked.Diagnostics)
				for range 2 {
					compiled, err := compiler.Compile(req)
					if err != nil {
						t.Fatal(err)
					}
					check(compiled.Diagnostics)
					prepared, err := compiler.Prepare(req)
					if err != nil {
						t.Fatal(err)
					}
					check(prepared.Checked.Diagnostics)
					if prepared.Image != nil {
						t.Fatal("invalid source produced an image")
					}
				}
			}
		})
	}
}

func TestImportedMemberChangeInvalidatesAnalysisAndCompilation(t *testing.T) {
	for _, replacement := range []string{"", "func Renamed() {}", "func present() {}", "var Present int"} {
		t.Run(replacement, func(t *testing.T) {
			backend := cache.New(cache.NewMemoryBackend())
			var previous *compiler.AnalysisResult
			for i, declaration := range []string{"type Present int", replacement, replacement, "type Present int"} {
				sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
					{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\n" + declaration + "\n"}}},
					{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/lib\"\nvar value lib.Present\nfunc main() {}\n"}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				req := compiler.Request{Root: "example/main", Sources: sources, Cache: backend}
				analysis, err := compiler.Analyze(compiler.AnalysisRequest{Request: req, Previous: previous, ContinueAfterErrors: true})
				if err != nil {
					t.Fatal(err)
				}
				valid := i == 0 || i == 3
				if source.HasErrors(analysis.Diagnostics) == valid {
					t.Fatalf("analysis step %d: %v", i, analysis.Diagnostics)
				}
				previous = &analysis
				prepared, err := compiler.Prepare(req)
				if err != nil || prepared.Checked.OK() != valid || (prepared.Image != nil) != valid {
					t.Fatalf("prepare step %d: %v %v", i, prepared.Checked.Diagnostics, err)
				}
			}
		})
	}
}

func TestImportedPublicAPIWithPrivateTypeMetadata(t *testing.T) {
	for _, declaration := range []string{`import lib "example/lib"`, `import . "example/lib"`} {
		body := `func main() { value := lib.New(); _ = value.Read(); fn := lib.Identity[int]; lib.Counter = fn(lib.Answer); var n lib.Number = lib.Number(lib.Counter); _ = n; _ = lib.Copy([]string{"ok"}) }`
		if declaration == `import . "example/lib"` {
			body = `func main() { value := New(); _ = value.Read(); fn := Identity[int]; Counter = fn(Answer); var n Number = Number(Counter); _ = n; _ = Copy([]string{"ok"}) }`
		}
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: `package lib
type hidden struct { value int }
func (v hidden) Read() int { return v.value }
func New() hidden { return hidden{value:42} }
type Number int
const Answer = 42
var Counter int
func Identity[T any](v T) T { return v }
func Copy[S ~[]E, E any](v S) S { result := make(S, 0, len(v)); return append(result, v...) }
`}}},
			{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\n" + declaration + "\n" + body + "\n"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := compiler.Request{Root: "example/main", Sources: sources}
		checked, err := compiler.Check(req)
		if err != nil || !checked.OK() {
			t.Fatalf("check: %v %v", checked.Diagnostics, err)
		}
		prepared, err := compiler.Prepare(req)
		if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
			t.Fatalf("prepare: %v %v", prepared.Checked.Diagnostics, err)
		}
	}
}

func TestLocalGenericTypeOperandRemainsAType(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: `package main
func Copy[S ~[]E, E any](v S) S { result := make(S, 0, len(v)); return append(result, v...) }
func main() { _ = Copy([]string{"ok"}) }
`}}}})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := compiler.Prepare(compiler.Request{Root: "example/main", Sources: sources})
	if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
		t.Fatalf("prepare: %v %v", prepared.Checked.Diagnostics, err)
	}
}

func TestFailedDependencyDoesNotPublishMembersToIncrementalAnalysis(t *testing.T) {
	var previous *compiler.AnalysisResult
	for _, body := range []string{"", "missing()", "missing()", ""} {
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "example/lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Present() { " + body + " }\n"}}},
			{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/lib\"\nfunc main() { lib.Present() }\n"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := compiler.Analyze(compiler.AnalysisRequest{Request: compiler.Request{Root: "example/main", Sources: sources}, Previous: previous, ContinueAfterErrors: true})
		if err != nil {
			t.Fatal(err)
		}
		if source.HasErrors(result.Diagnostics) != (body != "") {
			t.Fatalf("analysis diagnostics: %v", result.Diagnostics)
		}
		unavailable := false
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Code == "semantic.import.member.missing" {
				t.Fatalf("failed dependency reported a missing member: %v", result.Diagnostics)
			}
			unavailable = unavailable || diagnostic.Code == "semantic.dependency.unavailable" && diagnostic.ModulePath == "example/main"
		}
		if unavailable != (body != "") {
			t.Fatalf("dependency state not propagated: %v", result.Diagnostics)
		}
		previous = &result
	}
}
