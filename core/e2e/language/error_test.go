package tests

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
)

func TestNativeErrorRunCases(t *testing.T) {
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "new-is-as-and-stack",
			Imports: []string{"errors"},
			Body: `
err := errors.New("boom")
same := err
other := errors.New("boom")
var out error
test.Out(err.Error())
test.Out("|")
test.OutBool(errors.Is(err, same))
test.Out("|")
test.OutBool(errors.Is(err, other))
test.Out("|")
test.OutBool(errors.As(err, &out))
test.Out("|")
test.Out(out.Error())
test.Out("|")
test.OutBool(errors.Stack(err) != "")
`,
			Want: "boom|true|false|true|boom|true",
		},
		{
			Name:    "fmt-errorf-wraps-with-go-error-semantics",
			Imports: []string{"errors", "fmt"},
			Body: `
base := errors.New("root")
wrapped := fmt.Errorf("outer: %w", base)
unwrapped := errors.Unwrap(wrapped)
other := errors.New("other")
multi := fmt.Errorf("multi: %w and %w", base, other)
test.Out(wrapped.Error())
test.Out("|")
test.OutBool(errors.Is(wrapped, base))
test.Out("|")
test.Out(unwrapped.Error())
test.Out("|")
test.OutBool(errors.Stack(wrapped) != "")
test.Out("|")
test.OutBool(errors.Is(multi, base))
test.Out("|")
test.OutBool(errors.Is(multi, other))
test.Out("|")
test.OutBool(errors.Unwrap(multi) == nil)
`,
			Want: "outer: root|true|root|true|true|true|true",
		},
		{
			Name:    "errors-as-rejects-non-error-target",
			Imports: []string{"errors"},
			Body: `
recovered := false
defer func() {
	if recover() != nil {
		recovered = true
	}
	test.OutBool(recovered)
	test.Done()
}()
err := errors.New("boom")
var text String
errors.As(err, &text)
`,
			Want: "true",
		},
		{
			Name:    "errors-as-nil-error-and-any-target",
			Imports: []string{"errors"},
			Body: `
var err error
var out error
var anyOut any
test.OutBool(errors.As(err, &out))
test.Out("|")
test.OutBool(out == nil)
test.Out("|")
err = errors.New("typed")
test.OutBool(errors.As(err, &anyOut))
test.Out("|")
test.OutBool(anyOut != nil)
`,
			Want: "false|true|true|true",
		},
		{
			Name:    "errors-as-rejects-nil-target",
			Imports: []string{"errors"},
			Body: `
recovered := false
defer func() {
	if recover() != nil {
		recovered = true
	}
	test.OutBool(recovered)
	test.Done()
}()
err := errors.New("boom")
errors.As(err, nil)
`,
			Want: "true",
		},
		{
			Name:    "fmt-errorf-rejects-dynamic-non-string-format",
			Imports: []string{"fmt"},
			Body: `
recovered := false
defer func() {
	if recover() != nil {
		recovered = true
	}
	test.OutBool(recovered)
	test.Done()
}()
var format any = 123
fmt.Errorf(format)
`,
			WantCompileErr: "function argument 1 type mismatch: expected String, got Any",
		},
	})
}
