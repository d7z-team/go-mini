package engine_test

import (
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestDisassembleIncludesExecutableSectionsAndSymbols(t *testing.T) {
	scriptSource := `
	package main
	
	var GlobalVar = 100

	func fib(n int) int {
		if n <= 1 {
			return n
		}
		return fib(n-1) + fib(n-2)
	}

	func main() {
		res := fib(5)
		if res != 5 {
			panic("fib mismatch")
		}
		if GlobalVar != 100 {
			panic("global mismatch")
		}
	}
	`
	testExecutor := engine.MustNewMiniExecutor()
	testProgram, err := testExecutor.NewRuntimeByGoCode(scriptSource)
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	disassembly := testProgram.Disassemble()
	if disassembly == "" {
		t.Fatal("Disassemble output is empty")
	}

	// 打印输出以便在测试日志中查看
	fmt.Println("--- Generated Disassembly ---") //nolint:forbidigo // allowed for test output
	fmt.Println(disassembly)                     //nolint:forbidigo // allowed for test output
	fmt.Println("--- End of Disassembly ---")    //nolint:forbidigo // allowed for test output

	// 基本内容验证
	expectedSymbols := []string{
		"section .data",
		"section .text",
		"global _start",
		"fn.fib: ; signature",
		"_start:",
		"global.GlobalVar:",
		"BINARY_OP",
		"CALL               fib",
		"RETURN",
	}

	for _, sym := range expectedSymbols {
		if !strings.Contains(disassembly, sym) {
			t.Errorf("Expected symbol %q not found in disassembly", sym)
		}
	}
}
