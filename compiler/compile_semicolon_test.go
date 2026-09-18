package compiler

import "testing"

func TestCompileSourceWithSpecSemicolonInsertion(t *testing.T) {
	result, err := compileTestSource("example/semicolon", "semicolon.mgo", `
package main

const (
	A = 1
	B = 2
)

func Main() int64 {
	x := int64(A + B)
	if x > 0 {
		x++
	}
	return x
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

func TestCompileSourceLowersEmptyStatements(t *testing.T) {
	result, err := compileTestSource("example/main", "main.mgo", `
package main

func Main() int64 {
	;
	value := int64(42)
	;
	return value
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no diagnostics, got %#v", result.Diagnostics)
	}
}
