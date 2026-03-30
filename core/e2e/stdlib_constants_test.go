package e2e

import (
	"context"
	"testing"

	"gopkg.d7z.net/go-mini/core"
)

func TestStdlibConstants(t *testing.T) {
	// core 包的 package name 是 engine
	eng := engine.NewMiniExecutor()
	eng.InjectStandardLibraries()
	
	code := `
	package main
	import "os"
	import "time"
	import "io"

	func test_os() int64 {
		return os.O_CREATE | os.O_RDWR | os.O_APPEND
	}

	func test_time() int64 {
		return time.Second * 5
	}

	func test_io() int64 {
		return io.SeekEnd
	}
	`
	
	prog, err := eng.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Failed to compile: %v", err)
	}

	ctx := context.Background()
	
	// Test OS constants
	res, err := prog.Eval(ctx, "test_os()", nil)
	if err != nil {
		t.Errorf("test_os failed: %v", err)
	}
	// O_CREATE(64) | O_RDWR(2) | O_APPEND(1024) = 1090
	val, _ := res.ToInt()
	if val != 1090 {
		t.Errorf("Expected 1090, got %d", val)
	}

	// Test Time constants
	res, err = prog.Eval(ctx, "test_time()", nil)
	if err != nil {
		t.Errorf("test_time failed: %v", err)
	}
	// 1000000000 * 5 = 5000000000
	val, _ = res.ToInt()
	if val != 5000000000 {
		t.Errorf("Expected 5000000000, got %d", val)
	}

	// Test IO constants
	res, err = prog.Eval(ctx, "test_io()", nil)
	if err != nil {
		t.Errorf("test_io failed: %v", err)
	}
	val, _ = res.ToInt()
	if val != 2 { // SeekEnd = 2
		t.Errorf("Expected 2, got %d", val)
	}
}
