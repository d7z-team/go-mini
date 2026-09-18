package rpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

type resourceClose struct {
	done chan struct{}
	err  error
}

func (s *RouteSet) releaseResourceBorrows(scope *resourceBorrowScope, entries []*providerResource) {
	s.mu.Lock()
	scope.active = false
	for _, entry := range entries {
		if entry.borrowed == 0 {
			continue
		}
		entry.borrowed--
		if entry.borrowed == 0 {
			close(entry.borrowDone)
			entry.borrowDone = nil
		}
	}
	s.mu.Unlock()
}

func closeResource(ctx context.Context, resource Resource) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = StatusError{Code: CodeInternal, Message: fmt.Sprintf("rpc resource close panic: %v", recovered)}
		}
	}()
	return resource.Close(ctx)
}

func (s *RouteSet) disconnect() {
	if s == nil {
		return
	}
	cleanup, start := s.beginCleanup(true)
	if !start {
		return
	}
	// The transport has already revoked the remote binding. Local calls and
	// result decisions still have to settle before Shutdown reports completion.
	cleanup.remote = nil
	go func() { _ = s.finishCleanup(cleanup) }()
}

func (s *RouteSet) Resolve(ref ResourceRef) (Resource, error) {
	if err := validateResourceRef(ref); err != nil {
		return nil, StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.acceptsResourceCalls() || ref.Epoch != s.epoch {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "rpc resource belongs to another route set"}
	}
	entry := s.resources[ref.ObjectID]
	if entry == nil || !entry.active || entry.typeHash != ref.TypeHash || s.resourceCloses[ref.ObjectID] != nil {
		return nil, StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
	}
	return entry.resource, nil
}

func (s *RouteSet) Drop(ctx context.Context, ref ResourceRef) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateResourceRef(ref); err != nil {
		return StatusError{Code: CodeInvalidArgument, Message: err.Error()}
	}
	s.mu.Lock()
	if !s.state.acceptsResourceCalls() {
		s.mu.Unlock()
		return StatusError{Code: CodeUnavailable, Message: "rpc route set is closed"}
	}
	if ref.Epoch != s.epoch {
		s.mu.Unlock()
		return StatusError{Code: CodeInvalidArgument, Message: "rpc resource belongs to another route set"}
	}
	remote := s.remote
	var resource Resource
	if remote != nil {
		known, ok := s.remoteResources[ref.ObjectID]
		if !ok || known != ref {
			s.mu.Unlock()
			return StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
		}
	} else {
		entry := s.resources[ref.ObjectID]
		if entry == nil || !entry.active || entry.typeHash != ref.TypeHash {
			s.mu.Unlock()
			return StatusError{Code: CodeNotFound, Message: "rpc resource is closed"}
		}
		resource = entry.resource
		if scope, _ := ctx.Value(resourceBorrowScopeKey{}).(*resourceBorrowScope); scope != nil && scope.routes == s && scope.active {
			if _, borrowed := scope.resources[ref.ObjectID]; borrowed {
				s.mu.Unlock()
				return StatusError{Code: CodeFailedPrecondition, Message: "rpc resource cannot be closed from its active invocation"}
			}
		}
	}
	var borrowDone chan struct{}
	if remote == nil {
		borrowDone = s.resources[ref.ObjectID].borrowDone
	}
	if attempt := s.resourceCloses[ref.ObjectID]; attempt != nil {
		select {
		case <-attempt.done:
		default:
			s.mu.Unlock()
			select {
			case <-attempt.done:
				return attempt.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	attempt := &resourceClose{done: make(chan struct{})}
	if s.resourceCloses == nil {
		s.resourceCloses = make(map[uint64]*resourceClose)
	}
	s.resourceCloses[ref.ObjectID] = attempt
	s.calls.Add(1)
	s.mu.Unlock()
	cleanupContext := context.WithoutCancel(ctx)
	go func() {
		var err error
		if borrowDone != nil {
			<-borrowDone
		}
		if remote != nil {
			err = remote.drop(cleanupContext, ref)
		} else {
			err = closeResource(cleanupContext, resource)
		}
		s.mu.Lock()
		attempt.err = err
		if err == nil {
			if remote != nil {
				delete(s.remoteResources, ref.ObjectID)
				s.closeHandleLocked(ref.ObjectID)
			} else {
				s.detachResourceLocked(ref.ObjectID)
			}
			delete(s.resourceCloses, ref.ObjectID)
		}
		s.mu.Unlock()
		s.calls.Done()
		cleanupErr := s.finalizeIfIdle()
		s.mu.Lock()
		attempt.err = errors.Join(err, cleanupErr)
		close(attempt.done)
		s.mu.Unlock()
	}()
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RouteSet) finishResult(callID uint64, objects []uint64, accept bool) error {
	s.mu.Lock()
	if _, exists := s.provisionalResources[callID]; !exists {
		s.mu.Unlock()
		return nil
	}
	delete(s.provisionalResources, callID)
	if accept && s.state.acceptsResourceCalls() {
		for _, id := range objects {
			if entry := s.resources[id]; entry != nil {
				entry.active = true
				s.resourceOrder = append(s.resourceOrder, id)
			}
		}
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	return s.releaseObjects(context.Background(), objects)
}

// detachResourceLocked removes a resource after its cleanup attempt finishes.
func (s *RouteSet) detachResourceLocked(id uint64) {
	entry := s.resources[id]
	if entry == nil {
		return
	}
	delete(s.resources, id)
	s.closeHandleLocked(id)
	if index := slices.Index(s.resourceOrder, id); index >= 0 {
		s.resourceOrder = slices.Delete(s.resourceOrder, index, index+1)
	}
}

func (s *RouteSet) releaseObjects(ctx context.Context, ids []uint64) error {
	var cleanup []error
	for _, id := range ids {
		s.mu.Lock()
	waitClose:
		for previous := s.resourceCloses[id]; previous != nil; previous = s.resourceCloses[id] {
			select {
			case <-previous.done:
				break waitClose
			default:
				s.mu.Unlock()
				<-previous.done
				s.mu.Lock()
			}
		}
		entry := s.resources[id]
		attempt := &resourceClose{done: make(chan struct{})}
		var borrowDone chan struct{}
		if entry != nil {
			borrowDone = entry.borrowDone
			if s.resourceCloses == nil {
				s.resourceCloses = make(map[uint64]*resourceClose)
			}
			s.resourceCloses[id] = attempt
		}
		s.mu.Unlock()
		if entry != nil {
			if borrowDone != nil {
				<-borrowDone
			}
			err := closeResource(ctx, entry.resource)
			cleanup = append(cleanup, err)
			s.mu.Lock()
			attempt.err = err
			if err != nil && s.state < routeSetCleaning {
				if !slices.Contains(s.resourceOrder, id) {
					s.resourceOrder = append(s.resourceOrder, id)
				}
			} else {
				if handle := s.handles[id]; handle != nil {
					handle.closeErr = err
				}
				s.detachResourceLocked(id)
				delete(s.resourceCloses, id)
			}
			close(attempt.done)
			s.mu.Unlock()
		}
	}
	return errors.Join(cleanup...)
}

func (s *RouteSet) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.state >= routeSetDraining {
		s.mu.Unlock()
		return nil
	}
	s.state = routeSetDraining
	s.mu.Unlock()
	return s.finalizeIfIdle()
}

func (s *RouteSet) finalizeIfIdle() error {
	if s == nil {
		return nil
	}
	cleanup, start := s.beginCleanup(false)
	if !start {
		return nil
	}
	return s.finishCleanup(cleanup)
}

type routeSetCleanup struct {
	remote    *remoteBinding
	services  map[string]*serviceLease
	resources []uint64
	cancel    context.CancelFunc
}

func (s *RouteSet) beginCleanup(force bool) (routeSetCleanup, bool) {
	s.mu.Lock()
	if s.state >= routeSetCleaning || !force && (s.state != routeSetDraining || s.pendingCalls != 0 || s.pendingResults != 0 || len(s.resources) != 0 || len(s.remoteResources) != 0 || len(s.pendingRemoteResources) != 0) {
		s.mu.Unlock()
		return routeSetCleanup{}, false
	}
	s.state = routeSetCleaning
	cleanup := routeSetCleanup{remote: s.remote, services: s.services, cancel: s.cancelCalls}
	if force {
		cleanup.resources = append([]uint64(nil), s.resourceOrder...)
		for _, ids := range s.provisionalResources {
			cleanup.resources = append(cleanup.resources, ids...)
		}
	}
	s.cancelCalls = nil
	s.mu.Unlock()
	return cleanup, true
}

func (s *RouteSet) finishCleanup(cleanup routeSetCleanup) error {
	if cleanup.cancel != nil {
		cleanup.cancel()
	}
	s.calls.Wait()
	// Calls publish fully initialized result owners before releasing their
	// work reservation. No new result can appear after this barrier.
	s.mu.Lock()
	results := make([]*Result, 0, len(s.pendingResultOwners))
	for result := range s.pendingResultOwners {
		results = append(results, result)
	}
	s.mu.Unlock()
	var cleanupErr error
	for _, result := range results {
		cleanupErr = errors.Join(cleanupErr, result.finishForShutdown())
	}
	for left, right := 0, len(cleanup.resources)-1; left < right; left, right = left+1, right-1 {
		cleanup.resources[left], cleanup.resources[right] = cleanup.resources[right], cleanup.resources[left]
	}
	cleanupErr = errors.Join(cleanupErr, s.releaseObjects(context.Background(), cleanup.resources))
	if cleanup.remote != nil {
		cleanupErr = errors.Join(cleanupErr, cleanup.remote.close())
	}
	for _, lease := range cleanup.services {
		cleanupErr = errors.Join(cleanupErr, lease.Close())
	}
	s.mu.Lock()
	for id, handle := range s.handles {
		handle.closeErr = cleanupErr
		s.closeHandleLocked(id)
	}
	s.resourceCloses = nil
	s.pendingResultOwners = nil
	s.methods = nil
	s.resources = nil
	s.resourceOrder = nil
	s.remote = nil
	s.services = nil
	s.provisionalResources = nil
	s.remoteResources = nil
	s.pendingRemoteResources = nil
	s.cleanupErr = cleanupErr
	s.state = routeSetClosed
	close(s.cleanupDone)
	s.mu.Unlock()
	return cleanupErr
}

// Shutdown cancels active calls and releases every route resource and lease.
// The context limits only the caller's wait; cleanup continues to completion.
func (s *RouteSet) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanup, start := s.beginCleanup(true)
	if start {
		go func() { _ = s.finishCleanup(cleanup) }()
	}
	select {
	case <-s.cleanupDone:
		s.mu.Lock()
		err := s.cleanupErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Abort cancels active calls and waits until every route resource and lease is released.
func (s *RouteSet) Abort() error { return s.Shutdown(context.Background()) }

type routeExporter struct {
	routes *RouteSet
	callID uint64
}

func (e *routeExporter) Export(resource Resource, typeHash string) (Value, error) {
	if resource == nil || len(typeHash) != 64 {
		return Value{}, StatusError{Code: CodeInvalidArgument, Message: "invalid rpc resource export"}
	}
	s := e.routes
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.acceptsResourceCalls() {
		return Value{}, StatusError{Code: CodeUnavailable, Message: "rpc route set is closed"}
	}
	if len(s.resources) >= s.limits.MaxResources {
		return Value{}, StatusError{Code: CodeResourceExhausted, Message: "rpc resource limit exceeded"}
	}
	if s.nextResource == ^uint64(0) {
		return Value{}, StatusError{Code: CodeResourceExhausted, Message: "rpc resource id space exhausted"}
	}
	s.nextResource++
	id := s.nextResource
	s.resources[id] = &providerResource{resource: resource, typeHash: typeHash}
	s.provisionalResources[e.callID] = append(s.provisionalResources[e.callID], id)
	return Value{Type: typeHash, Resource: &ResourceRef{
		Epoch: s.epoch, ObjectID: id, TypeHash: typeHash,
	}}, nil
}
