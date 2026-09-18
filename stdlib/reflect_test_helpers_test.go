package stdlib_test

import (
	"context"
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler"
	minigoruntime "github.com/d7z-team/mini-go/runtime"
)

func testReflectCallableRevisionLifecycle(t *testing.T, root, sourceTemplate, label string) {
	t.Helper()
	entries := []compiler.EntryPoint{
		{Name: "install", ModulePath: root, Function: "Install"},
		{Name: "call", ModulePath: root, Function: "CallHeld"},
	}
	oldProgram := prepareStdlibProgram(t, root, strings.ReplaceAll(sourceTemplate, "VERSION", "1"), entries)
	newProgram := prepareStdlibProgram(t, root, strings.ReplaceAll(sourceTemplate, "VERSION", "2"), entries)
	instance, err := oldProgram.Instantiate(context.Background(), minigoruntime.InstanceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := instance.Close(); err != nil {
			t.Errorf("close instance: %v", err)
		}
	})
	if _, err = instance.Call(context.Background(), "install"); err != nil {
		t.Fatal(err)
	}
	plan, err := instance.PreparePatch(context.Background(), newProgram)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = instance.ApplyPatch(plan); err != nil {
		t.Fatal(err)
	}
	result, err := instance.Call(context.Background(), "call")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Values[0].Int64(); !ok || value != 1 {
		t.Fatalf("old %s result = %#v", label, result.Values)
	}
	if retained, err := instance.RetainedRevisions(context.Background()); err != nil || len(retained) != 2 {
		t.Fatalf("%s did not pin its defining revision: %#v", label, retained)
	}
	if _, err = instance.Call(context.Background(), "install"); err != nil {
		t.Fatal(err)
	}
	result, err = instance.Call(context.Background(), "call")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := result.Values[0].Int64(); !ok || value != 2 {
		t.Fatalf("new %s result = %#v", label, result.Values)
	}
	if retained, err := instance.RetainedRevisions(context.Background()); err != nil || len(retained) != 1 || retained[0].Generation != 2 {
		t.Fatalf("replaced %s retained old revision: %#v", label, retained)
	}
}
