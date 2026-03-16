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

func TestFFIMap(t *testing.T) {
	executor := engine.NewMiniExecutor()
	host := &MapTestHost{}
	
	RegisterE2EMapTestLibrary(executor, "e2e", host, nil)

	code := `
	package main
	import "e2e"
	func main() {
		m1 := map[string]string{ "hello": "world", "foo": "bar" }
		m2 := e2e.EchoMap(m1)
		if m2.val["hello"] != "world" {
			panic("m2[hello] mismatch")
		}

		m3 := e2e.GetMap()
		if m3.val["a"] != 1 || m3.val["b"] != 2 {
			panic("m3 mismatch")
		}

		s := e2e.ProcessMap(m3.val)
		if s.val != 3 {
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
