package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestParserRobustness(t *testing.T) {
	t.Run("InvalidJSON", func(t *testing.T) {
		_, err := engine.Unmarshal([]byte(`{invalid: json}`))
		assert.Error(t, err)
	})

	t.Run("UnknownMeta", func(t *testing.T) {
		jsonData := []byte(`[{"meta": "unknown_type", "id": "1"}]`)
		_, err := engine.Unmarshal(jsonData)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown type")
	})

	t.Run("MissingMeta", func(t *testing.T) {
		jsonData := []byte(`[{"id": "1"}]`)
		_, err := engine.Unmarshal(jsonData)
		assert.Error(t, err)
	})
}

func TestTypeSystemEdgeCases(t *testing.T) {
	t.Run("DeeplyNestedArray", func(t *testing.T) {
		// Array<Array<Array<...>>>
		typeStr := "Array<Array<Array<Array<Int64>>>>"
		miniType := ast.OPSType(typeStr)

		assert.True(t, miniType.IsArray())

		elem, ok := miniType.ReadArrayItemType()
		assert.True(t, ok)
		assert.Equal(t, ast.OPSType("Array<Array<Array<Int64>>>"), elem)

		elem2, _ := elem.ReadArrayItemType()
		elem3, _ := elem2.ReadArrayItemType()
		elem4, _ := elem3.ReadArrayItemType()
		assert.Equal(t, ast.OPSType("Int64"), elem4)
	})

	t.Run("ComplexMap", func(t *testing.T) {
		typeStr := "Map<String, Array<Ptr<Int64>>>"
		miniType := ast.OPSType(typeStr)

		assert.True(t, miniType.IsMap())
		k, v, ok := miniType.GetMapKeyValueTypes()
		assert.True(t, ok)
		assert.Equal(t, ast.OPSType("String"), k)
		assert.Equal(t, ast.OPSType("Array<Ptr<Int64>>"), v)
	})
}

func TestValidationErrors(t *testing.T) {
	t.Run("UndefinedVariable", func(t *testing.T) {
		code := `
package main
func main() {
	println(undefinedVar)
}
`
		_, err := utils.TestGoCode(code)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "变量 undefinedVar 不存在")
	})

	t.Run("TypeMismatchAssignment", func(t *testing.T) {
		code := `
package main
func main() {
	var a int = 10
	a = "string"
}
`
		e := engine.NewMiniExecutor()
		_, err := e.NewRuntimeByGoCode(code)
		assert.Error(t, err)
		// The error message might vary based on implementation
	})
}
