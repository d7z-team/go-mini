package compiler

import (
	"strings"
	"testing"

	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

func TestCompileSourceBuildsArtifactRoundTrip(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

func Main() int64 {
	return 42
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	if result.Artifact.Format != ir.Format {
		t.Fatalf("expected IR artifact format, got %q", result.Artifact.Format)
	}
	if _, err := result.Hash(); err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	disasm, err := result.Disassemble()
	if err != nil {
		t.Fatalf("Disassemble failed: %v", err)
	}
	if !strings.Contains(disasm, "export Main function") {
		t.Fatalf("expected Main export in disassembly:\n%s", disasm)
	}
	data, err := result.EncodeJSON()
	if err != nil {
		t.Fatalf("EncodeJSON failed: %v", err)
	}
	decoded, err := ir.DecodeJSON(data)
	if err != nil {
		t.Fatalf("DecodeJSON failed: %v", err)
	}
	if decoded.Module.Path != "example/main" || decoded.Module.Package != "main" {
		t.Fatalf("unexpected decoded module: %#v", decoded.Module)
	}
}

func TestCompileSourceValidatesProgramMainSignature(t *testing.T) {
	cases := []struct {
		name   string
		source string
		code   string
	}{{
		name: "parameter",
		source: `package main

func main(value int) {
}
`,
		code: "hirgen.main.signature",
	}, {
		name: "result",
		source: `package main

func main() int {
	return 1
}
`,
		code: "hirgen.main.signature",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			if result.OK() {
				t.Fatalf("expected diagnostic %s", tc.code)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if string(diagnostic.Code) == tc.code {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected diagnostic %s, got %#v", tc.code, result.Diagnostics)
			}
		})
	}
	result, err := compileTestSource("example/lib", "lib.mgo", `package lib

func main(value int) int {
	return value
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("non-main package main function should remain ordinary, got %#v", result.Diagnostics)
	}
}
