package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type VMChannel struct {
	mu       sync.Mutex
	elem     RuntimeType
	cap      int
	queue    []*Var
	closed   bool
	recvq    []*channelRecvWaiter
	sendq    []*channelSendWaiter
	waiters  map[uint64]channelWaiterKind
	endpoint ffigo.ChannelEndpoint
}

type channelWaiterKind uint8

const (
	channelWaitRecv channelWaiterKind = iota + 1
	channelWaitSend
)

type channelRecvWaiter struct {
	token   uint64
	deliver func(*Var, bool, string)
	wake    func(uint64) bool
}

type channelSendWaiter struct {
	token uint64
	value *Var
	ack   func(string)
	wake  func(uint64) bool
}

func NewVMChannel(elem RuntimeType, capacity int) *VMChannel {
	if capacity < 0 {
		capacity = 0
	}
	return &VMChannel{
		elem:    elem,
		cap:     capacity,
		queue:   make([]*Var, 0, capacity),
		waiters: make(map[uint64]channelWaiterKind),
	}
}

func NewExternalVMChannel(elem RuntimeType, endpoint ffigo.ChannelEndpoint) *VMChannel {
	ch := NewVMChannel(elem, 0)
	ch.endpoint = endpoint
	return ch
}

func (ch *VMChannel) ElemType() RuntimeType {
	if ch == nil {
		return RuntimeType{}
	}
	return ch.elem
}

func (ch *VMChannel) Cap() int {
	if ch == nil {
		return 0
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.cap
}

func (ch *VMChannel) Len() int {
	if ch == nil {
		return 0
	}
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return len(ch.queue)
}

func (ch *VMChannel) Endpoint() ffigo.ChannelEndpoint {
	if ch == nil {
		return nil
	}
	return ch.endpoint
}

func (ch *VMChannel) Close() error {
	if ch == nil {
		return errors.New("close of nil channel")
	}
	ch.mu.Lock()
	if ch.closed {
		ch.mu.Unlock()
		return errors.New("close of closed channel")
	}
	ch.closed = true
	recvq := ch.recvq
	sendq := ch.sendq
	endpoint := ch.endpoint
	ch.recvq = nil
	ch.sendq = nil
	for _, waiter := range recvq {
		delete(ch.waiters, waiter.token)
	}
	for _, waiter := range sendq {
		delete(ch.waiters, waiter.token)
	}
	ch.mu.Unlock()

	for _, waiter := range recvq {
		waiter.deliver(zeroVarForRuntimeType(ch.elem), false, "")
		waiter.wake(waiter.token)
	}
	for _, waiter := range sendq {
		waiter.ack("send on closed channel")
		waiter.wake(waiter.token)
	}
	if endpoint != nil {
		return endpoint.Close()
	}
	return nil
}

func (ch *VMChannel) TryRecv() (*Var, bool, bool, string) {
	if ch == nil {
		return nil, false, false, ""
	}
	ch.mu.Lock()
	if len(ch.queue) > 0 {
		val := ch.queue[0]
		copy(ch.queue, ch.queue[1:])
		ch.queue[len(ch.queue)-1] = nil
		ch.queue = ch.queue[:len(ch.queue)-1]
		acks := ch.fillBufferFromSendersLocked()
		ch.mu.Unlock()
		wakeSenders(acks)
		return val, true, true, ""
	}
	if len(ch.sendq) > 0 {
		waiter := ch.sendq[0]
		ch.sendq[0] = nil
		ch.sendq = ch.sendq[1:]
		delete(ch.waiters, waiter.token)
		ch.mu.Unlock()
		waiter.ack("")
		waiter.wake(waiter.token)
		return waiter.value, true, true, ""
	}
	if ch.closed {
		ch.mu.Unlock()
		return zeroVarForRuntimeType(ch.elem), false, true, ""
	}
	ch.mu.Unlock()
	return nil, false, false, ""
}

func (ch *VMChannel) TrySend(value *Var) (bool, string) {
	if ch == nil {
		return false, ""
	}
	if !ch.elem.IsEmpty() && !ch.elem.IsAny() {
		source := runtimeTypeForAssignment(value)
		if source.IsEmpty() || source.IsAny() || !source.IsAssignableTo(ch.elem) {
			return true, "channel send type mismatch"
		}
	}
	ch.mu.Lock()
	if ch.closed {
		ch.mu.Unlock()
		return true, "send on closed channel"
	}
	value = cloneVarForAssign(value)
	if len(ch.recvq) > 0 {
		waiter := ch.recvq[0]
		ch.recvq[0] = nil
		ch.recvq = ch.recvq[1:]
		delete(ch.waiters, waiter.token)
		ch.mu.Unlock()
		waiter.deliver(value, true, "")
		waiter.wake(waiter.token)
		return true, ""
	}
	if len(ch.queue) < ch.cap {
		ch.queue = append(ch.queue, value)
		ch.mu.Unlock()
		return true, ""
	}
	ch.mu.Unlock()
	return false, ""
}

func (ch *VMChannel) AddRecvWaiter(waiter *channelRecvWaiter) {
	if ch == nil || waiter == nil {
		return
	}
	ch.mu.Lock()
	if len(ch.queue) > 0 {
		val := ch.queue[0]
		copy(ch.queue, ch.queue[1:])
		ch.queue[len(ch.queue)-1] = nil
		ch.queue = ch.queue[:len(ch.queue)-1]
		acks := ch.fillBufferFromSendersLocked()
		ch.mu.Unlock()
		waiter.deliver(val, true, "")
		wakeSenders(acks)
		waiter.wake(waiter.token)
		return
	}
	if len(ch.sendq) > 0 {
		sender := ch.sendq[0]
		ch.sendq[0] = nil
		ch.sendq = ch.sendq[1:]
		delete(ch.waiters, sender.token)
		ch.mu.Unlock()
		waiter.deliver(sender.value, true, "")
		sender.ack("")
		sender.wake(sender.token)
		waiter.wake(waiter.token)
		return
	}
	if ch.closed {
		ch.mu.Unlock()
		waiter.deliver(zeroVarForRuntimeType(ch.elem), false, "")
		waiter.wake(waiter.token)
		return
	}
	ch.recvq = append(ch.recvq, waiter)
	ch.waiters[waiter.token] = channelWaitRecv
	ch.mu.Unlock()
}

func (ch *VMChannel) AddSendWaiter(waiter *channelSendWaiter) {
	if ch == nil || waiter == nil {
		return
	}
	ch.mu.Lock()
	if ch.closed {
		ch.mu.Unlock()
		waiter.ack("send on closed channel")
		waiter.wake(waiter.token)
		return
	}
	waiter.value = cloneVarForAssign(waiter.value)
	if len(ch.recvq) > 0 {
		receiver := ch.recvq[0]
		ch.recvq[0] = nil
		ch.recvq = ch.recvq[1:]
		delete(ch.waiters, receiver.token)
		ch.mu.Unlock()
		receiver.deliver(waiter.value, true, "")
		receiver.wake(receiver.token)
		waiter.ack("")
		waiter.wake(waiter.token)
		return
	}
	if len(ch.queue) < ch.cap {
		ch.queue = append(ch.queue, waiter.value)
		ch.mu.Unlock()
		waiter.ack("")
		waiter.wake(waiter.token)
		return
	}
	ch.sendq = append(ch.sendq, waiter)
	ch.waiters[waiter.token] = channelWaitSend
	ch.mu.Unlock()
}

func (ch *VMChannel) RemoveWaiter(token uint64) {
	if ch == nil || token == 0 {
		return
	}
	ch.mu.Lock()
	kind, ok := ch.waiters[token]
	if !ok {
		ch.mu.Unlock()
		return
	}
	delete(ch.waiters, token)
	if kind == channelWaitRecv || kind == channelWaitSend {
		for i := 0; i < len(ch.recvq); {
			waiter := ch.recvq[i]
			if waiter != nil && waiter.token == token {
				ch.recvq = append(ch.recvq[:i], ch.recvq[i+1:]...)
				continue
			}
			i++
		}
		for i := 0; i < len(ch.sendq); {
			waiter := ch.sendq[i]
			if waiter != nil && waiter.token == token {
				ch.sendq = append(ch.sendq[:i], ch.sendq[i+1:]...)
				continue
			}
			i++
		}
	}
	ch.mu.Unlock()
}

func (ch *VMChannel) fillBufferFromSendersLocked() []*channelSendWaiter {
	if ch.cap == 0 || ch.closed {
		return nil
	}
	acks := make([]*channelSendWaiter, 0)
	for len(ch.queue) < ch.cap && len(ch.sendq) > 0 {
		waiter := ch.sendq[0]
		ch.sendq[0] = nil
		ch.sendq = ch.sendq[1:]
		delete(ch.waiters, waiter.token)
		ch.queue = append(ch.queue, waiter.value)
		acks = append(acks, waiter)
	}
	return acks
}

func wakeSenders(waiters []*channelSendWaiter) {
	for _, waiter := range waiters {
		if waiter == nil {
			continue
		}
		waiter.ack("")
		waiter.wake(waiter.token)
	}
}

func asVMChannel(v *Var) (*VMChannel, bool) {
	if v == nil || v.VType != TypeChannel {
		return nil, false
	}
	ch, ok := v.Ref.(*VMChannel)
	return ch, ok && ch != nil
}

func channelWaitHandle(ch *VMChannel, token uint64, reason string) ffigo.WaitHandle {
	return ffigo.NewWaitHandle(ffigo.WaitDependsOnVM, reason, func() {
		if ch != nil {
			ch.RemoveWaiter(token)
		}
	})
}

var externalChannelWaiterToken uint64

func nextExternalChannelToken() uint64 {
	return (uint64(1) << 63) | atomic.AddUint64(&externalChannelWaiterToken, 1)
}

func (ch *VMChannel) RecvExternal(ctx context.Context) (*Var, bool, error) {
	if ch == nil {
		return nil, false, errors.New("receive from nil channel")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if value, ok, ready, errText := ch.TryRecv(); ready {
		if errText != "" {
			return nil, false, errors.New(errText)
		}
		return value, ok, nil
	}
	token := nextExternalChannelToken()
	done := make(chan struct{})
	var once sync.Once
	var value *Var
	var recvOK bool
	var errText string
	waiter := &channelRecvWaiter{
		token: token,
		deliver: func(v *Var, ok bool, text string) {
			value = v
			recvOK = ok
			errText = text
			once.Do(func() { close(done) })
		},
		wake: func(uint64) bool { return true },
	}
	ch.AddRecvWaiter(waiter)
	select {
	case <-done:
	case <-ctx.Done():
		ch.RemoveWaiter(token)
		select {
		case <-done:
		default:
			return nil, false, ctx.Err()
		}
	}
	if errText != "" {
		return nil, false, errors.New(errText)
	}
	return value, recvOK, nil
}

func (ch *VMChannel) SendExternal(ctx context.Context, value *Var) error {
	if ch == nil {
		return errors.New("send on nil channel")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ready, errText := ch.TrySend(value); ready {
		if errText != "" {
			return errors.New(errText)
		}
		return nil
	}
	token := nextExternalChannelToken()
	done := make(chan struct{})
	var once sync.Once
	var errText string
	waiter := &channelSendWaiter{
		token: token,
		value: cloneVarForAssign(value),
		ack: func(text string) {
			errText = text
			once.Do(func() { close(done) })
		},
		wake: func(uint64) bool { return true },
	}
	ch.AddSendWaiter(waiter)
	select {
	case <-done:
	case <-ctx.Done():
		ch.RemoveWaiter(token)
		select {
		case <-done:
		default:
			return ctx.Err()
		}
	}
	if errText != "" {
		return errors.New(errText)
	}
	return nil
}

func zeroVarForRuntimeType(typ RuntimeType) *Var {
	switch {
	case typ.IsVoid():
		return nil
	case typ.IsAny():
		return NewVarWithRuntimeType(typ, TypeAny)
	case typ.IsInt():
		v := NewInt(0)
		v.SetRuntimeType(typ)
		return v
	case typ.Raw == SpecFloat64:
		v := NewFloat(0)
		v.SetRuntimeType(typ)
		return v
	case typ.IsString():
		v := NewString("")
		v.SetRuntimeType(typ)
		return v
	case typ.IsBool():
		v := NewBool(false)
		v.SetRuntimeType(typ)
		return v
	case typ.Raw == SpecBytes:
		v := NewBytes(nil)
		v.SetRuntimeType(typ)
		return v
	case typ.IsArray():
		return &Var{TypeInfo: typ, VType: TypeArray}
	case typ.IsMap():
		return &Var{TypeInfo: typ, VType: TypeMap}
	case typ.IsChan():
		return &Var{TypeInfo: typ, VType: TypeChannel}
	case typ.IsHostRef():
		return NewVarWithRuntimeType(typ, TypeHostRef)
	case typ.IsPtr():
		return NewVarWithRuntimeType(typ, TypePointer)
	case typ.IsInterface():
		return NewVarWithRuntimeType(typ, TypeInterface)
	case typ.Raw == SpecClosure:
		return NewVarWithRuntimeType(typ, TypeClosure)
	case typ.Kind == RuntimeTypeTuple:
		return &Var{TypeInfo: typ, VType: TypeArray, Ref: &VMArray{Data: make([]*Var, len(typ.Params))}}
	case typ.Kind == RuntimeTypeStruct:
		fields := make([]*Slot, len(typ.Fields))
		byName := make(map[string]int, len(typ.Fields))
		for i, field := range typ.Fields {
			fields[i] = NewSlot(field.TypeInfo, zeroVarForRuntimeType(field.TypeInfo))
			byName[field.Name] = i
		}
		return &Var{TypeInfo: typ, VType: TypeStruct, Ref: &VMStruct{Spec: &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: typ.Fields}, Fields: fields, ByName: byName}}
	default:
		return NewVarWithRuntimeType(typ, TypeAny)
	}
}
