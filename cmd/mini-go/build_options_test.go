package main

import (
	"io"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
)

func TestBuildOptimizationOptions(t *testing.T) {
	options, operands, err := parseBuildOptions("test", []string{"-O=2", "-symbols", "main.mgo"}, io.Discard, false, true)
	if err != nil || options.optimization != compiler.OptimizationFull || !options.symbols || len(operands) != 1 {
		t.Fatalf("parseBuildOptions = %#v, %#v, %v", options, operands, err)
	}
	options, _, err = parseBuildOptions("test", nil, io.Discard, false, true)
	if err != nil || options.optimization != compiler.OptimizationDefault || options.symbols {
		t.Fatalf("default optimization = %d, %v", options.optimization, err)
	}
	if _, _, err := parseBuildOptions("test", []string{"-O=3"}, io.Discard, false, true); err == nil {
		t.Fatal("invalid optimization level was accepted")
	}
	if _, _, err := parseBuildOptions("test", []string{"-O=0"}, io.Discard, false, false); err == nil {
		t.Fatal("optimization flag was accepted by a non-codegen command")
	}
}
