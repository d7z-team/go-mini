package rpc

import (
	"context"
	"testing"
	"time"
)

func TestEndpointRequestQueuePreservesIDOrder(t *testing.T) {
	limits := normalizeLimits(Limits{})
	endpoint := &Endpoint{
		state: endpointOpen, origin: "local", limits: limits, outboundLimits: limits,
		pending: make(map[uint64]endpointPending), stopping: make(chan struct{}),
		requestGate: make(chan struct{}, 1), requestWrites: make(chan endpointWrite, 1),
		replyWrites: make(chan endpointWrite, 1),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const requests = 64
	done := make(chan error, requests)
	for i := range requests {
		go func() {
			frame := endpointFrame{Kind: "close", Binding: 1}
			if i%2 == 0 {
				frame = endpointFrame{Kind: "call", Binding: 1, Call: &wireCall{Method: Method{
					ID: "example/order::Service.Value", Service: "example/order::Service", Name: "Value", ContractHash: testContractHash,
				}}}
			}
			_, _, err := endpoint.enqueueRequest(ctx, frame)
			done <- err
		}()
	}
	for id := uint64(1); id <= requests; id++ {
		select {
		case write := <-endpoint.requestWrites:
			endpoint.releaseQueuedWrite(len(write.payload))
			if write.done != nil {
				write.done <- nil
			}
			frame, err := decodeEndpointFrame(write.payload, limits)
			if err != nil || frame.ID != id {
				t.Fatalf("request %d: frame=%+v error=%v", id, frame, err)
			}
			endpoint.mu.Lock()
			endpoint.removePendingLocked(frame.ID)
			endpoint.mu.Unlock()
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	for range requests {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if endpoint.queuedWriteBytes != 0 || len(endpoint.pending) != 0 {
		t.Fatalf("queue retained state: bytes=%d pending=%d", endpoint.queuedWriteBytes, len(endpoint.pending))
	}

	// Waiting for another sender must respect cancellation without consuming an ID.
	endpoint.requestGate <- struct{}{}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, _, err := endpoint.enqueueRequest(canceled, endpointFrame{Kind: "close", Binding: 1}); codeOf(err) != CodeCanceled {
		t.Fatalf("canceled enqueue: %v", err)
	}
	<-endpoint.requestGate
	if endpoint.nextID != requests {
		t.Fatalf("canceled enqueue consumed request ID: %d", endpoint.nextID)
	}
}
