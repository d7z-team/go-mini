//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out reverse_ffigen_test.go reverse_interface.go
package tests

import "context"

// ffigen:reverse
type ScriptCalculator interface {
	Add(a, b int64) int64
	Format(prefix string, val int64) string
	Divide(a, b int64) (int64, error)
	Log(ctx context.Context, msg string) string
	Join(prefix string, values ...string) string
	AcceptPoint(p ReversePoint) int64
}

type ReversePoint struct {
	X int64
	Y int64
}
