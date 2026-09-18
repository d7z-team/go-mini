package runtime

import (
	"container/heap"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

const (
	vmTimerActive uint32 = iota
	vmTimerFired
	vmTimerStopped
)

type VMTimerService struct {
	mu      sync.Mutex
	clock   *VMClock
	timers  vmTimerHeap
	stopped bool
	wakeCh  chan struct{}
	stopCh  chan struct{}
}

func NewVMTimerService(clock *VMClock) *VMTimerService {
	if clock == nil {
		clock = NewVMClock()
	}
	service := &VMTimerService{
		clock:  clock,
		wakeCh: make(chan struct{}, 1),
		stopCh: make(chan struct{}),
	}
	go service.run()
	return service
}

func (s *VMTimerService) NewTimer(d time.Duration) *VMTimer {
	timer := &VMTimer{
		service:   s,
		heapIndex: -1,
	}
	if d <= 0 {
		timer.state.Store(vmTimerFired)
		return timer
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		timer.state.Store(vmTimerStopped)
		return timer
	}
	timer.deadline = s.clock.Now().Add(d)
	timer.state.Store(vmTimerActive)
	timer.heapIndex = len(s.timers)
	heap.Push(&s.timers, timer)
	s.mu.Unlock()
	s.Wake()
	return timer
}

func (s *VMTimerService) Wake() {
	if s == nil {
		return
	}
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *VMTimerService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	timers := make([]*VMTimer, 0, len(s.timers))
	for len(s.timers) > 0 {
		timer := heap.Pop(&s.timers).(*VMTimer)
		timer.heapIndex = -1
		timers = append(timers, timer)
	}
	close(s.stopCh)
	s.mu.Unlock()

	for _, timer := range timers {
		timer.stopFromService()
	}
}

func (s *VMTimerService) remove(timer *VMTimer) {
	if s == nil || timer == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if timer.heapIndex < 0 || timer.heapIndex >= len(s.timers) || s.timers[timer.heapIndex] != timer {
		return
	}
	heap.Remove(&s.timers, timer.heapIndex)
	timer.heapIndex = -1
}

func (s *VMTimerService) run() {
	for {
		s.mu.Lock()
		for len(s.timers) > 0 {
			timer := s.timers[0]
			if timer.state.Load() == vmTimerActive {
				break
			}
			heap.Pop(&s.timers)
			timer.heapIndex = -1
		}
		if s.stopped {
			s.mu.Unlock()
			return
		}
		if len(s.timers) == 0 || s.clock.IsPaused() {
			wakeCh := s.wakeCh
			stopCh := s.stopCh
			s.mu.Unlock()
			select {
			case <-wakeCh:
			case <-stopCh:
				return
			}
			continue
		}
		timer := s.timers[0]
		wait := timer.deadline.Sub(s.clock.Now())
		wakeCh := s.wakeCh
		stopCh := s.stopCh
		s.mu.Unlock()

		if wait <= 0 {
			timer.fire()
			continue
		}

		hostTimer := time.NewTimer(wait)
		select {
		case <-hostTimer.C:
		case <-wakeCh:
		case <-stopCh:
			if !hostTimer.Stop() {
				select {
				case <-hostTimer.C:
				default:
				}
			}
			return
		}
		if !hostTimer.Stop() {
			select {
			case <-hostTimer.C:
			default:
			}
		}
	}
}

type VMTimer struct {
	service  *VMTimerService
	deadline time.Time

	state     atomic.Uint32
	mu        sync.Mutex
	waiters   map[uint64]ffigo.Completion[bool]
	nextID    uint64
	heapIndex int
}

func NewVMTimer(service *VMTimerService, d time.Duration) *VMTimer {
	if service == nil {
		timer := &VMTimer{heapIndex: -1}
		timer.state.Store(vmTimerStopped)
		return timer
	}
	return service.NewTimer(d)
}

func (t *VMTimer) Wait() ffigo.Async[bool] {
	return ffigo.AsyncFunc[bool](func(_ context.Context, done ffigo.Completion[bool]) (ffigo.WaitHandle, error) {
		if t == nil {
			done.Complete(false, nil)
			return nil, nil
		}
		if t.service == nil {
			done.Complete(false, errors.New("vm timer requires a timer service"))
			return nil, nil
		}
		switch t.state.Load() {
		case vmTimerFired:
			done.Complete(true, nil)
			return nil, nil
		case vmTimerStopped:
			done.Complete(false, nil)
			return nil, nil
		}

		t.mu.Lock()
		switch t.state.Load() {
		case vmTimerFired:
			t.mu.Unlock()
			done.Complete(true, nil)
			return nil, nil
		case vmTimerStopped:
			t.mu.Unlock()
			done.Complete(false, nil)
			return nil, nil
		}
		t.nextID++
		id := t.nextID
		if t.waiters == nil {
			t.waiters = make(map[uint64]ffigo.Completion[bool])
		}
		t.waiters[id] = done
		t.mu.Unlock()

		cancel := func() {
			t.mu.Lock()
			delete(t.waiters, id)
			t.mu.Unlock()
		}
		return ffigo.NewWaitHandle(ffigo.WaitExternal, "vm.Timer.Wait", cancel), nil
	})
}

func (t *VMTimer) Stop() bool {
	if t == nil || !t.state.CompareAndSwap(vmTimerActive, vmTimerStopped) {
		return false
	}
	if t.service != nil {
		t.service.remove(t)
	}
	for _, done := range t.drainWaiters() {
		done.Complete(false, nil)
	}
	return true
}

func (t *VMTimer) stopFromService() {
	if t == nil || !t.state.CompareAndSwap(vmTimerActive, vmTimerStopped) {
		return
	}
	for _, done := range t.drainWaiters() {
		done.Complete(false, nil)
	}
}

func (t *VMTimer) fire() {
	if t == nil || !t.state.CompareAndSwap(vmTimerActive, vmTimerFired) {
		return
	}
	if t.service != nil {
		t.service.remove(t)
	}
	for _, done := range t.drainWaiters() {
		done.Complete(true, nil)
	}
}

func (t *VMTimer) drainWaiters() []ffigo.Completion[bool] {
	t.mu.Lock()
	defer t.mu.Unlock()
	waiters := make([]ffigo.Completion[bool], 0, len(t.waiters))
	for id, done := range t.waiters {
		waiters = append(waiters, done)
		delete(t.waiters, id)
	}
	return waiters
}

type vmTimerHeap []*VMTimer

func (h vmTimerHeap) Len() int {
	return len(h)
}

func (h vmTimerHeap) Less(i, j int) bool {
	return h[i].deadline.Before(h[j].deadline)
}

func (h vmTimerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *vmTimerHeap) Push(x any) {
	timer := x.(*VMTimer)
	timer.heapIndex = len(*h)
	*h = append(*h, timer)
}

func (h *vmTimerHeap) Pop() any {
	old := *h
	n := len(old)
	timer := old[n-1]
	timer.heapIndex = -1
	*h = old[:n-1]
	return timer
}
