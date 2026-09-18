package rpc

import (
	"context"
	"sync/atomic"
)

// Resolve keeps a resource pinned for the duration of its active local
// invocation. A concurrent Close marks the entry as closing, but must not
// invalidate an already acquired borrow.
func (scope *resourceBorrowScope) Resolve(ref ResourceRef) (Resource, error) {
	if scope == nil || scope.routes == nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc call cannot resolve resources"}
	}
	if _, borrowed := scope.resources[ref.ObjectID]; !borrowed {
		return scope.routes.Resolve(ref)
	}
	routes := scope.routes
	if err := validateResourceRef(ref); err != nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	routes.mu.Lock()
	defer routes.mu.Unlock()
	if !scope.active {
		return nil, StatusError{Code: CodeFailedPrecondition, Message: "rpc resource invocation has finished"}
	}
	if ref.Epoch != routes.epoch {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource belongs to another route set"}
	}
	entry := routes.resources[ref.ObjectID]
	if entry == nil || !entry.active || entry.typeHash != ref.TypeHash {
		return nil, StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
	}
	return entry.resource, nil
}

// ResourceHandle shares the lifetime of one reference across generated wrappers.
// Its RouteSet owns the registry, which retains only live or provisional objects.
type ResourceHandle struct {
	routes   *RouteSet
	ref      ResourceRef
	closed   atomic.Bool
	closeErr error // protected by routes.mu
}

// BindResource validates a received reference and returns its shared handle.
// Provisional results may be bound before Accept; Discard invalidates the handle.
func (s *RouteSet) BindResource(ref ResourceRef) (*ResourceHandle, error) {
	if s == nil {
		return nil, StatusError{Code: CodeUnavailable, Message: "nil route set"}
	}
	if err := validateResourceRef(ref); err != nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.acceptsResourceCalls() || ref.Epoch != s.epoch {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "resource belongs to another binding or closed route set"}
	}
	valid := false
	if s.remote != nil {
		valid = s.remoteResources[ref.ObjectID] == ref || s.pendingRemoteResources[ref.ObjectID] == ref
	} else if entry := s.resources[ref.ObjectID]; entry != nil {
		valid = entry.typeHash == ref.TypeHash
	}
	if !valid {
		return nil, StatusError{Code: CodeNotFound, Message: "resource is closed"}
	}
	if s.resourceCloses[ref.ObjectID] != nil {
		return nil, StatusError{Code: CodeNotFound, Message: "resource is closing"}
	}
	if handle := s.handles[ref.ObjectID]; handle != nil {
		return handle, nil
	}
	handle := &ResourceHandle{routes: s, ref: ref}
	if s.handles == nil {
		s.handles = make(map[uint64]*ResourceHandle)
	}
	s.handles[ref.ObjectID] = handle
	return handle, nil
}

// RouteSet returns the binding that owns this handle.
func (h *ResourceHandle) RouteSet() *RouteSet {
	if h == nil {
		return nil
	}
	return h.routes
}

// Reference checks ownership and lifetime before passing a handle into a call.
func (h *ResourceHandle) Reference(ctx context.Context, owner *RouteSet) (Value, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Value{}, err
		}
	}
	if h == nil || owner == nil || h.routes != owner || h.closed.Load() {
		return Value{}, StatusError{Code: CodeInvalidArgument, Message: "resource is closed or belongs to another binding"}
	}
	owner.mu.Lock()
	closing := owner.resourceCloses[h.ref.ObjectID] != nil || !owner.state.acceptsResourceCalls()
	owner.mu.Unlock()
	if closing {
		return Value{}, StatusError{Code: CodeInvalidArgument, Message: "resource is closing"}
	}
	ref := h.ref
	return Value{Type: ref.TypeHash, Resource: &ref}, nil
}

// Close joins an ongoing release across wrappers. Failed releases may be retried;
// successful or final shutdown results are retained by the handle.
func (h *ResourceHandle) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.routes.mu.Lock()
	closed, err := h.closed.Load(), h.closeErr
	h.routes.mu.Unlock()
	if closed {
		return err
	}
	err = h.routes.Drop(ctx, h.ref)
	if code, _ := CodeOf(err); code == CodeNotFound {
		h.routes.mu.Lock()
		if h.closed.Load() {
			err = h.closeErr
		}
		h.routes.mu.Unlock()
	}
	return err
}

func (s *RouteSet) closeHandleLocked(id uint64) {
	if handle := s.handles[id]; handle != nil {
		handle.closed.Store(true)
		delete(s.handles, id)
	}
}
