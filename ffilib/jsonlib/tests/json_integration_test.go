package jsonlib_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/ffilib"
)

func TestJSONSurfaceIntegration(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.Schema("encoding/json", "Decode", "Marshal", "Unmarshal"),
	}, []testutil.Case{
		{
			Name:    "decode-dynamic-object",
			Imports: []string{"encoding/json"},
			Body: `
data, err := json.Marshal(map[string]any{"name": "mini"})
if err != nil {
	panic(err)
}
obj, err := json.Decode(data)
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
			Covers: []string{"Decode", "Marshal"},
		},
		{
			Name:    "unmarshal-typed-struct-roundtrip",
			Imports: []string{"encoding/json"},
			Decls: `
type Meta struct {
	Active bool
}

type Payload struct {
	Name string ` + "`json:\"name\"`" + `
	Meta Meta ` + "`json:\"meta\"`" + `
	Skip func() int64 ` + "`json:\"-\"`" + `
}
`,
			Body: `
data, err := json.Marshal(Payload{Name: "mini", Meta: Meta{Active: true}})
if err != nil {
	panic(err)
}
var payload Payload
if err := json.Unmarshal(data, &payload); err != nil {
	panic(err)
}
test.Out(payload.Name)
test.Out("|")
test.OutBool(payload.Meta.Active)
`,
			Want:   "mini|true",
			Covers: []string{"Marshal", "Unmarshal"},
		},
		{
			Name:    "unmarshal-rejects-pointer-field",
			Imports: []string{"encoding/json"},
			Decls: `
type Payload struct {
	Value *int64
}
`,
			Body: `
data := []byte("{\"Value\":1}")
var payload Payload
err := json.Unmarshal(data, &payload)
if err == nil {
	panic("expected unsupported pointer field")
}
test.Out(err.Error())
`,
			Want:   "json: field Value: json: unsupported target type Ptr<Int64>",
			Covers: []string{"Unmarshal"},
		},
		{
			Name:    "marshal-rejects-pointer-in-vm-any",
			Imports: []string{"encoding/json"},
			Body: `
n := int64(1)
var value any = &n
_, err := json.Marshal(map[string]any{"ptr": value})
if err == nil {
	panic("expected pointer marshal rejection")
}
test.Out(err.Error())
`,
			Want:   "json: pointer values are not supported",
			Covers: []string{"Marshal"},
		},
		{
			Name:    "marshal-rejects-cyclic-vm-any-value",
			Imports: []string{"encoding/json"},
			Body: `
	value := map[string]any{}
	value["self"] = value
	_, err := json.Marshal(value)
	if err == nil {
		panic("expected cyclic value marshal rejection")
	}
	test.Out(err.Error())
	`,
			Want:   "json: unsupported cyclic or too deeply nested value",
			Covers: []string{"Marshal"},
		},
		{
			Name:    "unmarshal-is-regular-function-value",
			Imports: []string{"encoding/json"},
			Decls: `
type Payload struct {
	Name string
}
`,
			Body: `
f := json.Unmarshal
var payload Payload
var out any = &payload
if err := f([]byte("{\"Name\":\"mini\"}"), out); err != nil {
	panic(err)
}
test.Out(payload.Name)
`,
			Want:   "mini",
			Covers: []string{"Unmarshal"},
		},
		{
			Name:    "unmarshal-composite-any-and-bytes",
			Imports: []string{"encoding/json"},
			Decls: `
type Payload struct {
	Raw []byte
	Items []any
	Lookup map[string]any
}
`,
			Body: `
var out Payload
err := json.Unmarshal([]byte("{\"Raw\":\"go\", \"Items\":[1,\"x\",true], \"Lookup\":{\"n\":2,\"s\":\"ok\"}}"), &out)
if err != nil {
	panic(err)
}
if string(out.Raw) != "go" {
	panic("bad bytes field")
}
if len(out.Items) != 3 || out.Items[0].(Float64) != 1 || out.Items[1].(String) != "x" || out.Items[2].(Bool) != true {
	panic("bad any array field")
}
if out.Lookup["n"].(Float64) != 2 || out.Lookup["s"].(String) != "ok" {
	panic("bad any map field")
}
test.Out("ok")
`,
			Want:   "ok",
			Covers: []string{"Unmarshal"},
		},
		{
			Name:    "decode-rejects-invalid-string-escape",
			Imports: []string{"encoding/json"},
			Body: `
_, err := json.Decode([]byte("\"\\x41\""))
if err == nil {
	panic("expected invalid escape")
}
test.Out("invalid")
`,
			Want:   "invalid",
			Covers: []string{"Decode"},
		},
		{
			Name:    "unmarshal-int64-is-exact",
			Imports: []string{"encoding/json"},
			Body: `
var n int64
if err := json.Unmarshal([]byte("9007199254740993"), &n); err != nil {
	panic(err)
}
if n != 9007199254740993 {
	panic("int64 precision was lost")
}
err := json.Unmarshal([]byte("9223372036854775808"), &n)
if err == nil {
	panic("expected int64 overflow")
}
test.Out("exact")
`,
			Want:   "exact",
			Covers: []string{"Unmarshal"},
		},
		{
			Name:    "json-public-members-do-not-leak-internal-api",
			Imports: []string{"encoding/json", "reflect"},
			Body: `
pkg, ok := reflect.Package("encoding/json")
if !ok {
	panic("encoding/json package not found")
}
for _, member := range reflect.Members(pkg) {
	if member.Name == "ConvertDecodedValue" {
		panic("private json helper leaked")
	}
}
test.Out("ok")
`,
			Want: "ok",
		},
	}, testutil.WithSurface(ffilib.Surface()))
}

func TestJSONUnmarshalWorksInsideSurfaceLibrary(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	if err := executor.UseSurface(surface.Merge(
		ffilib.Surface(),
		surface.Library("app", surface.GoFile("app.mgo", `
package app

import "encoding/json"

type Payload struct {
	Name string
}

func Decode(data []byte) (Payload, error) {
	var out Payload
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}
`)),
	)); err != nil {
		t.Fatalf("register surface: %v", err)
	}

	prog, err := executor.NewRuntimeByGoCode(`
package main

import "app"

func main() {
	value, err := app.Decode([]byte("{\"Name\":\"mini\"}"))
	if err != nil {
		panic(err)
	}
	if value.Name != "mini" {
		panic("bad decoded value")
	}
}
`)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}
