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
			Name: "range-closed-empty-channel",
			Body: `
	ch := make(chan Int64)
	close(ch)
	count := 0
	for v := range ch {
		_ = v
		count = count + 1
	}
	if count != 0 {
		panic("closed empty channel range should not run")
	}
`,
		},
		{
			Name: "buffered-receive-fills-slot-from-blocked-sender",
			Decls: `
func bufferedSender(data chan Int64, ready chan Int64, done chan Int64) {
	ready <- 1
	data <- 2
	done <- 1
}
`,
			Body: `
	data := make(chan Int64, 1)
	ready := make(chan Int64)
	done := make(chan Int64)
	data <- 1
	go bufferedSender(data, ready, done)
	<-ready
	select {
	case <-done:
		panic("sender should still be blocked")
	default:
	}
	if <-data != 1 {
		panic("buffered channel lost queued value")
	}
	if len(data) != 1 {
		panic("blocked sender did not refill buffer slot")
	}
	if <-data != 2 {
		panic("buffered channel lost blocked sender value")
	}
	<-done
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
			Name: "close-wakes-blocked-receiver",
			Decls: `
func closedReceiver(ch chan Int64, done chan Int64) {
	v, ok := <-ch
	if v != 0 || ok {
		panic("closed receive did not return zero,false")
	}
	done <- 1
}
`,
			Body: `
	ch := make(chan Int64)
	done := make(chan Int64)
	go closedReceiver(ch, done)
	close(ch)
	<-done
`,
		},
		{
			Name: "close-wakes-blocked-sender-with-panic",
			Decls: `
func blockedSender(ch chan Int64, ready chan Int64, done chan Int64) {
	recovered := false
	defer func() {
		if recover() != nil {
			recovered = true
		}
		if !recovered {
			panic("blocked sender did not panic after close")
		}
		done <- 1
	}()
	ready <- 1
	ch <- 7
}
`,
			Body: `
	ch := make(chan Int64)
	ready := make(chan Int64)
	done := make(chan Int64)
	go blockedSender(ch, ready, done)
	<-ready
	close(ch)
	<-done
`,
		},
		{
			Name: "close-nil-channel-panics",
			Body: `
	recovered := false
	defer func() {
		if recover() != nil {
			recovered = true
		}
		if !recovered {
			panic("close nil channel did not panic")
		}
		test.Done()
	}()
	var ch chan Int64
	close(ch)
`,
		},
		{
			Name: "select-send-closed-channel-panics",
			Body: `
	recovered := false
	defer func() {
		if recover() != nil {
			recovered = true
		}
		if !recovered {
			panic("select send on closed channel did not panic")
		}
		test.Done()
	}()
	ch := make(chan Int64, 1)
	close(ch)
	select {
	case ch <- 1:
		panic("closed send case body should not run")
	default:
		panic("default selected over closed send")
	}
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
			WantCompileErr: "cannot close receive-only channel",
		},
	})
}

func TestChannelAllBlockedReturnsError(t *testing.T) {
	expectChannelAllBlocked(t, `
package main

func main() {
	ch := make(chan Int64)
	<-ch
}
`, "")
}

func TestNilChannelSelectAllBlocked(t *testing.T) {
	expectChannelAllBlocked(t, `
package main

func main() {
	var ch chan Int64
	select {
	case <-ch:
	}
}
`, "select")
}

func TestNilChannelSendAllBlocked(t *testing.T) {
	expectChannelAllBlocked(t, `
package main

func main() {
	var ch chan Int64
	ch <- 1
}
`, "nil channel send")
}

func TestNilChannelRangeAllBlocked(t *testing.T) {
	expectChannelAllBlocked(t, `
package main

func main() {
	var ch chan Int64
	for v := range ch {
		_ = v
	}
}
`, "nil channel range")
}

func expectChannelAllBlocked(t *testing.T, code, reason string) {
	t.Helper()
	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	var blocked *runtime.VMAllBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected VM all-blocked error, got %T %v", err, err)
	}
	if len(blocked.Waits) != 1 {
		t.Fatalf("expected one blocked wait, got %#v", blocked.Waits)
	}
	if reason != "" && !strings.Contains(blocked.Waits[0].Reason, reason) {
		t.Fatalf("expected blocked wait reason containing %q, got %#v", reason, blocked.Waits)
	}
}
