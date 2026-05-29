//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg contextlib -out context_ffigen.go interface.go host.go
package contextlib

import (
	"context"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

// ffigen:module context/internal
type Module interface {
	Canceled() error
	DeadlineExceeded() error
	NewTimer(ctx context.Context, ns int64) *Timer
}

// ffigen:module context/internal
// ffigen:methods
type Timer struct {
	impl *timerState
}

func (t *Timer) Wait() ffigo.Async[bool] {
	if t == nil || t.impl == nil {
		return (*timerState)(nil).wait()
	}
	return t.impl.wait()
}

func (t *Timer) Stop() bool {
	if t == nil || t.impl == nil {
		return false
	}
	return t.impl.stop()
}
