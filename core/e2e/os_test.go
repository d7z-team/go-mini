package e2e

import (
	"context"
	"os"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestOSMethods(t *testing.T) {
	code := `
		package main
		import "os"
		func main() {
			f, err = os.Create("test_methods.txt")
			if err != nil {
				panic(err)
			}
			
			_, err = f.Write([]byte("hello ffigen"))
			if err != nil {
				panic(err)
			}
			
			err = f.Close()
			if err != nil {
				panic(err)
			}

			data, err = os.ReadFile("test_methods.txt")
			if err != nil {
				panic(err)
			}

			if string(data) != "hello ffigen" {
				panic("unexpected data: " + string(data))
			}
			
			err = os.Remove("test_methods.txt")
			if err != nil {
				panic(err)
			}
		}
	`

	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	err = runtime.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat("test_methods.txt"); !os.IsNotExist(err) {
		t.Errorf("file test_methods.txt should have been removed")
		_ = os.Remove("test_methods.txt")
	}
}
