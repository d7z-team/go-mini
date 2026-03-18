package e2e

import (
	"context"
	"sync"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestConcurrencySafety(t *testing.T) {
	executor := engine.NewMiniExecutor()

	// 一个简单的循环脚本，用于触发 stepCount
	code := `
package main
func main() {
	sum := 0
	for i := 0; i < 100; i++ {
		sum += i
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// 尝试并发执行同一个程序
			_ = prog.Execute(context.Background())
		}()
	}

	wg.Wait()
}
