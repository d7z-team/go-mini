package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestInterfaceMultiReturn(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	
	func main() {
		obj := make(map[String]Any)
		obj["Open"] = func(name String) (Int64, String) {
			if name == "ok" {
				return 42, ""
			}
			return 0, "file not found"
		}
		
		var i interface{Open(String) (Int64, String)} = obj
		
		// Case 1: Success
		fd, err := i.Open("ok")
		if err != "" {
			panic("Open(ok) failed: " + err)
		}
		if fd != 42 {
			panic("Open(ok) returned wrong fd")
		}
		
		// Case 2: Failure
		fd2, err2 := i.Open("bad")
		if err2 != "file not found" {
			panic("Open(bad) returned wrong error: " + err2)
		}
		if fd2 != 0 {
			panic("Open(bad) returned non-zero fd")
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
}

func TestInterfaceErrorToStringMapping(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
	code := `
	package main
	import "errors"
	func main() {
		obj := make(map[String]Any)
		obj["Close"] = func() error {
			return errors.New("closed with error")
		}
		
		// interface use 'error'
		var i interface{Close() error} = obj
		
		err := i.Close()
		if err == nil || err.Error() != "closed with error" {
			panic("Close() returned wrong error")
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
}
