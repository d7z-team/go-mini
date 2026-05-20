package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInterfaceAssignmentClonesStructReceiverForVTable(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	type Box struct {
		V int
	}

	func (b Box) Get() int {
		return b.V
	}

	type Getter interface {
		Get() int
	}

	func main() {
		b := Box{V: 1}
		var g Getter = b
		b.V = 2
		if g.Get() != 1 {
			panic("interface method read mutated source struct")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestInterfaceReassignmentRebindsClonedVTableReceiver(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main

	type Box struct {
		V int
	}

	func (b Box) Get() int {
		return b.V
	}

	type Getter interface {
		Get() int
	}

	func main() {
		b := Box{V: 1}
		var g Getter = b
		var h Getter = g
		b.V = 2
		if h.Get() != 1 {
			panic("interface reassignment vtable receiver did not follow cloned target")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
