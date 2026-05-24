package urllib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/net/urllib"
)

func TestURL(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("net/url", urllib.SurfaceURL(&urllib.URLHost{})),
	}, []testutil.Case{
		{
			Name:    "query-and-join-path",
			Imports: []string{"net/url"},
			Body: `
test.Out(url.QueryEscape("a b"))
test.Out("|")
unescaped, err := url.QueryUnescape("a+b")
if err != nil {
	panic(err)
}
test.Out(unescaped)
test.Out("|")
test.Out(url.JoinPath("https://example.com/api", "v1", "users"))
`,
			Want:   "a+b|a b|https://example.com/api/v1/users",
			Covers: []string{"QueryEscape", "QueryUnescape", "JoinPath"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
