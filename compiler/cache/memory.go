package cache

import "errors"

// MemoryConfig bounds combined action/output storage. Zero values use defaults.
type MemoryConfig struct {
	MaxEntries int
	MaxBytes   int64
}

// MemoryStats describes retained storage; Bytes includes logical entry overhead.
type MemoryStats struct {
	Entries   int
	Bytes     int64
	Evictions uint64
}

type memoryOrderEntry struct {
	key    string
	output bool
}

// MemoryBackend is a bounded FIFO store owned by the caller. Clear releases
// cached data without affecting any Cache's in-flight action locks.
type MemoryBackend struct {
	mu        cacheMutex
	actions   map[string]Entry
	outputs   map[string][]byte
	config    MemoryConfig
	order     []memoryOrderEntry
	bytes     int64
	evictions uint64
}

func NewMemoryBackend() Backend {
	return NewMemoryBackendWithConfig(MemoryConfig{})
}

func NewMemoryBackendWithConfig(config MemoryConfig) *MemoryBackend {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 4096
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = 256 << 20
	}
	return &MemoryBackend{config: config, actions: make(map[string]Entry), outputs: make(map[string][]byte)}
}

func (b *MemoryBackend) GetAction(id ActionID) (Entry, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	entry, ok := b.actions[id.String()]
	return entry, ok, nil
}

func (b *MemoryBackend) GetOutput(id OutputID) ([]byte, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	value, ok := b.outputs[id.String()]
	return append([]byte(nil), value...), ok, nil
}

func (b *MemoryBackend) PutOutput(id OutputID, data []byte) error {
	if OutputIDFor(data) != id {
		return errors.New("cache output does not match its content identifier")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := id.String()
	if _, ok := b.outputs[key]; ok {
		return nil
	}
	size := int64(len(data)) + 128
	if size > b.config.MaxBytes {
		return nil
	}
	b.outputs[key] = append([]byte(nil), data...)
	b.order = append(b.order, memoryOrderEntry{key: key, output: true})
	b.bytes += size
	b.evictLocked()
	return nil
}

func (b *MemoryBackend) PutAction(id ActionID, entry Entry) error {
	if entry.Size < 0 {
		return errors.New("negative cache output size")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := id.String()
	if _, ok := b.actions[key]; !ok {
		b.order = append(b.order, memoryOrderEntry{key: key})
		b.bytes += 128
	}
	b.actions[key] = entry
	b.evictLocked()
	return nil
}

func (b *MemoryBackend) evictLocked() {
	for len(b.actions)+len(b.outputs) > b.config.MaxEntries || b.bytes > b.config.MaxBytes {
		oldest := b.order[0]
		b.order[0] = memoryOrderEntry{}
		b.order = b.order[1:]
		if oldest.output {
			b.bytes -= int64(len(b.outputs[oldest.key])) + 128
			delete(b.outputs, oldest.key)
		} else {
			b.bytes -= 128
			delete(b.actions, oldest.key)
		}
		b.evictions++
	}
}

func (b *MemoryBackend) Stats() MemoryStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return MemoryStats{Entries: len(b.actions) + len(b.outputs), Bytes: b.bytes, Evictions: b.evictions}
}

func (b *MemoryBackend) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions = make(map[string]Entry)
	b.outputs = make(map[string][]byte)
	b.order = nil
	b.bytes = 0
}
