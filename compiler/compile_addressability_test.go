package compiler

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

func TestAddressabilityDiagnostics(t *testing.T) {
	for _, test := range []struct{ expression, code string }{
		{`lib.Constant = 2`, "semantic.assign.target"},
		{`lib.Function = nil`, "semantic.assign.target"},
		{`lib.Constant++`, "semantic.assign.target"},
		{`lib.Array()[0] = 1`, "semantic.assign.target"},
		{`lib.Box().N = 1`, "semantic.assign.target"},
		{`_ = &lib.Constant`, "semantic.address.target"},
		{`_ = &lib.Function`, "semantic.address.target"},
		{`_ = &lib.Map[0]`, "semantic.address.target"},
		{`_ = &lib.Array()[0]`, "semantic.address.target"},
		{`text := "x"; text[0] = 1`, "semantic.assign.target"},
	} {
		t.Run(test.expression, func(t *testing.T) {
			set, err := workspace.NewMemorySourceSet([]SourcePackage{
				{ModulePath: "lib", Files: []SourceFile{{Path: "lib.mgo", Text: `package lib
const Constant = 1
func Function() {}
func Array() [1]int { return [1]int{} }
func Box() struct{ N int } { return struct{ N int }{} }
var Map = map[int]int{}
`}}},
				{ModulePath: "main", Files: []SourceFile{{Path: "main.mgo", Text: "package main\nimport \"lib\"\nfunc main() { _ = lib.Constant; " + test.expression + " }"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Root: "main", Sources: set}
			checked, err := Check(request)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := Compile(request)
			if err != nil {
				t.Fatal(err)
			}
			for name, diagnostics := range map[string][]source.Diagnostic{"check": checked.Diagnostics, "compile": compiled.Diagnostics} {
				t.Run(name, func(t *testing.T) {
					requireCompileDiagnostic(t, diagnostics, test.code)
				})
			}
		})
	}
}

func TestGenericIndexAddressability(t *testing.T) {
	for _, test := range []struct{ constraint, body, code string }{
		{"~[]int", "values[0] = 1; _ = &values[0]", ""},
		{"~[2]int", "values[0] = 1; _ = &values[0]", ""},
		{"~map[int]int", "values[0] = 1", ""},
		{"~map[int]int", "_ = &values[0]", "semantic.address.target"},
		{"~string | ~[]byte", "values[0] = 1", "semantic.assign.target"},
	} {
		t.Run(test.constraint+test.body, func(t *testing.T) {
			set, err := workspace.NewMemorySourceSet([]SourcePackage{{ModulePath: "example", Files: []SourceFile{{Path: "example.mgo", Text: "package example\nfunc Write[S " + test.constraint + "](values S) { " + test.body + " }"}}}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := Check(Request{Root: "example", Sources: set})
			if err != nil {
				t.Fatal(err)
			}
			if test.code == "" {
				if !result.OK() {
					t.Fatal(result.Diagnostics)
				}
				return
			}
			requireCompileDiagnostic(t, result.Diagnostics, test.code)
		})
	}
}
