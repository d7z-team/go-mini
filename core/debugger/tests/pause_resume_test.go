package debugger_test

import (
	"context"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/debugger"
)

func TestPauseAndResume(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	// 一个无限循环的脚本，或者一个长循环
	sourceProgram := `
	package main
	func main() {
		count := 0
		for count < 1000000 {
			count = count + 1
		}
	}
	`
	testProgram, err := testExecutor.NewRuntimeByGoCode(sourceProgram)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	// 在后台运行
	errChan := make(chan error, 1)
	go func() {
		errChan <- testProgram.Execute(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	dbg.RequestPause()

	select {
	case <-dbg.EventChan:
		t.Log("Paused by debugger request")
		dbg.CommandChan <- debugger.CmdContinue
	case <-ctx.Done():
		t.Fatal("timeout waiting for debugger pause")
	}

	// 恢复后取消，因为 1000000 步太久
	cancel()
	<-errChan
}
