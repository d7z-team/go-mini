package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestRangeTypedLocals(t *testing.T) {
	exec := engine.MustNewMiniExecutor()

	t.Run("ArrayInt64ValueKeepsNumericType", func(t *testing.T) {
		const code = `
package main

var sum Int64

func main() {
	for _, v := range []Int64{1, 2, 3} {
		sum = sum + v
	}
	if sum != 6 {
		panic("unexpected sum")
	}
}
`
		prog, err := exec.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if err := prog.Execute(context.Background()); err != nil {
			t.Fatalf("execute failed: %v", err)
		}
	})

	t.Run("MapValueKeepsNumericType", func(t *testing.T) {
		const code = `
package main

var sum Int64
var keys String

func main() {
	for k, v := range map[String]Int64{"a": 1, "b": 2} {
		sum = sum + v
		keys = keys + k
	}
	if sum != 3 {
		panic("unexpected map sum")
	}
	if len(keys) != 2 {
		panic("unexpected key collection")
	}
}
`
		prog, err := exec.NewRuntimeByGoCode(code)
		if err != nil {
			t.Fatalf("compile failed: %v", err)
		}
		if err := prog.Execute(context.Background()); err != nil {
			t.Fatalf("execute failed: %v", err)
		}
	})
}
