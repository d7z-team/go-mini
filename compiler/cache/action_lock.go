package cache

import (
	"context"
	"errors"
)

type actionFlight struct {
	gate chan struct{}
	refs int
}

type actionLocks struct {
	mu      cacheMutex
	flights map[string]*actionFlight
}

// acquire counts owners and waiters together, so cancellation and cache Clear
// cannot replace a lock that another request still owns.
func (l *actionLocks) acquire(ctx context.Context, key string) (func(), error) {
	if l == nil {
		return nil, errors.New("uninitialized cache store")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.flights == nil {
		l.flights = make(map[string]*actionFlight)
	}
	flight := l.flights[key]
	if flight == nil {
		flight = &actionFlight{gate: make(chan struct{}, 1)}
		l.flights[key] = flight
	}
	flight.refs++
	l.mu.Unlock()
	drop := func() {
		l.mu.Lock()
		flight.refs--
		if flight.refs == 0 {
			delete(l.flights, key)
		}
		l.mu.Unlock()
	}
	select {
	case flight.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-flight.gate
			drop()
			return nil, err
		}
		return func() {
			<-flight.gate
			drop()
		}, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}

func (s Store) LockCompile(ctx context.Context, action Action) (func(), error) {
	id, err := action.ID()
	if err != nil {
		return nil, err
	}
	return s.locks.acquire(ctx, id.String())
}

func (s Store) LockPrepare(ctx context.Context, action PrepareAction) (func(), error) {
	id, err := action.ID()
	if err != nil {
		return nil, err
	}
	return s.locks.acquire(ctx, id.String())
}

func (s Store) LockSymbols(ctx context.Context, action SymbolAction) (func(), error) {
	id, err := action.ID()
	if err != nil {
		return nil, err
	}
	return s.locks.acquire(ctx, id.String())
}
