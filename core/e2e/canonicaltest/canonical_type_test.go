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
	executor.InjectStandardLibraries()

	impl := &CanonicalTestImpl{}
	registry := executor.HandleRegistry()

	// Register with canonical paths (automatically handled by RegisterXXX)
	canonicaltest.RegisterTestCanonicalService(executor, impl, registry)
	canonicaltest.RegisterATypeService(executor, &AImpl{}, registry)
	canonicaltest.RegisterBTypeService(executor, &BImpl{}, registry)

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
