package e2e

import (
	"context"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func TestTimelibStruct(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main
import "fmt"
import "time"

func main() (int, int, int) {
	now := time.Now()
	year := now.Year()
	month := now.Month()
	day := now.Day()
	
	fmt.Printf("Year: %d, Month: %d, Day: %d\n", year, month, day)
	
	unix := now.Unix()
	unixNano := now.UnixNano()
	
	formatted := now.Format("2006-01-02")
	fmt.Printf("Unix: %d, UnixNano: %d, Formatted: %s\n", unix, unixNano, formatted)

	// Test Add and Sub
	d, _ := time.ParseDuration("1h")
	later := now.Add(d)
	diff := later.Sub(now)
	
	fmt.Printf("Diff: %d\n", diff)

	// Test Parse
	parsed, _ := time.Parse("2006-01-02", "2023-10-27")
	fmt.Printf("Parsed Year: %d\n", parsed.Year())
	
	return int(year), int(diff), int(parsed.Year())
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	res, err := prog.Eval(context.Background(), "main()", nil)
	if err != nil {
		t.Fatalf("Failed to run main: %v", err)
	}

	// res is a tuple (TypeArray)
	if res.VType != runtime.TypeArray {
		t.Fatalf("Expected TypeArray, got %v", res.VType)
	}
	arr := res.Ref.(*runtime.VMArray).Data
	if arr[0].I64 != int64(time.Now().Year()) {
		t.Errorf("Expected year %d, got %v", time.Now().Year(), arr[0].I64)
	}
	if arr[1].I64 != int64(time.Hour) {
		t.Errorf("Expected diff %d, got %v", int64(time.Hour), arr[1].I64)
	}
	if arr[2].I64 != int64(2023) {
		t.Errorf("Expected parsed year 2023, got %v", arr[2].I64)
	}
}

func TestTimelibUntilSince(t *testing.T) {
	executor := engine.NewMiniExecutor()
	code := `
package main
import "time"

func main() bool {
	now := time.Now()
	time.Sleep(10 * time.Millisecond)
	elapsed := time.Since(now)
	return elapsed >= 10 * time.Millisecond
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("Failed to create runtime: %v", err)
	}

	res, err := prog.Eval(context.Background(), "main()", nil)
	if err != nil {
		t.Fatalf("Failed to run main: %v", err)
	}
	if !res.Bool {
		t.Errorf("Expected elapsed >= 10ms, got false")
	}
}
