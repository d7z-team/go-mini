package tests

import (
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
