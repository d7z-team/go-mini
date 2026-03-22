package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

type MapTestHost struct{}

func (h *MapTestHost) EchoMap(ctx context.Context, m map[string]string) (map[string]string, error) {
	return m, nil
}

func (h *MapTestHost) GetMap(ctx context.Context) (map[string]int64, error) {
	return map[string]int64{"a": 1, "b": 2}, nil
}

func (h *MapTestHost) ProcessMap(ctx context.Context, m map[string]int64) (int64, error) {
	var sum int64
	for _, v := range m {
		sum += v
	}
	return sum, nil
}

func (h *MapTestHost) EchoIntMap(ctx context.Context, m map[int64]string) (map[int64]string, error) {
	return m, nil
}

func TestFFIMap(t *testing.T) {
	executor := engine.NewMiniExecutor()
	host := &MapTestHost{}

	RegisterE2EMapTestLibrary(executor, "e2e", host, nil)

	code := `
	package main
	import "e2e"
	func main() {
		m1 := map[string]string{ "hello": "world", "foo": "bar" }
		m2, err := e2e.EchoMap(m1)
		if err != "" { panic(err) }
		if m2["hello"] != "world" {
			panic("m2[hello] mismatch")
		}

		m3, err1 := e2e.GetMap()
		if err1 != "" { panic(err1) }
		if m3["a"] != 1 || m3["b"] != 2 {
			panic("m3 mismatch")
		}

		s, err2 := e2e.ProcessMap(m3)
		if err2 != "" { panic(err2) }
		if s != 3 {
			panic("sum mismatch")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
