//go:generate go run ../../../cmd/ffigen/main.go -pkg structtest -path gopkg.d7z.net/go-mini/core/e2e/structtest -out ffigen.go host.go
package structtest

import "context"

// ffigen:methods
type Calculator struct {
	Base int64
}

func (c *Calculator) Add(ctx context.Context, x int64) int64 {
	return c.Base + x
}

func (c *Calculator) Multiply(x, y int64) int64 {
	return x * y
}

func (c *Calculator) GetBase() int64 {
	return c.Base
}

// ffigen:module calc
type Factory struct{}

func (f *Factory) New(base int64) *Calculator {
	return &Calculator{Base: base}
}
