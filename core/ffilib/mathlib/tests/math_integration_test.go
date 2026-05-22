package mathlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/mathlib"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
)

func TestMath(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("math", mathlib.Math_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "functions-and-constants",
			Imports: []string{"math"},
			Body: `
ok := math.Abs(-1.0) == 1.0 &&
	math.Ceil(1.2) == 2.0 &&
	math.Floor(1.8) == 1.0 &&
	math.Round(1.6) == 2.0 &&
	math.Sqrt(4.0) == 2.0 &&
	math.Pow(2.0, 3.0) == 8.0 &&
	math.Min(2.0, 3.0) == 2.0 &&
	math.Max(2.0, 3.0) == 3.0 &&
	math.Sin(0.0) == 0.0 &&
	math.Cos(0.0) == 1.0 &&
	math.Tan(0.0) == 0.0 &&
	math.Exp(0.0) == 1.0 &&
	math.Abs(math.Log(math.E)-1.0) < 0.000001 &&
	math.Log10(100.0) == 2.0 &&
	math.IsNaN(math.NaN()) &&
	math.IsInf(math.Inf(1), 1) &&
	math.Pi > 3.14 &&
	math.E > 2.71
test.OutBool(ok)
`,
			Want: "true",
			Covers: []string{
				"Abs", "Ceil", "Floor", "Round", "Sqrt", "Pow", "Min", "Max",
				"Sin", "Cos", "Tan", "Exp", "Log", "Log10", "NaN", "IsNaN",
				"Inf", "IsInf",
			},
		},
	})
}
