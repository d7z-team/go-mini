package runtime

import (
	"errors"
	"sync"
)

type LexicalContext struct {
	Executor *Executor
	Shared   *SharedState
	Stack    *Stack
}

type VMModule struct {
	mu      sync.RWMutex
	Name    string
	Data    map[string]*Var
	Context *LexicalContext
}

type vmModuleGlobalRef struct {
	Shared *SharedState
	Name   string
}

func (m *VMModule) Load(name string) (*Var, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.Data[name]
	if !ok || v == nil {
		return v, ok
	}
	if ref, ok := v.Ref.(*vmModuleGlobalRef); ok && ref != nil {
		return ref.Shared.LoadGlobal(ref.Name)
	}
	return v, ok
}

func (m *VMModule) Store(name string, v *Var) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Data == nil {
		m.Data = make(map[string]*Var)
	}
	m.Data[name] = v
}

func (m *VMModule) Snapshot() map[string]*Var {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	names := make([]string, 0, len(m.Data))
	for name := range m.Data {
		names = append(names, name)
	}
	m.mu.RUnlock()

	out := make(map[string]*Var, len(names))
	for _, name := range names {
		if v, ok := m.Load(name); ok {
			out[name] = v
		}
	}
	return out
}

func (lc *LexicalContext) Load(name string) (*Var, error) {
	if lc == nil {
		return nil, errors.New("missing lexical context")
	}
	return loadVarFromScope(lc.Executor, lc.Shared, lc.Stack, name)
}

func (lc *LexicalContext) Store(name string, v *Var) error {
	if lc == nil {
		return errors.New("missing lexical context")
	}
	return storeVarToScope(lc.Executor, lc.Shared, lc.Stack, name, v)
}
