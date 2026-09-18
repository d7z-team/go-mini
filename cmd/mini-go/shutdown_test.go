package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessShutdownBoundsUncooperativeHost(t *testing.T) {
	if mode := os.Getenv("MINIGO_SHUTDOWN_HELPER"); mode != "" {
		_ = runProcess(func(ctx context.Context) error {
			timeout := 100 * time.Millisecond
			if mode == "second" {
				timeout = 5 * time.Second
			}
			shutdown, _, err := (commandEnvironment{ctx: ctx}).shutdownPolicy(timeout)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "ready")
			if mode == "cleanup" {
				shutdown.begin()
			}
			select {} // A synchronous host callback that does not cooperate.
		})
		os.Exit(2)
	}
	for _, mode := range []string{"signal", "second", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessShutdownBoundsUncooperativeHost$")
			command.Env = append(os.Environ(), "MINIGO_SHUTDOWN_HELPER="+mode)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "ready\n" {
				t.Fatalf("readiness %q %v", line, err)
			}
			if mode != "cleanup" {
				if err := command.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
				if mode == "second" {
					if err := command.Process.Signal(os.Interrupt); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = command.Wait()
			if err == nil || ctx.Err() != nil {
				t.Fatalf("process wait: %v; context: %v", err, ctx.Err())
			}
			want := "deadline exceeded"
			if mode == "second" {
				want = "second signal"
			}
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("stderr: %s", &stderr)
			}
		})
	}
}

func TestCommandCleanupKeepsOneIndependentDeadline(t *testing.T) {
	run, cancel := context.WithCancel(t.Context())
	environment := commandEnvironment{ctx: run}
	shutdown, finish, err := environment.shutdownPolicy(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	first := shutdown.begin()
	cancel()
	if first.Err() != nil {
		t.Fatal("run cancellation canceled cleanup", first.Err())
	}
	if shutdown.begin() != first {
		t.Fatal("cleanup deadline restarted")
	}
	if _, _, err := environment.shutdownPolicy(0); err == nil {
		t.Fatal("zero cleanup deadline accepted")
	}
}

func TestRunBudgetOptions(t *testing.T) {
	for _, value := range []string{"0", "-1", "9223372036854775807"} {
		options, _, err := parseBuildOptions("mini-go run", []string{"-max-steps", value, "-shutdown-timeout", "2s"}, io.Discard, true, true)
		if err != nil || options.shutdownTimeout != 2*time.Second {
			t.Fatal(options, err)
		}
	}
	if _, _, err := parseBuildOptions("mini-go run", []string{"-max-steps", "-2"}, io.Discard, true, true); err == nil {
		t.Fatal("invalid budget accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	shutdown, finish, err := (commandEnvironment{ctx: ctx}).shutdownPolicy(time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	<-shutdown.begin().Done()
	if !errors.Is(shutdown.begin().Err(), context.DeadlineExceeded) {
		t.Fatal(shutdown.begin().Err())
	}
}
