package tests

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestStrictMapKeyValidation(t *testing.T) {
	e := engine.NewMiniExecutor()
	code := `package main
		func main() {
			m := make(map[string]int64)
			return m[1]
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Error("Expected validation error for map key mismatch (int key for string map), but got nil")
	}
}

func TestInvalidCapArgument(t *testing.T) {
	e := engine.NewMiniExecutor()
	code := `package main
		func main() {
			return cap(123)
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Error("Expected validation error for cap(int), but got nil")
	}
}

func TestErrorToStringAssignmentValidation(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.DeclareFuncSchema("getErr", runtime.MustParseRuntimeFuncSig("function() Error"))

	code := `package main
		func main() string {
			var s string
			s = getErr()
			return s
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err != nil {
		t.Errorf("Expected Error to String assignment to pass validation, but got: %v", err)
	}
}
