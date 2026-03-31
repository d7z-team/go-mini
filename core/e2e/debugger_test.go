package e2e

import (
	"context"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/debugger"
)

func TestDebugger_BasicBreakAndStep(t *testing.T) {
	executor := engine.NewMiniExecutor()
	// 注意行号：第一行是 package main
	code := `
	package main
	func main() {
		a := 10       // Line 4
		b := 20       // Line 5
		c := a + b    // Line 6
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(5) // Break at 'b := 20'

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	done := make(chan error, 1)
	go func() {
		done <- prog.Execute(ctx)
	}()

	// Wait for breakpoint at line 5
	select {
	case event := <-dbg.EventChan:
		t.Logf("Hit breakpoint at line %d", event.Loc.L)
		if event.Loc.L != 5 {
			t.Fatalf("Expected break at line 5, got %d", event.Loc.L)
		}
		if event.Variables["a"] != "10" {
			t.Fatalf("Expected a=10, got %v", event.Variables["a"])
		}
		if _, exists := event.Variables["b"]; exists {
			t.Fatalf("b should not be initialized yet")
		}
		// Send step into
		dbg.CommandChan <- debugger.CmdStepInto
	case <-ctx.Done():
		t.Fatal("Timeout waiting for breakpoint at line 5")
	}

	// Step repeatedly until we reach line 6
	for {
		select {
		case event := <-dbg.EventChan:
			t.Logf("Stepped to line %d", event.Loc.L)
			if event.Loc.L == 6 {
				if event.Variables["b"] != "20" {
					t.Fatalf("Expected b=20, got %v", event.Variables["b"])
				}
				// Reached line 6, continue
				dbg.CommandChan <- debugger.CmdContinue
				goto WAIT_DONE
			} else if event.Loc.L == 5 {
				// Keep stepping if still on line 5
				dbg.CommandChan <- debugger.CmdStepInto
			} else {
				t.Fatalf("Unexpected step to line %d", event.Loc.L)
			}
		case <-ctx.Done():
			t.Fatal("Timeout waiting for step to line 6")
		}
	}
WAIT_DONE:
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for program completion")
	}
}

func TestDebugger_SnippetMode(t *testing.T) {
	executor := engine.NewMiniExecutor()

	code := `
		x := 100 // Line 2
		y := 200 // Line 3
		z := x * y
	`

	dbg := debugger.NewSession()
	dbg.SetStepping(true) // 开启单步模式以观察每一行

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	done := make(chan error, 1)
	go func() {
		done <- executor.Execute(ctx, code, nil)
	}()

	linesSeen := []int{}

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case event := <-dbg.EventChan:
			linesSeen = append(linesSeen, event.Loc.L)
			if event.Loc.L == 3 {
				if event.Variables["x"] != "100" {
					t.Fatalf("Expected x=100, got %v", event.Variables["x"])
				}
			}
			dbg.CommandChan <- debugger.CmdStepInto
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			goto DONE
		case <-ctx.Done():
			t.Fatal("Timeout in TestDebugger_SnippetMode")
		case <-ticker.C:
			// Just to ensure we don't block forever if event and done are both empty
		}
	}
DONE:
	// Ensure we read from done if we haven't already
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}

	if len(linesSeen) == 0 {
		t.Fatalf("No statements were intercepted in snippet mode. Is stepping working?")
	}
	t.Logf("Lines seen: %v", linesSeen)
}

func TestDebugger_LoopExecution(t *testing.T) {
	executor := engine.NewMiniExecutor()
	// 测试循环内的断点命中情况
	code := `
	package main
	func main() {
		sum := 0       // Line 4
		for i := 1; i <= 3; i++ { // Line 5
			sum = sum + i // Line 6
		}
		return         // Line 8
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()
	dbg.AddBreakpoint(6) // 在循环体内打断点

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	done := make(chan error, 1)
	go func() {
		done <- prog.Execute(ctx)
	}()

	// 预期循环 3 次，所以断点应该被命中 3 次
	expectedI := []string{"1", "2", "3"}
	expectedSum := []string{"0", "1", "3"} // sum 在执行第 6 行时的状态 (赋值发生前)

	for loopCount := 0; loopCount < 3; loopCount++ {
		select {
		case event := <-dbg.EventChan:
			if event.Loc.L != 6 {
				t.Fatalf("Expected break at line 6, got %d", event.Loc.L)
			}

			// 验证循环变量 i 和 累加器 sum 的当前状态
			actualI := event.Variables["i"]
			actualSum := event.Variables["sum"]

			if actualI != expectedI[loopCount] {
				t.Errorf("Loop %d: Expected i=%s, got %s", loopCount, expectedI[loopCount], actualI)
			}
			if actualSum != expectedSum[loopCount] {
				t.Errorf("Loop %d: Expected sum=%s, got %s", loopCount, expectedSum[loopCount], actualSum)
			}

			// 继续执行，直到下一次命中该断点
			dbg.CommandChan <- debugger.CmdContinue

		case <-ctx.Done():
			t.Fatalf("Timeout waiting for breakpoint in loop %d", loopCount)
		}
	}

	// 确保没有多余的命中
	select {
	case <-dbg.EventChan:
		t.Fatal("Breakpoint hit more times than expected")
	case <-time.After(100 * time.Millisecond):
		// 正常，没有多余的命中
	}

	err = <-done
	if err != nil {
		t.Fatal(err)
	}
}

func TestDebugger_AnytimePause(t *testing.T) {
	executor := engine.NewMiniExecutor()
	// 一个执行较长时间的循环，方便我们有时间发起异步暂停
	code := `
	package main
	func main() {
		sum := 0
		for i := 0; i < 1000000; i++ {
			sum = sum + 1
		}
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	dbg := debugger.NewSession()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = debugger.WithDebugger(ctx, dbg)

	done := make(chan error, 1)
	go func() {
		done <- prog.Execute(ctx)
	}()

	// 发起异步暂停请求
	// 我们不再 sleep，而是直接发请求，或者稍微 sleep 一点点
	time.Sleep(10 * time.Millisecond)
	dbg.RequestPause()

	// 预期在下一条语句停下
	select {
	case event := <-dbg.EventChan:
		t.Logf("Successfully paused at line %d", event.Loc.L)
		// 恢复执行
		dbg.CommandChan <- debugger.CmdContinue
	case <-ctx.Done():
		t.Fatal("Timeout waiting for anytime pause")
	}

	// 再次尝试暂停
	time.Sleep(20 * time.Millisecond)
	dbg.RequestPause()

	select {
	case event := <-dbg.EventChan:
		t.Logf("Successfully paused second time at line %d", event.Loc.L)
		dbg.CommandChan <- debugger.CmdContinue
	case <-ctx.Done():
		t.Fatal("Timeout waiting for second anytime pause")
	}

	err = <-done
	if err != nil {
		t.Fatal(err)
	}
}
