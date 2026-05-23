package canonicaltest_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/e2e/canonicaltest"
	a_other "gopkg.d7z.net/go-mini/core/e2e/canonicaltest/internal/a/other"
	b_other "gopkg.d7z.net/go-mini/core/e2e/canonicaltest/internal/b/other"
)

type CanonicalTestImpl struct{}

func (i *CanonicalTestImpl) NewA(ctx context.Context, name string) *a_other.Type {
	return &a_other.Type{Name: name}
}

func (i *CanonicalTestImpl) NewB(ctx context.Context, id int) *b_other.Type {
	return &b_other.Type{ID: id}
}

type AImpl struct{}

func (i *AImpl) Hello(t *a_other.Type) string { return "Hello A: " + t.Name }

type BImpl struct{}

func (i *BImpl) Hello(t *b_other.Type) string { return "Hello B: " + string(rune(t.ID)) }

func TestCanonicalTypeSystem(t *testing.T) {
	executor := engine.NewMiniExecutor()

	impl := &CanonicalTestImpl{}
	if err := executor.UseSurface(canonicaltest.SurfaceTestCanonicalService(impl)); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(canonicaltest.SurfaceATypeService(&AImpl{})); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(canonicaltest.SurfaceBTypeService(&BImpl{})); err != nil {
		t.Fatal(err)
	}

	code := `
	package main
	import "test_canonical"

	func main() {
		a := test_canonical.NewA("Gemini")
		b := test_canonical.NewB(65) // 'A'

		// If the type system works, these should call different implementations 
		// even if their relative names are both "other.Type"
		resA := a.Hello()
		resB := b.Hello()

		if resA != "Hello A: Gemini" { panic("A mismatch: " + resA) }
		if resB != "Hello B: A" { panic("B mismatch: " + resB) }
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

func TestCanonicalTypeInterfaceAcrossPaths(t *testing.T) {
	executor := engine.NewMiniExecutor()

	impl := &CanonicalTestImpl{}
	if err := executor.UseSurface(canonicaltest.SurfaceTestCanonicalService(impl)); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(canonicaltest.SurfaceATypeService(&AImpl{})); err != nil {
		t.Fatal(err)
	}
	if err := executor.UseSurface(canonicaltest.SurfaceBTypeService(&BImpl{})); err != nil {
		t.Fatal(err)
	}

	code := `
	package main
	import "test_canonical"

	type Greeter interface {
		Hello() String
	}

	func readHello(g Greeter) String {
		return g.Hello()
	}

	func main() {
		a := test_canonical.NewA("Gemini")
		b := test_canonical.NewB(66)

		if readHello(a) != "Hello A: Gemini" {
			panic("canonical interface dispatch for A failed")
		}
		if readHello(b) != "Hello B: B" {
			panic("canonical interface dispatch for B failed")
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
}
