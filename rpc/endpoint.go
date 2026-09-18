package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MessageConn is the ordered full-duplex binary connection used by Endpoint.
// Each Read or Write transfers one physical protocol fragment. Close must
// unblock active operations.
type MessageConn interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

// EndpointOptions configures one symmetric RPC endpoint.
type EndpointOptions struct {
	Limits Limits
	Peer   PeerInfo
	// LeaseTTL is the receiver-granted owner lease. Zero selects 60 seconds.
	LeaseTTL time.Duration
	// AdmissionTimeout bounds receipt after a complete request write.
	AdmissionTimeout time.Duration
	// MaxCallDuration optionally bounds remote handler execution. Zero is unlimited.
	MaxCallDuration time.Duration
}

// EndpointServices contains optional inbound capabilities exposed to the peer.
type EndpointServices struct {
	Binder Binder
}

type endpointState uint8

const (
	endpointOpen endpointState = iota
	endpointClosing
	endpointClosed
)

type endpointPending struct {
	response  chan endpointFrame
	control   bool
	kind      string
	binding   uint64
	accepted  bool
	terminal  bool
	abandoned bool
}

type endpointActive struct {
	cancel  context.CancelFunc
	control bool
	binding uint64
}

type endpointResult struct {
	binding   uint64
	result    *Result
	expiresAt time.Time
	deciding  bool
	accepting bool
	retiring  bool
}

type endpointWrite struct {
	frame    endpointFrame
	payload  []byte
	done     chan error
	ctx      context.Context
	control  bool
	queuedAt time.Time
	sentAt   time.Time
}

const (
	endpointReplyQueueCapacity   = 32
	endpointRequestQueueCapacity = 64
)

// Endpoint is both an outbound Binder and an inbound dispatcher. Either side
// of a connection may bind and call services exposed by the other side.
type Endpoint struct {
	conn           MessageConn
	limits         Limits
	outboundLimits Limits
	services       EndpointServices
	peer           PeerInfo
	origin         string
	options        EndpointOptions

	replyWrites          chan endpointWrite
	controlWrites        chan endpointWrite
	requestWrites        chan endpointWrite
	writerDone           chan struct{}
	readerDone           chan struct{}
	stopping             chan struct{}
	requestGate          chan struct{}
	mu                   sync.Mutex
	nextID               uint64
	nextBind             uint64
	peerID               uint64
	peerRenewID          uint64
	pending              map[uint64]endpointPending
	active               map[uint64]endpointActive
	inbound              map[uint64]*RouteSet
	outbound             map[uint64]*RouteSet
	inboundLease         map[uint64]time.Time
	inboundOperations    map[uint64]time.Time
	outboundLease        map[uint64]time.Time
	outboundOperations   map[uint64]time.Time
	renewOffset          int
	results              map[uint64]endpointResult
	pendingData          int
	pendingControls      int
	activeData           int
	activeControls       int
	resultReservations   int
	queuedWriteBytes     int
	queuedControlBytes   int
	inboundReservations  int
	outboundReservations int
	peerOrigin           string
	peerLeaseTTL         time.Duration
	state                endpointState
	closeErr             error

	handshake       chan struct{}
	helloSent       chan struct{}
	ready           chan struct{}
	closeOnce       sync.Once
	enqueueWG       sync.WaitGroup
	helloOnce       sync.Once
	readyOnce       sync.Once
	maintenanceOnce sync.Once
	dispatchWG      sync.WaitGroup
	shutdownDone    chan struct{}
	shutdownErr     error
	transportCtx    context.Context
	stopTransport   context.CancelFunc
}

// OpenEndpoint starts a symmetric protocol session over conn. The returned
// endpoint owns the connection and closes it from Close.
func OpenEndpoint(conn MessageConn, services EndpointServices, options EndpointOptions) (*Endpoint, error) {
	if conn == nil {
		return nil, errors.New("rpc endpoint message connection is required")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("create rpc endpoint identity: %w", err)
	}
	limits := normalizeLimits(options.Limits)
	if err := validateWireLimits(limitsToWire(limits)); err != nil || limits.MaxPendingControls <= 0 || limits.MaxResources <= 0 {
		return nil, errors.New("rpc endpoint limits must be positive")
	}
	if options.LeaseTTL == 0 {
		options.LeaseTTL = 60 * time.Second
	}
	if options.AdmissionTimeout == 0 {
		options.AdmissionTimeout = 10 * time.Second
	}
	if options.LeaseTTL < time.Millisecond || options.AdmissionTimeout < time.Millisecond || options.MaxCallDuration < 0 {
		return nil, errors.New("rpc lease and admission timeouts must be at least 1ms; maximum call duration must not be negative")
	}
	replyQueueSize := min(limits.MaxPendingControls, endpointReplyQueueCapacity)
	requestQueueSize := min(limits.MaxPendingCalls, endpointRequestQueueCapacity)
	transportCtx, stopTransport := context.WithCancel(context.Background())
	peer := clonePeerInfo(options.Peer)
	options.Peer = PeerInfo{}
	endpoint := &Endpoint{
		conn: conn, limits: limits, outboundLimits: limits, services: services, peer: peer, options: options,
		origin: hex.EncodeToString(nonce[:]), pending: make(map[uint64]endpointPending),
		active: make(map[uint64]endpointActive), inbound: make(map[uint64]*RouteSet),
		outbound: make(map[uint64]*RouteSet), inboundLease: make(map[uint64]time.Time),
		inboundOperations: make(map[uint64]time.Time),
		outboundLease:     make(map[uint64]time.Time), outboundOperations: make(map[uint64]time.Time), results: make(map[uint64]endpointResult),
		replyWrites: make(chan endpointWrite, replyQueueSize), requestWrites: make(chan endpointWrite, requestQueueSize),
		controlWrites: make(chan endpointWrite, replyQueueSize),
		requestGate:   make(chan struct{}, 1),
		writerDone:    make(chan struct{}), readerDone: make(chan struct{}), stopping: make(chan struct{}), handshake: make(chan struct{}),
		helloSent: make(chan struct{}), ready: make(chan struct{}), shutdownDone: make(chan struct{}),
		transportCtx: transportCtx, stopTransport: stopTransport,
	}
	go endpoint.writeLoop()
	wiredLimits := limitsToWire(limits)
	if err := endpoint.write(endpointFrame{
		Kind: "hello", Protocol: EndpointProtocol, Limits: &wiredLimits, Origin: endpoint.origin,
		LeaseTTL: options.LeaseTTL.Nanoseconds(), AdmissionTimeout: options.AdmissionTimeout.Nanoseconds(),
		MaxCallDuration: options.MaxCallDuration.Nanoseconds(),
	}); err != nil {
		endpoint.fail(err)
	}
	endpoint.helloOnce.Do(func() { close(endpoint.helloSent) })
	go endpoint.readLoop()
	return endpoint, nil
}

// Ready waits until the peer has completed the protocol handshake.
func (e *Endpoint) Ready(ctx context.Context) error {
	if e == nil {
		return StatusError{Code: CodeUnavailable, Message: "rpc endpoint is nil"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-e.handshake:
	case <-ctx.Done():
		return statusError(ctx.Err())
	}
	select {
	case <-e.ready:
		return e.endpointError()
	case <-ctx.Done():
		return statusError(ctx.Err())
	}
}

func (e *Endpoint) Bind(ctx context.Context, request BindRequest) (*RouteSet, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := e.Ready(ctx); err != nil {
		return nil, err
	}
	e.mu.Lock()
	if len(e.outbound)+e.outboundReservations >= e.outboundLimits.MaxBindings {
		e.mu.Unlock()
		return nil, StatusError{Code: CodeResourceExhausted, Message: "rpc endpoint binding limit exceeded"}
	}
	e.outboundReservations++
	e.mu.Unlock()
	reserved := true
	defer func() {
		if reserved {
			e.mu.Lock()
			e.outboundReservations--
			e.mu.Unlock()
		}
	}()
	limits := e.outboundLimitSnapshot()
	contract, err := normalizeContract(request.Contract, limits.MaxMethods)
	if err != nil {
		return nil, err
	}
	response, err := e.request(ctx, endpointFrame{
		Kind:     "bind",
		Contract: &contract,
		Options: BindOptions{
			AffinityKey: request.Options.AffinityKey,
			Labels:      cloneLabels(request.Options.Labels),
		},
		Hops: request.Hops,
	})
	if err != nil {
		return nil, err
	}
	binding := response.Binding
	lease := time.Duration(response.LeaseTTL)
	var routes *RouteSet
	routes = newRemoteRouteSet(response.Epoch, e.limits, contract.Methods, &remoteBinding{
		call: func(callCtx context.Context, call Call) (*Result, error) {
			if err := e.Ready(callCtx); err != nil {
				return nil, err
			}
			arguments, err := EncodeValues(call.Arguments, limits)
			if err != nil {
				if errors.Is(err, errValueLimit) {
					return nil, StatusError{Code: CodeResourceExhausted, Message: err.Error()}
				}
				return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
			}
			wiredCall := &wireCall{Method: call.Method, Arguments: arguments}
			if call.Receiver != nil {
				ref := *call.Receiver
				wiredCall.Receiver = &ref
			}
			response, err := e.request(callCtx, endpointFrame{Kind: "call", Binding: binding, Call: wiredCall})
			if err != nil {
				return nil, err
			}
			values, err := DecodeValues(response.Values, limits)
			if err != nil {
				return nil, StatusError{Code: CodeProtocol, Message: err.Error()}
			}
			operationID := response.TargetID
			e.mu.Lock()
			currentExpiry, tracked := e.outboundOperations[operationID]
			now := time.Now()
			expired := !tracked || currentExpiry.IsZero() || !now.Before(currentExpiry)
			e.mu.Unlock()
			result := &Result{Values: values, accept: func(accept bool) error {
				controlCtx, cancel := e.controlContext()
				defer cancel()
				_, err := e.request(controlCtx, endpointFrame{Kind: "decision", Binding: binding, TargetID: operationID, Accept: accept})
				if err == nil {
					e.mu.Lock()
					delete(e.outboundOperations, operationID)
					e.mu.Unlock()
				}
				if endpointOutcomeUncertain(err) {
					e.invalidateOutbound(binding, routes)
				}
				return err
			}}
			if expired {
				_ = result.Discard(context.Background())
				return nil, StatusError{Code: CodeUnavailable, Message: "rpc call operation lease expired before offer"}
			}
			return result, nil
		},
		drop: func(dropCtx context.Context, ref ResourceRef) error {
			dropCtx, cancel := context.WithCancel(dropCtx)
			defer cancel()
			_, err := e.request(dropCtx, endpointFrame{Kind: "drop", Binding: binding, Ref: &ref})
			if endpointOutcomeUncertain(err) {
				e.invalidateOutbound(binding, routes)
			}
			return err
		},
		close: func() error {
			controlCtx, cancel := e.controlContext()
			defer cancel()
			_, err := e.request(controlCtx, endpointFrame{Kind: "close", Binding: binding})
			if endpointOutcomeUncertain(err) {
				e.invalidateOutbound(binding, routes)
			} else {
				e.mu.Lock()
				delete(e.outbound, binding)
				delete(e.outboundLease, binding)
				e.mu.Unlock()
			}
			return err
		},
	})
	e.mu.Lock()
	e.outboundReservations--
	reserved = false
	if e.state != endpointOpen {
		e.mu.Unlock()
		routes.disconnect()
		return nil, e.endpointError()
	}
	if e.outbound[binding] != nil {
		e.mu.Unlock()
		routes.disconnect()
		err := StatusError{Code: CodeProtocol, Message: "rpc binding identity is already active"}
		e.fail(err)
		return nil, err
	}
	e.outbound[binding] = routes
	e.outboundLease[binding] = time.Now().Add(lease)
	e.mu.Unlock()

	// A BIND response may have spent most of its granted lease in transit or
	// in the remote binder. Confirm the newly published owner once before
	// returning it to the caller; this gives the receiver a fresh, explicit
	// lease anchor instead of extending from response arrival time.
	targets := encodeLeaseTargetList([]leaseTarget{{kind: leaseTargetBinding, id: binding}})
	expected, decodeErr := decodeLeaseTargets(targets, e.limits)
	if decodeErr != nil {
		e.invalidateOutbound(binding, routes)
		return nil, StatusError{Code: CodeProtocol, Message: decodeErr.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, e.options.AdmissionTimeout)
	sentAt := time.Now()
	ack, renewErr := e.request(ctx, endpointFrame{Kind: "renew", Values: targets})
	cancel()
	if renewErr == nil {
		var acknowledged leaseTargets
		acknowledged, renewErr = decodeLeaseTargets(ack.Values, e.limits)
		if renewErr == nil {
			found := false
			for _, id := range acknowledged.bindings {
				if id == binding {
					found = true
					break
				}
			}
			if !found {
				renewErr = StatusError{Code: CodeUnavailable, Message: "rpc binding lease expired before confirmation"}
			} else {
				renewErr = e.renewOutboundTargets(expected, acknowledged, sentAt, time.Duration(ack.LeaseTTL))
			}
		}
	}
	if renewErr != nil {
		e.invalidateOutbound(binding, routes)
		return nil, renewErr
	}
	e.mu.Lock()
	expires, valid := e.outboundLease[binding]
	e.mu.Unlock()
	if !valid || !time.Now().Before(expires) || !time.Now().Before(sentAt.Add(time.Duration(ack.LeaseTTL))) {
		e.invalidateOutbound(binding, routes)
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc binding confirmation arrived after its lease"}
	}
	return routes, nil
}
