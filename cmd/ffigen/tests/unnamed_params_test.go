package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

type mockLogger struct {
	lastMsg   string
	lastLevel string
	lastCode  int64
}

func (m *mockLogger) Log(ctx context.Context, msg, level string, code int64) {
	m.lastMsg = msg
	m.lastLevel = level
	m.lastCode = code
}

func (m *mockLogger) Internal(msg, level string, code int64) {
	m.lastMsg = msg
	m.lastLevel = level
	m.lastCode = code
}

func TestUnnamedParams(t *testing.T) {
	executor := engine.NewMiniExecutor()
	logger := &mockLogger{}

	// Register generated Logger FFI
	RegisterLogger(executor, logger, nil)

	code := `
	package main
	import "logger"
	func main() {
		logger.Log("hello", "info", 200)
		if logger.Internal != nil {
			logger.Internal("raw", "debug", 500)
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

	if logger.lastMsg != "raw" || logger.lastLevel != "debug" || logger.lastCode != 500 {
		t.Errorf("Logger output mismatch: got %q, %q, %d", logger.lastMsg, logger.lastLevel, logger.lastCode)
	}
}
