package router

import (
	"context"
	"errors"
	"sync"

	"github.com/d7z-team/mini-go/rpc"
)

// RegistrationState describes provider selection availability.
type RegistrationState string

const (
	RegistrationActive   RegistrationState = "active"
	RegistrationDraining RegistrationState = "draining"
	RegistrationClosed   RegistrationState = "closed"
)

// RegistrationStatus is an immutable provider snapshot.
type RegistrationStatus struct {
	ID            uint64
	Name          string
	State         RegistrationState
	Healthy       bool
	PendingBinds  int
	ActiveLeases  int
	MaximumLeases int
}

// Registration owns provider selection and lease accounting.
type Registration struct {
	mu                sync.Mutex
	router            *Router
	id                uint64
	provider          rpc.Provider
	methods           map[rpc.Method]struct{}
	options           RegistrationOptions
	state             RegistrationState
	healthy           bool
	pending           map[*bindingReservation]struct{}
	leases            map[*registrationLease]struct{}
	ownedClose        func() error
	ownedCloseRunning bool
	forceStarted      bool
	closeErr          error
	ownerRemoved      bool
	drained           chan struct{}
	closed            chan struct{}
	drainOnce         sync.Once
	closeOnce         sync.Once
}

func newRegistration(router *Router, id uint64, provider rpc.Provider, methods map[rpc.Method]struct{}, options RegistrationOptions, ownedClose func() error) *Registration {
	return &Registration{
		router: router, id: id, provider: provider, methods: methods, options: options,
		state: RegistrationActive, healthy: true, pending: make(map[*bindingReservation]struct{}),
		leases: make(map[*registrationLease]struct{}), ownedClose: ownedClose,
		drained: make(chan struct{}), closed: make(chan struct{}),
	}
}

// ID returns the stable process-local registration ID.
func (r *Registration) ID() uint64 {
	if r == nil {
		return 0
	}
	return r.id
}

// Status reports registration state without retaining live objects.
func (r *Registration) Status() RegistrationStatus {
	if r == nil {
		return RegistrationStatus{State: RegistrationClosed}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return RegistrationStatus{
		ID: r.id, Name: r.options.Name, State: r.state, Healthy: r.healthy,
		PendingBinds: len(r.pending), ActiveLeases: len(r.leases), MaximumLeases: r.options.MaxLeases,
	}
}

// SetHealthy controls whether new bindings may select this provider.
func (r *Registration) SetHealthy(healthy bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	changed := r.state != RegistrationClosed && r.healthy != healthy
	if r.state != RegistrationClosed {
		r.healthy = healthy
	}
	r.mu.Unlock()
	if changed && r.router != nil {
		r.router.mu.Lock()
		r.router.changedLocked()
		r.router.mu.Unlock()
	}
}

type bindingReservation struct {
	registration *Registration
	ctx          context.Context
	cancel       context.CancelFunc
}

func (r *Registration) reserve(parent context.Context, labels map[string]string) (*bindingReservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != RegistrationActive || !r.healthy || !labelsMatch(r.options.Labels, labels) {
		return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC provider is no longer selectable"}
	}
	if r.options.MaxLeases > 0 && len(r.pending)+len(r.leases) >= r.options.MaxLeases {
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC provider lease limit exceeded"}
	}
	ctx, cancel := context.WithCancel(parent)
	reservation := &bindingReservation{registration: r, ctx: ctx, cancel: cancel}
	r.pending[reservation] = struct{}{}
	return reservation, nil
}

func (r *bindingReservation) release() {
	if r == nil || r.registration == nil {
		return
	}
	registration := r.registration
	registration.mu.Lock()
	if _, ok := registration.pending[r]; ok {
		delete(registration.pending, r)
		r.cancel()
		registration.finishIdleLocked()
	}
	registration.mu.Unlock()
}

func (r *bindingReservation) commit(provider rpc.ProviderLease) *registrationLease {
	if r == nil || r.registration == nil || provider == nil {
		return nil
	}
	registration := r.registration
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if _, ok := registration.pending[r]; !ok {
		return nil
	}
	if registration.state != RegistrationActive {
		return nil
	}
	delete(registration.pending, r)
	r.cancel()
	lease := &registrationLease{registration: registration, provider: provider}
	registration.leases[lease] = struct{}{}
	return lease
}

// Drain stops selection and waits for current leases.
func (r *Registration) Drain(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	changed := false
	if r.state == RegistrationActive {
		r.state = RegistrationDraining
		changed = true
	}
	r.finishIdleLocked()
	drained := r.drained
	r.mu.Unlock()
	if changed && r.router != nil {
		r.router.mu.Lock()
		r.router.changedLocked()
		r.router.mu.Unlock()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close removes the registration after existing routes finish.
func (r *Registration) Close(ctx context.Context) error { return r.close(ctx, false) }

// ForceClose aborts active routes and removes the registration.
func (r *Registration) ForceClose(ctx context.Context) error { return r.close(ctx, true) }

func (r *Registration) close(ctx context.Context, force bool) error {
	if r == nil {
		return nil
	}
	reservations, routes, leases, closed, first := r.beginClose(force)
	if first && r.router != nil {
		r.router.mu.Lock()
		delete(r.router.registrations, r.id)
		r.router.changedLocked()
		r.router.mu.Unlock()
	}
	runCloseActions(reservations, routes, leases)
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-closed:
		r.mu.Lock()
		err := r.closeErr
		r.mu.Unlock()
		if r.router != nil {
			r.router.removeOwner(r)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Registration) beginClose(force bool) ([]*bindingReservation, []*rpc.RouteSet, []*registrationLease, <-chan struct{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	first := r.state != RegistrationClosed
	r.state = RegistrationClosed
	reservations := make([]*bindingReservation, 0, len(r.pending))
	for reservation := range r.pending {
		reservations = append(reservations, reservation)
	}
	var routes []*rpc.RouteSet
	var leases []*registrationLease
	if force && !r.forceStarted {
		r.forceStarted = true
		seen := make(map[*rpc.RouteSet]struct{})
		for lease := range r.leases {
			if lease.routes == nil {
				leases = append(leases, lease)
			} else if _, ok := seen[lease.routes]; !ok {
				seen[lease.routes] = struct{}{}
				routes = append(routes, lease.routes)
			}
		}
	}
	r.finishIdleLocked()
	return reservations, routes, leases, r.closed, first
}

func (r *Registration) finishIdleLocked() {
	if len(r.pending) == 0 && len(r.leases) == 0 {
		r.drainOnce.Do(func() { close(r.drained) })
	}
	if r.state != RegistrationClosed || len(r.pending) != 0 || len(r.leases) != 0 || r.ownedCloseRunning {
		return
	}
	if r.ownedClose != nil {
		closeFn := r.ownedClose
		r.ownedClose = nil
		r.ownedCloseRunning = true
		go func() {
			err := closeFn()
			r.mu.Lock()
			r.closeErr = errors.Join(r.closeErr, err)
			r.ownedCloseRunning = false
			r.markClosedLocked()
			r.mu.Unlock()
		}()
		return
	}
	r.markClosedLocked()
}

func (r *Registration) recordCloseError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	if r.closeErr == nil {
		r.closeErr = err
	}
	r.mu.Unlock()
}

func (r *Registration) markClosedLocked() {
	r.closeOnce.Do(func() {
		close(r.closed)
		if r.router != nil && !r.ownerRemoved {
			r.ownerRemoved = true
			go r.router.removeOwner(r)
		}
	})
}

type registrationLease struct {
	once         sync.Once
	mu           sync.Mutex
	registration *Registration
	provider     rpc.ProviderLease
	routes       *rpc.RouteSet
	err          error
}

func (l *registrationLease) attach(routes *rpc.RouteSet) bool {
	l.registration.mu.Lock()
	defer l.registration.mu.Unlock()
	if l.registration.state != RegistrationActive {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.provider == nil || l.routes != nil {
		return false
	}
	l.routes = routes
	return true
}

func (l *registrationLease) Invoke(ctx context.Context, method rpc.Method, arguments []rpc.Value) (*rpc.ProviderResult, error) {
	l.mu.Lock()
	provider := l.provider
	l.mu.Unlock()
	if provider == nil {
		return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC provider lease is closed"}
	}
	return provider.Invoke(ctx, method, arguments)
}

func (l *registrationLease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		l.mu.Lock()
		provider := l.provider
		l.provider = nil
		registration := l.registration
		l.mu.Unlock()
		if provider != nil {
			l.err = provider.Close()
		}
		if registration != nil {
			registration.mu.Lock()
			if l.err != nil && registration.closeErr == nil {
				registration.closeErr = l.err
			}
			delete(registration.leases, l)
			registration.finishIdleLocked()
			registration.mu.Unlock()
		}
	})
	return l.err
}

func runCloseActions(reservations []*bindingReservation, routes []*rpc.RouteSet, leases []*registrationLease) {
	for _, reservation := range reservations {
		reservation.cancel()
	}
	for _, routes := range routes {
		go func() { _ = routes.Abort() }()
	}
	for _, lease := range leases {
		go func() { _ = lease.Close() }()
	}
}
