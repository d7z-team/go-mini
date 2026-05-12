package engine_test

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/fmtlib"
)

type outputRecorder struct {
	sb strings.Builder
}

func (o *outputRecorder) Print(_ context.Context, s string) {
	o.sb.WriteString(s)
}

func executeWithCapturedOutput(t *testing.T, prog *engine.MiniProgram) string {
	t.Helper()

	recorder := &outputRecorder{}
	ctx := fmtlib.WithOutputter(context.Background(), recorder)
	if err := prog.Execute(ctx); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	return recorder.sb.String()
}
