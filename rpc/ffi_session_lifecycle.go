package rpc

import "context"

type ffiLeaseCleanup struct {
	done chan struct{}
	err  error
}

func (b *ffiSession) closeLease(ctx context.Context, id uint64) error {
	return b.closeBinding(ctx, id, false)
}

func (b *ffiSession) closeBinding(ctx context.Context, id uint64, provider bool) error {
	cleanup := b.beginBindingClose(id, provider)
	if cleanup == nil {
		return nil
	}
	select {
	case <-cleanup.done:
		return cleanup.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// beginBindingClose retains retiring ownership and starts cleanup exactly once.
// Callers can initiate all closes before waiting for dependent workers.
func (b *ffiSession) beginBindingClose(id uint64, provider bool) *ffiLeaseCleanup {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cleanup := b.retiring[id]; cleanup != nil {
		return cleanup
	}
	var releaseBinding func() error
	if provider {
		if p := b.providers[id]; p != nil {
			delete(b.providers, id)
			releaseBinding = p.close
		}
	} else if routes := b.leases[id]; routes != nil {
		delete(b.leases, id)
		var pending []ffiPendingResult
		for resultID, result := range b.results {
			if result.lease == id {
				pending = append(pending, result)
				delete(b.results, resultID)
				b.decidingResults++
			}
		}
		releaseBinding = func() error {
			var first error
			for _, result := range pending {
				if err := result.result.Discard(context.Background()); first == nil {
					first = err
				}
				b.mu.Lock()
				b.decidingResults--
				b.resultBytes -= result.bytes
				b.mu.Unlock()
			}
			if err := routes.Shutdown(context.Background()); first == nil {
				first = err
			}
			return first
		}
	}
	if releaseBinding == nil {
		return nil
	}
	cleanup := &ffiLeaseCleanup{done: make(chan struct{})}
	b.retiring[id] = cleanup
	b.cleanups.Add(1)
	go func() {
		defer b.cleanups.Done()
		err := releaseBinding()
		b.mu.Lock()
		cleanup.err = err
		if b.shutdownErr == nil {
			b.shutdownErr = err
		}
		delete(b.retiring, id)
		close(cleanup.done)
		b.mu.Unlock()
	}()
	return cleanup
}

// Callbacks enqueue cleanup without blocking or reentering the VM. Once closed,
// the session shutdown owns every remaining resource instead.
func (b *ffiSession) scheduleCleanup(cleanup func() error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.workers.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.workers.Done()
		err := cleanup()
		b.mu.Lock()
		if b.shutdownErr == nil {
			b.shutdownErr = err
		}
		b.mu.Unlock()
	}()
}

func (b *ffiSession) reserveLease() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return StatusError{Code: CodeUnavailable, Message: "MRPC FFI session is closed"}
	}
	if len(b.leases)+len(b.providers)+len(b.retiring)+b.reservations >= b.limits.MaxBindings {
		return StatusError{Code: CodeResourceExhausted, Message: "MRPC FFI lease limit exceeded"}
	}
	b.reservations++
	b.constructors.Add(1)
	return nil
}

func (b *ffiSession) releaseLeaseReservation() {
	b.mu.Lock()
	b.reservations--
	b.mu.Unlock()
	b.constructors.Done()
}

func (b *ffiSession) commitLease(routes *RouteSet, provider *ffiProvider) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reservations--
	if b.closed {
		return 0, StatusError{Code: CodeUnavailable, Message: "MRPC FFI session is closed"}
	}
	if b.nextLease == ^uint64(0) {
		return 0, StatusError{Code: CodeResourceExhausted, Message: "MRPC FFI lease space exhausted"}
	}
	b.nextLease++
	if routes != nil {
		b.leases[b.nextLease] = routes
	} else {
		b.providers[b.nextLease] = provider
	}
	return b.nextLease, nil
}

// Shutdown rejects new leases, cancels active work, and waits for all
// session-owned route and provider leases to release their resources. The
// context bounds the caller's wait; cleanup continues in the background.
func (b *ffiSession) Shutdown(ctx context.Context) error {
	if b == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.shutdownOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		leases := make([]uint64, 0, len(b.leases))
		for id := range b.leases {
			leases = append(leases, id)
		}
		providers := make([]uint64, 0, len(b.providers))
		for id := range b.providers {
			providers = append(providers, id)
		}
		b.mu.Unlock()
		b.cancelCalls()
		go func() {
			// Wake all bindings before waiting for in-flight FFI workers.
			for _, id := range leases {
				b.beginBindingClose(id, false)
			}
			for _, id := range providers {
				b.beginBindingClose(id, true)
			}
			b.constructors.Wait()
			b.workers.Wait()
			b.cleanups.Wait()
			b.mu.Lock()
			b.leases = nil
			b.providers = nil
			b.retiring = nil
			b.results = nil
			b.mu.Unlock()
			if b.onShutdown != nil {
				b.onShutdown(b)
			}
			close(b.shutdownDone)
		}()
	})
	select {
	case <-b.shutdownDone:
		b.mu.Lock()
		err := b.shutdownErr
		b.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *ffiSession) Close() error { return b.Shutdown(context.Background()) }
