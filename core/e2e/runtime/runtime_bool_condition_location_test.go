package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func assertBoolConditionRuntimeErrorAtLine(t *testing.T, filename, code string, wantLine int) {
	t.Helper()

	executor := engine.MustNewMiniExecutor()
	prog, err := executor.NewRuntimeByGoFile(filename, code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = prog.Execute(context.Background())
	if err == nil {
		t.Fatal("expected bool-condition runtime failure")
	}

	var vmErr *runtime.VMError
	if !errors.As(err, &vmErr) {
		t.Fatalf("expected *runtime.VMError, got (%T) %v", err, err)
	}
	if len(vmErr.Frames) == 0 {
		t.Fatalf("expected stack frames, got %#v", vmErr)
	}
	if vmErr.Frames[0].Filename != filename {
		t.Fatalf("expected frame filename %s, got %q", filename, vmErr.Frames[0].Filename)
	}
	if vmErr.Frames[0].Line != wantLine {
		t.Fatalf("expected condition failure at line %d, got frame %+v", wantLine, vmErr.Frames[0])
	}
	if !strings.Contains(vmErr.Message, "dereference") && !strings.Contains(vmErr.Message, "nil") && !strings.Contains(vmErr.Message, "Bool") {
		t.Fatalf("expected bool-condition failure message, got: %v", vmErr.Message)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%s:%d:", filename, wantLine)) {
		t.Fatalf("expected formatted error to contain source location, got: %v", err)
	}
}

func TestNilPointerIfConditionCarriesSourceLine(t *testing.T) {
	code := `package main
func main() {
var flag *Bool
if *flag {
}
}`

	assertBoolConditionRuntimeErrorAtLine(t, "bool_if_nil_test.mgo", code, 4)
}

func TestNilPointerForConditionCarriesSourceLine(t *testing.T) {
	code := `package main
func main() {
var flag *Bool
for ; *flag; {
}
}`

	assertBoolConditionRuntimeErrorAtLine(t, "bool_for_nil_test.mgo", code, 4)
}

func TestNilPointerSwitchCaseCarriesSourceLine(t *testing.T) {
	code := `package main
func main() {
var flag *Bool
switch {
case *flag:
}
}`

	assertBoolConditionRuntimeErrorAtLine(t, "bool_switch_nil_test.mgo", code, 5)
}
