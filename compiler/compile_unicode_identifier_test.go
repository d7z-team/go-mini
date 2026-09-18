package compiler

import "testing"

func TestCompileSourceWithUnicodeIdentifierCategories(t *testing.T) {
	result, err := compileTestSource("example/unicode", "unicode.mgo", `
package main

const 变量١ = 41

func Main() int64 {
	return int64(变量١ + 1)
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
	if _, err := result.Hash(); err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
}

func TestCompileSourceRejectsInvalidUnicodeIdentifierStart(t *testing.T) {
	result, err := compileTestSource("example/unicode", "unicode.mgo", `
package main

var ١abc = 1
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected diagnostics for invalid Unicode identifier start")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "scanner.token.illegal" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scanner.token.illegal, got %#v", result.Diagnostics)
	}
}
