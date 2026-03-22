package e2e

import (
	"context"
	"os"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestStdlibInjection(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "os"
	import "fmt"

	func main() {
		fmt.Println("Testing stdlib injection...")
		
		fileName := "test_stdlib.txt"
		content := "Hello from Go-Mini Stdlib!"
		
		// Test OS WriteFile
		errW := os.WriteFile(fileName, []byte(content))
		if errW != "" {
			panic("write failed: " + errW)
		}
		
		// Test OS ReadFile
		data, errR := os.ReadFile(fileName)
		if errR != "" {
			panic("read failed: " + errR)
		}
		
		if string(data) != content {
			panic("content mismatch")
		}
		
		// Cleanup
		errRm := os.Remove(fileName)
		if errRm != "" {
			panic("remove failed: " + errRm)
		}
		fmt.Println("Stdlib test completed successfully.")
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

	// Verify file is actually gone
	if _, err := os.Stat("test_stdlib.txt"); !os.IsNotExist(err) {
		t.Error("test file was not removed")
	}
}
