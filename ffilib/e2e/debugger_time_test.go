package e2e_test

import (
	"context"
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/debugger"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func nextFFIDebugEvent(ctx context.Context, t *testing.T, dbg *debugger.Session) *debugger.Event {
	t.Helper()
	event, err := dbg.NextEvent(ctx)
	if err != nil {
		t.Fatalf("next debugger event failed: %v", err)
	}
	return event
}

func TestDebuggerStepOverTimeSleepStopsAtNextUserLine(t *testing.T) {
	executor := newStdExecutor()
	prog, err := executor.NewRuntimeByGoCode(`package main

import "time"

func main() {
	time.Sleep(1 * time.Millisecond)
	marker := 1
	_ = marker
}
`)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	if err := dbg.AddBreakpoint(debugger.Breakpoint{ModulePath: "main", File: "snippet", Line: 6}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	run, err := prog.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- run.Wait()
	}()

	event := nextFFIDebugEvent(ctx, t, dbg)
	if event.Loc.F != "snippet" || event.Loc.ModulePath != "main" || event.Loc.L != 6 {
		t.Fatalf("expected sleep breakpoint at snippet:6, got module=%s file=%s line=%d", event.Loc.ModulePath, event.Loc.F, event.Loc.L)
	}
	if err := run.StepOver(); err != nil {
		t.Fatal(err)
	}

	event = nextFFIDebugEvent(ctx, t, dbg)
	if event.Loc.F != "snippet" || event.Loc.ModulePath != "main" || event.Loc.L != 7 {
		t.Fatalf("step-over should stop after sleep at next user line, got module=%s file=%s line=%d", event.Loc.ModulePath, event.Loc.F, event.Loc.L)
	}
	if event.Reason != runtime.DebugStopStep {
		t.Fatalf("expected step reason, got %q", event.Reason)
	}
	if err := run.Continue(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
