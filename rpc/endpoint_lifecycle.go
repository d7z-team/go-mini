package rpc

import (
	"context"
	"errors"
	"io"
)

// Done closes after the stream, dispatches, bindings, results, and writer have stopped.
func (e *Endpoint) Done() <-chan struct{} {
	if e == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return e.shutdownDone
}

func (e *Endpoint) endpointError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state == endpointOpen {
		return nil
	}
	if e.closeErr == nil {
		return StatusError{Code: CodeUnavailable, Message: "rpc endpoint is closed"}
	}
	return StatusError{Code: CodeUnavailable, Message: e.closeErr.Error()}
}

func (e *Endpoint) fail(err error) {
	if e == nil {
		return
	}
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.state = endpointClosing
		close(e.stopping)
		if err == nil {
			err = io.ErrClosedPipe
		}
		e.closeErr = err
		if e.stopTransport != nil {
			e.stopTransport()
		}
		e.helloOnce.Do(func() { close(e.helloSent) })
		select {
		case <-e.handshake:
		default:
			close(e.handshake)
		}
		select {
		case <-e.ready:
		default:
			close(e.ready)
		}
		pending := e.pending
		active := make([]context.CancelFunc, 0, len(e.active))
		for _, request := range e.active {
			active = append(active, request.cancel)
		}
		inbound := e.inbound
		outbound := e.outbound
		results := e.results
		e.pending = nil
		e.active = nil
		e.pendingData = 0
		e.pendingControls = 0
		e.activeData = 0
		e.activeControls = 0
		e.inbound = nil
		e.inboundOperations = nil
		e.outbound = nil
		e.inboundLease = nil
		e.outboundLease = nil
		e.outboundOperations = nil
		e.results = nil
		e.mu.Unlock()
		if e.conn != nil {
			_ = e.conn.Close()
		}
		for _, request := range pending {
			select {
			case request.response <- endpointFrame{Code: CodeUnavailable, Message: err.Error()}:
			default:
			}
		}
		for _, cancel := range active {
			cancel()
		}
		go func() {
			var cleanupErr error
			for _, routes := range inbound {
				routes.disconnect()
			}
			for _, routes := range outbound {
				routes.disconnect()
			}
			for _, result := range results {
				cleanupErr = errors.Join(cleanupErr, result.result.finishForShutdown())
			}
			e.dispatchWG.Wait()
			// disconnect starts cleanup asynchronously so every binding is
			// revoked before the endpoint coordinator waits for its actual
			// shutdown. This keeps Done from racing a still-live RouteSet.
			for _, routes := range inbound {
				cleanupErr = errors.Join(cleanupErr, routes.Abort())
			}
			for _, routes := range outbound {
				cleanupErr = errors.Join(cleanupErr, routes.Abort())
			}
			<-e.writerDone
			<-e.readerDone
			e.mu.Lock()
			e.services = EndpointServices{}
			e.conn = nil
			e.peer = PeerInfo{}
			e.replyWrites = nil
			e.controlWrites = nil
			e.requestWrites = nil
			e.transportCtx = nil
			e.stopTransport = nil
			e.queuedWriteBytes = 0
			e.queuedControlBytes = 0
			e.inboundReservations = 0
			e.outboundReservations = 0
			e.resultReservations = 0
			e.state = endpointClosed
			e.shutdownErr = errors.Join(e.shutdownErr, cleanupErr)
			e.mu.Unlock()
			close(e.shutdownDone)
		}()
	})
}

// Close starts termination of both logical lanes. Cleanup continues in the
// endpoint coordinator; Shutdown waits for its completion when required.
func (e *Endpoint) Close() error {
	if e == nil {
		return nil
	}
	e.fail(io.ErrClosedPipe)
	e.mu.Lock()
	err := e.closeErr
	e.mu.Unlock()
	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}
	return err
}

// Wait waits for endpoint cleanup without initiating shutdown. It reports cleanup
// failures independently of the transport's reason for disconnecting.
func (e *Endpoint) Wait(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-e.shutdownDone:
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown starts endpoint termination and waits for all active dispatches and
// owned bindings to release. The context only limits the caller's wait.
func (e *Endpoint) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.fail(io.ErrClosedPipe)
	select {
	case <-e.shutdownDone:
		e.mu.Lock()
		err := e.shutdownErr
		closeErr := e.closeErr
		e.mu.Unlock()
		if !errors.Is(closeErr, io.ErrClosedPipe) {
			return errors.Join(closeErr, err)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
