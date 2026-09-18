//go:build minigo

package cache

type cacheMutex struct{}

func (*cacheMutex) Lock()    {}
func (*cacheMutex) Unlock()  {}
func (*cacheMutex) RLock()   {}
func (*cacheMutex) RUnlock() {}
