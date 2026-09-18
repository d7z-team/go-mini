package compiler

import "testing"

func TestCompileSourceRejectsInvalidSelectCommunication(t *testing.T) {
	result, err := compileTestSource("example/select", "main.mgo", `package main
func Main(ch chan int64) int64 {
	select {
	case var value = <-ch:
		return value
	default:
		return 0
	}
}
`)
	if err != nil {
		t.Fatalf("compileTestSource failed: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected invalid select communication diagnostic")
	}
	if result.Artifact.Format != "" {
		t.Fatalf("invalid select communication must not produce artifact, got %#v", result.Artifact)
	}
}
