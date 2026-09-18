package rpc

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"
)

const (
	leaseTargetBinding   byte = 1
	leaseTargetOperation byte = 2
)

type leaseTarget struct {
	kind byte
	id   uint64
}

func collectLeaseTargets(bindings, operations map[uint64]time.Time, now time.Time) []leaseTarget {
	targets := make([]leaseTarget, 0, len(bindings)+len(operations))
	for id, expires := range bindings {
		if id != 0 && (expires.IsZero() || now.Before(expires)) {
			targets = append(targets, leaseTarget{kind: leaseTargetBinding, id: id})
		}
	}
	for id, expires := range operations {
		if id != 0 && (expires.IsZero() || now.Before(expires)) {
			targets = append(targets, leaseTarget{kind: leaseTargetOperation, id: id})
		}
	}
	slices.SortFunc(targets, func(left, right leaseTarget) int {
		if order := cmp.Compare(left.kind, right.kind); order != 0 {
			return order
		}
		return cmp.Compare(left.id, right.id)
	})
	return targets
}

func encodeLeaseTargetList(groups ...[]leaseTarget) []byte {
	var encoder wireEncoder
	count := 0
	for _, targets := range groups {
		count += len(targets)
	}
	encoder.Uint(uint64(count))
	for _, targets := range groups {
		for _, target := range targets {
			encoder.data = append(encoder.data, target.kind)
			encoder.Uint(target.id)
		}
	}
	return encoder.data
}

func encodeLeaseTargetsBatch(targets []leaseTarget, limit int, offset *int) []byte {
	if limit <= 0 || len(targets) <= limit {
		return encodeLeaseTargetList(targets)
	}
	start := 0
	if offset != nil {
		start = *offset % len(targets)
		if start < 0 {
			start = 0
		}
		*offset = (start + limit) % len(targets)
	}
	end := min(start+limit, len(targets))
	return encodeLeaseTargetList(targets[start:end], targets[:limit-(end-start)])
}

type leaseTargets struct {
	bindings   []uint64
	operations []uint64
}

func decodeLeaseTargets(payload []byte, limits Limits) (leaseTargets, error) {
	decoder := newWireDecoder(payload, limits.MaxMessageBytes)
	count, err := decoder.Uint()
	if err != nil || count > uint64(limits.MaxPendingControls) {
		return leaseTargets{}, errors.New("invalid RPC lease target count")
	}
	targets := leaseTargets{}
	seen := make(map[leaseTarget]struct{})
	for i := uint64(0); i < count; i++ {
		kind, err := decoder.byte()
		if err != nil || kind != leaseTargetBinding && kind != leaseTargetOperation {
			return leaseTargets{}, errors.New("invalid RPC lease target kind")
		}
		id, err := decoder.Uint()
		if err != nil || id == 0 {
			return leaseTargets{}, errors.New("invalid RPC lease target id")
		}
		target := leaseTarget{kind: kind, id: id}
		if _, duplicate := seen[target]; duplicate {
			return leaseTargets{}, errors.New("duplicate RPC lease target")
		}
		seen[target] = struct{}{}
		if kind == leaseTargetBinding {
			targets.bindings = append(targets.bindings, id)
		} else {
			targets.operations = append(targets.operations, id)
		}
	}
	if err := decoder.Done(); err != nil {
		return leaseTargets{}, err
	}
	return targets, nil
}

func (e *Endpoint) maintainLeases() {
	e.mu.Lock()
	lease := e.peerLeaseTTL
	if lease <= 0 {
		lease = e.options.LeaseTTL
	}
	e.mu.Unlock()
	limit := e.renewalBatchLimit()
	if limit <= 0 {
		e.fail(StatusError{Code: CodeResourceExhausted, Message: "rpc renewal control frame cannot fit negotiated limits"})
		return
	}
	interval := min(lease, e.options.LeaseTTL) / 4
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastAcknowledged := time.Now()
maintenance:
	for {
		select {
		case <-e.stopping:
			return
		case <-ticker.C:
			e.expireLeases()
			for batch := 0; ; batch++ {
				e.mu.Lock()
				now := time.Now()
				all := collectLeaseTargets(e.outboundLease, e.outboundOperations, now)
				targets := encodeLeaseTargetsBatch(all, limit, &e.renewOffset)
				e.mu.Unlock()
				if len(all) > 0 && batch > 0 && len(targets) == 1 {
					// A single-byte zero payload is the empty target list. It is
					// only emitted for a health round after all owners are covered.
					break
				}
				expected, decodeErr := decodeLeaseTargets(targets, e.limits)
				if decodeErr != nil {
					e.fail(StatusError{Code: CodeProtocol, Message: decodeErr.Error()})
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), e.options.AdmissionTimeout)
				renewSent := time.Now()
				ack, err := e.request(ctx, endpointFrame{Kind: "renew", Values: targets})
				cancel()
				if err != nil {
					if e.endpointError() != nil {
						return
					}
					if time.Since(lastAcknowledged) >= min(lease, e.options.LeaseTTL) {
						e.fail(StatusError{Code: CodeUnavailable, Message: "rpc renewal health window expired"})
						return
					}
					continue maintenance
				}
				lastAcknowledged = time.Now()
				ids, decodeErr := decodeLeaseTargets(ack.Values, e.limits)
				if decodeErr != nil {
					e.fail(StatusError{Code: CodeProtocol, Message: decodeErr.Error()})
					return
				}
				if renewErr := e.renewOutboundTargets(expected, ids, renewSent, time.Duration(ack.LeaseTTL)); renewErr != nil {
					e.fail(renewErr)
					return
				}
				if len(all) == 0 || len(all) <= limit || batch+1 >= (len(all)+limit-1)/limit {
					break
				}
			}
		}
	}
}

func (e *Endpoint) peerLeaseDurationLocked() time.Duration {
	if e.peerLeaseTTL > 0 {
		return e.peerLeaseTTL
	}
	return e.options.LeaseTTL
}

func (e *Endpoint) renewalBatchLimit() int {
	e.mu.Lock()
	limits := e.outboundLimits
	origin := e.origin
	e.mu.Unlock()
	limit := min(limits.MaxPendingControls, (limits.MaxFrameBytes-maxFragmentHeaderBytes)/2)
	if limit <= 0 {
		return 0
	}
	// A control message is sent as one physical fragment. Probe the actual
	// envelope rather than relying on a guessed fixed overhead; this keeps
	// the target count valid when the negotiated frame size changes.
	for count := limit; count > 0; count-- {
		targets := make([]leaseTarget, count)
		for index := range targets {
			targets[index] = leaseTarget{kind: leaseTargetOperation, id: ^uint64(0)}
		}
		payload, err := encodeEndpointFrame(endpointFrame{
			Kind: "renew_ack", Origin: origin, TargetID: ^uint64(0), Reply: true, LeaseTTL: int64(^uint64(0) >> 1),
			Values: encodeLeaseTargetList(targets),
		}, limits.MaxMessageBytes)
		if err == nil && len(payload) <= limits.MaxFrameBytes-maxFragmentHeaderBytes {
			return count
		}
	}
	return 0
}

// renewOutboundTargets advances only owners explicitly acknowledged by the
// peer. The owner table is the source of truth; no timer may resurrect an
// entry removed by a terminal transition.
func (e *Endpoint) renewOutboundTargets(expected, targets leaseTargets, sentAt time.Time, grant time.Duration) error {
	allowed := make(map[leaseTarget]struct{}, len(expected.bindings)+len(expected.operations))
	for _, id := range expected.bindings {
		allowed[leaseTarget{kind: leaseTargetBinding, id: id}] = struct{}{}
	}
	for _, id := range expected.operations {
		allowed[leaseTarget{kind: leaseTargetOperation, id: id}] = struct{}{}
	}
	if grant <= 0 {
		return StatusError{Code: CodeProtocol, Message: "rpc renewal grant must be positive"}
	}
	for _, id := range targets.bindings {
		if _, ok := allowed[leaseTarget{kind: leaseTargetBinding, id: id}]; !ok {
			return StatusError{Code: CodeProtocol, Message: "RPC renewal acknowledgement contains an unexpected binding"}
		}
	}
	for _, id := range targets.operations {
		if _, ok := allowed[leaseTarget{kind: leaseTargetOperation, id: id}]; !ok {
			return StatusError{Code: CodeProtocol, Message: "RPC renewal acknowledgement contains an unexpected operation"}
		}
	}
	e.mu.Lock()
	expires := sentAt.Add(grant)
	if !time.Now().Before(expires) {
		e.mu.Unlock()
		return nil
	}
	for _, id := range targets.bindings {
		if current, ok := e.outboundLease[id]; ok && (current.IsZero() || time.Now().Before(current)) {
			if expires.After(current) {
				e.outboundLease[id] = expires
			}
		}
	}
	for _, id := range targets.operations {
		if current, ok := e.outboundOperations[id]; ok && (current.IsZero() || time.Now().Before(current)) {
			if expires.After(current) {
				e.outboundOperations[id] = expires
			}
		}
	}
	e.mu.Unlock()
	return nil
}

func (e *Endpoint) renewInboundTargets(payload []byte) ([]byte, error) {
	targets, err := decodeLeaseTargets(payload, e.limits)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	expires := time.Now().Add(e.options.LeaseTTL)
	acknowledged := make(map[uint64]time.Time, len(targets.bindings))
	acknowledgedOperations := make(map[uint64]time.Time, len(targets.operations))
	now := time.Now()
	for _, id := range targets.bindings {
		if current, ok := e.inboundLease[id]; ok && (current.IsZero() || now.Before(current)) {
			e.inboundLease[id] = expires
			acknowledged[id] = expires
		}
	}
	for _, id := range targets.operations {
		if current, ok := e.inboundOperations[id]; ok && (current.IsZero() || now.Before(current)) {
			e.inboundOperations[id] = expires
			acknowledgedOperations[id] = expires
		}
		if current, ok := e.results[id]; ok && !current.retiring && now.Before(current.expiresAt) {
			current.expiresAt = expires
			e.results[id] = current
			if _, acknowledged := acknowledgedOperations[id]; !acknowledged {
				acknowledgedOperations[id] = expires
			}
		}
	}
	e.mu.Unlock()
	return encodeLeaseTargetList(collectLeaseTargets(acknowledged, acknowledgedOperations, now)), nil
}

func (e *Endpoint) expireLeases() {
	now := time.Now()
	inbound := make(map[uint64]*RouteSet)
	outbound := make(map[uint64]*RouteSet)
	results := make(map[uint64]*Result)
	var operationCancels []context.CancelFunc
	e.mu.Lock()
	if e.state != endpointOpen {
		e.mu.Unlock()
		return
	}
	for id, expires := range e.inboundOperations {
		if expires.IsZero() || now.Before(expires) {
			continue
		}
		delete(e.inboundOperations, id)
		if active, ok := e.active[id]; ok {
			operationCancels = append(operationCancels, active.cancel)
		}
	}
	for id, expires := range e.inboundLease {
		if !expires.IsZero() && !now.Before(expires) {
			if routes := e.inbound[id]; routes != nil {
				inbound[id] = routes
			}
			delete(e.inboundLease, id)
			for operation, active := range e.active {
				if active.binding == id {
					operationCancels = append(operationCancels, active.cancel)
					delete(e.inboundOperations, operation)
				}
			}
		}
	}
	for id, expires := range e.outboundLease {
		if !expires.IsZero() && !now.Before(expires) {
			if routes := e.outbound[id]; routes != nil {
				outbound[id] = routes
			}
			delete(e.outboundLease, id)
		}
	}
	for id, expires := range e.outboundOperations {
		if !expires.IsZero() && !now.Before(expires) {
			delete(e.outboundOperations, id)
			if pending, ok := e.pending[id]; ok && !pending.terminal {
				select {
				case pending.response <- endpointFrame{Kind: "done", Code: CodeUnavailable, Message: "rpc operation lease expired"}:
				default:
				}
				e.removePendingLocked(id)
			}
		}
	}
	for operation, result := range e.results {
		_, parentValid := e.inboundLease[result.binding]
		if !result.retiring && (!parentValid || !now.Before(result.expiresAt)) {
			result.retiring = true
			e.results[operation] = result
			if !result.deciding {
				results[operation] = result.result
			}
		}
	}
	e.dispatchWG.Add(len(inbound) + len(outbound) + len(results))
	e.mu.Unlock()
	for _, cancel := range operationCancels {
		cancel()
	}
	for id, result := range results {
		go func() {
			defer e.dispatchWG.Done()
			err := result.finishForShutdown()
			e.mu.Lock()
			delete(e.results, id)
			delete(e.inboundOperations, id)
			if e.shutdownErr == nil {
				e.shutdownErr = err
			}
			e.mu.Unlock()
		}()
	}
	for id, routes := range inbound {
		routes.disconnect()
		go func() {
			defer e.dispatchWG.Done()
			err := routes.Shutdown(context.Background())
			e.mu.Lock()
			delete(e.inbound, id)
			if e.shutdownErr == nil {
				e.shutdownErr = err
			}
			e.mu.Unlock()
		}()
	}
	for id, routes := range outbound {
		routes.disconnect()
		go func() {
			defer e.dispatchWG.Done()
			err := routes.Shutdown(context.Background())
			e.mu.Lock()
			delete(e.outbound, id)
			if e.shutdownErr == nil {
				e.shutdownErr = err
			}
			e.mu.Unlock()
		}()
	}
}
