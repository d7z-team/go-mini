package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestPointerSemantics(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("Basic Dereference and Assignment", func(t *testing.T) {
		code := `
		package main
		import "fmt"
		func main() {
			p := new(Int64)
			*p = 123
			if *p != 123 {
				panic("pointer assignment failed")
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
	})

	t.Run("Struct Pointer Member Access", func(t *testing.T) {
		code := `
		package main
		type Point struct {
			X Int64
			Y Int64
		}
		func main() {
			p := new(Point)
			p.X = 10
			p.Y = 20
			if p.X != 10 || p.Y != 20 {
				panic("struct pointer member assignment failed")
			}
			
			// Test dereference of struct pointer
			p2 := *p
			if p2.X != 10 {
				panic("struct dereference failed")
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
	})

	t.Run("Pointer to Pointer", func(t *testing.T) {
		code := `
		package main
		func main() {
			val := 456
			p := new(Int64)
			*p = val
			
			p2 := new(*Int64)
			*p2 = p
			
			if **p2 != 456 {
				panic("nested dereference failed")
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
	})
}
