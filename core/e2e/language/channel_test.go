package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestChannelRunCases(t *testing.T) {
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name: "buffered-close-and-range",
			Body: `
	ch := make(chan Int64, 3)
	if cap(ch) != 3 {
		panic("bad channel cap")
	}
	ch <- 1
	ch <- 2
	ch <- 3
	if len(ch) != 3 {
		panic("bad channel len")
	}

	sum := 0
	for v := range ch {
		sum = sum + v
		if sum == 6 {
			close(ch)
		}
	}
	if sum != 6 {
		panic("bad channel range sum")
	}

	z, ok := <-ch
	if z != 0 || ok {
		panic("closed channel receive failed")
	}
`,
		},
		{
			Name: "unbuffered-goroutine-send-receive",
			Decls: `
func send(ch chan Int64) {
	ch <- 7
}
`,
			Body: `
	ch := make(chan Int64)
	go send(ch)
	got := <-ch
	if got != 7 {
		panic("bad unbuffered receive")
	}
`,
		},
		{
			Name: "range-assigns-existing-variable",
			Body: `
	ch := make(chan Int64, 2)
	ch <- 4
	ch <- 5
	close(ch)

	item := 0
	sum := 0
	for item = range ch {
		sum = sum + item
	}
	if item != 5 || sum != 9 {
		panic("channel range assignment failed")
	}
`,
		},
		{
			Name: "select-channel-cases",
			Body: `
	empty := make(chan Int64)
	picked := 0
	select {
	case <-empty:
		picked = 1
	default:
		picked = 2
	}
	if picked != 2 {
		panic("select default failed")
	}

	ready := make(chan Int64, 1)
	ready <- 9
	value := 0
	select {
	case value = <-ready:
	default:
		panic("select receive missed ready channel")
	}
	if value != 9 {
		panic("select receive got wrong value")
	}

	dst := make(chan Int64, 1)
	select {
	case dst <- 11:
	default:
		panic("select send missed ready channel")
	}
	if <-dst != 11 {
		panic("select send wrote wrong value")
	}

	closed := make(chan Int64)
	close(closed)
	z := 1
	ok := true
	select {
	case z, ok = <-closed:
	default:
		panic("closed channel receive was not ready")
	}
	if z != 0 || ok {
		panic("select closed receive failed")
	}

	select {
	case v, open := <-closed:
		if v != 0 || open {
			panic("select define receive failed")
		}
	default:
		panic("closed channel receive define was not ready")
	}
`,
		},
		{
			Name: "select-blocks-until-vm-channel-ready",
			Decls: `
func send(ch chan Int64) {
	ch <- 17
}
`,
			Body: `
	ch := make(chan Int64)
	go send(ch)
	picked := 0
	select {
	case picked = <-ch:
	}
	if picked != 17 {
		panic("blocking select receive failed")
	}
`,
		},
		{
			Name: "nil-channel-select-default",
			Body: `
	var ch chan Int64
	picked := 0
	select {
	case <-ch:
		picked = 1
	default:
		picked = 2
	}
	if picked != 2 {
		panic("nil channel default select failed")
	}
`,
		},
		{
			Name: "closed-channel-panics",
			Decls: `
func closeClosedPanics() {
	recovered := false
	defer func() {
		if recover() != nil {
			recovered = true
		}
		if !recovered {
			panic("close closed channel did not panic")
		}
	}()
	ch := make(chan Int64)
	close(ch)
	close(ch)
}

func sendClosedPanics() {
	recovered := false
	defer func() {
		if recover() != nil {
			recovered = true
		}
		if !recovered {
			panic("send closed channel did not panic")
		}
	}()
	ch := make(chan Int64, 1)
	close(ch)
	ch <- 1
}
`,
			Body: `
	closeClosedPanics()
	sendClosedPanics()
`,
		},
		{
			Name: "direction-send-recv-only",
			Body: `
	ch := make(chan Int64)
	var recv <-chan Int64 = ch
	recv <- 1
`,
			WantCompileErr: "<any>",
		},
		{
			Name: "direction-receive-send-only",
			Body: `
	ch := make(chan Int64)
	var send chan<- Int64 = ch
	value := <-send
	_ = value
`,
			WantCompileErr: "<any>",
		},
		{
			Name: "direction-close-recv-only",
			Body: `
	ch := make(chan Int64)
	var recv <-chan Int64 = ch
	close(recv)
`,
			WantCompileErr: "<any>",
		},
	})
}

func TestChannelAllBlockedReturnsError(t *testing.T) {
	executor := engine.NewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

func main() {
	ch := make(chan Int64)
	<-ch
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	var blocked *runtime.VMAllBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected VM all-blocked error, got %T %v", err, err)
	}
	if len(blocked.Waits) != 1 {
		t.Fatalf("expected one blocked context, got %#v", blocked.Waits)
	}
}

func TestNilChannelSelectAllBlocked(t *testing.T) {
	executor := engine.NewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(`
package main

func main() {
	var ch chan Int64
	select {
	case <-ch:
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	var blocked *runtime.VMAllBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected VM all-blocked error, got %T %v", err, err)
	}
	if len(blocked.Waits) != 1 || !strings.Contains(blocked.Waits[0].Reason, "select") {
		t.Fatalf("expected select blocked wait, got %#v", blocked.Waits)
	}
}
