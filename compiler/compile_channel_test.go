package compiler

import "testing"

func TestCompileSourceValidatesChannelSendDirection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		code   string
	}{
		{name: "ordinary send receive-only channel", source: "package main\nfunc Bad(ch <-chan int64) { ch <- 1 }\n", code: "hirgen.send.direction"},
		{name: "ordinary send non-channel", source: "package main\nfunc Main() { x := int64(0); x <- 1 }\n", code: "hirgen.send.direction"},
		{name: "select send receive-only channel", source: "package main\nfunc Bad(ch <-chan int64) { select { case ch <- 1: default: } }\n", code: "hirgen.select.comm.send.direction"},
		{name: "select send non-channel", source: "package main\nfunc Main() { x := int64(0); select { case x <- 1: default: } }\n", code: "hirgen.select.comm.send.direction"},
		{name: "close receive-only channel", source: "package main\nfunc Bad(ch <-chan int64) { close(ch) }\n", code: "hirgen.builtin.close.direction"},
		{name: "close non-channel", source: "package main\nfunc Main() { close(1) }\n", code: "hirgen.builtin.close.type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := compileTestSource("example/main", "main.mgo", tc.source)
			if err != nil {
				t.Fatalf("compileTestSource failed: %v", err)
			}
			requireCompileDiagnostic(t, result.Diagnostics, tc.code)
		})
	}
}
