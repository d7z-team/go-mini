package ast

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseFunc(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		method, b := ParseMethod(reflect.TypeOf(func() {}))
		assert.True(t, b)
		assert.Equal(t, "function() Void", method.String())

		method, b = ParseMethod(reflect.TypeOf(func(MiniString) {}))
		assert.True(t, b)
		assert.Equal(t, "function(String) Void", method.String())

		method, b = ParseMethod(reflect.TypeOf(func(string) {}))
		assert.True(t, b)
		assert.Equal(t, "function(String) Void", method.String())
	})

	t.Run("any", func(t *testing.T) {
		method, b := ParseMethod(reflect.TypeOf(func(any) {}))
		assert.True(t, b)
		assert.Equal(t, "function(Any) Void", method.String())
	})
}
