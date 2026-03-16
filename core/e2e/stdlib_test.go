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
		resW := os.WriteFile(fileName, []byte(content))
		if resW.err != nil {
			panic("write failed: " + resW.err)
		}
		
		// Test OS ReadFile
		resR := os.ReadFile(fileName)
		if resR.err != nil {
			panic("read failed: " + resR.err)
		}
		data := resR.val
		
		if string(data) != content {
			panic("content mismatch")
		}
		
		// Cleanup
		resRm := os.Remove(fileName)
		if resRm.err != nil {
			panic("remove failed")
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
