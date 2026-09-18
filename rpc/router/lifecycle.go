package router

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/d7z-team/mini-go/rpc"
)

// Status returns a point-in-time Router snapshot.
func (g *Router) Status() Status {
	if g == nil {
		return Status{State: Closed}
	}
	g.mu.Lock()
	state := g.state
	registrations := make([]*Registration, 0, len(g.owners))
	for _, registration := range g.owners {
		registrations = append(registrations, registration)
	}
	g.mu.Unlock()
	status := Status{State: state, Registrations: make([]RegistrationStatus, len(registrations))}
	for index, registration := range registrations {
		status.Registrations[index] = registration.Status()
	}
	sort.Slice(status.Registrations, func(i, j int) bool { return status.Registrations[i].ID < status.Registrations[j].ID })
	return status
}

// ResolveContract returns one exact immutable bundle retained by the Router.
func (g *Router) ResolveContract(ctx context.Context, reference ContractReference) (MRPCBundle, error) {
	if g == nil {
		return MRPCBundle{}, rpc.StatusError{Code: rpc.CodeNotFound, Message: "RPC Router is unavailable"}
	}
	return g.contracts.ResolveContract(ctx, reference)
}

// ContractReferences returns the current publication manifest.
func (g *Router) ContractReferences() []ContractReference {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	references := g.contractReferencesLocked()
	g.mu.Unlock()
	return references
}

func (g *Router) contractReferencesLocked() []ContractReference {
	references := make([]ContractReference, 0, len(g.contractRefs))
	for reference := range g.contractRefs {
		references = append(references, reference)
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].ImportPath != references[j].ImportPath {
			return references[i].ImportPath < references[j].ImportPath
		}
		return references[i].Hash < references[j].Hash
	})
	return references
}

// Revision returns a consistent routing and contract manifest view.
func (g *Router) Revision() Revision {
	if g == nil {
		return Revision{}
	}
	g.mu.Lock()
	revision := Revision{RouterID: g.routerID, RouteEpoch: g.routeEpoch, References: g.contractReferencesLocked()}
	g.mu.Unlock()
	return revision
}

// WatchRevision waits until the supplied Router identity or route epoch is
// no longer current and returns the new immutable revision.
func (g *Router) WatchRevision(ctx context.Context, routerID string, routeEpoch uint64) (Revision, error) {
	if g == nil {
		return Revision{}, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC Router is unavailable"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		g.mu.Lock()
		if g.routerID != routerID || g.routeEpoch != routeEpoch || g.state != Open {
			revision := Revision{RouterID: g.routerID, RouteEpoch: g.routeEpoch, References: g.contractReferencesLocked()}
			state := g.state
			g.mu.Unlock()
			if state != Open {
				return revision, rpc.StatusError{Code: rpc.CodeUnavailable, Message: "RPC Router is shutting down"}
			}
			return revision, nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return Revision{}, ctx.Err()
		}
	}
}

// Shutdown rejects new work and waits for existing routes.
func (g *Router) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.beginShutdown()
	return g.waitForShutdown(ctx)
}

// BeginShutdown rejects new registrations and bindings without waiting for
// existing routes. The owner subsequently calls Shutdown or ForceShutdown.
func (g *Router) BeginShutdown() {
	if g != nil {
		g.beginShutdown()
	}
}

// ForceShutdown aborts all active routes and waits for cleanup.
func (g *Router) ForceShutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.beginShutdown()
	g.forceOnce.Do(func() {
		g.mu.Lock()
		registrations := append([]*Registration(nil), g.shutdownRegs...)
		g.mu.Unlock()
		for _, registration := range registrations {
			reservations, routes, leases, _, _ := registration.beginClose(true)
			runCloseActions(reservations, routes, leases)
		}
	})
	return g.waitForShutdown(ctx)
}

// removeOwner drops a registration once its provider and all leases have
// reached their terminal state. A retired registration remains in owners until
// then, even though it is removed from the selectable registration table.
func (g *Router) removeOwner(registration *Registration) {
	if g == nil || registration == nil {
		return
	}
	// Registration close state is protected by registration.mu. Read it before
	// taking Router.mu so owner completion cannot introduce a lock inversion.
	registration.mu.Lock()
	cleanupErr := registration.closeErr
	registration.mu.Unlock()
	g.mu.Lock()
	if current := g.owners[registration.id]; current == registration {
		delete(g.owners, registration.id)
		if cleanupErr != nil {
			if g.retiredCleanupErr == nil {
				g.retiredCleanupErr = cleanupErr
			}
			g.retiredCleanupCount++
		}
	}
	g.mu.Unlock()
}

func (g *Router) retiredCleanupErrorLocked() error {
	if g.retiredCleanupErr == nil {
		return nil
	}
	if g.retiredCleanupCount <= 1 {
		return g.retiredCleanupErr
	}
	return fmt.Errorf("RPC Router cleanup failed for %d registrations: %w", g.retiredCleanupCount, g.retiredCleanupErr)
}

func (g *Router) beginShutdown() {
	g.shutdownOnce.Do(func() {
		g.mu.Lock()
		g.state = ShuttingDown
		g.shutdownRegs = make([]*Registration, 0, len(g.owners))
		for _, registration := range g.owners {
			g.shutdownRegs = append(g.shutdownRegs, registration)
		}
		g.registrations = make(map[uint64]*Registration)
		g.contractRefs = make(map[ContractReference]int)
		g.changedLocked()
		g.mu.Unlock()
		for _, registration := range g.shutdownRegs {
			reservations, routes, leases, _, _ := registration.beginClose(false)
			runCloseActions(reservations, routes, leases)
		}
		registrations := append([]*Registration(nil), g.shutdownRegs...)
		go func() {
			var shutdownErr error
			for _, registration := range registrations {
				<-registration.closed
				g.removeOwner(registration)
			}
			g.mu.Lock()
			shutdownErr = errors.Join(shutdownErr, g.retiredCleanupErrorLocked())
			g.shutdownErr = shutdownErr
			g.state = Closed
			g.mu.Unlock()
			close(g.shutdownDone)
		}()
	})
}

func (g *Router) waitForShutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-g.shutdownDone:
		g.mu.Lock()
		err := g.shutdownErr
		g.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
