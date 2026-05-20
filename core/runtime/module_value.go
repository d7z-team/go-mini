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

func (m *VMModule) Load(name string) (*Var, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.Data[name]
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
	defer m.mu.RUnlock()
	out := make(map[string]*Var, len(m.Data))
	for k, v := range m.Data {
		out[k] = v
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
