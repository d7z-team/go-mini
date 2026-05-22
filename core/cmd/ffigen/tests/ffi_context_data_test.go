package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

type ctxKey string

type ContextMockImpl struct{}

func (m *ContextMockImpl) WithContext(ctx context.Context, key string) string {
	val := ctx.Value(ctxKey(key))
	if val == nil {
		return "not found"
	}
	return val.(string)
}

func (m *ContextMockImpl) WithoutContext(val string) string {
	return "prefix:" + val
}

func TestFFIContextData(t *testing.T) {
	executor := engine.NewMiniExecutor()
	mock := &ContextMockImpl{}
	RegisterContextMock(executor, mock, nil)

	code := `
	package main
	import "ctx_test"
	func main() {
		r1 := ctx_test.WithContext("user")
		if r1 != "dragon" { panic("WithContext failed: " + r1) }
		
		r2 := ctx_test.WithoutContext("hello")
		if r2 != "prefix:hello" { panic("WithoutContext failed") }
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	// 注入带有数据的 context
	ctx := context.WithValue(context.Background(), ctxKey("user"), "dragon")
	err = prog.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
}
