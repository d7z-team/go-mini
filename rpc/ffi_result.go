package rpc

import (
	"context"
	"errors"
)

// ffiPendingResult keeps the host transaction alive across guest typed decoding.
// It belongs to one lease and is bounded by MaxPendingResults.
type ffiPendingResult struct {
	lease  uint64
	result *Result
	bytes  int
}

func (b *ffiSession) decideResult(ctx context.Context, lease, id uint64, accept bool) (func() error, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		if !accept {
			// Shutdown owns all undecided results and joins their cleanup.
			return nil, nil
		}
		return nil, StatusError{Code: CodeUnavailable, Message: "MRPC session is closed"}
	}
	pending, ok := b.results[id]
	if ok && pending.lease == lease {
		delete(b.results, id)
		b.decidingResults++
		b.workers.Add(1)
	}
	b.mu.Unlock()
	if !ok {
		if !accept {
			return nil, nil
		}
		return nil, StatusError{Code: CodeNotFound, Message: "MRPC result is closed"}
	}
	if pending.lease != lease {
		return nil, StatusError{Code: CodeInvalidArgument, Message: "MRPC result belongs to another lease"}
	}
	type decision struct {
		release func() error
		err     error
	}
	delivered := make(chan decision)
	go func() {
		defer b.workers.Done()
		defer func() {
			b.mu.Lock()
			b.decidingResults--
			b.resultBytes -= pending.bytes
			b.mu.Unlock()
		}()
		outcome := decision{}
		canceled := ctx.Err()
		if !accept || canceled != nil {
			outcome.err = pending.result.Discard(context.Background())
		} else {
			outcome.err = pending.result.Accept(context.Background())
			if outcome.err == nil {
				outcome.release = pending.result.release
			}
		}
		cleanupErr := outcome.err
		if canceled != nil {
			outcome.err = errors.Join(canceled, outcome.err)
		}
		select {
		case delivered <- outcome:
		case <-ctx.Done():
			if outcome.release != nil {
				cleanupErr = errors.Join(cleanupErr, outcome.release())
			}
			if cleanupErr != nil {
				b.mu.Lock()
				if b.shutdownErr == nil {
					b.shutdownErr = cleanupErr
				}
				b.mu.Unlock()
			}
		}
	}()
	select {
	case outcome := <-delivered:
		return outcome.release, outcome.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
