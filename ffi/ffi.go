// Package ffi defines the asynchronous boundary between a Mini-Go instance
// and host-provided capabilities.
package ffi

import (
	"context"
	"errors"
)

// ErrRouteUnavailable means no host implementation is installed for the route.
// It does not describe a failed call to an installed implementation.
var ErrRouteUnavailable = errors.New("FFI route unavailable")

// Request contains one opaque call. Payload ownership transfers to the Session
// for the duration of the call, including work after Start returns. The Session
// must release it when the call finishes or Start returns an error, and must
// not mutate it after reporting Completion.
type Request struct {
	Route   string
	Payload []byte
}

// Result is the terminal outcome of one call. Payload ownership transfers to
// the runtime when Completion is invoked; the Session must not mutate or reuse
// the backing array afterwards.
type Result struct {
	Payload []byte
	Err     error
	// Discard releases resources represented by a result that the runtime cannot
	// deliver to the guest. It is called at most once for each Completion, outside
	// runtime locks. It must not block or reenter the VM; lengthy cleanup must be
	// scheduled by the Session. A consumed result belongs to the guest and Session.
	Discard func()
}

// Completion reports a terminal call outcome. A Bridge must invoke it at
// most once, but may do so before Start returns.
type Completion func(Result)

// Call controls one established host call.
type Call interface {
	Cancel()
}

// Bridge opens one host session for each Mini-Go instance. Implementations may
// be shared by multiple instances; the returned Session may not.
type Bridge interface {
	Open(context.Context) (Session, error)
}

// CapabilityBridge declares the immutable opaque capability names installed
// by a bridge. Runtime copies these names before opening an instance session
// and uses them only to validate a linked Program and later patches.
type CapabilityBridge interface {
	Bridge
	HostCapabilities() []string
}

// Session starts asynchronous host calls and owns all host state established
// by one Mini-Go instance. Start errors mean that no call was established;
// later failures are reported through Completion.
// Stateful implementations reject Start after shutdown begins. Shutdown cancels
// owned work and waits for calls and resource cleanup; its context bounds only
// this wait. Subsequent Shutdown calls wait for the same terminal outcome.
// Completion and resource cleanup must not synchronously call Shutdown on their
// own session, because shutdown may be waiting for them to return.
type Session interface {
	Start(context.Context, Request, Completion) (Call, error)
	Shutdown(context.Context) error
}

// CallFunc adapts a stateless function to Bridge. Every Open returns a distinct
// Session with no additional shutdown work.
type CallFunc func(context.Context, Request, Completion) (Call, error)

func (f CallFunc) Open(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f == nil {
		return nil, nil
	}
	return callSession{start: f}, nil
}

type callSession struct {
	start CallFunc
}

func (s callSession) Start(ctx context.Context, request Request, complete Completion) (Call, error) {
	return s.start(ctx, request, complete)
}

func (callSession) Shutdown(context.Context) error { return nil }

// CancelFunc adapts a function to Call.
type CancelFunc func()

func (f CancelFunc) Cancel() {
	if f != nil {
		f()
	}
}
