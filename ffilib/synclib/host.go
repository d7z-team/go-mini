package synclib

import (
	"context"
	"fmt"
	"sync"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type ModuleHost struct{}

func (h *ModuleHost) NewWaitGroup() *WaitGroup {
	return &WaitGroup{}
}

type waiterID uint64

// ffigen:module sync
// ffigen:methods
type WaitGroup struct {
	mu      sync.Mutex
	count   int64
	nextID  waiterID
	waiters map[waiterID]ffigo.Completion[ffigo.Void]
}

type waitGroupWaitHandle struct {
	group *WaitGroup
	id    waiterID
}

func (h *waitGroupWaitHandle) Snapshot() ffigo.WaitSnapshot {
	if h == nil || h.group == nil {
		return ffigo.WaitSnapshot{Kind: ffigo.WaitDependsOnVM, Reason: "sync.WaitGroup"}
	}
	h.group.mu.Lock()
	count := h.group.count
	h.group.mu.Unlock()
	return ffigo.WaitSnapshot{
		Kind:   ffigo.WaitDependsOnVM,
		Reason: fmt.Sprintf("sync.WaitGroup count=%d", count),
	}
}

func (h *waitGroupWaitHandle) Cancel() {
	if h == nil || h.group == nil {
		return
	}
	h.group.mu.Lock()
	delete(h.group.waiters, h.id)
	h.group.mu.Unlock()
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
	return ffigo.AsyncFunc[ffigo.Void](func(_ context.Context, done ffigo.Completion[ffigo.Void]) (ffigo.WaitHandle, error) {
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

		return &waitGroupWaitHandle{group: w, id: id}, nil
	})
}
