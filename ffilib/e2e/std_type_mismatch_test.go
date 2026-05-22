package e2e_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
)

func TestStandardLibraryTypeMismatch(t *testing.T) {
	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "time-hostref-passes-user-function",
			Imports: []string{"time"},
			Decls: `
func printTime(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return "ok"
}
`,
			Expr: `printTime(time.Now())`,
			Want: "ok",
		},
	}, testutil.WithRegister(ffilib.RegisterAll))
}
