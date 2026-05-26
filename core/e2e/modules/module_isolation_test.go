package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/surface"
)

func TestModuleInitFailureDoesNotPolluteParentSession(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Library("broken", surface.GoFile("broken.mgo", `
			package broken

			var Exported = "partial"
			var Trigger = 1 / 0
			`))); err != nil {
		t.Fatalf("register broken surface: %v", err)
	}

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "broken"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected broken module init to fail")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	shared := runtime.SharedState()
	if shared == nil {
		t.Fatal("expected shared state")
	}
	if shared.HasModule("broken") {
		mod, _ := shared.Module("broken")
		t.Fatalf("broken module should not be committed into cache: %#v", mod)
	}
	if shared.IsModuleLoading("broken") {
		t.Fatal("broken module should not remain in loading set")
	}
}

func TestModuleInitPanicFunctionDoesNotPolluteParentSession(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Library("panicmod", surface.GoFile("panicmod.mgo", `
			package panicmod

			func fail() string {
				panic("boom")
			}

			var Exported = "partial"
			var Trigger = fail()
			`))); err != nil {
		t.Fatalf("register panicmod surface: %v", err)
	}

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "panicmod"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected panicing module init to fail")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	shared := runtime.SharedState()
	if shared == nil {
		t.Fatal("expected shared state")
	}
	if shared.HasModule("panicmod") {
		mod, _ := shared.Module("panicmod")
		t.Fatalf("panicmod should not be committed into cache: %#v", mod)
	}
	if shared.IsModuleLoading("panicmod") {
		t.Fatal("panicmod should not remain in loading set")
	}
}

func TestTransitivePartialInitDoesNotPolluteImporterChain(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Libraries(
		surface.LibraryModule{Path: "childbroken", Files: []surface.LibraryFile{surface.GoFile("childbroken.mgo", `
			package childbroken

			var Exported = "child-partial"
			var Trigger = 1 / 0
			`)}},
		surface.LibraryModule{Path: "parentbroken", Files: []surface.LibraryFile{surface.GoFile("parentbroken.mgo", `
			package parentbroken
			import "childbroken"

			var ParentExported = "parent-partial"
			var ChildValue = childbroken.Exported
			`)}},
	)); err != nil {
		t.Fatalf("register parent/child surface: %v", err)
	}

	runtime, err := executor.NewRuntimeByGoCode(`
	package main
	import "parentbroken"

	func main() {}
	`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	err = runtime.Execute(context.Background())
	if err == nil {
		t.Fatal("expected transitive partial-init module chain to fail")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("unexpected execute error: %v", err)
	}

	shared := runtime.SharedState()
	if shared == nil {
		t.Fatal("expected shared state")
	}
	if shared.HasModule("childbroken") {
		mod, _ := shared.Module("childbroken")
		t.Fatalf("childbroken should not be committed into cache: %#v", mod)
	}
	if shared.HasModule("parentbroken") {
		mod, _ := shared.Module("parentbroken")
		t.Fatalf("parentbroken should not be committed into cache: %#v", mod)
	}
	if shared.IsModuleLoading("childbroken") {
		t.Fatal("childbroken should not remain in loading set")
	}
	if shared.IsModuleLoading("parentbroken") {
		t.Fatal("parentbroken should not remain in loading set")
	}
}

func TestModuleInitContextCancelClearsLoadingState(t *testing.T) {
	executor := engine.NewMiniExecutor()

	if err := executor.UseSurface(surface.Library("slowmod", surface.GoFile("slowmod.mgo", `
package slowmod

var Exported = wait()

func wait() int {
	for true {
	}
	return 1
}
`))); err != nil {
		t.Fatalf("register slowmod surface: %v", err)
	}

	runtime, err := executor.NewRuntimeByGoCode(`
package main
import "slowmod"

func main() {}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = runtime.Execute(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %T %v", err, err)
	}

	shared := runtime.SharedState()
	if shared == nil {
		t.Fatal("expected shared state")
	}
	if shared.HasModule("slowmod") {
		mod, _ := shared.Module("slowmod")
		t.Fatalf("slowmod should not be committed into cache after cancellation: %#v", mod)
	}
	if shared.IsModuleLoading("slowmod") {
		t.Fatal("slowmod should not remain in loading set after cancellation")
	}
}
