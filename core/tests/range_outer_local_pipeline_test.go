package engine_test

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestRangeOuterLocalCounterAcrossAllLoaders(t *testing.T) {
	const code = `
package main

var trace = ""
var finalRowScan Int64 = 0
var finalKept Int64 = 0

func mark(s string) {
	trace = trace + s + "|"
}

func main() {
	rowScan := 0
	kept := 0

	for _, day := range []Int64{12, 12, 9} {
		rowScan++
		mark("row-" + string(rowScan))
		if day > 9 {
			mark("continue-" + string(rowScan))
			continue
		}
		kept = kept + 1
		mark("keep-" + string(rowScan))
	}
	finalRowScan = rowScan
	finalKept = kept
}
`

	for _, loader := range pipelineLoaders(code) {
		t.Run(loader.name, func(t *testing.T) {
			exec := engine.NewMiniExecutor()
			prog, err := loader.load(exec)
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}
			snapshot := executeAndSnapshot(t, prog)
			rowScan, ok := snapshot.LoadGlobal("finalRowScan")
			if !ok || rowScan == nil || rowScan.I64 != 3 {
				t.Fatalf("unexpected finalRowScan: %#v", rowScan)
			}
			kept, ok := snapshot.LoadGlobal("finalKept")
			if !ok || kept == nil || kept.I64 != 1 {
				t.Fatalf("unexpected finalKept: %#v", kept)
			}
			trace, ok := snapshot.LoadGlobal("trace")
			if !ok || trace == nil || trace.Str != "row-1|continue-1|row-2|continue-2|row-3|keep-3|" {
				t.Fatalf("unexpected trace: %#v", trace)
			}
		})
	}
}
