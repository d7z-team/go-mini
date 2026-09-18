package rpc

import (
	"context"
	"errors"
	"time"
)

func (e *Endpoint) dispatch(ctx context.Context, frame endpointFrame, cancel context.CancelFunc) {
	defer e.dispatchWG.Done()
	reserved := frame.Kind == "bind" || frame.Kind == "call"
	releaseReservation := func() {
		if reserved {
			e.mu.Lock()
			if frame.Kind == "bind" {
				e.inboundReservations--
			} else {
				e.resultReservations--
			}
			e.mu.Unlock()
			reserved = false
		}
	}
	defer releaseReservation()
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		cancel()
		e.mu.Lock()
		if active, ok := e.active[frame.ID]; ok {
			delete(e.active, frame.ID)
			if active.control {
				e.activeControls--
			} else {
				e.activeData--
			}
		}
		if frame.Kind != "renew" {
			if _, offered := e.results[frame.ID]; !offered {
				delete(e.inboundOperations, frame.ID)
			}
		}
		e.mu.Unlock()
	}
	defer finish()
	replyKind := "done"
	switch frame.Kind {
	case "call":
		replyKind = "offer"
	case "renew":
		replyKind = frame.Kind
	}
	reply := endpointFrame{Kind: replyKind, Origin: e.origin, Reply: true, TargetID: frame.ID, Binding: frame.Binding}
	fail := func(err error) {
		reply.Code, reply.Message = CodeOf(err)
		reply.writeDone = make(chan error, 1)
		if writeErr := e.write(reply); writeErr != nil {
			e.fail(writeErr)
		} else {
			select {
			case writeErr := <-reply.writeDone:
				if writeErr != nil {
					e.fail(writeErr)
				}
			case <-e.stopping:
			}
		}
	}
	// Admission is owned by this operation. Wait for the physical write so
	// an OFFER in the data lane cannot overtake ACCEPTED in the control lane.
	if frame.Kind != "renew" {
		accepted := endpointFrame{Kind: "accepted", Origin: e.origin, Reply: true, TargetID: frame.ID, Binding: frame.Binding, writeDone: make(chan error, 1)}
		if err := e.write(accepted); err != nil {
			e.fail(err)
			return
		}
		select {
		case err := <-accepted.writeDone:
			if err != nil {
				e.fail(err)
				return
			}
		case <-e.stopping:
			return
		}
	}
	switch frame.Kind {
	case "bind":
		if e.services.Binder == nil {
			fail(StatusError{Code: CodeUnimplemented, Message: "rpc endpoint has no inbound services"})
			return
		}
		routes, err := e.services.Binder.Bind(ctx, BindRequest{
			Contract: cloneContract(*frame.Contract),
			Options: BindOptions{
				AffinityKey: frame.Options.AffinityKey,
				Labels:      cloneLabels(frame.Options.Labels),
			},
			Peer: e.peer,
			Hops: frame.Hops,
		})
		if err != nil {
			releaseReservation()
			fail(err)
			return
		}
		if err := ctx.Err(); err != nil {
			releaseReservation()
			_ = routes.Abort()
			fail(err)
			return
		}
		e.mu.Lock()
		_, active := e.active[frame.ID]
		idExhausted := e.nextBind == ^uint64(0)
		if e.state == endpointOpen && active && ctx.Err() == nil && e.nextBind != ^uint64(0) {
			e.nextBind++
			reply.Binding = e.nextBind
			reply.Epoch = routes.Epoch()
			e.inbound[reply.Binding] = routes
			lease := e.options.LeaseTTL
			e.inboundLease[reply.Binding] = time.Now().Add(lease)
			reply.LeaseTTL = lease.Nanoseconds()
		}
		e.inboundReservations--
		reserved = false
		e.mu.Unlock()
		if reply.Binding == 0 {
			_ = routes.Abort()
			err := ctx.Err()
			if idExhausted {
				err = StatusError{Code: CodeResourceExhausted, Message: "rpc binding id space exhausted"}
			} else if err == nil {
				err = StatusError{Code: CodeUnavailable, Message: "rpc endpoint closed during bind"}
			}
			fail(err)
			return
		}
	case "call":
		e.mu.Lock()
		routes := e.inbound[frame.Binding]
		if expires, valid := e.inboundLease[frame.Binding]; !valid || !time.Now().Before(expires) {
			routes = nil
		}
		e.mu.Unlock()
		if routes == nil || frame.Call == nil {
			fail(StatusError{Code: CodeNotFound, Message: "rpc binding is closed"})
			return
		}
		arguments, err := DecodeValues(frame.Call.Arguments, e.limits)
		if err != nil {
			releaseReservation()
			fail(StatusError{Code: CodeInvalidArgument, Message: err.Error()})
			return
		}
		call := Call{Method: frame.Call.Method, Arguments: arguments}
		if frame.Call.Receiver != nil {
			ref := *frame.Call.Receiver
			call.Receiver = &ref
		}
		result, err := routes.Call(ctx, call)
		if err != nil {
			releaseReservation()
			fail(err)
			return
		}
		values, err := EncodeValues(result.Values, e.limits)
		if err != nil {
			releaseReservation()
			_ = result.Discard(context.Background())
			if errors.Is(err, errValueLimit) {
				fail(StatusError{Code: CodeResourceExhausted, Message: err.Error()})
				return
			}
			fail(StatusError{Code: CodeInternal, Message: err.Error()})
			return
		}
		e.mu.Lock()
		_, active := e.active[frame.ID]
		expires, live := e.inboundOperations[frame.ID]
		offered := e.state == endpointOpen && active && ctx.Err() == nil && e.inbound[frame.Binding] == routes && live && time.Now().Before(expires)
		if offered {
			// v12 uses the CALL operation ID for the whole provisional-result
			// lifecycle. There is no independently allocated result identity.
			reply.TargetID = frame.ID
			lease := e.options.LeaseTTL
			e.results[frame.ID] = endpointResult{binding: frame.Binding, result: result, expiresAt: expires}
			reply.LeaseTTL = lease.Nanoseconds()
		}
		e.resultReservations--
		reserved = false
		e.mu.Unlock()
		if !offered {
			_ = result.Discard(context.Background())
			err := ctx.Err()
			if err == nil {
				err = StatusError{Code: CodeUnavailable, Message: "rpc endpoint closed during call"}
			}
			fail(err)
			return
		}
		reply.Values = values
		// Execution has ended; the provisional result now owns this operation.
		finish()
	case "decision":
		e.mu.Lock()
		entry, ok := e.results[frame.TargetID]
		if ok && entry.binding == frame.Binding && !entry.retiring && time.Now().Before(entry.expiresAt) {
			entry.deciding, entry.accepting = true, frame.Accept
			e.results[frame.TargetID] = entry
		} else {
			ok = false
		}
		e.mu.Unlock()
		if !ok || entry.binding != frame.Binding {
			fail(StatusError{Code: CodeNotFound, Message: "rpc result decision is stale"})
			return
		}
		var err error
		if frame.Accept {
			err = entry.result.Accept(context.Background())
		} else {
			err = entry.result.Discard(context.Background())
		}
		if err == nil {
			e.mu.Lock()
			current, present := e.results[frame.TargetID]
			abandoned := !present || current.retiring || !time.Now().Before(current.expiresAt)
			e.mu.Unlock()
			if frame.Accept && abandoned && entry.result.release != nil {
				err = entry.result.release()
			}
		}
		if err != nil {
			reply.Code, reply.Message = CodeOf(err)
		}
		written := make(chan error, 1)
		reply.writeDone = written
		if err := e.write(reply); err != nil {
			e.fail(err)
		} else {
			select {
			case err := <-written:
				if err != nil {
					e.fail(err)
				}
			case <-e.stopping:
			}
		}
		e.mu.Lock()
		delete(e.results, frame.TargetID)
		delete(e.inboundOperations, frame.TargetID)
		e.mu.Unlock()
		return
	case "drop":
		e.mu.Lock()
		routes := e.inbound[frame.Binding]
		if expires, valid := e.inboundLease[frame.Binding]; !valid || !time.Now().Before(expires) {
			routes = nil
		}
		e.mu.Unlock()
		if routes == nil || frame.Ref == nil {
			fail(StatusError{Code: CodeNotFound, Message: "rpc binding is closed"})
			return
		}
		if err := routes.Drop(context.Background(), *frame.Ref); err != nil {
			fail(err)
			return
		}
	case "close":
		e.mu.Lock()
		routes := e.inbound[frame.Binding]
		delete(e.inboundLease, frame.Binding)
		var results []*Result
		for resultID, entry := range e.results {
			if entry.binding == frame.Binding {
				delete(e.inboundOperations, resultID)
				entry.retiring = true
				e.results[resultID] = entry
				results = append(results, entry.result)
			}
		}
		e.mu.Unlock()
		var closeErr error
		if routes != nil {
			closeErr = routes.Abort()
		}
		for _, result := range results {
			closeErr = errors.Join(closeErr, result.finishForShutdown())
		}
		e.mu.Lock()
		delete(e.inbound, frame.Binding)
		for id, entry := range e.results {
			if entry.binding == frame.Binding && !entry.deciding {
				delete(e.results, id)
			}
		}
		e.mu.Unlock()
		if closeErr != nil {
			fail(closeErr)
			return
		}
	case "renew":
		// A renewal is deliberately handled by the endpoint owner. The empty
		// v12 round proves transport health; object-specific renewals are added
		// to Values by the maintenance owner once they are registered.
		reply.Kind = "renew_ack"
		e.mu.Lock()
		reply.LeaseTTL = e.options.LeaseTTL.Nanoseconds()
		e.mu.Unlock()
		var err error
		reply.Values, err = e.renewInboundTargets(frame.Values)
		if err != nil {
			fail(err)
			return
		}
	default:
		fail(StatusError{Code: CodeProtocol, Message: "unknown rpc frame kind"})
		return
	}
	reply.writeDone = make(chan error, 1)
	if err := e.write(reply); err != nil {
		e.fail(err)
	} else {
		select {
		case err := <-reply.writeDone:
			if err != nil {
				e.fail(err)
			}
		case <-e.stopping:
		}
	}
}

func (e *Endpoint) discardReply(reply endpointFrame) {
	if reply.Code != "" {
		return
	}
	var cleanup endpointFrame
	switch reply.Kind {
	case "done":
		if reply.Binding == 0 || reply.Epoch == 0 {
			return
		}
		cleanup = endpointFrame{Kind: "close", Binding: reply.Binding}
	case "offer":
		if reply.TargetID == 0 {
			return
		}
		cleanup = endpointFrame{Kind: "decision", Binding: reply.Binding, TargetID: reply.TargetID}
	default:
		return
	}
	_, err := e.request(context.Background(), cleanup)
	if endpointOutcomeUncertain(err) {
		e.mu.Lock()
		routes := e.outbound[reply.Binding]
		e.mu.Unlock()
		if routes != nil {
			e.invalidateOutbound(reply.Binding, routes)
		}
	}
}
