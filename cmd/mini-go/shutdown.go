package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

type shutdownContextKey struct{}

// One deadline belongs to the command, including all dependent cleanup owners.
type commandShutdown struct {
	mu      sync.Mutex
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
	started chan struct{}
}

func (shutdown *commandShutdown) begin() context.Context {
	shutdown.mu.Lock()
	defer shutdown.mu.Unlock()
	if shutdown.ctx == nil {
		timeout := shutdown.timeout
		if timeout == 0 {
			timeout = defaultShutdownTimeout
		}
		shutdown.ctx, shutdown.cancel = context.WithTimeout(context.Background(), timeout)
		if shutdown.started != nil {
			close(shutdown.started)
		}
	}
	return shutdown.ctx
}

func (environment commandEnvironment) shutdownPolicy(timeout time.Duration) (*commandShutdown, func(), error) {
	if timeout <= 0 {
		return nil, nil, errors.New("-shutdown-timeout must be positive")
	}
	shutdown, _ := environment.context().Value(shutdownContextKey{}).(*commandShutdown)
	if shutdown == nil {
		shutdown = &commandShutdown{}
	}
	shutdown.mu.Lock()
	shutdown.timeout = timeout
	shutdown.mu.Unlock()
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(environment.context(), func() {
		shutdown.begin()
		close(callbackDone)
	})
	return shutdown, func() {
		if !stop() {
			<-callbackDone
		}
		shutdown.mu.Lock()
		defer shutdown.mu.Unlock()
		if shutdown.cancel != nil {
			shutdown.cancel()
		}
	}, nil
}
