package stringslib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
	"gopkg.d7z.net/go-mini/core/ffilib/stringslib"
)

func TestStrings(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("strings", stringslib.Strings_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "predicates",
			Imports: []string{"strings"},
			Body: `
test.OutBool(strings.Contains("hello", "ell"))
test.Out("|")
test.OutBool(strings.ContainsAny("hello", "xyzol"))
test.Out("|")
test.OutInt(strings.Count("banana", "na"))
test.Out("|")
test.OutBool(strings.HasPrefix("gopher", "go"))
test.Out("|")
test.OutBool(strings.HasSuffix("gopher", "her"))
`,
			Want:   "true|true|2|true|true",
			Covers: []string{"Contains", "ContainsAny", "Count", "HasPrefix", "HasSuffix"},
		},
		{
			Name:    "index-and-case",
			Imports: []string{"strings"},
			Body: `
test.OutInt(strings.Index("gopher", "op"))
test.Out("|")
test.OutInt(strings.LastIndex("gopher-gopher", "go"))
test.Out("|")
test.Out(strings.ToLower("GO"))
test.Out("|")
test.Out(strings.ToUpper("go"))
`,
			Want:   "1|7|go|GO",
			Covers: []string{"Index", "LastIndex", "ToLower", "ToUpper"},
		},
		{
			Name:    "trim-and-replace",
			Imports: []string{"strings"},
			Body: `
test.Out(strings.Trim("!!go!!", "!"))
test.Out("|")
test.Out(strings.TrimSpace("  go  "))
test.Out("|")
test.Out(strings.TrimPrefix("gopher", "go"))
test.Out("|")
test.Out(strings.TrimSuffix("gopher", "her"))
test.Out("|")
test.Out(strings.Replace("aaaa", "a", "b", 2))
test.Out("|")
test.Out(strings.ReplaceAll("a-a", "a", "b"))
`,
			Want:   "go|go|pher|gop|bbaa|b-b",
			Covers: []string{"Trim", "TrimSpace", "TrimPrefix", "TrimSuffix", "Replace", "ReplaceAll"},
		},
		{
			Name:    "split-join-expression",
			Imports: []string{"strings"},
			Expr:    `strings.Join(strings.Split("a,b,c", ","), "/")`,
			Want:    "a/b/c",
			Covers:  []string{"Split", "Join"},
		},
	})
}
