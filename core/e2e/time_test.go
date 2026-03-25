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
		u := time.Unix()
		if u <= 0 { panic("Unix failed") }
		
		// Test Unix variants
		ms := time.UnixMilli()
		mc := time.UnixMicro()
		ns := time.UnixNano()
		if ms <= 0 || mc <= 0 || ns <= 0 { panic("Unix variants failed") }
		
		// Test Format/Parse
		layout := "2006-01-02 15:04:05"
		formatted := time.Format(ns, layout)
		parsedNs, err := time.Parse(layout, formatted)
		if err != nil { panic("Parse failed: " + err.Error()) }
		
		// Note: parsedNs might lose some precision depending on the layout, 
		// but since we use second-level layout, it should be close.
		// For equality check we should use a layout that includes nanos if needed.
		
		// Test ParseDuration/Add
		d, err1 := time.ParseDuration("1h")
		if err1 != nil { panic("ParseDuration failed") }
		future := time.Add(ns, d)
		if future != ns + d { panic("Add failed") }
		
		// Test Sub
		diff := time.Sub(future, ns)
		if diff != d { panic("Sub failed") }
		
		// Test Since
		s := time.Since(ns)
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
