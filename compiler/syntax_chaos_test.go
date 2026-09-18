package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestSyntaxCorpus(t *testing.T) {
	valid, err := filepath.Glob("../testdata/syntax/valid/*.mgo")
	if err != nil || len(valid) == 0 {
		t.Fatalf("load valid syntax corpus: %v, files=%d", err, len(valid))
	}
	sort.Strings(valid)
	for _, path := range valid {
		t.Run(filepath.Base(path), func(t *testing.T) {
			text, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := Prepare(syntaxRequest(string(text), Limits{}))
			if err != nil || !prepared.Checked.OK() || prepared.Image == nil {
				t.Fatalf("valid corpus failed: err=%v diagnostics=%#v", err, prepared.Checked.Diagnostics)
			}
		})
	}

	recovery, err := filepath.Glob("../testdata/syntax/recovery/*.mgo")
	if err != nil || len(recovery) == 0 {
		t.Fatalf("load recovery syntax corpus: %v, files=%d", err, len(recovery))
	}
	for _, path := range recovery {
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		result, checkErr := Check(syntaxRequest(string(text), Limits{MaxDiagnostics: 8}))
		if checkErr != nil || len(result.Diagnostics) == 0 || len(result.Diagnostics) > 9 {
			t.Fatalf("recovery corpus %s: err=%v diagnostics=%#v", path, checkErr, result.Diagnostics)
		}
	}

	limitFiles, err := filepath.Glob("../testdata/syntax/limits/*.mgo")
	if err != nil || len(limitFiles) == 0 {
		t.Fatalf("load syntax limit corpus: %v, files=%d", err, len(limitFiles))
	}
	for _, path := range limitFiles {
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		result, checkErr := Check(syntaxRequest(string(text), Limits{MaxTokens: 8, MaxDiagnostics: 4}))
		if checkErr != nil {
			t.Fatal(checkErr)
		}
		requireCompileDiagnostic(t, result.Diagnostics, "scanner.token.limit")
	}
}

func TestSyntaxChaosMutationsAreDeterministic(t *testing.T) {
	text, err := os.ReadFile("../testdata/syntax/valid/control.mgo")
	if err != nil {
		t.Fatal(err)
	}
	mutations := [][]byte{
		text[:len(text)/2],
		append(append([]byte(nil), text...), text...),
		bytes.Replace(text, []byte("range"), nil, 1),
		bytes.Replace(text, []byte("select"), []byte("select select"), 1),
		bytes.Replace(text, []byte("total += value"), []byte("value += total"), 1),
		bytes.ReplaceAll(text, []byte("{"), []byte("(")),
		bytes.ReplaceAll(text, []byte("}"), []byte("]")),
		bytes.Replace(text, []byte("total += value"), []byte("total; += value"), 1),
		append(append([]byte(nil), text...), []byte("\nvar truncated = \"")...),
		append(append([]byte(nil), text...), []byte("\n/* truncated")...),
		append([]byte{0, 0xff, 0xfe}, text...),
	}
	for index, mutation := range mutations {
		request := syntaxRequest(string(mutation), Limits{
			MaxSourceBytes: 64 << 10, MaxTokens: 4096, MaxSyntaxDepth: 64,
			MaxASTNodes: 4096, MaxDiagnostics: 16, MaxSpecializations: 64,
		})
		first, firstErr := Check(request)
		second, secondErr := Check(request)
		if firstErr != nil || secondErr != nil {
			t.Fatalf("mutation %d returned host error: %v, %v", index, firstErr, secondErr)
		}
		if !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
			t.Fatalf("mutation %d diagnostics changed: %#v != %#v", index, first.Diagnostics, second.Diagnostics)
		}
		if len(first.Diagnostics) > 17 {
			t.Fatalf("mutation %d exceeded diagnostic budget: %d", index, len(first.Diagnostics))
		}
	}
}

func TestSyntaxWorkspaceOrderIsDeterministic(t *testing.T) {
	files := []source.File{
		{Path: "main.mgo", Text: "package main\nimport \"syntax/lib\"\nfunc main() { _ = lib.Value() }\n"},
		{Path: "tagged.mgo", Text: "//go:build minigo\n\npackage main\nvar Selected = true\n"},
	}
	build := func(files []source.File) CheckResult {
		sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
			{ModulePath: "syntax/main", Files: files},
			{ModulePath: "syntax/lib", Files: []source.File{{Path: "lib.mgo", Text: "package lib\nfunc Value() int { return 1 }\n"}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := Check(Request{Root: "syntax/main", Sources: sources})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	forward := build(files)
	reverse := build([]source.File{files[1], files[0]})
	if !forward.OK() || !reverse.OK() || forward.GraphHash != reverse.GraphHash || !reflect.DeepEqual(forward.Diagnostics, reverse.Diagnostics) {
		t.Fatalf("source order changed workspace result: forward=%#v reverse=%#v", forward.Diagnostics, reverse.Diagnostics)
	}
}

func FuzzCompilerPipeline(f *testing.F) {
	for _, path := range []string{
		"../testdata/syntax/valid/control.mgo",
		"../testdata/syntax/valid/functions.mgo",
		"../testdata/syntax/valid/generic.mgo",
		"../testdata/syntax/recovery/unterminated.mgo",
	} {
		text, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text []byte) {
		if len(text) > 64<<10 {
			t.Skip()
		}
		limits := Limits{
			MaxPackages: 4, MaxFiles: 8, MaxTotalSourceBytes: 64 << 10,
			MaxSourceBytes: 64 << 10, MaxTokens: 8192, MaxSyntaxDepth: 64,
			MaxASTNodes: 8192, MaxDiagnostics: 16, MaxSpecializations: 128,
		}
		request := syntaxRequest(string(text), limits)
		first, err := Check(request)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Check(request)
		if err != nil || !reflect.DeepEqual(first.Diagnostics, second.Diagnostics) {
			t.Fatalf("compiler check is not deterministic: err=%v first=%#v second=%#v", err, first.Diagnostics, second.Diagnostics)
		}
		if len(first.Diagnostics) > limits.MaxDiagnostics+1 {
			t.Fatalf("diagnostic budget exceeded: %d", len(first.Diagnostics))
		}
		if first.OK() {
			compiled, compileErr := Compile(request)
			if compileErr != nil {
				t.Fatal(compileErr)
			}
			if len(compiled.Diagnostics) > limits.MaxDiagnostics+1 {
				t.Fatalf("compile diagnostic budget exceeded: %d", len(compiled.Diagnostics))
			}
		}
	})
}

func syntaxRequest(text string, limits Limits) Request {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "syntax/main",
		Files:      []source.File{{Path: "main.mgo", Text: text}},
	}})
	if err != nil {
		panic(err)
	}
	return Request{Root: "syntax/main", Sources: sources, Limits: limits}
}
