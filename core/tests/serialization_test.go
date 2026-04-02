package engine_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestCompileAndExecuteSeparation(t *testing.T) {
	// 阶段 1：在节点 A 进行编译和 bytecode 序列化
	sourceCompiler := engine.NewMiniExecutor()
	sourceProgram := `
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

	// 将编译好的产物导出为 bytecode JSON 字节流 (模拟网络传输或数据库存储)
	bytecodeJSON, err := sourceCompiler.CompileGoCodeToBytecodeJSON(sourceProgram)
	if err != nil {
		t.Fatalf("Failed to marshal bytecode to JSON: %v", err)
	}

	// 阶段 2：在节点 B 接收 JSON 并高并发执行
	bytecodeLoader := engine.NewMiniExecutor()

	// 从 bytecode JSON 数据直接恢复为可执行的 MiniProgram 蓝图
	loadedProgram, err := bytecodeLoader.NewRuntimeByBytecodeJSON(bytecodeJSON)
	if err != nil {
		t.Fatalf("Server B failed to load program from bytecode JSON: %v", err)
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
			err := loadedProgram.Execute(context.Background())
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
