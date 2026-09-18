package compiler

import (
	"reflect"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestImportedConstantsInvalidateAndRecoverIncrementalFacts(t *testing.T) {
	var previous *AnalysisResult
	for _, declaration := range []string{"const Limit int8 = 127", "const Limit int8 = 128", "const Limit int8 = -128"} {
		set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "example/main", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"example/limits\"\nvar Value int8 = limits.Limit"}}},
			{ModulePath: "example/limits", Files: []source.File{{Path: "main.mgo", Text: "package limits\n" + declaration}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		request := Request{Root: "example/main", Sources: set}
		cold, err := Analyze(AnalysisRequest{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		incremental, err := Analyze(AnalysisRequest{Request: request, Previous: previous})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cold.Diagnostics, incremental.Diagnostics) {
			t.Fatalf("changed dependency diagnostics: cold=%+v incremental=%+v", cold.Diagnostics, incremental.Diagnostics)
		}
		compiled, err := Compile(request)
		if err != nil {
			t.Fatal(err)
		}
		invalid := declaration == "const Limit int8 = 128"
		if compiled.OK() == invalid || (len(cold.Diagnostics) != 0) != invalid {
			t.Fatalf("dependency %q: compile=%+v analyze=%+v", declaration, compiled.Diagnostics, cold.Diagnostics)
		}
		previous = &incremental
	}
}

func TestCheckAndCompileAgreeOnConstantTargets(t *testing.T) {
	for _, body := range []string{
		"const X int8 = 128", "const X uint = -1", "var X int = 1.2", "var X int = nil", "var X bool = 1",
		"func F(int8) {}; func Main() { F(128) }", "func F() int8 { return 128 }", "var X = int8(128)",
		"var X any = 1 << 100", "var X = 1 << 100", "var X = []int8{128}", "var X = map[int8]int{128: 1}",
		"type N int8; var X N = 128", "func F[T ~int8 | ~int16]() T { return 128 }",
		"var X complex64 = 1e100i", "var X complex64 = complex(1, 1e100)",
		"func F(c chan int8) { c <- 128 }", "func F(x int8) { switch x { case 128: } }",
		"func F() { var x int8; x, y := 128, 1; _, _ = x, y }",
		"func F(...int8) {}; func Main() { F(1,128) }",
		"var X = struct{ A int8 }{A: 128}", "var X [1]int; var Y = X[2]",
		"const X = complex128(1e400)", "var X bool = nil", "var X string = 65",
		"var X = any(1<<100)", "const X int8 = 127; var Y int8 = X+1",
		"type B bool; type C bool; const X B = true; const Y = X && true; var Z C = Y",
	} {
		t.Run(body, func(t *testing.T) {
			set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "main", Files: []source.File{{Path: "main.mgo", Text: "package main\n" + body + "\n"}}}})
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Root: "main", Sources: set}
			checked, err := Check(request)
			if err != nil {
				t.Fatal(err)
			}
			if checked.OK() {
				t.Fatal("Check accepted invalid constant")
			}
			compiled, err := Compile(request)
			if err != nil {
				t.Fatal(err)
			}
			if compiled.OK() {
				t.Fatal("Compile accepted invalid constant")
			}
			first, second := checked.Diagnostics[0], compiled.Diagnostics[0]
			if first.Code != second.Code || first.Primary != second.Primary {
				t.Fatalf("diagnostics differ: %#v / %#v", first, second)
			}
			analysis, err := Analyze(AnalysisRequest{Request: request})
			if err != nil || len(analysis.Diagnostics) == 0 || analysis.Diagnostics[0].Code != first.Code || analysis.Diagnostics[0].Primary != first.Primary {
				t.Fatalf("Analyze disagrees: %v %#v", err, analysis.Diagnostics)
			}
			warm, err := Analyze(AnalysisRequest{Request: request, Previous: &analysis})
			if err != nil || len(warm.Diagnostics) == 0 || warm.Diagnostics[0].Code != first.Code || warm.Diagnostics[0].Primary != first.Primary {
				t.Fatalf("incremental Analyze disagrees: %v %#v", err, warm.Diagnostics)
			}
		})
	}
}

func TestConstantTargetsAcceptRepresentableValues(t *testing.T) {
	for _, body := range []string{
		"const X int8 = -128; const Y uint64 = 18446744073709551615",
		"type N int8; const X N = 127; var Y N = X",
		"var X int = 1.0; var Y complex64 = complex(1,2); var Z float32 = 0x1.fffffep127",
		"var A any = 1; var B *int = nil; var C []int = nil; var D chan int = nil",
		"const A = true && !false; const B = 1.2 < 1.3; const C = real(1+2i); const D = imag(1+2i)",
		"func F[T ~int8 | ~int16]() T { return 127 }",
	} {
		t.Run(body, func(t *testing.T) {
			set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "main", Files: []source.File{{Path: "main.mgo", Text: "package main\n" + body}}}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Compile(Request{Root: "main", Sources: set})
			if err != nil || !result.OK() {
				t.Fatalf("valid constants: %v %#v", err, result.Diagnostics)
			}
		})
	}
}
