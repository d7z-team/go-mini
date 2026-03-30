package e2e

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestTimeLibrary(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	import "time"

	func main() {
		// Test Now/Unix
		now := time.Now()
		u := now.Unix()
		if u <= 0 { panic("Unix failed") }
		
		// Test Unix variants
		ms := now.UnixMilli()
		mc := now.UnixMicro()
		ns := now.UnixNano()
		if ms <= 0 || mc <= 0 || ns <= 0 { panic("Unix variants failed") }
		
		// Test Format/Parse
		layout := "2006-01-02 15:04:05"
		formatted := now.Format(layout)
		parsed, err := time.Parse(layout, formatted)
		if err != nil { panic("Parse failed: " + err.Error()) }
		
		// Test ParseDuration/Add/Sub
		d, err1 := time.ParseDuration("1h")
		if err1 != nil { panic("ParseDuration failed") }
		future := now.Add(d)
		diff := future.Sub(now)
		if diff != d { panic("Sub/Add failed") }
		
		// Test Since
		s := time.Since(now)
		if s < 0 { panic("Since failed") }
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
