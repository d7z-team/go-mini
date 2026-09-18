package rpc

import (
	"context"
	"errors"
	"slices"
	"sync"
)

type ffiProviderEvent struct {
	kind     string
	id       uint64
	method   Method
	receiver *ResourceRef
	payload  []byte
}

type ffiProviderOutcome struct {
	values []Value
	err    error
}

type ffiProviderCall struct {
	result    chan ffiProviderOutcome
	queued    bool
	accepted  bool
	canceled  bool
	responded bool
}

const ffiProviderQueueCapacity = 64

// ffiProvider is a pull-based proxy. Host callers enqueue work and generated
// Mini-Go code receives it through accept/respond FFI operations.
type ffiProvider struct {
	contract Contract
	limits   Limits
	name     string

	mu       sync.Mutex
	closed   bool
	nextCall uint64
	pending  map[uint64]*ffiProviderCall
	events   chan ffiProviderEvent
	canceled []uint64
	cancel   chan struct{}
	done     chan struct{}
	release  func() error
}

func (b *ffiSession) openProvider(ctx context.Context, name string, contract Contract) (uint64, error) {
	if b.publish == nil {
		return 0, StatusError{Code: CodeUnavailable, Message: "MRPC provider publication is not configured"}
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
	normalized, err := normalizeContract(contract, b.limits.MaxMethods)
	if err != nil {
		return 0, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	provider := &ffiProvider{
		contract: normalized, limits: b.limits, name: name,
		pending: make(map[uint64]*ffiProviderCall),
		events:  make(chan ffiProviderEvent, min(b.limits.MaxPendingCalls, ffiProviderQueueCapacity)),
		cancel:  make(chan struct{}, 1), done: make(chan struct{}),
	}
	release, err := b.publish(ctx, provider)
	if err != nil {
		return 0, statusError(err)
	}
	provider.release = release
	if err := ctx.Err(); err != nil {
		_ = provider.close()
		return 0, statusError(err)
	}
	lease, err := b.commitLease(nil, provider)
	reserved = false
	if err != nil {
		_ = provider.close()
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		_ = b.closeProvider(lease)
		return 0, statusError(err)
	}
	return lease, nil
}

func (b *ffiSession) acceptProvider(ctx context.Context, lease uint64) (ffiProviderEvent, error) {
	b.mu.Lock()
	provider := b.providers[lease]
	closed := b.closed
	b.mu.Unlock()
	if closed || provider == nil {
		return ffiProviderEvent{}, StatusError{Code: CodeUnavailable, Message: "MRPC provider lease is closed"}
	}
	return provider.accept(ctx)
}

func (b *ffiSession) respondProvider(lease, requestID uint64, payload []byte, code Code, message string) error {
	b.mu.Lock()
	provider := b.providers[lease]
	closed := b.closed
	b.mu.Unlock()
	if closed || provider == nil {
		return StatusError{Code: CodeUnavailable, Message: "MRPC provider lease is closed"}
	}
	return provider.respond(requestID, payload, code, message)
}

func (b *ffiSession) closeProvider(lease uint64) error {
	return b.closeBinding(context.Background(), lease, true)
}

func (p *ffiProvider) RPCContract() Contract { return cloneContract(p.contract) }

func (p *ffiProvider) RPCProviderID() string { return p.name }

func (p *ffiProvider) BindRPC(ctx context.Context, request BindRequest) (ProviderLease, error) {
	contract, err := normalizeContract(request.Contract, p.limits.MaxMethods)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	closed := p.closed
	available := make(map[Method]struct{}, len(p.contract.Methods))
	for _, method := range p.contract.Methods {
		available[method] = struct{}{}
	}
	p.mu.Unlock()
	if closed {
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
	}
	if err := CheckMethodSupport(p.contract.Methods, contract.Methods); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, statusError(err)
	}
	return &ffiProviderLease{provider: p, methods: available}, nil
}

type ffiProviderLease struct {
	mu       sync.Mutex
	provider *ffiProvider
	methods  map[Method]struct{}
	closed   bool
}

func (l *ffiProviderLease) Invoke(ctx context.Context, method Method, arguments []Value) (*ProviderResult, error) {
	l.mu.Lock()
	provider, closed := l.provider, l.closed
	_, available := l.methods[method]
	l.mu.Unlock()
	if closed || provider == nil || !available {
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider binding is closed"}
	}
	return provider.invoke(ctx, method, nil, arguments)
}

func (l *ffiProviderLease) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.closed = true
	l.provider = nil
	l.methods = nil
	l.mu.Unlock()
	return nil
}

func (p *ffiProvider) invoke(ctx context.Context, method Method, receiver *ResourceRef, arguments []Value) (*ProviderResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	guestArguments := append([]Value(nil), arguments...)
	for index := range guestArguments {
		if guestArguments[index].Resource == nil {
			continue
		}
		resource, err := Resolve(ctx, *guestArguments[index].Resource)
		if err != nil {
			return nil, err
		}
		guest, ok := resource.(*guestProviderResource)
		if !ok || guest.provider != p {
			return nil, StatusError{Code: CodeInvalidArgument, Message: "resource belongs to another MRPC provider"}
		}
		ref := guest.ref
		guestArguments[index].Resource = &ref
	}
	payload, err := EncodeValues(guestArguments, p.limits)
	if err != nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
	}
	if len(p.pending) >= p.limits.MaxPendingCalls || p.nextCall == ^uint64(0) {
		p.mu.Unlock()
		return nil, StatusError{Code: CodeResourceExhausted, Message: "MRPC guest pending call limit exceeded"}
	}
	p.nextCall++
	id := p.nextCall
	call := &ffiProviderCall{result: make(chan ffiProviderOutcome, 1)}
	p.pending[id] = call
	p.mu.Unlock()
	var receiverCopy *ResourceRef
	if receiver != nil {
		cloned := *receiver
		receiverCopy = &cloned
	}
	event := ffiProviderEvent{kind: "call", id: id, method: method, receiver: receiverCopy, payload: payload}
	select {
	case p.events <- event:
		p.mu.Lock()
		if pending := p.pending[id]; pending != nil {
			pending.queued = true
		}
		p.mu.Unlock()
	case <-p.done:
		p.removePending(id)
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
	case <-ctx.Done():
		p.removePending(id)
		return nil, statusError(ctx.Err())
	}
	select {
	case outcome := <-call.result:
		p.removePending(id)
		return p.providerResult(ctx, outcome)
	case <-p.done:
		p.removePending(id)
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
	case <-ctx.Done():
		if p.cancelPending(id) {
			return nil, statusError(ctx.Err())
		}
		select {
		case outcome := <-call.result:
			p.removePending(id)
			return p.providerResult(ctx, outcome)
		case <-p.done:
			p.removePending(id)
			return nil, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
		}
	}
}

func (p *ffiProvider) providerResult(ctx context.Context, outcome ffiProviderOutcome) (*ProviderResult, error) {
	if outcome.err != nil {
		return nil, outcome.err
	}
	if !slices.ContainsFunc(outcome.values, func(value Value) bool { return value.Resource != nil }) {
		return &ProviderResult{Values: outcome.values}, nil
	}
	transferred := make(map[ResourceRef]Value)
	if exporter, ok := ctx.Value(callExporterKey{}).(*routeExporter); ok {
		routes := exporter.routes
		routes.mu.Lock()
		for id, entry := range routes.resources {
			if guest, ok := entry.resource.(*guestProviderResource); ok {
				guest.mu.Lock()
				// Existing ownership includes provisional and closing resources.
				if guest.provider == p {
					transferred[guest.ref] = Value{Type: entry.typeHash, Resource: &ResourceRef{Epoch: routes.epoch, ObjectID: id, TypeHash: entry.typeHash}}
				}
				guest.mu.Unlock()
			}
		}
		routes.mu.Unlock()
	}
	for index := range outcome.values {
		if outcome.values[index].Resource == nil {
			continue
		}
		guestRef := *outcome.values[index].Resource
		if value, ok := transferred[guestRef]; ok {
			outcome.values[index] = value
			continue
		}
		exported, err := Export(ctx, &guestProviderResource{provider: p, ref: guestRef}, guestRef.TypeHash)
		if err != nil {
			for _, value := range outcome.values[index:] {
				if value.Resource == nil {
					continue
				}
				ref := *value.Resource
				if _, ok := transferred[ref]; ok {
					continue
				}
				transferred[ref] = Value{}
				resource := &guestProviderResource{provider: p, ref: ref}
				err = errors.Join(err, resource.Close(context.Background()))
			}
			return nil, err
		}
		transferred[guestRef] = exported
		outcome.values[index] = exported
	}
	return &ProviderResult{Values: outcome.values}, nil
}

func (p *ffiProvider) accept(ctx context.Context) (ffiProviderEvent, error) {
	for {
		p.mu.Lock()
		closed := p.closed
		var canceled uint64
		if len(p.canceled) != 0 {
			canceled = p.canceled[0]
			p.canceled[0] = 0
			p.canceled = p.canceled[1:]
			if len(p.canceled) == 0 {
				p.canceled = nil
			}
		}
		call := p.pending[canceled]
		if call != nil && call.canceled {
			delete(p.pending, canceled)
		}
		p.mu.Unlock()
		if closed {
			return ffiProviderEvent{}, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
		}
		if call != nil && call.canceled {
			return ffiProviderEvent{kind: "cancel", id: canceled}, nil
		}
		select {
		case <-p.cancel:
			continue
		case event := <-p.events:
			p.mu.Lock()
			call := p.pending[event.id]
			if event.kind == "call" && call != nil {
				if call.canceled {
					delete(p.pending, event.id)
					event = ffiProviderEvent{kind: "cancel", id: event.id}
				} else {
					call.accepted = true
				}
			}
			closed = p.closed
			p.mu.Unlock()
			if closed {
				return ffiProviderEvent{}, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
			}
			return event, nil
		case <-p.done:
			return ffiProviderEvent{}, StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}
		case <-ctx.Done():
			return ffiProviderEvent{}, statusError(ctx.Err())
		}
	}
}

func (p *ffiProvider) respond(id uint64, payload []byte, code Code, message string) error {
	p.mu.Lock()
	call := p.pending[id]
	if call == nil || call.responded {
		p.mu.Unlock()
		return StatusError{Code: CodeNotFound, Message: "MRPC guest request is no longer pending"}
	}
	if call.canceled {
		p.mu.Unlock()
		return StatusError{Code: CodeCanceled, Message: "MRPC guest request was canceled"}
	}
	call.responded = true
	p.mu.Unlock()
	outcome := ffiProviderOutcome{}
	if code != "" {
		outcome.err = StatusError{Code: code, Message: message}
	} else {
		values, err := DecodeValues(payload, p.limits)
		if err != nil {
			outcome.err = StatusError{Code: CodeProtocol, Message: err.Error()}
		} else {
			outcome.values = values
		}
	}
	call.result <- outcome
	return nil
}

func (p *ffiProvider) cancelPending(id uint64) bool {
	p.mu.Lock()
	call := p.pending[id]
	if call == nil || call.canceled {
		p.mu.Unlock()
		return false
	}
	if call.responded {
		p.mu.Unlock()
		return false
	}
	call.canceled = true
	if !call.queued {
		delete(p.pending, id)
		p.mu.Unlock()
		return true
	}
	accepted := call.accepted
	if accepted {
		p.canceled = append(p.canceled, id)
	}
	p.mu.Unlock()
	if accepted {
		select {
		case p.cancel <- struct{}{}:
		default:
		}
	}
	return true
}

func (p *ffiProvider) removePending(id uint64) bool {
	p.mu.Lock()
	_, ok := p.pending[id]
	delete(p.pending, id)
	p.mu.Unlock()
	return ok
}

func (p *ffiProvider) close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.done)
	pending := p.pending
	p.pending = nil
	p.canceled = nil
	release := p.release
	p.release = nil
	p.mu.Unlock()
	closed := ffiProviderOutcome{err: StatusError{Code: CodeUnavailable, Message: "MRPC guest provider is closed"}}
	for _, call := range pending {
		if !call.responded && !call.canceled {
			call.result <- closed
		}
	}
	if release != nil {
		return release()
	}
	return nil
}
