package compiler

import "testing"

func TestCompileSourceAssignsValuesToDefinedAny(t *testing.T) {
	source := `package main

type Token any
type Delim rune

func read() Token { return Delim('[') }
func main() { var value Token = "text"; _, _ = value, read() }
`
	result, err := compileTestSource("main", "main.mgo", source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("compile failed: %#v", result.Diagnostics)
	}
}
