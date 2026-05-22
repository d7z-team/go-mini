package synclib

import (
	"context"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type ModuleHost struct{}

func (h *ModuleHost) NewWaitGroup() *WaitGroup {
	return &WaitGroup{}
}

type waiterID uint64

// ffigen:methods
type WaitGroup struct {
	mu      sync.Mutex
	count   int64
	nextID  waiterID
	waiters map[waiterID]ffigo.Completion[ffigo.Void]
}

func (w *WaitGroup) Add(delta int64) {
	if w == nil {
		panic("sync: nil WaitGroup")
	}
	if delta == 0 {
		return
	}

	var pending []ffigo.Completion[ffigo.Void]

	w.mu.Lock()
	next := w.count + delta
	if next < 0 {
		w.mu.Unlock()
		panic("sync: negative WaitGroup counter")
	}
	w.count = next
	if next == 0 && len(w.waiters) > 0 {
		pending = make([]ffigo.Completion[ffigo.Void], 0, len(w.waiters))
		for id, done := range w.waiters {
			pending = append(pending, done)
			delete(w.waiters, id)
		}
	}
	w.mu.Unlock()

	for _, done := range pending {
		done.Complete(ffigo.Void{}, nil)
	}
}

func (w *WaitGroup) Done() {
	w.Add(-1)
}

func (w *WaitGroup) Wait() ffigo.Async[ffigo.Void] {
	if w == nil {
		panic("sync: nil WaitGroup")
	}
	return ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (func(), error) {
		w.mu.Lock()
		if w.count == 0 {
			w.mu.Unlock()
			done.Complete(ffigo.Void{}, nil)
			return nil, nil
		}
		w.nextID++
		id := w.nextID
		if w.waiters == nil {
			w.waiters = make(map[waiterID]ffigo.Completion[ffigo.Void])
		}
		w.waiters[id] = done
		w.mu.Unlock()

		return func() {
			w.mu.Lock()
			delete(w.waiters, id)
			w.mu.Unlock()
		}, nil
	})
}

func RegisterSyncAll(executor interface {
	RegisterFFISchema(string, ffigo.FFIBridge, uint32, *runtime.RuntimeFuncSig, string)
	RegisterStructSchema(string, *runtime.RuntimeStructSpec)
	RegisterConstant(string, string)
}, impl Module, registry *ffigo.HandleRegistry,
) {
	RegisterModule(executor, impl, registry)
	RegisterWaitGroup(executor, registry)
}
