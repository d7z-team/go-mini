package tests

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMapKeyDynamicValidation(t *testing.T) {
	e := engine.NewMiniExecutor()
	code := `package main
		func identity(v any) any { return v }
		func main() {
			m := make(map[string]int64)
			k := identity(123)
			_ = m[k]
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected compile-time map key type rejection")
	}
	if !strings.Contains(err.Error(), "Map 键类型不匹配") {
		t.Fatalf("unexpected compile error: %v", err)
	}
}

func TestRecursionDepthLimit(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.SetMaxTypeDepth(5)

	deepType := "int64"
	for i := 0; i < 10; i++ {
		deepType = "*" + deepType
	}

	code := fmt.Sprintf(`package main
		func main() {
			var x %s
			var y int64 = x
		}`, deepType)

	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Error("Expected recursion depth error during semantic check, but got nil")
	}
}

func TestConfigurableRecursionDepth(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.SetMaxTypeDepth(100)

	deepType := "int64"
	for i := 0; i < 40; i++ {
		deepType = "*" + deepType
	}

	code := fmt.Sprintf(`package main
		func main() {
			var x %s
			var y int64 = x
		}`, deepType)

	_, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Errorf("Expected depth 40 to pass with MaxTypeDepth 100, but got: %v", err)
	}
}
