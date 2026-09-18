package runtime

import (
	"sync"
	"time"
)

type VMClock struct {
	mu       sync.Mutex
	baseHost time.Time
	baseVM   time.Time
	paused   bool
	frozen   time.Time
}

func NewVMClock() *VMClock {
	now := time.Now()
	return &VMClock{
		baseHost: now,
		baseVM:   now,
		frozen:   now,
	}
}

func (c *VMClock) Now() time.Time {
	if c == nil {
		return time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused {
		return c.frozen
	}
	return c.baseVM.Add(time.Since(c.baseHost))
}

func (c *VMClock) Pause() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused {
		return
	}
	c.frozen = c.nowLocked()
	c.paused = true
}

func (c *VMClock) Resume() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.paused {
		return
	}
	now := time.Now()
	c.baseHost = now
	c.baseVM = c.frozen
	c.paused = false
}

func (c *VMClock) IsPaused() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

func (c *VMClock) Since(t time.Time) time.Duration {
	if c == nil {
		return time.Since(t)
	}
	return c.Now().Sub(t)
}

func (c *VMClock) Until(t time.Time) time.Duration {
	if c == nil {
		return time.Until(t)
	}
	return t.Sub(c.Now())
}

func (c *VMClock) nowLocked() time.Time {
	if c.paused {
		return c.frozen
	}
	return c.baseVM.Add(time.Since(c.baseHost))
}
