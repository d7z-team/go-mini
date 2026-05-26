package contextlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/contextlib"
	"gopkg.d7z.net/go-mini/ffilib/timelib"
)

func TestContext(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("context/internal", contextlib.SurfaceModule(&contextlib.ModuleHost{})),
		testutil.SurfaceFFISchema("context/internal.Timer", contextlib.SurfaceTimer()),
	}, []testutil.Case{
		{
			Name:    "background-and-todo-basics",
			Imports: []string{"context"},
			Body: `
ctx := context.Background()
_, ok := ctx.Deadline()
test.OutBool(!ok)
test.Out("|")
select {
case <-ctx.Done():
	test.Out("ready")
default:
	test.Out("blocked")
}
test.Out("|")
test.OutBool(ctx.Err() == nil)
test.Out("|")
test.OutBool(ctx.Value("missing") == nil)
test.Out("|")
test.OutBool(context.TODO().Err() == nil)
`,
			Want:   "true|blocked|true|true|true",
			Covers: []string{"Canceled", "DeadlineExceeded"},
		},
		{
			Name:    "with-cancel-closes-done-and-sets-error",
			Imports: []string{"context", "errors"},
			Body: `
ctx, cancel := context.WithCancel(context.Background())
select {
case <-ctx.Done():
	test.Out("closed")
default:
	test.Out("open")
}
cancel()
cancel()
select {
case <-ctx.Done():
	test.Out("|closed")
default:
	test.Out("|open")
}
test.Out("|")
test.OutBool(errors.Is(ctx.Err(), context.Canceled))
`,
			Want: "open|closed|true",
		},
		{
			Name:    "parent-cancel-propagates-to-child",
			Imports: []string{"context", "errors"},
			Body: `
parent, cancel := context.WithCancel(context.Background())
child, _ := context.WithCancel(parent)
cancel()
<-child.Done()
test.OutBool(errors.Is(child.Err(), context.Canceled))
`,
			Want: "true",
		},
		{
			Name:    "timeout-deadline-expires",
			Imports: []string{"context", "errors", "time"},
			Body: `
ctx, _ := context.WithTimeout(context.Background(), time.Millisecond)
<-ctx.Done()
test.OutBool(errors.Is(ctx.Err(), context.DeadlineExceeded))
`,
			Want:   "true",
			Covers: []string{"NewTimer", "Wait"},
		},
		{
			Name:    "expired-timeout-is-ready-immediately",
			Imports: []string{"context", "errors"},
			Body: `
ctx, _ := context.WithTimeout(context.Background(), 0)
select {
case <-ctx.Done():
	test.Out("ready")
default:
	test.Out("blocked")
}
test.Out("|")
test.OutBool(errors.Is(ctx.Err(), context.DeadlineExceeded))
`,
			Want: "ready|true",
		},
		{
			Name:    "cancel-before-timeout-stops-timer",
			Imports: []string{"context", "errors", "time"},
			Body: `
ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
cancel()
<-ctx.Done()
test.OutBool(errors.Is(ctx.Err(), context.Canceled))
`,
			Want:   "true",
			Covers: []string{"Stop"},
		},
		{
			Name:    "deadline-and-value-chain",
			Imports: []string{"context", "time"},
			Body: `
deadline := time.Now().Add(time.Second)
ctx, cancel := context.WithDeadline(context.Background(), deadline)
defer cancel()
got, ok := ctx.Deadline()
test.OutBool(ok && got.Equal(deadline))
test.Out("|")
withValue := context.WithValue(context.WithValue(ctx, "key", "parent"), "key", "child")
test.OutBool(withValue.Value("key") == "child")
test.Out("|")
test.OutBool(withValue.Value("missing") == nil)
`,
			Want:   "true|true|true",
			Covers: []string{"ValidValueKey"},
		},
		{
			Name:    "deadline-forwarding-cancel-context",
			Imports: []string{"context", "errors", "time"},
			Body: `
var missing *time.Time
ctx, cancel := context.WithDeadline(context.Background(), missing)
cancel()
<-ctx.Done()
test.OutBool(errors.Is(ctx.Err(), context.Canceled))
test.Out("|")
parent, _ := context.WithTimeout(context.Background(), time.Millisecond)
child, stopChild := context.WithTimeout(parent, time.Hour)
defer stopChild()
<-child.Done()
test.OutBool(errors.Is(child.Err(), context.DeadlineExceeded))
`,
			Want: "true|true",
		},
		{
			Name:    "expired-deadline-is-ready-immediately",
			Imports: []string{"context", "errors", "time"},
			Body: `
deadline := time.Now().Add(0 - time.Millisecond)
ctx, _ := context.WithDeadline(context.Background(), deadline)
select {
case <-ctx.Done():
	test.Out("ready")
default:
	test.Out("blocked")
}
test.Out("|")
test.OutBool(errors.Is(ctx.Err(), context.DeadlineExceeded))
`,
			Want: "ready|true",
		},
		{
			Name:    "parent-cancel-propagates-through-registered-children",
			Imports: []string{"context", "errors"},
			Body: `
parent, cancel := context.WithCancel(context.Background())
childA, _ := context.WithCancel(parent)
childB, _ := context.WithCancel(context.WithValue(parent, "key", "value"))
cancel()
<-childA.Done()
<-childB.Done()
test.OutBool(errors.Is(childA.Err(), context.Canceled))
test.Out("|")
test.OutBool(errors.Is(childB.Err(), context.Canceled))
`,
			Want: "true|true",
		},
		{
			Name:    "child-cancel-remains-stable-after-parent-cancel",
			Imports: []string{"context", "errors"},
			Body: `
parent, cancelParent := context.WithCancel(context.Background())
child, cancelChild := context.WithCancel(parent)
cancelChild()
<-child.Done()
cancelParent()
test.OutBool(errors.Is(child.Err(), context.Canceled))
`,
			Want: "true",
		},
		{
			Name:       "with-value-rejects-nil-key",
			Imports:    []string{"context"},
			WantRunErr: "nil context key",
			Body: `
context.WithValue(context.Background(), nil, "value")
`,
		},
		{
			Name:       "with-value-rejects-array-key",
			Imports:    []string{"context"},
			WantRunErr: "context key is not comparable",
			Body: `
context.WithValue(context.Background(), []Int64{1}, "value")
`,
		},
		{
			Name:       "with-value-rejects-map-key",
			Imports:    []string{"context"},
			WantRunErr: "context key is not comparable",
			Body: `
context.WithValue(context.Background(), map[String]Int64{"a": 1}, "value")
`,
		},
		{
			Name:           "done-is-receive-only",
			Imports:        []string{"context"},
			WantCompileErr: "<any>",
			Body: `
ctx, _ := context.WithCancel(context.Background())
close(ctx.Done())
`,
		},
	}, testutil.WithSurface(ffilib.Surface()))
}

func TestContextSurfaceCanLoadWithoutFullFFILib(t *testing.T) {
	testutil.ExpectBlock(t, testutil.BlockCase{
		Name:    "standalone-context-surface",
		Imports: []string{"context", "errors", "time"},
		Body: `
ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
defer cancel()
<-ctx.Done()
test.OutBool(errors.Is(ctx.Err(), context.DeadlineExceeded))
`,
		Want: "true",
	}, testutil.WithSurface(surface.Merge(
		timelib.SurfaceModule(&timelib.TimeHost{}),
		timelib.SurfaceTime(),
		contextlib.Surface(),
	)))
}
