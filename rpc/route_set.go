package rpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Route is one method bound to a RouteSet.
type Route struct {
	set    *RouteSet
	method Method
}

func (r Route) Method() Method { return r.method }

// RouteSet returns the owner of this bound route.
func (r Route) RouteSet() *RouteSet { return r.set }

func (r Route) Call(ctx context.Context, receiver *ResourceRef, arguments []Value) (*Result, error) {
	if r.set == nil {
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc route is not bound"}
	}
	return r.set.Call(ctx, Call{Method: r.method, Receiver: receiver, Arguments: arguments})
}

// RouteSet owns bound methods, calls, and resources for one routing epoch.
type RouteSet struct {
	mu                     sync.Mutex
	callContext            context.Context
	cancelCalls            context.CancelFunc
	calls                  sync.WaitGroup
	epoch                  uint64
	limits                 Limits
	state                  routeSetState
	nextCall               uint64
	nextResource           uint64
	pendingCalls           int
	pendingResults         int
	methods                map[Method]struct{}
	services               map[string]*serviceLease
	resources              map[uint64]*providerResource
	handles                map[uint64]*ResourceHandle
	resourceCloses         map[uint64]*resourceClose
	pendingResultOwners    map[*Result]struct{}
	resourceOrder          []uint64
	provisionalResources   map[uint64][]uint64
	remote                 *remoteBinding
	remoteResources        map[uint64]ResourceRef
	pendingRemoteResources map[uint64]ResourceRef
	cleanupDone            chan struct{}
	cleanupErr             error
}

type providerResource struct {
	resource   Resource
	typeHash   string
	active     bool
	borrowed   int
	borrowDone chan struct{}
}

// resourceBorrowScope identifies resources borrowed by one local handler.
// It is attached to the handler context so a resource can reject a
// synchronous Close from its own invocation before waiting on itself.
type resourceBorrowScope struct {
	routes    *RouteSet
	resources map[uint64]struct{}
	active    bool // protected by routes.mu
}

type resourceBorrowScopeKey struct{}

type routeSetState uint8

const (
	routeSetOpen routeSetState = iota
	routeSetDraining
	routeSetCleaning
	routeSetClosed
)

func (state routeSetState) acceptsServiceCalls() bool { return state == routeSetOpen }

func (state routeSetState) acceptsResourceCalls() bool { return state < routeSetCleaning }

func newRouteSet(epoch uint64, limits Limits, methods []Method, services map[string]*serviceLease) *RouteSet {
	callContext, cancelCalls := context.WithCancel(context.Background())
	routes := &RouteSet{
		epoch: epoch, limits: limits, callContext: callContext, cancelCalls: cancelCalls,
		methods: make(map[Method]struct{}, len(methods)), services: services,
		resources: make(map[uint64]*providerResource), provisionalResources: make(map[uint64][]uint64),
		pendingResultOwners: make(map[*Result]struct{}),
		remoteResources:     make(map[uint64]ResourceRef), pendingRemoteResources: make(map[uint64]ResourceRef),
		cleanupDone: make(chan struct{}),
	}
	for _, method := range methods {
		routes.methods[method] = struct{}{}
	}
	return routes
}

type remoteBinding struct {
	call  func(context.Context, Call) (*Result, error)
	drop  func(context.Context, ResourceRef) error
	close func() error
}

func newRemoteRouteSet(epoch uint64, limits Limits, methods []Method, remote *remoteBinding) *RouteSet {
	routes := newRouteSet(epoch, limits, methods, nil)
	routes.remote = remote
	return routes
}

func (s *RouteSet) Epoch() uint64 {
	if s == nil {
		return 0
	}
	return s.epoch
}

func (s *RouteSet) Route(method Method) (Route, error) {
	if s == nil {
		return Route{}, StatusError{Code: CodeUnavailable, Message: "rpc route set is nil"}
	}
	if err := validateMethod(method); err != nil {
		return Route{}, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	s.mu.Lock()
	_, ok := s.methods[method]
	available := s.state.acceptsResourceCalls()
	if method.ResourceTypeHash == "" {
		available = s.state.acceptsServiceCalls()
	}
	s.mu.Unlock()
	if !available || !ok {
		return Route{}, StatusError{Code: CodeUnavailable, Message: "rpc method is not bound: " + method.ID}
	}
	return Route{set: s, method: method}, nil
}

func (s *RouteSet) Call(ctx context.Context, call Call) (*Result, error) {
	if s == nil {
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc route set is nil"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, statusError(err)
	}
	s.mu.Lock()
	remote := s.remote
	_, bound := s.methods[call.Method]
	method := call.Method
	available := s.state.acceptsResourceCalls()
	if call.Receiver == nil {
		available = s.state.acceptsServiceCalls()
	}
	if !available || !bound {
		s.mu.Unlock()
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc method is not bound"}
	}
	if (call.Receiver == nil) != (method.ResourceTypeHash == "") {
		s.mu.Unlock()
		return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc method receiver does not match its contract"}
	}
	if remote != nil {
		if err := validateValues(call.Arguments, s.limits); err != nil {
			s.mu.Unlock()
			return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
		}
		if call.Receiver != nil {
			if err := validateResourceRef(*call.Receiver); err != nil {
				s.mu.Unlock()
				return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
			}
			known, ok := s.remoteResources[call.Receiver.ObjectID]
			ok = ok && s.resourceCloses[call.Receiver.ObjectID] == nil
			if !ok || known != *call.Receiver || call.Receiver.Epoch != s.epoch || method.ResourceTypeHash != call.Receiver.TypeHash {
				s.mu.Unlock()
				return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource belongs to another route set"}
			}
		}
		for _, value := range call.Arguments {
			if value.Resource == nil {
				continue
			}
			known, ok := s.remoteResources[value.Resource.ObjectID]
			ok = ok && s.resourceCloses[value.Resource.ObjectID] == nil
			if !ok || known != *value.Resource || value.Resource.Epoch != s.epoch {
				s.mu.Unlock()
				return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource argument belongs to another route set"}
			}
		}
		if s.pendingCalls+s.pendingResults >= s.limits.MaxPendingCalls {
			s.mu.Unlock()
			return nil, StatusError{Code: CodeResourceExhausted, Message: "rpc pending call limit exceeded"}
		}
		s.pendingCalls++
		s.calls.Add(1)
		callContext := s.callContext
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.pendingCalls--
			s.mu.Unlock()
			s.calls.Done()
			_ = s.finalizeIfIdle()
		}()
		callCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(callContext, cancel)
		result, err := remote.call(callCtx, call)
		stop()
		cancel()
		if err != nil || result == nil {
			_ = s.finalizeIfIdle()
			if code, _ := CodeOf(err); code == CodeCanceled && ctx.Err() == nil && callContext.Err() != nil {
				err = StatusError{Code: CodeUnavailable, Message: "rpc binding was revoked"}
			}
			if err == nil {
				err = StatusError{Code: CodeInternal, Message: "rpc endpoint returned a nil result"}
			}
			return result, err
		}
		if err := validateValues(result.Values, s.limits); err != nil {
			_ = result.Discard(context.Background())
			_ = s.finalizeIfIdle()
			return nil, StatusError{Code: CodeProtocol, Message: "rpc endpoint returned invalid values: " + err.Error()}
		}
		refs := make([]ResourceRef, 0, len(result.Values))
		s.mu.Lock()
		invalid := ""
		if !s.state.acceptsResourceCalls() {
			invalid = "rpc route set is closed"
		}
		seen := make(map[uint64]ResourceRef)
		for _, value := range result.Values {
			if value.Resource == nil {
				continue
			}
			ref := *value.Resource
			if ref.Epoch != s.epoch {
				invalid = "rpc endpoint returned a resource for another route set"
				break
			}
			if previous, duplicate := seen[ref.ObjectID]; duplicate {
				if previous != ref {
					invalid = "rpc endpoint returned conflicting resource references"
					break
				}
				continue
			}
			seen[ref.ObjectID] = ref
			if previous, exists := s.remoteResources[ref.ObjectID]; exists {
				if previous != ref || s.resourceCloses[ref.ObjectID] != nil {
					invalid = "rpc endpoint returned an invalid existing resource"
					break
				}
				continue
			}
			if _, exists := s.pendingRemoteResources[ref.ObjectID]; exists {
				invalid = "rpc endpoint reused a pending resource id"
				break
			}
			refs = append(refs, ref)
		}
		if invalid == "" && len(s.remoteResources)+len(s.pendingRemoteResources)+len(refs) > s.limits.MaxResources {
			invalid = "rpc resource limit exceeded"
		}
		if invalid == "" {
			for _, ref := range refs {
				s.pendingRemoteResources[ref.ObjectID] = ref
			}
			s.pendingResults++
		}
		s.mu.Unlock()
		if invalid != "" {
			_ = result.Discard(context.Background())
			_ = s.finalizeIfIdle()
			return nil, StatusError{Code: CodeProtocol, Message: invalid}
		}
		decision := result.accept
		result.accept = func(accept bool) error {
			var err error
			if decision != nil {
				err = decision(accept)
			}
			s.mu.Lock()
			for _, ref := range refs {
				delete(s.pendingRemoteResources, ref.ObjectID)
				if err == nil && accept && s.state.acceptsResourceCalls() {
					s.remoteResources[ref.ObjectID] = ref
				} else {
					s.closeHandleLocked(ref.ObjectID)
				}
			}
			if s.pendingResults > 0 {
				s.pendingResults--
			}
			delete(s.pendingResultOwners, result)
			s.mu.Unlock()
			_ = s.finalizeIfIdle()
			return err
		}
		if len(refs) != 0 {
			result.release = func() error {
				var cleanupErr error
				for _, ref := range refs {
					err := s.Drop(context.Background(), ref)
					if err != nil {
						s.mu.Lock()
						shutdownOwnsResources := s.state >= routeSetCleaning
						s.mu.Unlock()
						if shutdownOwnsResources {
							return cleanupErr
						}
					}
					cleanupErr = errors.Join(cleanupErr, err)
				}
				return cleanupErr
			}
		}
		s.mu.Lock()
		s.pendingResultOwners[result] = struct{}{}
		s.mu.Unlock()
		return result, nil
	}
	s.mu.Unlock()
	if err := validateValues(call.Arguments, s.limits); err != nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	s.mu.Lock()
	available = s.state.acceptsResourceCalls()
	if call.Receiver == nil {
		available = s.state.acceptsServiceCalls()
	}
	if !available || !bound {
		s.mu.Unlock()
		return nil, StatusError{Code: CodeUnavailable, Message: "rpc method is not bound"}
	}
	if s.pendingCalls+s.pendingResults >= s.limits.MaxPendingCalls {
		s.mu.Unlock()
		return nil, StatusError{Code: CodeResourceExhausted, Message: "rpc pending call limit exceeded"}
	}
	var lease *serviceLease
	var resource Resource
	var borrowed []*providerResource
	var borrowScope *resourceBorrowScope
	var borrowIDs map[uint64]struct{}
	for _, value := range call.Arguments {
		if value.Resource == nil {
			continue
		}
		ref := *value.Resource
		entry := s.resources[ref.ObjectID]
		if ref.Epoch != s.epoch || entry == nil || !entry.active || entry.typeHash != ref.TypeHash || s.resourceCloses[ref.ObjectID] != nil {
			s.mu.Unlock()
			return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource argument belongs to another route set"}
		}
		if borrowIDs == nil {
			borrowIDs = make(map[uint64]struct{})
		}
		borrowIDs[ref.ObjectID] = struct{}{}
	}
	if s.nextCall == ^uint64(0) {
		s.mu.Unlock()
		return nil, StatusError{Code: CodeResourceExhausted, Message: "rpc call id space exhausted"}
	}
	s.nextCall++
	callID := s.nextCall
	s.pendingCalls++
	if call.Receiver == nil {
		lease = s.services[method.Service]
		if lease == nil || lease.provider == nil {
			s.pendingCalls--
			s.mu.Unlock()
			return nil, StatusError{Code: CodeUnavailable, Message: "rpc service is not bound"}
		}
	} else {
		if err := validateResourceRef(*call.Receiver); err != nil {
			s.pendingCalls--
			s.mu.Unlock()
			return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
		}
		if call.Receiver.Epoch != s.epoch || method.ResourceTypeHash == "" || method.ResourceTypeHash != call.Receiver.TypeHash {
			s.pendingCalls--
			s.mu.Unlock()
			return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource belongs to another route set"}
		}
		entry := s.resources[call.Receiver.ObjectID]
		if entry == nil || !entry.active || entry.typeHash != call.Receiver.TypeHash || s.resourceCloses[call.Receiver.ObjectID] != nil {
			s.pendingCalls--
			s.mu.Unlock()
			return nil, StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
		}
		resource = entry.resource
		if borrowIDs == nil {
			borrowIDs = make(map[uint64]struct{}, 1)
		}
		borrowIDs[call.Receiver.ObjectID] = struct{}{}
	}
	if len(borrowIDs) != 0 {
		borrowScope = &resourceBorrowScope{routes: s, resources: borrowIDs, active: true}
		for id := range borrowIDs {
			entry := s.resources[id]
			if entry.borrowed == 0 {
				entry.borrowDone = make(chan struct{})
			}
			entry.borrowed++
			borrowed = append(borrowed, entry)
		}
	}
	s.calls.Add(1)
	callContext := s.callContext
	s.mu.Unlock()

	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(callContext, cancel)
	if borrowScope != nil {
		callCtx = context.WithValue(callCtx, resourceBorrowScopeKey{}, borrowScope)
	}
	defer func() {
		stop()
		cancel()
		s.mu.Lock()
		s.pendingCalls--
		s.mu.Unlock()
		s.calls.Done()
		_ = s.finalizeIfIdle()
	}()
	exporter := &routeExporter{routes: s, callID: callID}
	callCtx = context.WithValue(callCtx, callExporterKey{}, Exporter(exporter))
	if borrowScope != nil {
		callCtx = context.WithValue(callCtx, callResolverKey{}, Resolver(borrowScope))
	} else {
		callCtx = context.WithValue(callCtx, callResolverKey{}, Resolver(s))
	}
	var providerResult *ProviderResult
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = StatusError{Code: CodeInternal, Message: fmt.Sprintf("rpc handler panic: %v", recovered)}
			}
		}()
		if resource != nil {
			var values []Value
			values, err = resource.Invoke(callCtx, method.Name, append([]Value(nil), call.Arguments...))
			providerResult = &ProviderResult{Values: values}
		} else {
			providerResult, err = lease.Invoke(callCtx, method, call.Arguments)
		}
	}()
	if borrowScope != nil {
		s.releaseResourceBorrows(borrowScope, borrowed)
	}
	s.mu.Lock()
	provisional := append([]uint64(nil), s.provisionalResources[callID]...)
	if err != nil {
		delete(s.provisionalResources, callID)
	}
	s.mu.Unlock()
	if err != nil {
		_ = s.releaseObjects(context.Background(), provisional)
		_ = s.finalizeIfIdle()
		return nil, statusError(err)
	}
	if providerResult == nil {
		s.mu.Lock()
		delete(s.provisionalResources, callID)
		s.mu.Unlock()
		_ = s.releaseObjects(context.Background(), provisional)
		_ = s.finalizeIfIdle()
		return nil, StatusError{Code: CodeInternal, Message: "rpc provider returned a nil result"}
	}
	if err := validateValues(providerResult.Values, s.limits); err != nil {
		s.mu.Lock()
		delete(s.provisionalResources, callID)
		s.mu.Unlock()
		_ = providerResult.discard()
		_ = s.releaseObjects(context.Background(), provisional)
		_ = s.finalizeIfIdle()
		return nil, StatusError{Code: CodeInternal, Message: "rpc handler returned invalid values: " + err.Error()}
	}
	result := &Result{Values: cloneValues(providerResult.Values)}
	if len(provisional) != 0 {
		result.release = func() error {
			s.mu.Lock()
			if s.state >= routeSetCleaning {
				s.mu.Unlock()
				return nil
			}
			s.calls.Add(1)
			s.mu.Unlock()
			err := s.releaseObjects(context.Background(), provisional)
			s.calls.Done()
			return errors.Join(err, s.finalizeIfIdle())
		}
	}
	result.accept = func(accept bool) error {
		defer func() {
			s.mu.Lock()
			if s.pendingResults > 0 {
				s.pendingResults--
			}
			delete(s.pendingResultOwners, result)
			s.mu.Unlock()
			_ = s.finalizeIfIdle()
		}()
		if accept {
			if err := providerResult.accept(); err != nil {
				return errors.Join(err, s.finishResult(callID, provisional, false))
			}
		} else {
			return errors.Join(providerResult.discard(), s.finishResult(callID, provisional, false))
		}
		return s.finishResult(callID, provisional, accept)
	}
	s.mu.Lock()
	s.pendingResults++
	s.pendingResultOwners[result] = struct{}{}
	s.mu.Unlock()
	return result, nil
}
