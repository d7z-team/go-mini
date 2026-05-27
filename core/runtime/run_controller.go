package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type RunPhase uint8

const (
	RunPhaseRunning RunPhase = iota
	RunPhasePausing
	RunPhasePaused
	RunPhaseDone
)

type PauseReason struct {
	Kind string
	Meta string
}

var nextRunID atomic.Uint64

type RunController struct {
	mu        sync.Mutex
	id        uint64
	phase     RunPhase
	reason    PauseReason
	commandCh chan struct{}
	resumeCh  chan struct{}
	doneCh    chan struct{}
	clock     *VMClock
	timers    *VMTimerService
	err       error
}

func NewRunController(clock *VMClock) *RunController {
	if clock == nil {
		clock = NewVMClock()
	}
	return &RunController{
		id:        nextRunID.Add(1),
		phase:     RunPhaseRunning,
		commandCh: make(chan struct{}, 1),
		resumeCh:  make(chan struct{}),
		doneCh:    make(chan struct{}),
		clock:     clock,
		timers:    NewVMTimerService(clock),
	}
}

func (c *RunController) ID() uint64 {
	if c == nil {
		return 0
	}
	return c.id
}

func (c *RunController) Clock() *VMClock {
	if c == nil {
		return nil
	}
	return c.clock
}

func (c *RunController) TimerService() *VMTimerService {
	if c == nil {
		return nil
	}
	return c.timers
}

func (c *RunController) Phase() RunPhase {
	if c == nil {
		return RunPhaseDone
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

func (c *RunController) PauseReason() PauseReason {
	if c == nil {
		return PauseReason{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason
}

func (c *RunController) Signal() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.commandCh
}

func (c *RunController) HasPauseRequest() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase == RunPhasePausing || c.phase == RunPhasePaused
}

func (c *RunController) RequestPause(reason PauseReason) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.phase {
	case RunPhaseDone:
		return false
	case RunPhaseRunning:
		c.phase = RunPhasePausing
		c.reason = reason
		c.signalLocked()
	case RunPhasePausing, RunPhasePaused:
		c.reason = reason
	}
	return true
}

func (c *RunController) Resume() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.phase {
	case RunPhasePaused:
		c.phase = RunPhaseRunning
		c.reason = PauseReason{}
		if c.clock != nil {
			c.clock.Resume()
		}
		if c.timers != nil {
			c.timers.Wake()
		}
		close(c.resumeCh)
		c.resumeCh = make(chan struct{})
		c.signalLocked()
		return true
	default:
		return false
	}
}

func (c *RunController) EnterPaused() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.phase {
	case RunPhasePausing:
		c.phase = RunPhasePaused
		if c.clock != nil {
			c.clock.Pause()
		}
		if c.timers != nil {
			c.timers.Wake()
		}
		c.signalLocked()
		return true
	case RunPhasePaused:
		return true
	default:
		return false
	}
}

func (c *RunController) WaitPaused(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		c.mu.Lock()
		switch c.phase {
		case RunPhaseRunning:
			c.mu.Unlock()
			return nil
		case RunPhasePausing:
			c.mu.Unlock()
			if !c.EnterPaused() {
				continue
			}
		case RunPhasePaused:
			resumeCh := c.resumeCh
			doneCh := c.doneCh
			c.mu.Unlock()
			select {
			case <-resumeCh:
			case <-doneCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		case RunPhaseDone:
			err := c.err
			c.mu.Unlock()
			if err != nil {
				return err
			}
			return context.Canceled
		}
	}
}

func (c *RunController) Checkpoint(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	switch c.Phase() {
	case RunPhaseRunning:
		return nil
	case RunPhasePausing, RunPhasePaused:
		c.EnterPaused()
		return c.WaitPaused(ctx)
	case RunPhaseDone:
		err := c.Err()
		if err != nil {
			return err
		}
		return context.Canceled
	default:
		return nil
	}
}

func (c *RunController) Stop(err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.phase == RunPhaseDone {
		c.mu.Unlock()
		return
	}
	wasPaused := c.phase == RunPhasePausing || c.phase == RunPhasePaused
	c.phase = RunPhaseDone
	if err != nil || c.err == nil {
		c.err = err
	}
	close(c.doneCh)
	timers := c.timers
	c.signalLocked()
	if wasPaused {
		close(c.resumeCh)
		c.resumeCh = make(chan struct{})
	}
	c.mu.Unlock()
	if timers != nil {
		timers.Stop()
	}
}

func (c *RunController) Err() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *RunController) signalLocked() {
	select {
	case c.commandCh <- struct{}{}:
	default:
	}
}

type RunHandle struct {
	controller *RunController
	debugger   Debugger
	done       <-chan struct{}
	errMu      sync.Mutex
	err        error
}

func NewRunHandle(controller *RunController, dbg Debugger, done <-chan struct{}) *RunHandle {
	return &RunHandle{controller: controller, debugger: dbg, done: done}
}

func (h *RunHandle) ID() uint64 {
	if h == nil || h.controller == nil {
		return 0
	}
	return h.controller.ID()
}

func (h *RunHandle) Pause(reason PauseReason) error {
	if h == nil || h.controller == nil {
		return errors.New("missing run controller")
	}
	if ok := h.controller.RequestPause(reason); !ok {
		return errors.New("run is not pausable")
	}
	return nil
}

func (h *RunHandle) Resume() error {
	if h == nil || h.controller == nil {
		return errors.New("missing run controller")
	}
	if ok := h.controller.Resume(); !ok {
		return errors.New("run is not paused")
	}
	return nil
}

func (h *RunHandle) Continue() error {
	return h.Resume()
}

func (h *RunHandle) StepInto() error {
	if h == nil || h.debugger == nil {
		return errors.New("run has no debugger session")
	}
	h.debugger.RequestStep(h.ID())
	if err := h.Resume(); err != nil {
		h.debugger.ClearStep(h.ID())
		return err
	}
	return nil
}

func (h *RunHandle) Phase() RunPhase {
	if h == nil || h.controller == nil {
		return RunPhaseDone
	}
	return h.controller.Phase()
}

func (h *RunHandle) Wait() error {
	if h == nil {
		return nil
	}
	if h.done != nil {
		<-h.done
	}
	h.errMu.Lock()
	defer h.errMu.Unlock()
	return h.err
}

func (h *RunHandle) setResult(err error) {
	if h == nil {
		return
	}
	h.errMu.Lock()
	h.err = err
	h.errMu.Unlock()
}
