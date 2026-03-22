package ffigo

import (
	"sync"
	"sync/atomic"
)

// HandleRegistry manages the mapping between uint32 IDs and Go object instances for FFI crossing.
type HandleRegistry struct {
	mu      sync.RWMutex
	handles map[uint32]interface{}
	reverse map[interface{}]uint32 // Reverse lookup for identity (deduplication)
	nextID  uint32
}

// ManagedHandle is a wrapper for a handle ID that uses a finalizer to
// notify the registry when the handle is no longer referenced in the VM.
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
		reverse: make(map[interface{}]uint32),
		nextID:  1, // 0 is reserved for invalid/nil handles
	}
}

// Register adds an object to the registry and returns its unique ID.
// If the object is already registered and is comparable, it returns the existing ID.
func (r *HandleRegistry) Register(obj interface{}) uint32 {
	if obj == nil {
		return 0
	}

	// For comparable types, check if we already have an ID
	isComparable := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				isComparable = false
			}
		}()
		_ = interface{}(obj) == interface{}(obj)
		isComparable = true
	}()

	r.mu.Lock()
	defer r.mu.Unlock()

	if isComparable {
		if id, ok := r.reverse[obj]; ok {
			return id
		}
	}

	id := atomic.AddUint32(&r.nextID, 1)
	r.handles[id] = obj
	if isComparable {
		r.reverse[obj] = id
	}
	return id
}

// AllocateManaged creates a ManagedHandle for an ID.
// The caller is responsible for ensuring the finalizer is set if needed.
func (r *HandleRegistry) AllocateManaged(id uint32) *ManagedHandle {
	if id == 0 {
		return nil
	}
	return &ManagedHandle{ID: id, registry: r}
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

// Remove deletes an object from the registry by its ID.
func (r *HandleRegistry) Remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if obj, ok := r.handles[id]; ok {
		delete(r.reverse, obj)
		delete(r.handles, id)
	}
}
