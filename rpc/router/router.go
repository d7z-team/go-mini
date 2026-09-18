// Package router provides an optional in-process RPC registry and router.
package router

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/d7z-team/mini-go/rpc"
)

// State describes the Router lifecycle.
type State string

const (
	Open         State = "open"
	ShuttingDown State = "shutting_down"
	Closed       State = "closed"
)

// RegistrationOptions controls selection and capacity for a provider.
type RegistrationOptions struct {
	Name      string
	Priority  int
	Weight    int
	MaxLeases int
	Labels    map[string]string
}

// Options configures routing limits and bind authorization.
type Options struct {
	Limits     rpc.Limits
	Authorizer func(context.Context, rpc.PeerInfo, rpc.Contract) error
}

// Status is an immutable Router state snapshot.
type Status struct {
	State         State
	Registrations []RegistrationStatus
}

// Revision identifies one immutable view of the current routing and contract
// manifest. RouterID changes when a Router process is recreated.
type Revision struct {
	RouterID   string
	RouteEpoch uint64
	References []ContractReference
}

// Router owns provider registrations and routes new exact bindings.
type Router struct {
	mu            sync.Mutex
	limits        rpc.Limits
	authorizer    func(context.Context, rpc.PeerInfo, rpc.Contract) error
	contracts     *ContractRepository
	state         State
	nextID        uint64
	registrations map[uint64]*Registration
	// owners tracks every registration whose provider or leases still need
	// cleanup. registrations only contains providers eligible for new binds.
	owners       map[uint64]*Registration
	roundRobin   map[string]uint64
	contractRefs map[ContractReference]int
	routerID     string
	routeEpoch   uint64
	changed      chan struct{}
	shutdownRegs []*Registration
	shutdownDone chan struct{}
	shutdownErr  error
	// retiredCleanupErr retains a bounded summary after a registration leaves
	// owners. The first error is preserved for errors.Is/errors.As callers;
	// retiredCleanupCount records additional failed registrations.
	retiredCleanupErr   error
	retiredCleanupCount uint64
	shutdownOnce        sync.Once
	forceOnce           sync.Once
}

// New constructs an empty Router.
func New(options Options) *Router {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		fallback := routerIdentityFallback.Add(1)
		return newRouter(options, fmt.Sprintf("%x-%x", time.Now().UnixNano(), fallback))
	}
	return newRouter(options, hex.EncodeToString(identity[:]))
}

var routerIdentityFallback atomic.Uint64

func newRouter(options Options, routerID string) *Router {
	return &Router{
		limits: rpc.NormalizeLimits(options.Limits), authorizer: options.Authorizer,
		contracts: NewContractRepository(), state: Open,
		registrations: make(map[uint64]*Registration), owners: make(map[uint64]*Registration),
		roundRobin:   make(map[string]uint64),
		contractRefs: make(map[ContractReference]int), routerID: routerID, changed: make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}
}

// Register exposes a provider until the returned Registration closes.
func (g *Router) Register(provider rpc.Provider, options RegistrationOptions) (*Registration, error) {
	return g.register(provider, options, nil)
}

// RegisterOwned also closes the provider after its final lease is released.
func (g *Router) RegisterOwned(provider rpc.Provider, options RegistrationOptions, closeProvider func() error) (*Registration, error) {
	if closeProvider == nil {
		return nil, errors.New("owned RPC provider requires a close function")
	}
	return g.register(provider, options, closeProvider)
}

func (g *Router) register(provider rpc.Provider, options RegistrationOptions, ownedClose func() error) (*Registration, error) {
	if g == nil || provider == nil {
		return nil, errors.New("RPC Router and provider are required")
	}
	contract, err := rpc.NormalizeContract(provider.RPCContract(), g.limits)
	if err != nil {
		return nil, err
	}
	methods := make(map[rpc.Method]struct{}, len(contract.Methods))
	for _, method := range contract.Methods {
		methods[method] = struct{}{}
	}
	options = normalizeRegistrationOptions(options)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != Open {
		return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC Router is shutting down"}
	}
	if len(g.owners) >= g.limits.MaxBindings {
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC registration owner limit exceeded"}
	}
	if g.nextID == ^uint64(0) {
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC registration ID space exhausted"}
	}
	g.nextID++
	registration := newRegistration(g, g.nextID, provider, methods, options, ownedClose)
	g.registrations[registration.id] = registration
	g.owners[registration.id] = registration
	g.changedLocked()
	return registration, nil
}

func (g *Router) changedLocked() {
	if len(g.roundRobin) != 0 {
		services := make(map[string]bool)
		for _, registration := range g.registrations {
			for method := range registration.methods {
				services[method.Service] = true
			}
		}
		for service := range g.roundRobin {
			if !services[service] {
				delete(g.roundRobin, service)
			}
		}
	}
	g.routeEpoch++
	close(g.changed)
	g.changed = make(chan struct{})
}

// Bind selects one registration per requested service and atomically creates a RouteSet.
func (g *Router) Bind(ctx context.Context, request rpc.BindRequest) (*rpc.RouteSet, error) {
	if g == nil {
		return nil, errors.New("RPC Router is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, asStatus(err)
	}
	contract, err := rpc.NormalizeContract(request.Contract, g.limits)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	open := g.state == Open
	g.mu.Unlock()
	if !open {
		return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC Router is shutting down"}
	}
	if g.authorizer != nil {
		if err := g.authorizer(ctx, clonePeer(request.Peer), contract); err != nil {
			return nil, asStatus(err)
		}
	}
	if request.Hops == 0 {
		request.Hops = 16
	}
	if request.Hops < 0 {
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC Router hop limit exceeded"}
	}
	request.Options.Labels = cloneLabels(request.Options.Labels)
	groups := make(map[string][]rpc.Method)
	for _, method := range contract.Methods {
		if method.ResourceTypeHash == "" {
			groups[method.Service] = append(groups[method.Service], method)
		}
	}
	services := make([]string, 0, len(groups))
	for service := range groups {
		services = append(services, service)
	}
	sort.Strings(services)
	selected := make(map[string]*registrationLease, len(services))
	rollback := func() {
		for _, service := range services {
			if lease := selected[service]; lease != nil {
				_ = lease.Close()
			}
		}
	}
	for _, service := range services {
		var bindErr error
		selectionMethods := groups[service]
		if len(services) == 1 {
			selectionMethods = contract.Methods
		}
		candidates, selectionErr := g.candidates(service, selectionMethods, request.Options)
		for _, candidate := range candidates {
			reservation, reserveErr := candidate.reserve(ctx, request.Options.Labels)
			if reserveErr != nil {
				bindErr = preferBindingError(bindErr, reserveErr)
				continue
			}
			lease, providerErr := candidate.provider.BindRPC(reservation.ctx, rpc.BindRequest{
				Contract: rpc.Contract{Protocol: rpc.ContractProtocol, Methods: groups[service]},
				Options:  request.Options, Peer: clonePeer(request.Peer), Provider: candidate.options.Name, Hops: request.Hops - 1,
			})
			if providerErr != nil {
				if lease != nil {
					candidate.recordCloseError(lease.Close())
				}
				bindErr = preferBindingError(bindErr, asStatus(providerErr))
				if code, _ := rpc.CodeOf(providerErr); code == rpc.CodeProtocol {
					candidate.SetHealthy(false)
				}
				reservation.release()
				continue
			}
			if lease == nil {
				bindErr = preferBindingError(bindErr, rpc.StatusError{Code: rpc.CodeInternal, Message: "RPC provider returned a nil lease"})
				reservation.release()
				continue
			}
			if err := ctx.Err(); err != nil {
				candidate.recordCloseError(lease.Close())
				reservation.release()
				rollback()
				return nil, asStatus(err)
			}
			bound := reservation.commit(lease)
			if bound == nil {
				candidate.recordCloseError(lease.Close())
				reservation.release()
				bindErr = preferBindingError(bindErr, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC provider stopped while binding"})
				continue
			}
			selected[service] = bound
			break
		}
		if selected[service] == nil {
			rollback()
			if err := ctx.Err(); err != nil {
				return nil, asStatus(err)
			}
			return nil, preferBindingError(bindErr, selectionErr)
		}
	}
	var available, resources []rpc.Method
	for _, lease := range selected {
		for method := range lease.registration.methods {
			available = append(available, method)
		}
	}
	for _, method := range contract.Methods {
		if method.ResourceTypeHash == "" {
			continue
		}
		resources = append(resources, method)
	}
	if err := rpc.CheckMethodSupport(available, resources); err != nil {
		rollback()
		return nil, err
	}
	providerLeases := make(map[string]rpc.ProviderLease, len(selected))
	for service, lease := range selected {
		providerLeases[service] = lease
	}
	routes, err := rpc.NewRouteSet(g.limits, contract, providerLeases)
	if err != nil {
		rollback()
		return nil, err
	}
	for _, lease := range selected {
		if !lease.attach(routes) {
			_ = routes.Abort()
			return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC provider stopped while binding"}
		}
	}
	return routes, nil
}

func (g *Router) candidates(service string, methods []rpc.Method, options rpc.BindOptions) ([]*Registration, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	candidates := make([]*Registration, 0, len(g.registrations))
	var rejected error = rpc.StatusError{Code: rpc.CodeUnimplemented, Message: "RPC service is not implemented: " + service}
	for _, registration := range g.registrations {
		declared := make([]rpc.Method, 0, len(registration.methods))
		for method := range registration.methods {
			declared = append(declared, method)
		}
		if err := rpc.CheckMethodSupport(declared, methods); err != nil {
			rejected = preferBindingError(rejected, err)
			continue
		}
		registration.mu.Lock()
		selectable := registration.state == RegistrationActive && registration.healthy && labelsMatch(registration.options.Labels, options.Labels)
		registration.mu.Unlock()
		if !selectable {
			rejected = preferBindingError(rejected, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC service has no selectable provider: " + service})
			continue
		}
		candidates = append(candidates, registration)
	}
	if options.AffinityKey != "" {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].options.Priority != candidates[j].options.Priority {
				return candidates[i].options.Priority > candidates[j].options.Priority
			}
			return affinityScore(options.AffinityKey, candidates[i].id) > affinityScore(options.AffinityKey, candidates[j].id)
		})
		return candidates, rejected
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].options.Priority != candidates[j].options.Priority {
			return candidates[i].options.Priority > candidates[j].options.Priority
		}
		return candidates[i].id < candidates[j].id
	})
	for start := 0; start < len(candidates); {
		end, weight := start+1, candidates[start].options.Weight
		for end < len(candidates) && candidates[end].options.Priority == candidates[start].options.Priority {
			weight += candidates[end].options.Weight
			end++
		}
		position := int(g.roundRobin[service] % uint64(weight))
		g.roundRobin[service]++
		chosen := start
		for index := start; index < end; index++ {
			if position < candidates[index].options.Weight {
				chosen = index
				break
			}
			position -= candidates[index].options.Weight
		}
		if chosen != start {
			candidate := candidates[chosen]
			copy(candidates[start+1:chosen+1], candidates[start:chosen])
			candidates[start] = candidate
		}
		start = end
	}
	return candidates, rejected
}

// Selection failures prefer a real operational error over absent declarations.
// Stable ties avoid exposing map iteration or provider scheduling as status.
func preferBindingError(current, next error) error {
	var selected error
	priority := -1
	message := ""
	for _, err := range []error{current, next} {
		if err == nil {
			continue
		}
		code, text := rpc.CodeOf(err)
		rank := 4
		switch code {
		case rpc.CodeUnimplemented:
			rank = 0
		case rpc.CodeFailedPrecondition:
			rank = 1
		case rpc.CodeUnavailable:
			rank = 2
		case rpc.CodeResourceExhausted:
			rank = 3
		}
		key := string(code) + ":" + text
		if rank > priority || rank == priority && key < message {
			selected, priority, message = err, rank, key
		}
	}
	return selected
}

func affinityScore(key string, id uint64) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	var encoded [8]byte
	for index := range encoded {
		encoded[index] = byte(id >> (index * 8))
	}
	_, _ = hash.Write(encoded[:])
	return hash.Sum64()
}

func normalizeRegistrationOptions(options RegistrationOptions) RegistrationOptions {
	if options.Weight <= 0 {
		options.Weight = 1
	}
	options.Labels = cloneLabels(options.Labels)
	return options
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func clonePeer(peer rpc.PeerInfo) rpc.PeerInfo {
	peer.Attributes = cloneLabels(peer.Attributes)
	return peer
}

func labelsMatch(provider, request map[string]string) bool {
	for key, value := range request {
		if actual, exists := provider[key]; !exists || actual != value {
			return false
		}
	}
	return true
}

func asStatus(err error) error {
	if err == nil {
		return nil
	}
	if code, message := rpc.CodeOf(err); code != "" {
		return rpc.StatusError{Code: code, Message: message}
	}
	return rpc.StatusError{Code: rpc.CodeInternal, Message: err.Error()}
}

func duplicateProviderName(entries []ProviderEntry) error {
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Options.Name == "" {
			continue
		}
		if _, duplicate := names[entry.Options.Name]; duplicate {
			return fmt.Errorf("RPC publication contains duplicate provider name %q", entry.Options.Name)
		}
		names[entry.Options.Name] = struct{}{}
	}
	return nil
}
