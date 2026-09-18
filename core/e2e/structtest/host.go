//go:generate go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg structtest -out ffigen.go host.go
package structtest

import "context"

// ffigen:module calc
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
// ffigen:methods
type Table struct {
	Values map[string]string
}

func (t *Table) SetString(row, col int, val string) {
	if t.Values == nil {
		t.Values = make(map[string]string)
	}
	key := string(rune('A'+row)) + ":" + string(rune('A'+col))
	t.Values[key] = val
}

func (t *Table) GetString(row, col int) string {
	if t.Values == nil {
		return ""
	}
	key := string(rune('A'+row)) + ":" + string(rune('A'+col))
	return t.Values[key]
}

// ffigen:module calc
type Factory struct{}

func (f *Factory) New(base int64) *Calculator {
	return &Calculator{Base: base}
}

func (f *Factory) NewTable() *Table {
	return &Table{Values: make(map[string]string)}
}
