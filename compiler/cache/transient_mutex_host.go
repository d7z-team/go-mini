//go:build !minigo

package cache

import "sync"

type cacheMutex struct{ sync.RWMutex }
