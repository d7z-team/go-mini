package compiler

import "testing"

func TestCompileSourceValidatesMethodReceiverDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{
			name: "multiple receiver fields",
			source: `package main
type T int
func (a, b T) M() {}
`,
			code: "parser.receiver.count",
		},
		{
			name: "variadic receiver",
			source: `package main
type T int
func (a ...T) M() {}
`,
			code: "parser.receiver.variadic",
		},
		{
			name: "pointer to pointer receiver",
			source: `package main
type T int
func (p **T) M() {}
`,
			code: "ast.func.receiver.type",
		},
		{
			name: "slice receiver",
			source: `package main
type T int
func (s []T) M() {}
`,
			code: "ast.func.receiver.type",
		},
		{
			name: "undeclared receiver",
			source: `package main
func (m Missing) M() {}
`,
			code: "semantic.receiver.type",
		},
		{
			name: "interface receiver",
			source: `package main
type I interface{}
func (i I) M() {}
`,
			code: "semantic.receiver.type",
		},
		{
			name: "duplicate value pointer receiver method",
			source: `package main
type T int
func (T) M() {}
func (*T) M() {}
`,
			code: "semantic.method.duplicate",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := compileTestSource("example/method", "method.mgo", test.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected diagnostic %s", test.code)
			}
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == test.code {
					return
				}
			}
			t.Fatalf("expected diagnostic %s, got %#v", test.code, result.Diagnostics)
		})
	}
}

func TestCompileSourceAllowsUnnamedAndBlankMethodReceivers(t *testing.T) {
	tests := []string{
		`package main
type Error struct { Message string }
func (e *Error) Error() string { return e.Message }
func Main() string { return (&Error{Message: "ok"}).Error() }
`,
		`package main
type T int
func (T) Value() int64 { return 7 }
func Main() int64 {
	var t T
	return t.Value()
}
`,
		`package main
type T int
func (*T) Value() int64 { return 8 }
func Main() int64 {
	var t T
	return t.Value()
}
`,
		`package main
type T int
func (_ T) Value() int64 { return 9 }
func Main() int64 {
	var t T
	return t.Value()
}
`,
	}
	for _, source := range tests {
		result, err := compileTestSource("example/method", "method.mgo", source)
		if err != nil {
			t.Fatalf("compileTestSource failed: %v", err)
		}
		if !result.OK() {
			t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
		}
	}
}

func TestCompileWorkspaceAssignsImportedPointerMethodsToInterface(t *testing.T) {
	result, err := compileTestWorkspace([]SourcePackage{
		{
			ModulePath: "example/errors",
			Files: []SourceFile{{Path: "errors.mgo", Text: `package errors

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func New(message string) *Error { return &Error{Message: message} }
`}},
		},
		{
			ModulePath: "example/main",
			Files: []SourceFile{{Path: "main.mgo", Text: `package main

import "example/errors"

func Main() error { return errors.New("failure") }
`}},
		},
	})
	if err != nil {
		t.Fatalf("compileTestWorkspace failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected imported pointer method set to satisfy error, got %#v", result.Diagnostics)
	}
	artifact, ok := result.Artifact("example/errors")
	if !ok {
		t.Fatal("missing dependency artifact")
	}
	function, _, ok := artifactFunctionByName(artifact, result.PackageSymbols["example/errors"], "Error")
	if ok && function.ID == "method.Ptr<Error>.Error" {
		return
	}
	t.Fatalf("imported method receiver was not normalized to package-local identity: %#v", artifact.Functions)
}

func TestCompileSourceEncodesGoStringEscapesAsArtifactJSON(t *testing.T) {
	result, err := compileTestSource("example/literal", "literal.mgo", `package literal

func Value() string { return "prefix\x00suffix" }
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected escaped string literal to compile, got %#v", result.Diagnostics)
	}
}
