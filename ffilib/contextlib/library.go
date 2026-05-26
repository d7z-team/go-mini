package contextlib

import "gopkg.d7z.net/go-mini/core/surface"

func Surface() *surface.Bundle {
	return surface.Merge(
		SurfaceModule(&ModuleHost{}),
		SurfaceTimer(),
		surface.Library("context", surface.GoFile("context.mgo", contextSource)),
	)
}

const contextSource = `
package context

import internal "context/internal"
import "time"

type Context interface {
	Deadline() (*time.Time, bool)
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}

type CancelFunc func()

var Canceled = internal.Canceled()
var DeadlineExceeded = internal.DeadlineExceeded()

type canceler interface {
	Done() <-chan struct{}
	cancel(err error)
}

type childRegistrar interface {
	addChild(child canceler) bool
	removeChild(child canceler)
}

type rootCtx struct {
}

type cancelCtx struct {
	parent Context
	done chan struct{}
	err error
	canceled bool
	children []canceler
}

type timerCtx struct {
	base *cancelCtx
	deadline *time.Time
	timer *internal.Timer
}

type valueCtx struct {
	parent Context
	key any
	val any
}

var background = &rootCtx{}
var todo = &rootCtx{}

func Background() Context {
	return background
}

func TODO() Context {
	return todo
}

func WithCancel(parent Context) (Context, CancelFunc) {
	if parent == nil {
		parent = Background()
	}
	c, cancel := newCancelContext(parent)
	linkCancel(parent, c)
	var ctx Context = c
	return ctx, cancel
}

func WithDeadline(parent Context, deadline *time.Time) (Context, CancelFunc) {
	if parent == nil {
		parent = Background()
	}
	if deadline == nil {
		return WithCancel(parent)
	}
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		return WithCancel(parent)
	}

	return newTimerContext(parent, deadline)
}

func WithTimeout(parent Context, timeout int64) (Context, CancelFunc) {
	if parent == nil {
		parent = Background()
	}
	deadline := time.Now().Add(timeout)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		return WithCancel(parent)
	}

	return newTimerContext(parent, deadline)
}

func WithValue(parent Context, key any, val any) Context {
	if parent == nil {
		parent = Background()
	}
	if key == nil {
		panic("nil context key")
	}
	return &valueCtx{parent: parent, key: key, val: val}
}

func newCancelContext(parent Context) (*cancelCtx, CancelFunc) {
	c := &cancelCtx{parent: parent, done: make(chan struct{})}
	cancel := func() {
		c.cancel(Canceled)
	}
	var cancelFunc CancelFunc = cancel
	return c, cancelFunc
}

func newTimerContext(parent Context, deadline *time.Time) (Context, CancelFunc) {
	base := &cancelCtx{parent: parent, done: make(chan struct{})}
	c := &timerCtx{
		base: base,
		deadline: deadline,
	}
	cancel := func() {
		c.cancel(Canceled)
	}
	if err := parent.Err(); err != nil {
		c.cancel(err)
	} else if timeout := time.Until(deadline); timeout <= 0 {
		c.cancel(DeadlineExceeded)
	} else {
		c.timer = internal.NewTimer(timeout)
		linkCancel(parent, c)
		go waitTimer(c)
	}
	var ctx Context = c
	var cancelFunc CancelFunc = cancel
	return ctx, cancelFunc
}

func cancelContext(c *cancelCtx, err error) {
	if c.canceled {
		return
	}
	c.canceled = true
	c.err = err
	close(c.done)
	children := c.children
	c.children = nil
	for _, child := range children {
		child.cancel(err)
	}
}

func cancelTimerContext(c *timerCtx, err error) {
	cancelContext(c.base, err)
	if c.timer != nil {
		c.timer.Stop()
	}
}

func (c *rootCtx) Deadline() (*time.Time, bool) {
	return nil, false
}

func (c *rootCtx) Done() <-chan struct{} {
	var done <-chan struct{}
	return done
}

func (c *rootCtx) Err() error {
	var err error
	return err
}

func (c *rootCtx) Value(key any) any {
	var val any
	return val
}

func (c *cancelCtx) Deadline() (*time.Time, bool) {
	deadline, ok := c.parent.Deadline()
	return deadline, ok
}

func (c *cancelCtx) Done() <-chan struct{} {
	return c.done
}

func (c *cancelCtx) Err() error {
	return c.err
}

func (c *cancelCtx) Value(key any) any {
	return c.parent.Value(key)
}

func (c *cancelCtx) cancel(err error) {
	unlinkCancel(c.parent, c)
	cancelContext(c, err)
}

func (c *cancelCtx) addChild(child canceler) bool {
	if c.canceled {
		child.cancel(c.err)
		return true
	}
	c.children = append(c.children, child)
	return true
}

func (c *cancelCtx) removeChild(child canceler) {
	for i, existing := range c.children {
		if existing == child {
			c.children = append(c.children[:i], c.children[i+1:]...)
			return
		}
	}
}

func (c *timerCtx) Deadline() (*time.Time, bool) {
	return c.deadline, true
}

func (c *timerCtx) Done() <-chan struct{} {
	return c.base.done
}

func (c *timerCtx) Err() error {
	return c.base.err
}

func (c *timerCtx) Value(key any) any {
	return c.base.parent.Value(key)
}

func (c *timerCtx) cancel(err error) {
	unlinkCancel(c.base.parent, c)
	cancelTimerContext(c, err)
}

func (c *timerCtx) addChild(child canceler) bool {
	return c.base.addChild(child)
}

func (c *timerCtx) removeChild(child canceler) {
	c.base.removeChild(child)
}

func (c *valueCtx) Deadline() (*time.Time, bool) {
	deadline, ok := c.parent.Deadline()
	return deadline, ok
}

func (c *valueCtx) Done() <-chan struct{} {
	return c.parent.Done()
}

func (c *valueCtx) Err() error {
	return c.parent.Err()
}

func (c *valueCtx) Value(key any) any {
	if c.key == key {
		return c.val
	}
	return c.parent.Value(key)
}

func (c *valueCtx) addChild(child canceler) bool {
	if registrar, ok := c.parent.(childRegistrar); ok {
		return registrar.addChild(child)
	}
	return false
}

func (c *valueCtx) removeChild(child canceler) {
	if registrar, ok := c.parent.(childRegistrar); ok {
		registrar.removeChild(child)
	}
}

func linkCancel(parent Context, child canceler) {
	if err := parent.Err(); err != nil {
		child.cancel(err)
		return
	}
	if registrar, ok := parent.(childRegistrar); ok && registrar.addChild(child) {
		return
	}
	if parent.Done() == nil {
		return
	}
	go propagateCancel(parent, child)
}

func unlinkCancel(parent Context, child canceler) {
	if registrar, ok := parent.(childRegistrar); ok {
		registrar.removeChild(child)
	}
}

func propagateCancel(parent Context, child canceler) {
	done := parent.Done()
	if done == nil {
		return
	}
	select {
	case <-done:
		child.cancel(parent.Err())
	case <-child.Done():
	}
}

func waitTimer(c *timerCtx) {
	if c.timer.Wait() {
		c.cancel(DeadlineExceeded)
	}
}
`
