package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestContextCancellation(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
	package main
	func main() {
		a := 0
		for i := 0; i < 2000000000; i = i + 1 {
			a = i
		}
	}
	`
	prog, _ := executor.NewRuntimeByGoCode(code)

	// 创建一个带超时的 Context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := prog.Execute(ctx)
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: (%T) %v", err, err)
	}
}
