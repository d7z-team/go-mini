package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestMethodValueExtractionPreservesReceiverBinding(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
		package main
		import "os"
		func main() {
			f, err := os.Create("method_val.txt")
			if err != nil { panic(err) }

			writeFn := f.Write
			writeFn([]byte("Hello Method Value"))
			f.Close()

			data, err1 := os.ReadFile("method_val.txt")
			if err1 != nil { panic(err1) }
			if string(data) != "Hello Method Value" {
				panic("method value call failed: " + string(data))
			}
			os.Remove("method_val.txt")
		}
		`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("method value test failed: %v", err)
	}
}
