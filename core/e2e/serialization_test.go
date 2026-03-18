package e2e

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestCompileAndExecuteSeparation(t *testing.T) {
	// 阶段 1：在节点 A 进行编译和序列化
	compiler := engine.NewMiniExecutor()
	code := `
		package main
		
		func compute() int {
			sum := 0
			for i := 0; i < 100; i++ {
				sum += i
			}
			return sum
		}

		func main() {
			res := compute()
			if res != 4950 {
				panic("compute failed")
			}
		}
	`

	progA, err := compiler.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Server A compilation failed: %v", err)
	}

	// 将编译好的蓝图导出为 JSON 字节流 (模拟网络传输或数据库存储)
	jsonPayload, err := progA.MarshalJSON()
	if err != nil {
		t.Fatalf("Failed to marshal AST to JSON: %v", err)
	}

	// 阶段 2：在节点 B 接收 JSON 并高并发执行
	executorB := engine.NewMiniExecutor()

	// 从 JSON 数据直接恢复为可执行的 MiniProgram 蓝图
	progB, err := executorB.NewRuntimeByJSON(jsonPayload)
	if err != nil {
		t.Fatalf("Server B failed to load program from JSON: %v", err)
	}

	// 验证：开启高并发执行恢复出的蓝图
	const goroutines = 50
	var wg sync.WaitGroup
	var successCount atomic.Int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 这里的 Execute 在内部会创建独立的 StackContext
			err := progB.Execute(context.Background())
			if err == nil {
				successCount.Add(1)
			} else {
				t.Errorf("Concurrent execution failed: %v", err)
			}
		}()
	}

	wg.Wait()

	if successCount.Load() != goroutines {
		t.Fatalf("Expected %d successful executions, got %d", goroutines, successCount.Load())
	}
}
