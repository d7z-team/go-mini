package tests

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

	t.Run("NilMatching", func(t *testing.T) {
		code := `
		package main
		
		func isNil(v Any) Bool {
			switch v.(type) {
			case nil:
				return true
			default:
				return false
			}
		}

		func main() {
			var empty Any
			if !isNil(nil) { panic("nil should be nil") }
			if !isNil(empty) { panic("uninitialized Any should be nil") }
			if isNil(10) { panic("10 is not nil") }
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

	t.Run("InterfaceEmbedding", func(t *testing.T) {
		code := `
		package main
		
		type Reader interface {
			Read() String
		}

		type Writer interface {
			Write(String)
		}

		type ReadWriter interface {
			Reader
			Writer
		}

		var writeLog String

		func main() {
			obj := make(map[String]Any)
			obj["Read"] = func() String { return "data" }
			obj["Write"] = func(s String) { 
				writeLog = s
			}
			
			var rw ReadWriter = obj
			if rw.Read() != "data" { panic("read failed") }
			rw.Write("hello")
			if writeLog != "hello" { panic("write failed") }
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
