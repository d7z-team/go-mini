package jsonlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestMarshalUnmarshal(t *testing.T) {
	testutil.Run(t, `
package main
import "encoding/json"

func main() {
	data, err := json.Marshal(map[string]any{"name": "mini"})
	if err != nil { panic(err) }

	obj, err := json.Unmarshal(data)
	if err != nil || obj.name != "mini" {
		panic("json roundtrip failed")
	}
}
`)
}

func TestMarshalStruct(t *testing.T) {
	testutil.Run(t, `
package main
import "encoding/json"

type Profile struct {
	Name string
	Age int
}

func main() {
	p := Profile{
		Name: "mini",
		Age: 7,
	}

	data, err := json.Marshal(p)
	if err != nil { panic(err) }
	obj, err := json.Unmarshal(data)
	if err != nil { panic(err) }
	if obj.Name != "mini" {
		panic("unexpected struct name after marshal: " + string(data))
	}
	if obj.Age != 7 {
		panic("unexpected struct age after marshal: " + string(data))
	}
}
`)
}

func TestStructMarshalThenUnmarshal(t *testing.T) {
	testutil.Run(t, `
package main
import "encoding/json"

type Meta struct {
	Active bool
}

type Payload struct {
	Name string
	Meta Meta
}

func main() {
	p := Payload{
		Name: "mini",
		Meta: Meta{Active: true},
	}

	data, err := json.Marshal(p)
	if err != nil { panic(err) }

	obj, err := json.Unmarshal(data)
	if err != nil { panic(err) }
	if obj.Name != "mini" {
		panic("struct field lost after unmarshal")
	}
	if !obj.Meta.Active {
		panic("nested struct field lost after unmarshal")
	}
}
`)
}
