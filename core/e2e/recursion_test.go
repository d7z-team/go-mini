package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestRecursion(t *testing.T) {
	t.Run("factorial", func(t *testing.T) {
		code := `
func factorial(n Int64) Int64 {
	if n <= 1 {
		return 1
	}
	return n * factorial(n - 1)
}

func main() {
	push(factorial(5))
}
`
		result, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		assert.Equal(t, []string{"120"}, result)
	})

	t.Run("fibonacci", func(t *testing.T) {
		code := `
func fib(n Int64) Int64 {
	if n <= 1 {
		return n
	}
	return fib(n - 1) + fib(n - 2)
}

func main() {
	push(fib(7))
}
`
		result, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		assert.Equal(t, []string{"13"}, result)
	})

	t.Run("mutual_recursion", func(t *testing.T) {
		code := `
func is_even(n Int64) Bool {
	if n == 0 {
		return true
	}
	return is_odd(n - 1)
}

func is_odd(n Int64) Bool {
	if n == 0 {
		return false
	}
	return is_even(n - 1)
}

func main() {
	push(is_even(10))
	push(is_even(11))
}
`
		result, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		assert.Equal(t, []string{"true", "false"}, result)
	})

	t.Run("method_recursion", func(t *testing.T) {
		code := `
type Math struct {}

func (m Math) factorial(n Int64) Int64 {
	if n <= 1 {
		return 1
	}
	return n * m.factorial(n - 1)
}

func main() {
	m := Math{}
	push(m.factorial(5))
}
`
		result, err := utils.TestGoCode(code)
		assert.NoError(t, err)
		assert.Equal(t, []string{"120"}, result)
	})
}
