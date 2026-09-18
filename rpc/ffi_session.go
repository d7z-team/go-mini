package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/d7z-team/mini-go/ffi"
)

// FFIRoute identifies the MRPC extension on the generic FFI boundary.
const FFIRoute = "minigo.mrpc/v2"

// PublishProviderFunc makes a guest provider visible to an application-owned
// registry. The returned function removes the publication.
type PublishProviderFunc func(context.Context, Provider) (func() error, error)

// sessionConfig configures the MRPC extension for the generic FFI boundary.
// Binder enables outbound calls and PublishProvider enables guest providers;
// at least one must be set.
type sessionConfig struct {
	Binder          Binder
	Limits          Limits
	PublishProvider PublishProviderFunc
	Interceptors    []UnaryInterceptor
}

// ffiSession exposes one Binder through the opaque VM FFI boundary. The session
// owns RouteSets that it creates, but it does not own the Binder or Providers.
type ffiSession struct {
	binder Binder
	limits Limits

	mu              sync.Mutex
	closed          bool
	nextLease       uint64
	reservations    int
	constructors    sync.WaitGroup
	workers         sync.WaitGroup
	cleanups        sync.WaitGroup
	callContext     context.Context
	cancelCalls     context.CancelFunc
	leases          map[uint64]*RouteSet
	providers       map[uint64]*ffiProvider
	retiring        map[uint64]*ffiLeaseCleanup
	publish         PublishProviderFunc
	interceptors    []UnaryInterceptor
	nextResult      uint64
	results         map[uint64]ffiPendingResult
	resultBytes     int
	decidingResults int
	shutdownOnce    sync.Once
	shutdownDone    chan struct{}
	shutdownErr     error
	onShutdown      func(*ffiSession)
}

func newFFISession(config sessionConfig) (*ffiSession, error) {
	if config.Binder == nil && config.PublishProvider == nil {
		return nil, errors.New("MRPC FFI session requires a binder or provider publisher")
	}
	callContext, cancelCalls := context.WithCancel(context.Background())
	return &ffiSession{
		binder: config.Binder, limits: normalizeLimits(config.Limits), publish: config.PublishProvider,
		interceptors: append([]UnaryInterceptor(nil), config.Interceptors...),
		leases:       make(map[uint64]*RouteSet), providers: make(map[uint64]*ffiProvider),
		shutdownDone: make(chan struct{}),
		retiring:     make(map[uint64]*ffiLeaseCleanup),
		results:      make(map[uint64]ffiPendingResult),
		callContext:  callContext, cancelCalls: cancelCalls,
	}, nil
}

func (b *ffiSession) Start(ctx context.Context, request ffi.Request, complete ffi.Completion) (ffi.Call, error) {
	if request.Route != FFIRoute {
		return nil, fmt.Errorf("%w: %s", ffi.ErrRouteUnavailable, request.Route)
	}
	if complete == nil {
		return nil, errors.New("MRPC FFI completion is nil")
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errors.New("MRPC FFI session is closed")
	}
	b.workers.Add(1)
	b.mu.Unlock()
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(b.callContext, cancel)
	go func() {
		defer b.workers.Done()
		defer stop()
		defer cancel()
		complete(b.handle(callCtx, request.Payload))
	}()
	return ffi.CancelFunc(cancel), nil
}

func (b *ffiSession) handle(ctx context.Context, payload []byte) ffi.Result {
	request, err := decodeFFIRequest(payload, b.limits)
	if err != nil {
		return ffi.Result{Err: fmt.Errorf("decode MRPC FFI request: %w", err)}
	}
	if request.Version != FFIProtocol {
		return ffi.Result{Err: errors.New("unsupported MRPC FFI version")}
	}
	if request.TimeoutNanos < 0 {
		return ffi.Result{Err: errors.New("invalid negative MRPC timeout")}
	}
	if request.TimeoutNanos > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutNanos))
		defer cancel()
	}
	response := ffiResponse{Version: FFIProtocol}
	var release func() error
	switch request.Operation {
	case "open":
		response.Lease, err = b.open(ctx, request.Contract, request.Options)
	case "call", "call_owned":
		response.Payload, response.RequestID, release, err = b.call(ctx, request)
	case "accept_result", "discard_result":
		release, err = b.decideResult(ctx, request.Lease, request.RequestID, request.Operation == "accept_result")
	case "release":
		if request.Resource == nil {
			err = StatusError{Code: CodeInvalidArgument, Message: "release requires a resource"}
		} else {
			err = b.withLease(request.Lease, func(routes *RouteSet) error { return routes.Drop(ctx, *request.Resource) })
		}
	case "close":
		err = b.closeLease(ctx, request.Lease)
	case "open_provider":
		response.Lease, err = b.openProvider(ctx, request.Target, request.Contract)
	case "accept":
		var event ffiProviderEvent
		event, err = b.acceptProvider(ctx, request.Lease)
		if err == nil {
			response.Operation = event.kind
			response.RequestID = event.id
			response.Method = event.method
			response.Receiver = event.receiver
			response.Payload = event.payload
		}
	case "respond":
		err = b.respondProvider(request.Lease, request.RequestID, request.Payload, request.Code, request.Message)
	case "close_provider":
		err = b.closeBinding(ctx, request.Lease, true)
	default:
		err = StatusError{Code: CodeInvalidArgument, Message: "unknown MRPC FFI operation"}
	}
	if err != nil {
		response.Code, response.Message = CodeOf(err)
	}
	result := ffi.Result{Payload: encodeFFIResponse(response)}
	if err == nil && release != nil {
		result.Discard = sync.OnceFunc(func() { b.scheduleCleanup(release) })
	}
	if err == nil && response.Lease != 0 {
		// The lease is already session-owned. A dropped open response must not
		// retain it until instance shutdown. Close remains idempotent with Shutdown.
		lease := response.Lease
		provider := request.Operation == "open_provider"
		result.Discard = sync.OnceFunc(func() {
			b.scheduleCleanup(func() error {
				if provider {
					return b.closeProvider(lease)
				}
				return b.closeLease(context.Background(), lease)
			})
		})
	}
	return result
}

func (b *ffiSession) open(ctx context.Context, contract Contract, options BindOptions) (uint64, error) {
	if b.binder == nil {
		return 0, StatusError{Code: CodeUnimplemented, Message: "MRPC outbound binder is not configured"}
	}
	if err := b.reserveLease(); err != nil {
		return 0, err
	}
	reserved := true
	defer func() {
		if reserved {
			b.releaseLeaseReservation()
		} else {
			b.constructors.Done()
		}
	}()
	routes, err := b.binder.Bind(ctx, BindRequest{Contract: contract, Options: options})
	if err != nil {
		return 0, err
	}
	if routes == nil {
		return 0, StatusError{Code: CodeInternal, Message: "MRPC binder returned a nil route set"}
	}
	if err := ctx.Err(); err != nil {
		_ = routes.Shutdown(context.Background())
		return 0, statusError(err)
	}
	lease, err := b.commitLease(routes, nil)
	reserved = false
	if err != nil {
		_ = routes.Shutdown(context.Background())
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		_ = b.closeLease(context.Background(), lease)
		return 0, statusError(err)
	}
	return lease, nil
}

func (b *ffiSession) call(ctx context.Context, request ffiRequest) ([]byte, uint64, func() error, error) {
	arguments, err := DecodeValues(request.Payload, b.limits)
	if err != nil {
		return nil, 0, nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	call := Call{Method: request.Method, Receiver: request.Receiver, Arguments: arguments}
	invoke := UnaryInvoker(func(ctx context.Context, call Call) (*Result, error) {
		var result *Result
		err := b.withLease(request.Lease, func(routes *RouteSet) error {
			var callErr error
			result, callErr = routes.Call(ctx, call)
			return callErr
		})
		return result, err
	})
	for index := len(b.interceptors) - 1; index >= 0; index-- {
		interceptor := b.interceptors[index]
		if interceptor == nil {
			continue
		}
		next := invoke
		invoke = func(ctx context.Context, call Call) (*Result, error) {
			return interceptor(ctx, call, next)
		}
	}
	result, err := invoke(ctx, call)
	if err != nil {
		return nil, 0, nil, err
	}
	if result == nil {
		return nil, 0, nil, StatusError{Code: CodeInternal, Message: "MRPC interceptor returned a nil result"}
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = result.Discard(context.Background())
		}
	}()
	encoded, err := EncodeValues(result.Values, b.limits)
	if err != nil {
		return nil, 0, nil, StatusError{Code: CodeInternal, Message: err.Error()}
	}
	if request.Operation == "call_owned" {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.closed || b.leases[request.Lease] == nil {
			return nil, 0, nil, StatusError{Code: CodeUnavailable, Message: "MRPC FFI lease is closed"}
		}
		if len(b.results)+b.decidingResults >= b.limits.MaxPendingResults || b.nextResult == ^uint64(0) || len(encoded) > b.limits.MaxInFlightBytes || b.resultBytes > b.limits.MaxInFlightBytes-len(encoded) {
			return nil, 0, nil, StatusError{Code: CodeResourceExhausted, Message: "MRPC FFI pending result limit exceeded"}
		}
		b.nextResult++
		id := b.nextResult
		b.results[id] = ffiPendingResult{lease: request.Lease, result: result, bytes: len(encoded)}
		b.resultBytes += len(encoded)
		transferred = true
		return encoded, id, func() error { _, err := b.decideResult(context.Background(), request.Lease, id, false); return err }, nil
	}
	if err := result.Accept(context.Background()); err != nil {
		return nil, 0, nil, err
	}
	return encoded, 0, result.release, nil
}

func (b *ffiSession) withLease(id uint64, use func(*RouteSet) error) error {
	b.mu.Lock()
	routes := b.leases[id]
	closed := b.closed
	b.mu.Unlock()
	if closed || routes == nil {
		return StatusError{Code: CodeUnavailable, Message: "MRPC FFI lease is closed"}
	}
	return use(routes)
}
