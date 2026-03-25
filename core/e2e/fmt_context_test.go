package e2e

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
)

type testOutputter struct {
	sb strings.Builder
}

func (o *testOutputter) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

func TestFmtContextOutputter(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	code := `
	package main
	import "fmt"
	func main() {
		fmt.Print("hello", " world")
		fmt.Println("!")
		println("direct println")
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}

	output := &testOutputter{}
	ctx := fmtlib.WithOutputter(context.Background(), output)

	err = prog.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got := output.sb.String()
	expected := []string{"hello world", "!", "direct println"}
	for _, exp := range expected {
		if !strings.Contains(got, exp) {
			t.Errorf("expected output to contain %q, got %q", exp, got)
		}
	}
}
