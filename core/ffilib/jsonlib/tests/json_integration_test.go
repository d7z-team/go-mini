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
