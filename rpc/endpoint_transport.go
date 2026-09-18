package rpc

import (
	"context"
	"fmt"
	"io"
	"time"
)

func (e *Endpoint) request(ctx context.Context, frame endpointFrame) (endpointFrame, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	handshakeTimer := time.NewTimer(e.options.LeaseTTL)
	defer handshakeTimer.Stop()
	select {
	case <-e.helloSent:
		if err := e.endpointError(); err != nil {
			return endpointFrame{}, err
		}
	case <-ctx.Done():
		if err := e.endpointError(); err != nil {
			return endpointFrame{}, err
		}
		return endpointFrame{}, statusError(ctx.Err())
	case <-handshakeTimer.C:
		return endpointFrame{}, StatusError{Code: CodeUnavailable, Message: "rpc handshake stalled"}
	}
	frame.writeDone = make(chan error, 1)
	frame, response, err := e.enqueueRequest(ctx, frame)
	if err != nil {
		if endpointErr := e.endpointError(); endpointErr != nil {
			return endpointFrame{}, endpointErr
		}
		return endpointFrame{}, err
	}
	select {
	case err := <-frame.writeDone:
		if err != nil {
			e.mu.Lock()
			e.removePendingLocked(frame.ID)
			delete(e.outboundOperations, frame.ID)
			e.mu.Unlock()
			if ctx.Err() != nil {
				return endpointFrame{}, statusError(ctx.Err())
			}
			return endpointFrame{}, StatusError{Code: CodeUnavailable, Message: err.Error()}
		}
	case <-ctx.Done():
		return e.cancelRequest(ctx.Err(), frame, response, false)
	case <-e.stopping:
		return endpointFrame{}, e.endpointError()
	}
	admission := time.NewTimer(e.options.AdmissionTimeout)
	defer admission.Stop()
	select {
	case reply := <-response:
		admission.Stop()
		for reply.Kind == "accepted" {
			select {
			case reply = <-response:
			case <-ctx.Done():
				return e.cancelRequest(ctx.Err(), frame, response, true)
			}
		}
		e.mu.Lock()
		e.removePendingLocked(frame.ID)
		if frame.Kind != "call" || reply.Kind != "offer" || reply.Code != "" {
			delete(e.outboundOperations, frame.ID)
		}
		e.mu.Unlock()
		if reply.Code != "" {
			if frame.Kind == "call" {
				e.mu.Lock()
				delete(e.outboundOperations, frame.ID)
				e.mu.Unlock()
			}
			return endpointFrame{}, StatusError{Code: reply.Code, Message: reply.Message}
		}
		validKind := reply.Kind == frame.Kind
		switch frame.Kind {
		case "call":
			validKind = reply.Kind == "offer" || reply.Kind == "done"
		case "renew":
			validKind = reply.Kind == "renew_ack"
		case "bind", "decision", "drop", "close":
			validKind = reply.Kind == "done"
		}
		if !validKind {
			err := StatusError{Code: CodeProtocol, Message: fmt.Sprintf("rpc response kind %q does not match request %q", reply.Kind, frame.Kind)}
			if e.stopTransport != nil {
				e.fail(err)
			}
			return endpointFrame{}, err
		}
		if frame.Kind == "call" && reply.Kind == "done" {
			e.mu.Lock()
			delete(e.outboundOperations, frame.ID)
			e.mu.Unlock()
		}
		return reply, nil
	case <-ctx.Done():
		return e.cancelRequest(ctx.Err(), frame, response, true)
	case <-admission.C:
		return e.cancelRequest(StatusError{Code: CodeUnavailable, Message: "rpc request admission was not confirmed"}, frame, response, true)
	}
}

func (e *Endpoint) cancelRequest(reason error, frame endpointFrame, response <-chan endpointFrame, sent bool) (endpointFrame, error) {
	e.mu.Lock()
	delete(e.outboundOperations, frame.ID)
	pending, exists := e.pending[frame.ID]
	if exists {
		pending.abandoned = true
		e.pending[frame.ID] = pending
	}
	owned := exists && e.state == endpointOpen
	if owned {
		e.dispatchWG.Add(1)
	}
	e.mu.Unlock()
	if owned {
		go func() {
			defer e.dispatchWG.Done()
			defer func() { e.mu.Lock(); e.removePendingLocked(frame.ID); e.mu.Unlock() }()
			timer := time.NewTimer(e.options.LeaseTTL)
			defer timer.Stop()
			if !sent {
				select {
				case err := <-frame.writeDone:
					if err != nil {
						return
					}
				case <-timer.C:
					return
				case <-e.stopping:
					return
				}
			}
			controlCtx, cancel := context.WithTimeout(context.Background(), e.options.LeaseTTL)
			defer cancel()
			written := make(chan error, 1)
			if err := e.writeContext(controlCtx, endpointFrame{Kind: "cancel", Origin: e.origin, TargetID: frame.ID, writeDone: written}); err != nil {
				e.fail(err)
				return
			}
			select {
			case err := <-written:
				if err != nil {
					e.fail(err)
					return
				}
			case <-controlCtx.Done():
				e.fail(controlCtx.Err())
				return
			case <-e.stopping:
				return
			}
			for {
				select {
				case reply := <-response:
					if reply.Kind == "accepted" {
						continue
					}
					e.mu.Lock()
					e.removePendingLocked(frame.ID)
					e.mu.Unlock()
					e.discardReply(reply)
					return
				case <-timer.C:
					return
				case <-e.stopping:
					return
				}
			}
		}()
	}
	return endpointFrame{}, statusError(reason)
}

// Allocate IDs and enqueue requests in the same order. Waiting for queue space
// must not hold the endpoint state lock or prevent caller cancellation.
func (e *Endpoint) enqueueRequest(ctx context.Context, frame endpointFrame) (endpointFrame, <-chan endpointFrame, error) {
	var stalled <-chan time.Time
	if e.options.LeaseTTL > 0 {
		timer := time.NewTimer(e.options.LeaseTTL)
		defer timer.Stop()
		stalled = timer.C
	}
	select {
	case e.requestGate <- struct{}{}:
		defer func() { <-e.requestGate }()
	case <-ctx.Done():
		return endpointFrame{}, nil, statusError(ctx.Err())
	case <-e.stopping:
		return endpointFrame{}, nil, e.endpointError()
	case <-stalled:
		return endpointFrame{}, nil, StatusError{Code: CodeUnavailable, Message: "rpc request queue stalled before send"}
	}
	if err := ctx.Err(); err != nil {
		return endpointFrame{}, nil, statusError(err)
	}
	e.mu.Lock()
	if e.state != endpointOpen {
		err := e.closeErr
		e.mu.Unlock()
		if err == nil {
			err = io.ErrClosedPipe
		}
		return endpointFrame{}, nil, StatusError{Code: CodeUnavailable, Message: err.Error()}
	}
	control := endpointControlFrame(frame.Kind)
	if frame.Kind == "call" {
		expires, valid := e.outboundLease[frame.Binding]
		if e.outboundLease != nil && (!valid || !time.Now().Before(expires)) {
			e.mu.Unlock()
			return endpointFrame{}, nil, StatusError{Code: CodeUnavailable, Message: "rpc binding lease expired"}
		}
	}
	if control && e.pendingControls >= e.outboundLimits.MaxPendingControls {
		e.mu.Unlock()
		return endpointFrame{}, nil, StatusError{Code: CodeResourceExhausted, Message: "rpc endpoint pending control limit exceeded"}
	}
	if !control && e.pendingData >= e.outboundLimits.MaxPendingCalls {
		e.mu.Unlock()
		return endpointFrame{}, nil, StatusError{Code: CodeResourceExhausted, Message: "rpc endpoint pending call limit exceeded"}
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			e.mu.Unlock()
			return endpointFrame{}, nil, statusError(context.DeadlineExceeded)
		}
		frame.Timeout = remaining.Nanoseconds()
		if frame.Timeout == 0 {
			frame.Timeout = 1
		}
	}
	if frame.Kind == "decision" {
		// DECIDE is the second phase of the CALL operation and therefore
		// reuses its operation identity instead of allocating a new one.
		if frame.TargetID == 0 {
			e.mu.Unlock()
			return endpointFrame{}, nil, StatusError{Code: CodeInvalidArgument, Message: "rpc decision operation is missing"}
		}
		frame.ID = frame.TargetID
	} else {
		if e.nextID == ^uint64(0) {
			e.mu.Unlock()
			return endpointFrame{}, nil, StatusError{Code: CodeResourceExhausted, Message: "rpc request id space exhausted"}
		}
		e.nextID++
		frame.ID = e.nextID
	}
	frame.Origin = e.origin
	response := make(chan endpointFrame, 2)
	e.pending[frame.ID] = endpointPending{response: response, control: control, kind: frame.Kind, binding: frame.Binding}
	if control {
		e.pendingControls++
	} else {
		e.pendingData++
	}
	e.mu.Unlock()
	if err := e.writeContext(ctx, frame); err != nil {
		e.mu.Lock()
		e.removePendingLocked(frame.ID)
		if frame.Kind == "call" && e.outboundOperations != nil {
			delete(e.outboundOperations, frame.ID)
		}
		e.mu.Unlock()
		if ctx.Err() != nil {
			return endpointFrame{}, nil, statusError(ctx.Err())
		}
		if code, _ := CodeOf(err); code == CodeResourceExhausted {
			return endpointFrame{}, nil, err
		}
		e.fail(err)
		return endpointFrame{}, nil, e.endpointError()
	}
	return frame, response, nil
}

func (e *Endpoint) write(frame endpointFrame) error {
	return e.writeContext(context.Background(), frame)
}

func (e *Endpoint) writeContext(ctx context.Context, frame endpointFrame) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if frame.Timeout > 0 && frame.queuedAt.IsZero() {
		frame.queuedAt = time.Now()
	}
	e.mu.Lock()
	limits := e.outboundLimits
	e.mu.Unlock()
	payload, err := encodeEndpointFrame(frame, limits.MaxMessageBytes)
	if err != nil && frame.Reply {
		frame = endpointFrame{
			Kind: frame.Kind, Origin: frame.Origin, Reply: true, TargetID: frame.TargetID,
			Binding: frame.Binding, Code: CodeResourceExhausted, Message: "rpc response exceeds message limit",
			writeDone: frame.writeDone,
		}
		payload, err = encodeEndpointFrame(frame, limits.MaxMessageBytes)
	}
	if err != nil {
		return err
	}
	control := endpointPhysicalControl(frame.Kind) && len(payload) <= limits.MaxFrameBytes-maxFragmentHeaderBytes
	if (frame.Kind == "renew" || frame.Kind == "renew_ack") && !control {
		return StatusError{Code: CodeResourceExhausted, Message: "rpc renewal exceeds control frame limit"}
	}
	e.mu.Lock()
	if e.state != endpointOpen {
		e.mu.Unlock()
		return e.endpointError()
	}
	if control {
		budget := limits.MaxInFlightBytes
		if limits.MaxFrameBytes <= budget/endpointReplyQueueCapacity {
			budget = limits.MaxFrameBytes * endpointReplyQueueCapacity
		}
		if e.queuedControlBytes > budget-len(payload) {
			e.mu.Unlock()
			return StatusError{Code: CodeResourceExhausted, Message: "rpc control byte limit exceeded"}
		}
		e.queuedControlBytes += len(payload)
	} else {
		if len(payload) > limits.MaxInFlightBytes || e.queuedWriteBytes > limits.MaxInFlightBytes-len(payload) {
			e.mu.Unlock()
			return StatusError{Code: CodeResourceExhausted, Message: "rpc endpoint in-flight byte limit exceeded"}
		}
		e.queuedWriteBytes += len(payload)
	}
	e.enqueueWG.Add(1)
	e.mu.Unlock()
	defer e.enqueueWG.Done()
	queue := e.requestWrites
	if frame.Reply || frame.Kind == "hello" || frame.Kind == "ready" {
		queue = e.replyWrites
	}
	if control {
		queue = e.controlWrites
	}
	write := endpointWrite{frame: frame, payload: payload, done: frame.writeDone, control: control, queuedAt: time.Now(), ctx: context.Background()}
	if write.done != nil {
		write.ctx = ctx
	}
	queueCtx := ctx
	if e.options.LeaseTTL > 0 {
		var cancel context.CancelFunc
		queueCtx, cancel = context.WithTimeout(ctx, e.options.LeaseTTL)
		defer cancel()
	}
	select {
	case queue <- write:
		return nil
	case <-queueCtx.Done():
		e.completeWrite(write, queueCtx.Err())
		return queueCtx.Err()
	case <-e.stopping:
		e.completeWrite(write, e.endpointError())
		return e.endpointError()
	}
}

func (e *Endpoint) writeLoop() {
	var current *endpointWrite
	defer func() {
		if current != nil {
			e.completeWrite(*current, io.ErrClosedPipe)
		}
		e.enqueueWG.Wait()
		for {
			select {
			case write := <-e.controlWrites:
				e.completeWrite(write, io.ErrClosedPipe)
			case write := <-e.replyWrites:
				e.completeWrite(write, io.ErrClosedPipe)
			case write := <-e.requestWrites:
				e.completeWrite(write, io.ErrClosedPipe)
			default:
				close(e.writerDone)
				return
			}
		}
	}()
	var messageID uint64
	var fragmentBuffer []byte
	var dataPayload []byte
	var offset, controlBurst int
	for {
		select {
		case <-e.stopping:
			return
		default:
		}
		// Retain one data message while giving the reserved control lane a
		// finite burst between fragments. Both data queues remain eligible.
		if current == nil {
			select {
			case write := <-e.replyWrites:
				current = &write
			case write := <-e.requestWrites:
				current = &write
			default:
			}
		}
		var control *endpointWrite
		if current == nil || controlBurst < 4 {
			select {
			case write := <-e.controlWrites:
				control = &write
			default:
			}
		}
		if current == nil && control == nil {
			select {
			case <-e.stopping:
				return
			case write := <-e.controlWrites:
				control = &write
			case write := <-e.replyWrites:
				current = &write
			case write := <-e.requestWrites:
				current = &write
			}
		}
		write := current
		if control != nil {
			write = control
		}
		e.mu.Lock()
		limits := e.outboundLimits
		e.mu.Unlock()
		err := write.ctx.Err()
		if err == nil && (control != nil || offset == 0) && time.Since(write.queuedAt) >= e.options.LeaseTTL {
			err = StatusError{Code: CodeUnavailable, Message: "rpc send queue stalled"}
		}
		if err != nil {
			e.completeWrite(*write, err)
			if control == nil {
				current = nil
			}
			if control == nil && offset != 0 || write.done == nil {
				e.fail(err)
				return
			}
			continue
		}
		payload, id, position := write.payload, uint64(0), 0
		if control == nil {
			if offset == 0 {
				dataPayload = write.payload
				frame := write.frame
				if frame.Timeout > 0 && !frame.queuedAt.IsZero() {
					frame.Timeout = max(1, int64(time.Duration(frame.Timeout)-time.Since(frame.queuedAt)))
					dataPayload, err = encodeEndpointFrame(frame, limits.MaxMessageBytes)
				}
				if messageID == ^uint64(0) {
					err = StatusError{Code: CodeResourceExhausted, Message: "rpc transport message id space exhausted"}
				}
				messageID++
			}
			payload, id, position = dataPayload, messageID, offset
		}
		end := min(position+limits.MaxFrameBytes-maxFragmentHeaderBytes, len(payload))
		var fragment []byte
		if err == nil {
			fragment, err = encodeEndpointFragmentTo(fragmentBuffer, id, len(payload), position, payload[position:end], limits.MaxFrameBytes)
		}
		if err == nil {
			writeCtx, cancel := context.WithTimeout(e.transportCtx, e.options.LeaseTTL)
			stop := context.AfterFunc(write.ctx, cancel)
			write.sentAt = time.Now()
			err = e.conn.Write(writeCtx, fragment)
			if err == nil {
				err = writeCtx.Err()
			}
			stop()
			cancel()
		}
		if err != nil {
			e.completeWrite(*write, err)
			if control == nil {
				current = nil
			}
			e.fail(err)
			return
		}
		fragmentBuffer = fragment[:0]
		if control != nil {
			controlBurst++
			e.completeWrite(*control, nil)
		} else {
			controlBurst = 0
			offset = end
			if offset == len(payload) {
				e.completeWrite(*current, nil)
				current, dataPayload, offset = nil, nil, 0
			}
		}
	}
}

func endpointPhysicalControl(kind string) bool {
	switch kind {
	case "accepted", "renew", "renew_ack", "cancel", "decision", "done":
		return true
	default:
		return false
	}
}

func (e *Endpoint) completeWrite(write endpointWrite, err error) {
	if err == nil && !write.frame.Reply {
		switch write.frame.Kind {
		case "bind", "call", "drop", "close":
			e.mu.Lock()
			if pending, ok := e.pending[write.frame.ID]; ok && !pending.abandoned && e.outboundOperations != nil {
				e.outboundOperations[write.frame.ID] = write.sentAt.Add(e.peerLeaseDurationLocked())
			}
			e.mu.Unlock()
		}
	}
	if write.control {
		e.mu.Lock()
		e.queuedControlBytes -= len(write.payload)
		e.mu.Unlock()
	} else {
		e.releaseQueuedWrite(len(write.payload))
	}
	if write.done != nil {
		write.done <- err
	}
}

func (e *Endpoint) releaseQueuedWrite(size int) {
	e.mu.Lock()
	e.queuedWriteBytes -= size
	e.mu.Unlock()
}

func (e *Endpoint) outboundLimitSnapshot() Limits {
	e.mu.Lock()
	limits := e.outboundLimits
	e.mu.Unlock()
	return limits
}

func (e *Endpoint) controlContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

func (e *Endpoint) inboundContext(frame endpointFrame) (context.Context, context.CancelFunc) {
	var timeout time.Duration
	if frame.Timeout > 0 {
		remote := time.Duration(frame.Timeout)
		timeout = remote
	}
	if frame.Kind == "call" && e.options.MaxCallDuration > 0 && (timeout <= 0 || e.options.MaxCallDuration < timeout) {
		timeout = e.options.MaxCallDuration
	}
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

func (e *Endpoint) invalidateOutbound(binding uint64, routes *RouteSet) {
	e.mu.Lock()
	_, authorized := e.outboundLease[binding]
	owned := e.state == endpointOpen && e.outbound[binding] == routes && authorized
	if owned {
		delete(e.outboundLease, binding)
		e.dispatchWG.Add(1)
	}
	e.mu.Unlock()
	if routes != nil {
		routes.disconnect()
	}
	if owned {
		go func() {
			defer e.dispatchWG.Done()
			err := routes.Shutdown(context.Background())
			e.mu.Lock()
			delete(e.outbound, binding)
			if e.shutdownErr == nil {
				e.shutdownErr = err
			}
			e.mu.Unlock()
		}()
	}
}

func endpointOutcomeUncertain(err error) bool {
	code, _ := CodeOf(err)
	return code == CodeUnavailable || code == CodeCanceled || code == CodeDeadlineExceeded
}

func (e *Endpoint) readLoop() {
	defer close(e.readerDone)
	var assembly endpointAssembly
	var previousMessageID uint64
	var dataProgress time.Time
	for {
		var payload []byte
		var control bool
		for payload == nil {
			deadline := time.Now().Add(e.options.LeaseTTL)
			if assembly.data != nil {
				deadline = dataProgress.Add(e.options.LeaseTTL)
			}
			readCtx, cancel := context.WithDeadline(e.transportCtx, deadline)
			fragmentPayload, err := e.conn.Read(readCtx)
			if err == nil {
				err = readCtx.Err()
			}
			cancel()
			if err != nil {
				e.fail(err)
				return
			}
			fragment, err := decodeEndpointFragment(fragmentPayload, e.limits)
			if err != nil {
				e.fail(StatusError{Code: CodeProtocol, Message: err.Error()})
				return
			}
			control = fragment.messageID == 0
			if !control {
				dataProgress = time.Now()
			}
			payload, err = assembly.append(fragment, &previousMessageID)
			if err != nil {
				e.fail(StatusError{Code: CodeProtocol, Message: err.Error()})
				return
			}
		}
		frame, err := decodeEndpointFrame(payload, e.limits)
		if err != nil {
			e.fail(err)
			return
		}
		if control && !endpointPhysicalControl(frame.Kind) {
			e.fail(StatusError{Code: CodeProtocol, Message: "rpc data message used control lane"})
			return
		}
		if frame.Kind == "hello" {
			if frame.Protocol != EndpointProtocol || frame.Origin == "" || frame.Origin == e.origin {
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc endpoint handshake mismatch"})
				return
			}
			e.mu.Lock()
			if e.state != endpointOpen {
				e.mu.Unlock()
				return
			}
			if e.peerOrigin == "" {
				e.peerOrigin = frame.Origin
				e.outboundLimits = minLimits(e.limits, limitsFromWire(*frame.Limits))
				e.peerLeaseTTL = time.Duration(frame.LeaseTTL)
				close(e.handshake)
			} else {
				e.mu.Unlock()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc endpoint received a duplicate hello"})
				return
			}
			e.mu.Unlock()
			e.readyOnce.Do(func() {
				if err := e.write(endpointFrame{Kind: "ready", Origin: e.origin}); err != nil {
					e.fail(err)
				}
			})
			continue
		}
		e.mu.Lock()
		peerOrigin := e.peerOrigin
		e.mu.Unlock()
		if peerOrigin == "" {
			e.fail(StatusError{Code: CodeProtocol, Message: "rpc " + frame.Kind + " frame arrived before handshake"})
			return
		}
		if frame.Origin == "" {
			e.fail(StatusError{Code: CodeProtocol, Message: "rpc frame has no origin"})
			return
		}
		if frame.Origin != peerOrigin {
			e.fail(StatusError{Code: CodeProtocol, Message: "rpc frame origin changed"})
			return
		}
		if frame.Reply {
			e.mu.Lock()
			pending, ok := e.pending[frame.TargetID]
			if !ok {
				e.mu.Unlock()
				continue
			}
			valid := !pending.terminal
			if frame.Kind == "accepted" {
				valid = valid && !pending.accepted && pending.kind != "renew"
				pending.accepted = true
			} else {
				switch pending.kind {
				case "call":
					valid = valid && (frame.Kind == "offer" || frame.Kind == "done" && frame.Code != "")
				case "renew":
					valid = valid && frame.Kind == "renew_ack"
				default:
					valid = valid && frame.Kind == "done"
				}
				pending.terminal = true
			}
			if pending.binding != 0 && frame.Binding != pending.binding {
				valid = false
			}
			if pending.kind == "bind" && frame.Kind == "done" && frame.Code == "" {
				valid = valid && frame.Binding != 0 && frame.Epoch != 0 && frame.LeaseTTL >= int64(time.Millisecond) && e.outbound[frame.Binding] == nil
			}
			if !valid {
				e.mu.Unlock()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc reply does not match operation state"})
				return
			}
			e.pending[frame.TargetID] = pending
			e.mu.Unlock()
			pending.response <- frame
			continue
		}
		if frame.Kind == "ready" {
			e.mu.Lock()
			select {
			case <-e.ready:
				e.mu.Unlock()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc endpoint received a duplicate ready"})
				return
			default:
				close(e.ready)
			}
			e.mu.Unlock()
			e.maintenanceOnce.Do(func() { go e.maintainLeases() })
			continue
		}
		if frame.Kind == "cancel" {
			e.mu.Lock()
			if entry, ok := e.results[frame.TargetID]; ok && entry.deciding {
				entry.retiring = true
				e.results[frame.TargetID] = entry
			}
			if active, ok := e.active[frame.TargetID]; ok {
				active.cancel()
			}
			e.mu.Unlock()
			continue
		}
		ctx, cancel := e.inboundContext(frame)
		control = endpointControlFrame(frame.Kind)
		e.mu.Lock()
		if e.state != endpointOpen {
			e.mu.Unlock()
			cancel()
			continue
		}
		if frame.Kind == "renew" {
			if frame.ID <= e.peerRenewID {
				e.mu.Unlock()
				cancel()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc renewal id is not strictly increasing"})
				return
			}
			e.peerRenewID = frame.ID
		} else if frame.Kind == "decision" {
			if frame.ID != frame.TargetID || frame.ID > e.peerID {
				e.mu.Unlock()
				cancel()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc decision does not reference an existing operation"})
				return
			}
			if entry, exists := e.results[frame.ID]; exists && entry.deciding {
				e.mu.Unlock()
				cancel()
				if entry.accepting != frame.Accept {
					e.fail(StatusError{Code: CodeProtocol, Message: "rpc result decision changed"})
					return
				}
				continue
			}
			if _, exists := e.active[frame.ID]; exists {
				e.mu.Unlock()
				cancel()
				e.fail(StatusError{Code: CodeProtocol, Message: "rpc operation is already active"})
				return
			}
		} else if frame.ID <= e.peerID {
			e.mu.Unlock()
			cancel()
			e.fail(StatusError{Code: CodeProtocol, Message: "rpc request id is not strictly increasing"})
			return
		} else {
			e.peerID = frame.ID
		}
		if control && e.activeControls >= e.limits.MaxPendingControls || !control && e.activeData >= e.limits.MaxPendingCalls ||
			frame.Kind == "bind" && len(e.inbound)+e.inboundReservations >= e.limits.MaxBindings ||
			frame.Kind == "call" && len(e.results)+e.resultReservations >= e.limits.MaxPendingResults {
			e.mu.Unlock()
			cancel()
			kind := "done"
			switch frame.Kind {
			case "call":
				kind = "offer"
			case "renew":
				kind = "renew_ack"
			}
			if err := e.write(endpointFrame{Kind: kind, Binding: frame.Binding, Origin: e.origin, Reply: true, TargetID: frame.ID, Code: CodeResourceExhausted, Message: "rpc endpoint request limit exceeded"}); err != nil {
				e.fail(err)
				return
			}
			continue
		}
		if frame.Kind == "bind" {
			e.inboundReservations++
		}
		if frame.Kind == "call" {
			e.resultReservations++
		}
		e.active[frame.ID] = endpointActive{cancel: cancel, control: control, binding: frame.Binding}
		if frame.Kind != "renew" && frame.Kind != "decision" {
			e.inboundOperations[frame.ID] = time.Now().Add(e.options.LeaseTTL)
		}
		if control {
			e.activeControls++
		} else {
			e.activeData++
		}
		e.dispatchWG.Add(1)
		e.mu.Unlock()
		go e.dispatch(ctx, frame, cancel)
	}
}

func endpointControlFrame(kind string) bool {
	switch kind {
	case "bind", "decision", "drop", "close", "renew", "renew_ack", "accepted", "offer", "done":
		return true
	default:
		return false
	}
}

func (e *Endpoint) removePendingLocked(id uint64) {
	pending, ok := e.pending[id]
	if !ok {
		return
	}
	delete(e.pending, id)
	if pending.control {
		e.pendingControls--
	} else {
		e.pendingData--
	}
}
