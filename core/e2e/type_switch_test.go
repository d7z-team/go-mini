package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTypeSwitch(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	t.Run("BasicTypes", func(t *testing.T) {
		code := `
		package main
		
		func check(v Any) String {
			switch v.(type) {
			case Int64:
				return "int"
			case String:
				return "string"
			case Bool:
				return "bool"
			default:
				return "unknown"
			}
		}

		func main() {
			if check(10) != "int" { panic("10 is int") }
			if check("hello") != "string" { panic("hello is string") }
			if check(true) != "bool" { panic("true is bool") }
			if check(1.5) != "unknown" { panic("1.5 is unknown") }
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

	t.Run("InterfaceMatch", func(t *testing.T) {
		code := `
		package main
		
		type Reader interface {
			Read() String
		}

		func check(v Any) String {
			switch v.(type) {
			case Reader:
				return "reader"
			default:
				return "not reader"
			}
		}

		func main() {
			r := make(map[String]Any)
			r["Read"] = func() String { return "ok" }
			
			if check(r) != "reader" { panic("map should be reader") }
			if check(10) != "not reader" { panic("int is not reader") }
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

	t.Run("WithAssignment", func(t *testing.T) {
		code := `
		package main
		
		func format(v Any) String {
			switch x := v.(type) {
			case Int64:
				return "int:" + String(x)
			case String:
				return "str:" + x
			default:
				return "other"
			}
		}

		func main() {
			if format(123) != "int:123" { panic("format int failed: " + format(123)) }
			if format("abc") != "str:abc" { panic("format str failed") }
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
