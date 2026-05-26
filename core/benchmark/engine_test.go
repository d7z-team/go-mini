package benchmark

import (
	"context"
	"errors"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

// 1. Fibonacci (递归性能测试 - 函数调用开销)

func fibNative(n int) int {
	if n <= 1 {
		return n
	}
	return fibNative(n-1) + fibNative(n-2)
}

func BenchmarkFibNative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fibNative(15)
	}
}

func BenchmarkFibMini(b *testing.B) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func fib(n int) int {
		if n <= 1 { return n }
		return fib(n-1) + fib(n-2)
	}
	func Run() int {
		return fib(15)
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = prog.Eval(ctx, "Run()", nil)
	}
}

// 2. Sum Loop (循环性能测试 - 指令解码与算术开销)

func sumNative(n int) int {
	res := 0
	for i := 0; i < n; i++ {
		res += i
	}
	return res
}

func BenchmarkSumNative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = sumNative(1000)
	}
}

func BenchmarkSumMini(b *testing.B) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func Run() int {
		res := 0
		for i := 0; i < 1000; i++ {
			res = res + i
		}
		return res
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = prog.Eval(ctx, "Run()", nil)
	}
}

// 3. FFI Overhead (外部调用开销)

func BenchmarkFFINative(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("hello %d", i)
	}
}

func BenchmarkFFIMini(b *testing.B) {
	executor := engine.NewMiniExecutor()
	testsurface.UseRoute(b, executor, "bench.Sprintf", benchBridge{}, 1, runtime.MustParseRuntimeFuncSig("function(String, Int64) String"), "")
	code := `
	package main
	import "bench"
	func Run() string {
		return bench.Sprintf("hello %d", 1)
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = prog.Eval(ctx, "Run()", nil)
	}
}

type benchBridge struct{}

func (benchBridge) Call(_ context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	format := reader.ReadString()
	value := reader.ReadVarint()
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)
	buf.WriteString(fmt.Sprintf(format, value))
	return append([]byte(nil), buf.Bytes()...), nil
}

func (benchBridge) Invoke(context.Context, *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, errors.New("unexpected invoke")
}

func (benchBridge) DestroyHandle(uint32) error {
	return nil
}

// 4. Eval (纯表达式模式开销 - 环境变量注入)

func BenchmarkEvalMini(b *testing.B) {
	executor := engine.NewMiniExecutor()
	ctx := context.Background()
	env := map[string]interface{}{"a": 10, "b": 20}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = executor.Eval(ctx, "a + b * 2", env)
	}
}
