package jsonlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/jsonlib"
)

func TestJSONSurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("encoding/json", jsonlib.SurfaceJSON(&jsonlib.JSONHost{})),
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
name, ok := obj.name.(String)
if !ok {
	panic("name must be string")
}
test.Out(name)
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
name, ok := obj.Name.(String)
if !ok {
	panic("Name must be string")
}
test.Out(name)
test.Out("|")
active, ok := obj.Meta.Active.(Bool)
if !ok {
	panic("Active must be bool")
}
test.OutBool(active)
`,
			Want:   "mini|true",
			Covers: []string{"Marshal", "Unmarshal"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
