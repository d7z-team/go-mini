package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestOperators(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "fmt"

	func main() {
		// 1. Modulo
		if 10 % 3 != 1 { panic("10 % 3 != 1") }
		if 10 % 2 != 0 { panic("10 % 2 != 0") }

		// 2. Bitwise
		if (1 & 1) != 1 { panic("1 & 1") }
		if (1 & 0) != 0 { panic("1 & 0") }
		if (1 | 0) != 1 { panic("1 | 0") }
		if (1 ^ 1) != 0 { panic("1 ^ 1") }
		if (1 ^ 0) != 1 { panic("1 ^ 0") }

		// 3. Shifts
		if (1 << 3) != 8 { panic("1 << 3") }
		if (8 >> 2) != 2 { panic("8 >> 2") }

		// 4. Unary Bitwise NOT
		if ^1 != -2 { panic("^1") }

		fmt.Println("Operator tests passed")
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

type IntMapHost struct{}

func (h *IntMapHost) EchoMap(ctx context.Context, m map[string]string) (map[string]string, error) { return m, nil }
func (h *IntMapHost) GetMap(ctx context.Context) (map[string]int64, error) { return nil, nil }
func (h *IntMapHost) ProcessMap(ctx context.Context, m map[string]int64) (int64, error) { return 0, nil }
func (h *IntMapHost) EchoIntMap(ctx context.Context, m map[int64]string) (map[int64]string, error) {
	return m, nil
}

func TestIntKeyMap(t *testing.T) {
	executor := engine.NewMiniExecutor()
	host := &IntMapHost{}
	RegisterE2EMapTestLibrary(executor, "e2e", host, nil)

	code := `
	package main
	import "e2e"
	func main() {
		m1 := map[Int64]String{ "1": "one", "2": "two" }
		m2 := e2e.EchoIntMap(m1)
		if m2.val[1] != "one" || m2.val[2] != "two" {
			panic("m2 mismatch")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
