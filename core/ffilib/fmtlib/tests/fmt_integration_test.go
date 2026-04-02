package fmtlib_test

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

func TestFmtOutputter(t *testing.T) {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	prog, err := executor.NewRuntimeByGoCode(`
package main
import "fmt"

func main() {
	fmt.Print("hello")
	fmt.Println(" world")
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	output := &testOutputter{}
	ctx := fmtlib.WithOutputter(context.Background(), output)
	if err := prog.Execute(ctx); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if got := output.sb.String(); !strings.Contains(got, "hello world") {
		t.Fatalf("unexpected output %q", got)
	}
}
