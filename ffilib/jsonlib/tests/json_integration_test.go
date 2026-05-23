package jsonlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/jsonlib"
)

func TestJSON(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("encoding/json", jsonlib.JSON_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "map-roundtrip",
			Imports: []string{"encoding/json"},
			Body: `
data, err := json.Marshal(map[string]any{"name": "mini"})
if err != nil {
	panic(err)
}
obj, err := json.Unmarshal(data)
if err != nil {
	panic(err)
}
test.Out(obj.name)
`,
			Want:   "mini",
			Covers: []string{"Marshal", "Unmarshal"},
		},
		{
			Name:    "struct-roundtrip",
			Imports: []string{"encoding/json"},
			Decls: `
type Meta struct {
	Active bool
}

type Payload struct {
	Name string
	Meta Meta
}
`,
			Body: `
data, err := json.Marshal(Payload{Name: "mini", Meta: Meta{Active: true}})
if err != nil {
	panic(err)
}
obj, err := json.Unmarshal(data)
if err != nil {
	panic(err)
}
test.Out(obj.Name)
test.Out("|")
test.OutBool(obj.Meta.Active)
`,
			Want:   "mini|true",
			Covers: []string{"Marshal", "Unmarshal"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
