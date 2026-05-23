package sortlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/sortlib"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
)

func TestSort(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("sort", sortlib.Sort_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "sorts-and-checks",
			Imports: []string{"sort"},
			Body: `
ints := []Int64{3, 1, 2}
floats := []Float64{2.5, 1.5}
strings := []string{"b", "a"}
sort.Ints(ints)
sort.Float64s(floats)
sort.Strings(strings)
test.OutInt(ints[0])
test.OutInt(ints[1])
test.OutInt(ints[2])
test.Out("|")
test.OutFloat(floats[0])
test.OutFloat(floats[1])
test.Out("|")
test.Out(strings[0])
test.Out(strings[1])
test.Out("|")
test.OutBool(sort.IntsAreSorted(ints))
test.OutBool(sort.Float64sAreSorted(floats))
test.OutBool(sort.StringsAreSorted(strings))
`,
			Want: "123|1.52.5|ab|truetruetrue",
			Covers: []string{
				"Ints", "Float64s", "Strings",
				"IntsAreSorted", "Float64sAreSorted", "StringsAreSorted",
			},
		},
		{
			Name:    "slice-window",
			Imports: []string{"sort"},
			Body: `
ints := []Int64{9, 3, 1, 2, 8}
sort.Ints(ints[1:4])
test.OutInt(ints[0])
test.Out("|")
test.OutInt(ints[1])
test.Out("|")
test.OutInt(ints[2])
test.Out("|")
test.OutInt(ints[3])
test.Out("|")
test.OutInt(ints[4])
`,
			Want: "9|1|2|3|8",
		},
	})
}

func TestSortDoesNotReturnArray(t *testing.T) {
	testutil.ExpectCompileError(t, testutil.BlockCase{
		Imports: []string{"sort"},
		Body: `
ints := sort.Ints([]Int64{3, 1, 2})
_ = ints
`,
	})
}
