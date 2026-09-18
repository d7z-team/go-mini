package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := runProcess(func(ctx context.Context) error {
		return runCLIContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Hard termination belongs only to the executable, never to a library owner.
func runProcess(command func(context.Context) error) error {
	shutdown := &commandShutdown{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), shutdownContextKey{}, shutdown))
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-signals:
			shutdown.begin()
			cancel()
		case <-done:
			return
		}
		select {
		case <-signals:
			fmt.Fprintln(os.Stderr, "shutdown interrupted by second signal")
			os.Exit(1)
		case <-done:
		}
	}()
	go func() {
		select {
		case <-shutdown.started:
		case <-done:
			return
		}
		select {
		case <-shutdown.begin().Done():
			// Canceling the completed command's cleanup context is not a timeout.
			if shutdown.begin().Err() == context.DeadlineExceeded {
				fmt.Fprintln(os.Stderr, "shutdown deadline exceeded; cleanup incomplete")
				os.Exit(1)
			}
		case <-done:
		}
	}()
	return command(ctx)
}
