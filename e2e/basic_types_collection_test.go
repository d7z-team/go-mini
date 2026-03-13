package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestBasicTypesCollections(t *testing.T) {
	types := []string{"Bool", "Int64", "Float64", "String", "Uint8"}

	t.Run("array_all_basic_types", func(t *testing.T) {
		for _, typ := range types {
			t.Run(typ, func(t *testing.T) {
				var code string
				var expected []string

				switch typ {
				case "Bool":
					code = `arr := []Bool{true, false}; push(arr[0]); push(arr[1])`
					expected = []string{"true", "false"}
				case "Int64":
					code = `arr := []Int64{1, 2}; push(arr[0]); push(arr[1])`
					expected = []string{"1", "2"}
				case "Float64":
					code = `arr := []Float64{1.1, 2.2}; push(arr[0]); push(arr[1])`
					expected = []string{"1.1", "2.2"}
				case "String":
					code = `arr := []String{"a", "b"}; push(arr[0]); push(arr[1])`
					expected = []string{"a", "b"}
				case "Uint8":
					code = `arr := []Uint8{65, 66}; push(arr[0]); push(arr[1])`
					expected = []string{"65", "66"}
				}

				result, err := utils.TestGoExpr(code)
				assert.NoError(t, err, "Type: %s", typ)
				assert.Equal(t, expected, result, "Type: %s", typ)
			})
		}
	})

	t.Run("map_all_basic_types_key_string", func(t *testing.T) {
		for _, typ := range types {
			t.Run(typ, func(t *testing.T) {
				var code string
				var expected []string

				switch typ {
				case "Bool":
					code = `m := map[String]Bool{"k": true}; push(m["k"])`
					expected = []string{"true"}
				case "Int64":
					code = `m := map[String]Int64{"k": 42}; push(m["k"])`
					expected = []string{"42"}
				case "Float64":
					code = `m := map[String]Float64{"k": 3.14}; push(m["k"])`
					expected = []string{"3.14"}
				case "String":
					code = `m := map[String]String{"k": "v"}; push(m["k"])`
					expected = []string{"v"}
				case "Uint8":
					code = `m := map[String]Uint8{"k": 100}; push(m["k"])`
					expected = []string{"100"}
				}

				result, err := utils.TestGoExpr(code)
				assert.NoError(t, err, "Type: %s", typ)
				assert.Equal(t, expected, result, "Type: %s", typ)
			})
		}
	})

	t.Run("map_all_basic_types_key_int64", func(t *testing.T) {
		for _, typ := range types {
			t.Run(typ, func(t *testing.T) {
				var code string
				var expected []string

				switch typ {
				case "Bool":
					code = `m := map[Int64]Bool{1: true}; push(m[1])`
					expected = []string{"true"}
				case "Int64":
					code = `m := map[Int64]Int64{1: 42}; push(m[1])`
					expected = []string{"42"}
				case "Float64":
					code = `m := map[Int64]Float64{1: 3.14}; push(m[1])`
					expected = []string{"3.14"}
				case "String":
					code = `m := map[Int64]String{1: "v"}; push(m[1])`
					expected = []string{"v"}
				case "Uint8":
					code = `m := map[Int64]Uint8{1: 100}; push(m[1])`
					expected = []string{"100"}
				}

				result, err := utils.TestGoExpr(code)
				assert.NoError(t, err, "Type: %s", typ)
				assert.Equal(t, expected, result, "Type: %s", typ)
			})
		}
	})
}
