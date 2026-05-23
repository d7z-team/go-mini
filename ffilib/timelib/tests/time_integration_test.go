package timelib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/timelib"
)

func TestTime(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("time", timelib.Module_FFI_Schemas),
		testutil.FFISchema("time.Time", timelib.Time_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "module-and-time-methods",
			Imports: []string{"time"},
			Body: `
now := time.Now()
epoch := time.Unix(1700000000, 123000000)
parsed, err := time.Parse(time.RFC3339, "2024-01-02T03:04:05Z")
if err != nil {
	panic(err)
}
d, err := time.ParseDuration("1h")
if err != nil {
	panic(err)
}
later := parsed.Add(d)
time.Sleep(0)

test.OutBool(now.Unix() > 0)
test.Out("|")
test.OutBool(time.Since(now) >= 0)
test.Out("|")
test.OutBool(time.Until(later) != 0)
test.Out("|")
test.OutInt(parsed.Year())
test.Out("|")
test.OutInt(parsed.Month())
test.Out("|")
test.OutInt(parsed.Day())
test.Out("|")
test.OutInt(parsed.Hour())
test.Out("|")
test.OutInt(parsed.Minute())
test.Out("|")
test.OutInt(parsed.Second())
test.Out("|")
test.OutInt(epoch.Nanosecond())
test.Out("|")
test.OutInt(epoch.Unix())
test.Out("|")
test.OutBool(epoch.UnixMilli() > 0)
test.Out("|")
test.OutBool(epoch.UnixMicro() > 0)
test.Out("|")
test.OutBool(epoch.UnixNano() > 0)
test.Out("|")
test.Out(parsed.Format("2006-01-02"))
test.Out("|")
test.OutBool(later.Sub(parsed) == d)
test.Out("|")
test.OutBool(parsed.Before(later))
test.Out("|")
test.OutBool(later.After(parsed))
test.Out("|")
test.OutBool(parsed.Equal(parsed))
test.Out("|")
test.OutBool(!parsed.IsZero())
test.Out("|")
test.OutBool(parsed.String() != "")
test.Out("|")
test.OutBool(time.Second > 0)
`,
			Want:   "true|true|true|2024|1|2|3|4|5|123000000|1700000000|true|true|true|2024-01-02|true|true|true|true|true|true|true",
			Covers: []string{"Now", "Unix", "Sleep", "Since", "Until", "Parse", "ParseDuration", "Year", "Month", "Day", "Hour", "Minute", "Second", "Nanosecond", "UnixMilli", "UnixMicro", "UnixNano", "Format", "Add", "Sub", "IsZero", "Before", "After", "Equal", "String"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
