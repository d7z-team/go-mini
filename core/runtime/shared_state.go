package runtime

import (
	"errors"
	"sync"
)

type sharedInitState uint8

const (
	sharedInitUninitialized sharedInitState = iota
	sharedInitInitializing
	sharedInitReady
)

type SharedState struct {
	mu             sync.RWMutex
	cond           *sync.Cond
	globals        map[string]*Slot
	moduleCache    map[string]*Var
	loadingModules map[string]bool
	moduleWaiters  map[string][]moduleWaiter
	initState      sharedInitState
}

type ModuleLoadState uint8

const (
	ModuleLoadReady ModuleLoadState = iota
	ModuleLoadStart
	ModuleLoadWait
)

type moduleWaiter struct {
	ExecutionContext *VMExecutionContext
	Frame            *ExecutionContextFrame
	Resume           Task
}

type SharedStateSnapshot struct {
	initialized    bool
	globals        map[string]*Var
	moduleCache    map[string]*Var
	loadingModules map[string]bool
}

func NewSharedState() *SharedState {
	state := &SharedState{
		globals:        make(map[string]*Slot),
		moduleCache:    make(map[string]*Var),
		loadingModules: make(map[string]bool),
		moduleWaiters:  make(map[string][]moduleWaiter),
	}
	state.cond = sync.NewCond(&state.mu)
	return state
}

func (s *SharedStateSnapshot) IsInitialized() bool {
	if s == nil {
		return false
	}
	return s.initialized
}

func (s *SharedStateSnapshot) LoadGlobal(name string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.globals[name]
	if !ok || v == nil {
		return nil, ok
	}
	return v.DeepCopy(), true
}

func (s *SharedStateSnapshot) HasGlobal(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.globals[name]
	return ok
}

func (s *SharedStateSnapshot) HasModule(path string) bool {
	if s == nil {
		return false
	}
	_, ok := s.moduleCache[path]
	return ok
}

func (s *SharedStateSnapshot) Module(path string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.moduleCache[path]
	if !ok || v == nil {
		return nil, ok
	}
	return v.DeepCopy(), true
}

func (s *SharedStateSnapshot) IsModuleLoading(path string) bool {
	if s == nil {
		return false
	}
	return s.loadingModules[path]
}

func (s *SharedStateSnapshot) Globals() map[string]*Var {
	if s == nil {
		return nil
	}
	out := make(map[string]*Var, len(s.globals))
	for k, v := range s.globals {
		if v != nil {
			out[k] = v.DeepCopy()
		} else {
			out[k] = nil
		}
	}
	return out
}

func (s *SharedStateSnapshot) ModuleCache() map[string]*Var {
	if s == nil {
		return nil
	}
	out := make(map[string]*Var, len(s.moduleCache))
	for k, v := range s.moduleCache {
		if v != nil {
			out[k] = v.DeepCopy()
		} else {
			out[k] = nil
		}
	}
	return out
}

func (s *SharedStateSnapshot) LoadingModules() map[string]bool {
	if s == nil {
		return nil
	}
	out := make(map[string]bool, len(s.loadingModules))
	for k, v := range s.loadingModules {
		out[k] = v
	}
	return out
}

func (s *SharedState) IsInitialized() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initState == sharedInitReady
}

func (s *SharedState) BeginInitialization() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.initState == sharedInitInitializing {
		s.cond.Wait()
	}
	if s.initState == sharedInitReady {
		return false
	}
	s.initState = sharedInitInitializing
	return true
}

func (s *SharedState) FinishInitialization(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.initState = sharedInitReady
	} else {
		s.initState = sharedInitUninitialized
	}
	if s.cond != nil {
		s.cond.Broadcast()
	}
}

func (s *SharedState) Snapshot() *SharedStateSnapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	globals := make(map[string]*Var, len(s.globals))
	for k, slot := range s.globals {
		if slot != nil && slot.Value != nil {
			globals[k] = slot.Value.DeepCopy()
		}
	}
	moduleCache := make(map[string]*Var, len(s.moduleCache))
	for k, v := range s.moduleCache {
		moduleCache[k] = v.DeepCopy()
	}
	loadingModules := make(map[string]bool, len(s.loadingModules))
	for k, v := range s.loadingModules {
		loadingModules[k] = v
	}
	return &SharedStateSnapshot{
		initialized:    s.initState == sharedInitReady,
		globals:        globals,
		moduleCache:    moduleCache,
		loadingModules: loadingModules,
	}
}

func (s *SharedState) LoadGlobal(name string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.globals[name]
	if v == nil {
		return nil, ok
	}
	return v.Value, ok
}

func (s *SharedState) HasGlobal(name string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.globals[name]
	return ok
}

func (s *SharedState) StoreGlobal(name string, v *Var) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v == nil {
		s.globals[name] = NewSlot(MustParseRuntimeType("Any"), NewVarWithRuntimeType(MustParseRuntimeType("Any"), TypeAny))
		return
	}
	s.globals[name] = NewSlot(v.RuntimeType(), v)
}

func (ctx *StackContext) InitGlobal(name string, kind RuntimeType, expr *Var) error {
	if ctx == nil || ctx.Shared == nil {
		return errors.New("missing shared state for global init")
	}
	if kind.IsEmpty() {
		kind = MustParseRuntimeType("Any")
	}
	return ctx.Shared.UpdateGlobal(name, func(current *Slot, exists bool) (*Slot, error) {
		var value *Var
		var err error
		if expr == nil {
			if ctx.Executor != nil {
				value, err = ctx.Executor.initializeType(ctx, kind, 0)
			} else {
				value, err = nilValueForType(kind)
			}
		} else {
			value, err = ctx.prepareAssignedValue(kind, expr)
		}
		if err != nil {
			return nil, err
		}
		if exists && current != nil {
			current.Decl = kind
			current.Value = value
			return current, nil
		}
		return NewSlot(kind, value), nil
	})
}

func (s *SharedState) UpdateGlobal(name string, fn func(current *Slot, exists bool) (*Slot, error)) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.globals[name]
	next, err := fn(current, ok)
	if err != nil {
		return err
	}
	if next == nil {
		delete(s.globals, name)
		return nil
	}
	s.globals[name] = next
	return nil
}

func (s *SharedState) CaptureGlobalSlot(name string) (*Slot, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.globals[name]
	return v, ok
}

func (s *SharedState) ApplyEnv(env map[string]*Var) {
	if s == nil || len(env) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range env {
		s.globals[k] = NewSlot(v.RuntimeType(), cloneVarForAssign(v))
	}
}

func (s *SharedState) Module(path string) (*Var, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.moduleCache[path]
	return v, ok
}

func (s *SharedState) HasModule(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.moduleCache[path]
	return ok
}

func (s *SharedState) StoreModule(path string, v *Var) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moduleCache[path] = v
	if s.cond != nil {
		s.cond.Broadcast()
	}
}

func (s *SharedState) DeleteModule(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.moduleCache, path)
}

func (s *SharedState) IsModuleLoading(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadingModules[path]
}

func (s *SharedState) SetModuleLoading(path string, loading bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if loading {
		s.loadingModules[path] = true
		if s.cond != nil {
			s.cond.Broadcast()
		}
		return
	}
	delete(s.loadingModules, path)
	if s.cond != nil {
		s.cond.Broadcast()
	}
}

func (s *SharedState) BeginModuleLoad(path string) (*Var, ModuleLoadState) {
	if s == nil {
		return nil, ModuleLoadWait
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.moduleCache[path]; ok {
		return v, ModuleLoadReady
	}
	if !s.loadingModules[path] {
		s.loadingModules[path] = true
		return nil, ModuleLoadStart
	}
	return nil, ModuleLoadWait
}

func (s *SharedState) AddModuleWaiter(path string, waiter moduleWaiter) {
	if s == nil || waiter.ExecutionContext == nil || waiter.Frame == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moduleWaiters[path] = append(s.moduleWaiters[path], waiter)
}

func (s *SharedState) finishModuleLoad(path string, v *Var) []moduleWaiter {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v != nil {
		s.moduleCache[path] = v
	}
	delete(s.loadingModules, path)
	waiters := append([]moduleWaiter(nil), s.moduleWaiters[path]...)
	delete(s.moduleWaiters, path)
	if s.cond != nil {
		s.cond.Broadcast()
	}
	return waiters
}

func (s *SharedState) cancelModuleLoads() []moduleWaiter {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := make([]moduleWaiter, 0)
	for _, items := range s.moduleWaiters {
		waiters = append(waiters, items...)
	}
	s.loadingModules = make(map[string]bool)
	s.moduleWaiters = make(map[string][]moduleWaiter)
	if s.cond != nil {
		s.cond.Broadcast()
	}
	return waiters
}
