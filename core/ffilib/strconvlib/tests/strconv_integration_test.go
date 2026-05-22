package strconvlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
	"gopkg.d7z.net/go-mini/core/ffilib/strconvlib"
)

func TestStrconv(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("strconv", strconvlib.Strconv_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "atoi-itoa",
			Imports: []string{"strconv"},
			Body: `
v, err := strconv.Atoi("456")
if err != nil {
	test.Out("err")
} else {
	test.OutInt(v)
}
test.Out("|")
test.Out(strconv.Itoa(123))
`,
			Want:   "456|123",
			Covers: []string{"Atoi", "Itoa"},
		},
		{
			Name:    "parse",
			Imports: []string{"strconv"},
			Body: `
b, berr := strconv.ParseBool("true")
f, ferr := strconv.ParseFloat("3.1415", 64)
i, ierr := strconv.ParseInt("-42", 10, 64)
test.OutBool(berr == nil && b)
test.Out("|")
test.OutBool(ferr == nil && f > 3.14 && f < 3.15)
test.Out("|")
if ierr != nil {
	test.Out("err")
} else {
	test.OutInt(i)
}
`,
			Want:   "true|true|-42",
			Covers: []string{"ParseBool", "ParseFloat", "ParseInt"},
		},
		{
			Name:    "format",
			Imports: []string{"strconv"},
			Body: `
test.Out(strconv.FormatBool(true))
test.Out("|")
test.Out(strconv.FormatFloat(3.5, 102, 1, 64))
test.Out("|")
test.Out(strconv.FormatInt(-42, 16))
`,
			Want:   "true|3.5|-2a",
			Covers: []string{"FormatBool", "FormatFloat", "FormatInt"},
		},
		{
			Name:    "quote-unquote",
			Imports: []string{"strconv"},
			Body: `
q := strconv.Quote("go")
u, err := strconv.Unquote(q)
if err != nil {
	test.Out("err")
} else {
	test.Out(q)
	test.Out("|")
	test.Out(u)
}
`,
			Want:   `"go"|go`,
			Covers: []string{"Quote", "Unquote"},
		},
	})
}
