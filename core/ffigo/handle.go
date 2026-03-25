package ffigo

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type auditEntry struct {
	ID        uint32
	DeletedAt time.Time
}

// HandleRegistry manages the mapping between uint32 IDs and Go object instances.
type HandleRegistry struct {
	mu      sync.RWMutex
	handles map[uint32]interface{}
	nextID  uint32

	// 审计追踪：记录最近 100 个被删除的句柄
	recentDeletions []auditEntry
}

// ... (ManagedHandle and Release remain unchanged)

// NewHandleRegistry creates a new handle registry.
func NewHandleRegistry() *HandleRegistry {
	return &HandleRegistry{
		handles:         make(map[uint32]interface{}),
		nextID:          1,
		recentDeletions: make([]auditEntry, 0, 100),
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
	obj, ok := r.handles[id]
	r.mu.RUnlock()
	return obj, ok
}

// GetWithAudit 提供更详细的错误原因
func (r *HandleRegistry) GetWithAudit(id uint32) (interface{}, error) {
	obj, ok := r.Get(id)
	if ok {
		return obj, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.recentDeletions {
		if entry.ID == id {
			return nil, fmt.Errorf("handle ID %d was deleted at %v (likely GC'd or session ended)", id, entry.DeletedAt.Format("15:04:05.000"))
		}
	}
	return nil, fmt.Errorf("handle ID %d was never registered or already purged from audit", id)
}

// Remove deletes a handle entry.
func (r *HandleRegistry) Remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handles[id]; ok {
		delete(r.handles, id)
		// 维持 100 个审计记录
		if len(r.recentDeletions) >= 100 {
			r.recentDeletions = r.recentDeletions[1:]
		}
		r.recentDeletions = append(r.recentDeletions, auditEntry{ID: id, DeletedAt: time.Now()})
	}
}
