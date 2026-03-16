package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTimeLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "time"
	import "fmt"

	func main() {
		start := time.Now()
		fmt.Println("Starting sleep...")
		
		// Sleep 100ms (100 * 1000 * 1000 ns)
		time.Sleep(100 * 1000 * 1000)
		
		elapsed := time.Since(start)
		fmt.Println("Finished sleep, elapsed ns:", elapsed)
		
		if elapsed < 100*1000*1000 {
			panic("sleep too short")
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
