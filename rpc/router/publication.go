package router

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/d7z-team/mini-go/rpc"
)

// PublicationState describes whether a provider group accepts new bindings.
type PublicationState string

const (
	PublicationActive   PublicationState = "active"
	PublicationDraining PublicationState = "draining"
	PublicationClosed   PublicationState = "closed"
)

// PublicationStatus is an immutable provider group snapshot.
type PublicationStatus struct {
	State         PublicationState
	Registrations []RegistrationStatus
}

// Publication atomically owns registrations and their current catalog references.
type Publication struct {
	mu            sync.Mutex
	router        *Router
	state         PublicationState
	registrations []*Registration
	references    []ContractReference
	releasedRefs  bool
}

type preparedProviderEntry struct {
	provider rpc.Provider
	methods  map[rpc.Method]struct{}
	options  RegistrationOptions
}

type preparedPublication struct {
	entries    []preparedProviderEntry
	bundles    []MRPCBundle
	references []ContractReference
	pathHashes map[string]string
}

type closeActions struct {
	reservations []*bindingReservation
	routes       []*rpc.RouteSet
	leases       []*registrationLease
	closed       <-chan struct{}
}

func (g *Router) commitPublication(current *Publication, prepared preparedPublication) (*Publication, error) {
	if current != nil {
		current.mu.Lock()
		defer current.mu.Unlock()
		if current.router != g || current.state != PublicationActive || current.releasedRefs {
			return nil, errors.New("RPC publication replacement target is not active")
		}
	}
	g.mu.Lock()
	if g.state != Open {
		g.mu.Unlock()
		return nil, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC Router is shutting down"}
	}
	if len(prepared.entries) > g.limits.MaxBindings-len(g.owners) {
		g.mu.Unlock()
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC registration owner limit exceeded"}
	}
	if uint64(len(prepared.entries)) > ^uint64(0)-g.nextID {
		g.mu.Unlock()
		return nil, rpc.StatusError{Code: rpc.CodeResourceExhausted, Message: "RPC registration ID space exhausted"}
	}
	replacedReferences := make(map[ContractReference]struct{})
	if current != nil {
		for _, reference := range current.references {
			replacedReferences[reference] = struct{}{}
		}
	}
	for reference := range g.contractRefs {
		count := g.contractRefs[reference]
		if _, replaced := replacedReferences[reference]; replaced {
			count--
		}
		if hash, exists := prepared.pathHashes[reference.ImportPath]; count > 0 && exists && hash != reference.Hash {
			g.mu.Unlock()
			return nil, errors.New("RPC import path already has a different active contract: " + reference.ImportPath)
		}
	}
	if err := g.contracts.Publish(prepared.bundles...); err != nil {
		g.mu.Unlock()
		return nil, err
	}
	registrations := make([]*Registration, len(prepared.entries))
	for index, entry := range prepared.entries {
		g.nextID++
		registration := newRegistration(g, g.nextID, entry.provider, entry.methods, entry.options, nil)
		registrations[index] = registration
	}
	var oldActions []closeActions
	releasedReferences := make(map[ContractReference]struct{})
	if current != nil {
		current.state = PublicationClosed
		current.releasedRefs = true
		oldActions = make([]closeActions, 0, len(current.registrations))
		for _, registration := range current.registrations {
			reservations, routes, leases, closed, _ := registration.beginClose(false)
			delete(g.registrations, registration.id)
			oldActions = append(oldActions, closeActions{reservations: reservations, routes: routes, leases: leases, closed: closed})
		}
		for _, reference := range current.references {
			if g.contractRefs[reference] <= 1 {
				delete(g.contractRefs, reference)
				releasedReferences[reference] = struct{}{}
			} else {
				g.contractRefs[reference]--
			}
		}
	}
	for _, registration := range registrations {
		g.registrations[registration.id] = registration
		g.owners[registration.id] = registration
	}
	for _, reference := range prepared.references {
		g.contractRefs[reference]++
		delete(releasedReferences, reference)
	}
	for reference := range releasedReferences {
		g.contracts.remove(reference)
	}
	g.changedLocked()
	g.mu.Unlock()
	for _, action := range oldActions {
		runCloseActions(action.reservations, action.routes, action.leases)
	}
	return &Publication{router: g, state: PublicationActive, registrations: registrations, references: prepared.references}, nil
}

// Status reports publication and registration state.
func (p *Publication) Status() PublicationStatus {
	if p == nil {
		return PublicationStatus{State: PublicationClosed}
	}
	p.mu.Lock()
	state := p.state
	registrations := append([]*Registration(nil), p.registrations...)
	p.mu.Unlock()
	status := PublicationStatus{State: state, Registrations: make([]RegistrationStatus, len(registrations))}
	for index, registration := range registrations {
		status.Registrations[index] = registration.Status()
	}
	sort.Slice(status.Registrations, func(i, j int) bool { return status.Registrations[i].ID < status.Registrations[j].ID })
	return status
}

// Drain stops new bindings and waits for active leases.
func (p *Publication) Drain(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.state == PublicationActive {
		p.state = PublicationDraining
	}
	registrations := append([]*Registration(nil), p.registrations...)
	p.mu.Unlock()
	var err error
	for _, registration := range registrations {
		err = errors.Join(err, registration.Drain(ctx))
	}
	return err
}

// Close removes the publication after active routes finish.
func (p *Publication) Close(ctx context.Context) error { return p.close(ctx, false) }

// ForceClose aborts active routes and removes the publication.
func (p *Publication) ForceClose(ctx context.Context) error { return p.close(ctx, true) }

func (p *Publication) close(ctx context.Context, force bool) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.state = PublicationClosed
	registrations := append([]*Registration(nil), p.registrations...)
	actions := make([]closeActions, 0, len(registrations))
	if !p.releasedRefs {
		p.releasedRefs = true
		if p.router != nil {
			p.router.mu.Lock()
			for _, registration := range registrations {
				reservations, routes, leases, closed, _ := registration.beginClose(force)
				delete(p.router.registrations, registration.id)
				actions = append(actions, closeActions{reservations: reservations, routes: routes, leases: leases, closed: closed})
			}
			for _, reference := range p.references {
				if p.router.contractRefs[reference] <= 1 {
					delete(p.router.contractRefs, reference)
					p.router.contracts.remove(reference)
				} else {
					p.router.contractRefs[reference]--
				}
			}
			p.router.changedLocked()
			p.router.mu.Unlock()
		}
	} else {
		for _, registration := range registrations {
			reservations, routes, leases, closed, _ := registration.beginClose(force)
			actions = append(actions, closeActions{reservations: reservations, routes: routes, leases: leases, closed: closed})
		}
	}
	p.mu.Unlock()
	var err error
	for _, action := range actions {
		runCloseActions(action.reservations, action.routes, action.leases)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for index, action := range actions {
		select {
		case <-action.closed:
			registrations[index].mu.Lock()
			err = errors.Join(err, registrations[index].closeErr)
			registrations[index].mu.Unlock()
			if p.router != nil {
				p.router.removeOwner(registrations[index])
			}
			p.router.removeOwner(registrations[index])
		case <-ctx.Done():
			err = errors.Join(err, ctx.Err())
			return err
		}
	}
	return err
}
