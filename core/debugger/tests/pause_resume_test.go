package debugger_test

import (
	"context"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 在后台运行
	errChan := make(chan error, 1)
	go func() {
		errChan <- testProgram.Execute(ctx)
	}()

	// 等待直到 session 准备好且有步进
	var session *engine.StackContext
	for i := 0; i < 1000; i++ {
		session = testProgram.LastSession()
		if session != nil && session.StepCount > 0 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	if session == nil || session.StepCount == 0 {
		t.Fatal("session not started or no steps executed")
	}

	// 暂停
	session.Pause()
	t.Logf("Paused at step %d", session.StepCount)

	// 记录当前步数
	stepAtPause := session.StepCount
	time.Sleep(50 * time.Millisecond)

	t.Logf("Step count after 50ms pause: %d", session.StepCount)

	// 确认步数没动（或者动得极少）
	if session.StepCount > stepAtPause+10 {
		t.Fatalf("execution did not pause: steps increased from %d to %d", stepAtPause, session.StepCount)
	}

	// 恢复
	session.Resume()
	t.Log("Resumed")

	// 等待一会儿确认它在跑
	time.Sleep(10 * time.Millisecond)
	if session.StepCount <= stepAtPause {
		t.Fatalf("execution did not resume: steps still at %d", session.StepCount)
	}

	// 恢复后取消，因为 1000000 步太久
	cancel()
	<-errChan
}
