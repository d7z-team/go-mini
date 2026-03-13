package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestAnyMethodCall(t *testing.T) {
	// Test calling a method on a value that is typed as Any in the AST
	// We use range loop which now declares variables as Any
	result, err := utils.TestGoExpr(`
s := "a,b,c".Split(",")
for i, v := range s {
	push(v)
}
`)
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}
