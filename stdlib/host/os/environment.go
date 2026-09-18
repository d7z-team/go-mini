package oshost

import (
	"context"
	"errors"
	"strings"

	"github.com/d7z-team/mini-go/rpc"
)

// Environment supplies immutable environment lookups.
type Environment interface {
	LookupEnv(key string) (string, bool)
}

type environmentProviderBinding struct{ environment Environment }

func (binding environmentProviderBinding) Lookup(_ context.Context, key string) (string, bool, error) {
	value, found := binding.environment.LookupEnv(key)
	return value, found, nil
}

// NewEnvironmentProvider exposes environment through the generated os RPC contract.
func NewEnvironmentProvider(environment Environment) (rpc.Provider, error) {
	if environment == nil {
		return nil, errors.New("environment is required")
	}
	return NewOsEnvironmentProvider(environmentProviderBinding{environment: environment})
}

// Map is an in-memory environment.
type Map map[string]string

func (environment Map) LookupEnv(key string) (string, bool) {
	value, ok := environment[key]
	return value, ok
}

// Snapshot returns an immutable environment captured from os.Environ
// style key=value entries.
func Snapshot(entries []string) Map {
	environment := make(Map, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			environment[key] = value
		}
	}
	return environment
}
