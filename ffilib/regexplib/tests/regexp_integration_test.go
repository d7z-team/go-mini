package regexplib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/regexplib"
)

func TestRegexpSurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("regexp", regexplib.SurfaceRegexp(&regexplib.RegexpHost{})),
	}, []testutil.Case{
		{
			Name:    "matching-and-search",
			Imports: []string{"regexp"},
			Body: `
bytesOK, err := regexp.Match("a.c", []byte("abc"))
if err != nil {
	panic(err)
}
stringOK, err := regexp.MatchString("a.c", "abc")
if err != nil {
	panic(err)
}
all, err := regexp.FindAllString("[a-z]+", "a1 bb22 ccc", -1)
if err != nil {
	panic(err)
}
idx, err := regexp.FindStringIndex("[0-9]+", "abc123")
if err != nil {
	panic(err)
}
subIdx, err := regexp.FindStringSubmatchIndex("([a-z]+)([0-9]+)", "abc123")
if err != nil {
	panic(err)
}
submatch := regexp.FindStringSubmatch("([a-z]+)([0-9]+)", "abc123")
groups, err := regexp.FindAllStringSubmatch("([a-z]+)([0-9]+)", "a1 bb22", -1)
if err != nil {
	panic(err)
}
test.OutBool(bytesOK)
test.Out("|")
test.OutBool(stringOK)
test.Out("|")
test.Out(regexp.QuoteMeta("a+b"))
test.Out("|")
test.Out(regexp.FindString("[0-9]+", "abc123"))
test.Out("|")
test.Out(all[0])
test.Out(",")
test.Out(all[2])
test.Out("|")
test.OutInt(idx[0])
test.Out(",")
test.OutInt(idx[1])
test.Out("|")
test.OutInt(subIdx[2])
test.Out(",")
test.OutInt(subIdx[5])
test.Out("|")
test.Out(submatch[1])
test.Out(",")
test.Out(submatch[2])
test.Out("|")
test.Out(groups[1][1])
test.Out(",")
test.Out(groups[1][2])
`,
			Want:   `true|true|a\+b|123|a,ccc|3,6|0,6|abc,123|bb,22`,
			Covers: []string{"Match", "MatchString", "QuoteMeta", "FindString", "FindAllString", "FindStringIndex", "FindStringSubmatchIndex", "FindStringSubmatch", "FindAllStringSubmatch"},
		},
		{
			Name:    "replace-split-and-error",
			Imports: []string{"regexp"},
			Body: `
var err error
var replaced String
replaced, err = regexp.ReplaceAllString("[0-9]+", "a1b22", "#")
if err != nil {
	panic(err)
}
var literal String
literal, err = regexp.ReplaceAllLiteralString("[0-9]+", "a1", "$x")
if err != nil {
	panic(err)
}
var parts []String
parts, err = regexp.Split("[,;]", "a,b;c", -1)
if err != nil {
	panic(err)
}
_, err = regexp.FindAllString("[", "abc", -1)
test.Out(replaced)
test.Out("|")
test.Out(literal)
test.Out("|")
test.Out(parts[1])
test.Out("|")
test.OutBool(err != nil)
`,
			Want:   "a#b#|a$x|b|true",
			Covers: []string{"ReplaceAllString", "ReplaceAllLiteralString", "Split", "FindAllString"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
