package ffigo

import (
	"sync"
	"sync/atomic"
)

// HandleRegistry manages the mapping between uint32 IDs and Go object instances.
type HandleRegistry struct {
	mu      sync.RWMutex
	handles map[uint32]interface{}
	nextID  uint32
}

// ManagedHandle is a wrapper for a handle ID.
type ManagedHandle struct {
	ID       uint32
	registry *HandleRegistry
}

func (h *ManagedHandle) Release() {
	if h.ID != 0 && h.registry != nil {
		h.registry.Remove(h.ID)
		h.ID = 0
	}
}

// NewHandleRegistry creates a new handle registry.
func NewHandleRegistry() *HandleRegistry {
	return &HandleRegistry{
		handles: make(map[uint32]interface{}),
		nextID:  1,
	}
}

// Register adds an object to the registry and returns its unique ID.
func (r *HandleRegistry) Register(obj interface{}) uint32 {
	if obj == nil {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	id := atomic.AddUint32(&r.nextID, 1)
	r.handles[id] = obj
	return id
}

// Get retrieves an object by its ID.
func (r *HandleRegistry) Get(id uint32) (interface{}, bool) {
	if id == 0 {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	obj, ok := r.handles[id]
	return obj, ok
}

// Remove deletes a handle entry.
func (r *HandleRegistry) Remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handles, id)
}
