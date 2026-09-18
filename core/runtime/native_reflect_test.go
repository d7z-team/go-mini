package runtime

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/reflectspec"
)

func TestNativeReflectImplementsDeclaredRoutes(t *testing.T) {
	exec, err := NewExecutorFromPrepared(&PreparedProgram{})
	if err != nil {
		t.Fatalf("new executor failed: %v", err)
	}
	session := &StackContext{Executor: exec}
	for _, decl := range reflectspec.Routes() {
		_, err := NativeReflect(exec, session, FFIRoute{Name: decl.RouteName}, nil, nil)
		if err != nil && strings.Contains(err.Error(), "unknown native reflect route") {
			t.Fatalf("missing native reflect implementation for %s", decl.RouteName)
		}
	}
}
