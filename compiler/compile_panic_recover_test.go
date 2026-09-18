package compiler

import "testing"

func TestCompileSourceAllowsDeferredRecoverCall(t *testing.T) {
	result, err := compileTestSource("example/recover", "recover.mgo", `
package main

func Main() {
	defer recover()
	panic("failed")
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}

func TestCompileSourceRejectsDeferredBuiltinWithDiscardedResult(t *testing.T) {
	result, err := compileTestSource("example/recover", "recover.mgo", `
package main

func Main() {
	value := "go"
	defer len(value)
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected deferred builtin diagnostic")
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "hirgen.builtin.defer" {
			return
		}
	}
	t.Fatalf("expected hirgen.builtin.defer diagnostic, got %#v", result.Diagnostics)
}
