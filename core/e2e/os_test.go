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
			resC = os.Create("test_methods.txt")
			if resC.err != nil {
				panic(resC.err)
			}
			f = resC.val
			
			resW = f.Write([]byte("hello ffigen"))
			if resW.err != nil {
				panic(resW.err)
			}
			
			resCl = f.Close()
			if resCl.err != nil {
				panic(resCl.err)
			}

			resR = os.ReadFile("test_methods.txt")
			if resR.err != nil {
				panic(resR.err)
			}
			data = resR.val

			if string(data) != "hello ffigen" {
				panic("unexpected data: " + string(data))
			}
			
			resRm = os.Remove("test_methods.txt")
			if resRm.err != nil {
				panic(resRm.err)
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
