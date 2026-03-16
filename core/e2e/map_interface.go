package e2e

import "context"

type MapTest interface {
	EchoMap(ctx context.Context, m map[string]string) (map[string]string, error)
	GetMap(ctx context.Context) (map[string]int64, error)
	ProcessMap(ctx context.Context, m map[string]int64) (int64, error)
	EchoIntMap(ctx context.Context, m map[int64]string) (map[int64]string, error)
}
