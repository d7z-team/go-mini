package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

type HostService interface {
	Close() error
	Shutdown(context.Context) error
}

type VMCallbackResult struct {
	Values []*Var
	Err    error
}

type VMEventKind uint8

const (
	VMEventHostCallback VMEventKind = iota + 1
	VMEventAsyncFFIComplete
)

type VMEvent struct {
	Kind VMEventKind
	Data any
}

type VMEventLoop struct {
	mu     sync.Mutex
	events []VMEvent
	head   int
	wake   chan struct{}
}

type VMHostCallbackEvent struct {
	Context  context.Context
	Callable *Var
	Sig      *RuntimeFuncSig
	Args     []*Var
	Reply    chan VMCallbackResult
}

type VMAsyncFFICompleteEvent struct {
	Token uint64
	Ret   []byte
	Err   error
}

func NewVMEventLoop() *VMEventLoop {
	return &VMEventLoop{wake: make(chan struct{}, 1)}
}

func (l *VMEventLoop) Post(event VMEvent) error {
	if l == nil {
		return errors.New("missing VM event loop")
	}
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
	return nil
}

func (l *VMEventLoop) WakeChan() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.wake
}

func (l *VMEventLoop) Drain(handle func(VMEvent) error) error {
	if l == nil || handle == nil {
		return nil
	}
	for {
		l.mu.Lock()
		if len(l.events) == 0 {
			l.mu.Unlock()
			return nil
		}
		event := l.events[l.head]
		var zero VMEvent
		l.events[l.head] = zero
		l.head++
		if l.head == len(l.events) {
			l.events = l.events[:0]
			l.head = 0
		} else if l.head > 1024 && l.head*2 >= len(l.events) {
			copy(l.events, l.events[l.head:])
			l.events = l.events[:len(l.events)-l.head]
			l.head = 0
		}
		l.mu.Unlock()
		if err := handle(event); err != nil {
			return err
		}
	}
}

type VMCallbackProxy struct {
	reactor        *VMReactor
	target         *Var
	sig            *RuntimeFuncSig
	hostResourceID atomic.Uint64
}

func (p *VMCallbackProxy) RegisterHostService(service HostService) uint64 {
	if p == nil || p.reactor == nil {
		return 0
	}
	return p.reactor.RegisterService(service)
}

func (p *VMCallbackProxy) UnregisterHostService(id uint64) {
	if p != nil && p.reactor != nil {
		p.reactor.UnregisterService(id)
	}
}

func (p *VMCallbackProxy) BindHostHandle(registry *ffigo.HandleRegistry, handle uint32) {
	if p == nil || p.reactor == nil {
		return
	}
	p.ReleaseHostHandle()
	p.hostResourceID.Store(p.reactor.RegisterHostHandle(registry, handle))
}

func (p *VMCallbackProxy) ReleaseHostHandle() {
	if p == nil || p.reactor == nil {
		return
	}
	id := p.hostResourceID.Swap(0)
	p.reactor.UnregisterHostHandle(id)
}

func (p *VMCallbackProxy) Invoke(ctx context.Context, args []*Var) VMCallbackResult {
	if p == nil || p.reactor == nil {
		return VMCallbackResult{Err: errors.New("missing VM callback reactor")}
	}
	return p.reactor.Invoke(ctx, p.target, p.sig, args)
}

type VMInterfaceProxy struct {
	reactor        *VMReactor
	target         *Var
	spec           *RuntimeInterfaceSpec
	hostResourceID atomic.Uint64
}

func (p *VMInterfaceProxy) BindHostHandle(registry *ffigo.HandleRegistry, handle uint32) {
	if p == nil || p.reactor == nil {
		return
	}
	p.ReleaseHostHandle()
	p.hostResourceID.Store(p.reactor.RegisterHostHandle(registry, handle))
}

func (p *VMInterfaceProxy) ReleaseHostHandle() {
	if p == nil || p.reactor == nil {
		return
	}
	id := p.hostResourceID.Swap(0)
	p.reactor.UnregisterHostHandle(id)
}

func (p *VMInterfaceProxy) InvokeMethod(ctx context.Context, method string, args []*Var) VMCallbackResult {
	if p == nil || p.reactor == nil {
		return VMCallbackResult{Err: errors.New("missing VM interface reactor")}
	}
	callable, ok := p.reactor.executor.resolveMethodValue(p.target, method)
	if !ok {
		return VMCallbackResult{Err: fmt.Errorf("VM interface method %s not found", method)}
	}
	var sig *RuntimeFuncSig
	if p.spec != nil {
		if idx, ok := p.spec.MethodIndex[method]; ok && idx < len(p.spec.Methods) {
			sig = p.spec.Methods[idx].Spec
		}
	}
	return p.reactor.Invoke(ctx, callable, sig, args)
}

type VMReactor struct {
	executor *Executor
	mu       sync.RWMutex
	run      *VMRun
}

type VMRun struct {
	Executor  *Executor
	Scheduler *ExecutionContextScheduler
	Reactor   *VMReactor
	Events    *VMEventLoop
	Hosts     *VMHostRegistry
	Resources *VMResourceScope

	Controller *RunController
}

type hostHandleResource struct {
	registry *ffigo.HandleRegistry
	handle   uint32
}

type VMHostRegistry struct {
	handles sync.Map
	nextID  atomic.Uint64
}

func NewVMHostRegistry() *VMHostRegistry {
	return &VMHostRegistry{}
}

func (r *VMHostRegistry) RegisterHandle(registry *ffigo.HandleRegistry, handle uint32) uint64 {
	if r == nil || registry == nil || handle == 0 {
		return 0
	}
	id := r.nextID.Add(1)
	r.handles.Store(id, hostHandleResource{registry: registry, handle: handle})
	return id
}

func (r *VMHostRegistry) UnregisterHandle(id uint64) {
	if r == nil || id == 0 {
		return
	}
	if value, ok := r.handles.LoadAndDelete(id); ok {
		if handle, ok := value.(hostHandleResource); ok && handle.registry != nil {
			handle.registry.Remove(handle.handle)
		}
	}
}

func (r *VMHostRegistry) Close() {
	if r == nil {
		return
	}
	r.handles.Range(func(key, value any) bool {
		if handle, ok := value.(hostHandleResource); ok && handle.registry != nil {
			handle.registry.Remove(handle.handle)
		}
		r.handles.Delete(key)
		return true
	})
}

type VMResourceScope struct {
	services sync.Map
	nextID   atomic.Uint64
}

func NewVMResourceScope() *VMResourceScope {
	return &VMResourceScope{}
}

func (s *VMResourceScope) RegisterService(service HostService) uint64 {
	if s == nil || service == nil {
		return 0
	}
	id := s.nextID.Add(1)
	s.services.Store(id, service)
	return id
}

func (s *VMResourceScope) UnregisterService(id uint64) {
	if s != nil && id != 0 {
		s.services.Delete(id)
	}
}

func (s *VMResourceScope) Close() {
	if s == nil {
		return
	}
	s.services.Range(func(key, value any) bool {
		if service, ok := value.(HostService); ok && service != nil {
			_ = service.Close()
		}
		s.services.Delete(key)
		return true
	})
}

func NewVMReactor(exec *Executor, controller *RunController) *VMReactor {
	r := &VMReactor{executor: exec}
	r.SetController(controller)
	return r
}

func NewVMRun(exec *Executor, scheduler *ExecutionContextScheduler, reactor *VMReactor, controller *RunController) *VMRun {
	return &VMRun{
		Executor:   exec,
		Scheduler:  scheduler,
		Reactor:    reactor,
		Events:     NewVMEventLoop(),
		Hosts:      NewVMHostRegistry(),
		Resources:  NewVMResourceScope(),
		Controller: controller,
	}
}

func (r *VMReactor) SetRun(run *VMRun) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.run = run
}

func (r *VMReactor) SetController(controller *RunController) {
	if r == nil {
		return
	}
	if controller == nil {
		r.SetRun(nil)
		return
	}
	r.SetRun(NewVMRun(r.executor, nil, r, controller))
}

func (e *Executor) currentRun() *VMRun {
	if e == nil {
		return nil
	}
	return e.activeRun
}

func (e *Executor) currentScheduler() *ExecutionContextScheduler {
	if run := e.currentRun(); run != nil {
		return run.Scheduler
	}
	return nil
}

func (e *Executor) currentReactor() *VMReactor {
	if run := e.currentRun(); run != nil {
		return run.Reactor
	}
	return nil
}

func (r *VMReactor) currentRun() *VMRun {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.run
}

func (r *VMReactor) RegisterService(service HostService) uint64 {
	run := r.currentRun()
	if run == nil || run.Resources == nil {
		return 0
	}
	return run.Resources.RegisterService(service)
}

func (r *VMReactor) UnregisterService(id uint64) {
	if run := r.currentRun(); run != nil && run.Resources != nil {
		run.Resources.UnregisterService(id)
	}
}

func (r *VMReactor) RegisterHostHandle(registry *ffigo.HandleRegistry, handle uint32) uint64 {
	run := r.currentRun()
	if run == nil || run.Hosts == nil {
		return 0
	}
	return run.Hosts.RegisterHandle(registry, handle)
}

func (r *VMReactor) UnregisterHostHandle(id uint64) {
	if run := r.currentRun(); run != nil && run.Hosts != nil {
		run.Hosts.UnregisterHandle(id)
	}
}

func (r *VMReactor) CloseServices() {
	if r == nil {
		return
	}
	if run := r.currentRun(); run != nil {
		if run.Resources != nil {
			run.Resources.Close()
		}
		if run.Hosts != nil {
			run.Hosts.Close()
		}
	}
}

func (r *VMReactor) NewCallbackProxy(target *Var, sig *RuntimeFuncSig) *VMCallbackProxy {
	if r == nil || target == nil {
		return nil
	}
	return &VMCallbackProxy{reactor: r, target: cloneVarForAssign(target), sig: CloneRuntimeFuncSig(sig)}
}

func (r *VMReactor) NewInterfaceProxy(target *Var, spec *RuntimeInterfaceSpec) *VMInterfaceProxy {
	if r == nil || target == nil {
		return nil
	}
	return &VMInterfaceProxy{reactor: r, target: cloneVarForAssign(target), spec: spec}
}

func (r *VMReactor) Invoke(ctx context.Context, callable *Var, sig *RuntimeFuncSig, args []*Var) VMCallbackResult {
	if r == nil || r.executor == nil {
		return VMCallbackResult{Err: errors.New("missing VM reactor")}
	}
	if callable == nil {
		return VMCallbackResult{Err: errors.New("missing VM callback")}
	}
	run := r.currentRun()
	if run == nil || run.Events == nil {
		return VMCallbackResult{Err: errors.New("VM callback event loop is not initialized")}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if run.Controller != nil {
		ctx = ContextWithRunController(ctx, run.Controller)
	}
	done := make(chan VMCallbackResult, 1)
	if err := run.Events.Post(VMEvent{Kind: VMEventHostCallback, Data: &VMHostCallbackEvent{
		Context:  ctx,
		Callable: cloneVarForAssign(callable),
		Sig:      CloneRuntimeFuncSig(sig),
		Args:     append([]*Var(nil), args...),
		Reply:    done,
	}}); err != nil {
		return VMCallbackResult{Err: err}
	}
	select {
	case res := <-done:
		return res
	case <-ctx.Done():
		return VMCallbackResult{Err: ctx.Err()}
	}
}

func (e *Executor) handleVMEvent(event VMEvent) error {
	switch event.Kind {
	case VMEventHostCallback:
		data, _ := event.Data.(*VMHostCallbackEvent)
		if data == nil {
			return errors.New("missing VM host callback event")
		}
		return e.enqueueHostCallback(data)
	case VMEventAsyncFFIComplete:
		data, _ := event.Data.(*VMAsyncFFICompleteEvent)
		if data == nil {
			return errors.New("missing VM async FFI completion event")
		}
		scheduler := e.currentScheduler()
		if scheduler == nil {
			return errors.New("missing VM scheduler for async FFI completion")
		}
		scheduler.completeWire(data.Token, data.Ret, data.Err)
		return nil
	default:
		return fmt.Errorf("unknown VM event kind %d", event.Kind)
	}
}

func (e *Executor) enqueueHostCallback(data *VMHostCallbackEvent) error {
	run := e.currentRun()
	if run == nil || run.Scheduler == nil {
		return errors.New("VM callback scheduler is not initialized")
	}
	if data.Context == nil {
		data.Context = context.Background()
	}
	session := e.NewSession(data.Context, "host-callback")
	session.Shared = e.shared
	session.StepLimit = e.stepLimit
	session.TaskStack = append(session.TaskStack, Task{
		Op: OpInvokeDirect,
		Data: &DirectCallData{
			Callable: cloneVarForAssign(data.Callable),
			Args:     append([]*Var(nil), data.Args...),
		},
	})
	frame := &ExecutionContextFrame{
		Executor: e,
		Session:  session,
		Kind:     FrameHostCallback,
		Cleanup:  true,
		OnDone: func(frame *ExecutionContextFrame) error {
			values, err := collectCallbackReturns(frame, data.Sig)
			if err != nil {
				data.Reply <- VMCallbackResult{Err: err}
				return nil
			}
			data.Reply <- VMCallbackResult{Values: values}
			return nil
		},
		OnError: func(_ *ExecutionContextFrame, err error) error {
			data.Reply <- VMCallbackResult{Err: err}
			return nil
		},
	}
	if _, err := run.Scheduler.GoFrame(frame); err != nil {
		e.CleanupSession(session)
		data.Reply <- VMCallbackResult{Err: err}
		return nil
	}
	return nil
}

func collectCallbackReturns(frame *ExecutionContextFrame, sig *RuntimeFuncSig) ([]*Var, error) {
	if sig == nil || sig.ReturnType.IsVoid() {
		return nil, nil
	}
	if frame == nil || frame.Session == nil || frame.Session.ValueStack == nil || frame.Session.ValueStack.Len() == 0 {
		return nil, errors.New("VM callback returned no value")
	}
	value := frame.Session.ValueStack.Pop()
	if sig.ReturnType.Kind != RuntimeTypeTuple {
		return []*Var{value}, nil
	}
	value = frame.Executor.unwrapValue(value)
	if value == nil || value.VType != TypeArray {
		got := TypeAny
		if value != nil {
			got = value.VType
		}
		return nil, fmt.Errorf("VM callback returned %v, want tuple %s", got, sig.ReturnType.Raw)
	}
	items := arrayRef(value).Snapshot()
	if len(items) != len(sig.ReturnType.Params) {
		return nil, fmt.Errorf("VM callback tuple return count mismatch: got %d, want %d", len(items), len(sig.ReturnType.Params))
	}
	return items, nil
}

func (e *Executor) RegisterHostService(service HostService) uint64 {
	reactor := e.currentReactor()
	if reactor == nil {
		return 0
	}
	return reactor.RegisterService(service)
}

func (e *Executor) UnregisterHostService(id uint64) {
	if reactor := e.currentReactor(); reactor != nil {
		reactor.UnregisterService(id)
	}
}

func (e *Executor) NewHostRefVar(id uint32, bridge ffigo.FFIBridge, typeName string) *Var {
	return NewHostRefVar(id, bridge, typeName)
}

func NewHostRefVar(id uint32, bridge ffigo.FFIBridge, typeName string) *Var {
	v := &Var{VType: TypeHostRef, Handle: id, Bridge: bridge}
	if id != 0 {
		v.Ref = NewVMHandle(id, bridge)
	}
	v.SetRawType(HostRefType(TypeSpec(typeName)).String())
	return v
}
