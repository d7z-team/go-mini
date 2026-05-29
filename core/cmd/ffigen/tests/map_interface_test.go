//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg tests -out ffi_map_ffigen_test.go map_interface_test.go
package tests

import "context"

// ffigen:module ffigen_test
type MapTest interface {
	EchoMap(ctx context.Context, m map[string]string) (map[string]string, error)
	GetMap(ctx context.Context) (map[string]int64, error)
	ProcessMap(ctx context.Context, m map[string]int64) (int64, error)
	EchoIntMap(ctx context.Context, m map[int64]string) (map[int64]string, error)
}
