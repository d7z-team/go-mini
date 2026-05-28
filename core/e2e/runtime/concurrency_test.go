package tests

import (
	"context"
	"sync"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestConcurrencySafety(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	// 一个简单的循环脚本，用于触发 stepCount
	code := `
package main
func main() {
	sum := 0
	for i := 0; i < 100; i++ {
		sum += i
	}
}
`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// 尝试并发执行同一个程序
			_ = prog.Execute(context.Background())
		}()
	}

	wg.Wait()
}

func TestConcurrentModuleImportSingleflight(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	if err := executor.UseSurface(surface.Library("sharedmod", surface.GoFile("sharedmod.mgo", `
package sharedmod

var Value = 1

func Get() int {
	return Value
}
`))); err != nil {
		t.Fatalf("register sharedmod surface: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "sharedmod"

func main() {
	if sharedmod.Get() != 1 {
		panic("bad module value")
	}
}
`)
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 8
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			errCh <- prog.Execute(context.Background())
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent execute failed: %v", err)
		}
	}
}

func TestConcurrentSharedMapMutationDoesNotPanic(t *testing.T) {
	executor := engine.MustNewMiniExecutor()

	if err := executor.UseSurface(surface.Library("counter", surface.GoFile("counter.mgo", `
package counter

var Stats = map[string]int{"n": 0}

func Bump() {
	Stats["n"] = Stats["n"] + 1
}
`))); err != nil {
		t.Fatalf("register counter surface: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "counter"

func main() {
	counter.Bump()
}
`)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 16
	const iterations = 20
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*iterations)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				errCh <- prog.Execute(context.Background())
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("shared map execute failed: %v", err)
		}
	}

	shared := prog.SharedState()
	if shared == nil {
		t.Fatal("missing shared state")
	}
	modVar, ok := shared.Module("counter")
	if !ok || modVar == nil || modVar.VType != runtime.TypeModule {
		t.Fatalf("missing counter module in shared state: %#v", modVar)
	}
	statsVar := modVar.Ref.(*runtime.VMModule).Data["Stats"]
	if statsVar == nil || statsVar.VType != runtime.TypeMap {
		t.Fatalf("missing shared stats map: %#v", statsVar)
	}
	count, ok := statsVar.Ref.(*runtime.VMMap).Load("n")
	if !ok || count == nil || count.VType != runtime.TypeInt || count.I64 <= 0 {
		t.Fatalf("unexpected shared counter value: %#v", count)
	}
}
