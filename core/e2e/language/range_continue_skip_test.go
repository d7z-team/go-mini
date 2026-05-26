package tests

import (
	"context"
	"fmt"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/testsurface"
)

type mockContinueBridge struct{}

func (b *mockContinueBridge) Call(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	reader := ffigo.NewReader(req.Args)
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	switch req.MethodID {
	case 1:
		val := reader.ReadVarint()
		buf.WriteBool(val >= 3)
		return buf.Bytes(), nil
	case 2:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected method id %d", req.MethodID)
	}
}

func (b *mockContinueBridge) Invoke(ctx context.Context, req *ffigo.FFICallRequest) (ffigo.FFIReturn, error) {
	return nil, fmt.Errorf("unexpected invoke: %s", req.Method)
}

func (b *mockContinueBridge) DestroyHandle(handle uint32) error {
	return nil
}

// TestRangeContinueSkipBody 验证 for-range 中 continue 能正确跳过循环体剩余代码。
func TestRangeContinueSkipBody(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

func main() {
	arr := []any{2, 3, 4, 5, 6}
	count := 0

	for _, v := range arr {
		if v > 4 {
			continue
		}
		count = count + 1
	}

	if count != 3 {
		panic("expected count=3, got " + count)
	}
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
}

// TestRangeContinueInsideIfBlock 测试 continue 在 if 块内部时能否正确跳过循环体
func TestRangeContinueInsideIfBlock(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
package main

func main() {
	arr := []any{1, 2, 3, 4, 5}
	sum := 0

	for _, v := range arr {
		if v > 3 {
			continue
		}
		sum = sum + v
	}

	if sum != 6 {
		panic("expected sum=6 (1+2+3), got " + sum)
	}
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
}

// TestRangeContinueWithFFICall 测试 for-range 中 continue 后跟随 FFI 调用时，
// continue 能否正确跳过 FFI 调用。模拟 1.mgo 中 continue 后执行
// item1.Locator(...).TextContent() 和 page.Eval(...) 等 FFI 调用的场景。
func TestRangeContinueWithFFICall(t *testing.T) {
	executor := engine.NewMiniExecutor()

	bridge := &mockContinueBridge{}
	testsurface.UseRoutes(t, executor, bridge,
		testsurface.Route("mock.ShouldContinue", 1, runtime.MustParseRuntimeFuncSig("function(Int64) Bool"), ""),
		testsurface.Route("mock.DoFFICall", 2, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), ""),
	)

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "mock"

func main() {
	items := []any{1, 2, 3, 4, 5}
	count := 0

	for _, item := range items {
		if mock.ShouldContinue(item) {
			continue
		}
		mock.DoFFICall(item)
		count = count + 1
	}

	if count != 2 {
		panic("expected count=2, got " + count)
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// TestRangeContinueWithFFICallInIfBlock 模拟 1.mgo 更精确的模式：
// continue 在 if 块内，后续还有嵌套 if true 块包含 FFI 调用。
func TestRangeContinueWithFFICallInIfBlock(t *testing.T) {
	executor := engine.NewMiniExecutor()

	bridge := &mockContinueBridge{}
	testsurface.UseRoutes(t, executor, bridge,
		testsurface.Route("mock.ShouldContinue", 1, runtime.MustParseRuntimeFuncSig("function(Int64) Bool"), ""),
		testsurface.Route("mock.DoFFICall", 2, runtime.MustParseRuntimeFuncSig("function(Int64) Void"), ""),
	)

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "mock"

func main() {
	items := []any{1, 2, 3, 4, 5}
	count := 0

	for _, item := range items {
		if mock.ShouldContinue(item) {
			continue
		}

		mock.DoFFICall(item)
		count = count + 1
	}

	if count != 2 {
		panic("expected count=2, got " + count)
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}

	err = prog.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
