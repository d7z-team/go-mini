package errorslib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
)

func TestNewAndIs(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("errors", ffilib.Surface()),
	}, []testutil.Case{
		{
			Name:    "new-and-is",
			Imports: []string{"errors"},
			Body: `
target := errors.New("boom")
other := errors.New("other")
var nilErr error
test.Out(target.Error())
test.Out("|")
test.OutBool(errors.Is(target, target))
test.Out("|")
test.OutBool(errors.Is(target, other))
test.Out("|")
test.OutBool(errors.Is(nilErr, nil))
test.Out("|")
test.OutBool(errors.Is(target, nil))
`,
			Want:   "boom|true|false|true|false",
			Covers: []string{"New", "Is"},
		},
	})
}

func TestIsRequiresErrorTarget(t *testing.T) {
	testutil.ExpectCompileError(t, testutil.BlockCase{
		Imports: []string{"errors"},
		Body: `
target := errors.New("boom")
if errors.Is(target, 1) {
	panic("unexpected match")
}
`,
	})
}
