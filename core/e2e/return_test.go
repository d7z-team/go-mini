package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core/utils"
)

func TestVoidReturn(t *testing.T) {
	t.Run("return-in-if", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return(a int) {
	push("start")
	if a > 0 {
		push("in-if")
		return
	}
	push("end-of-func")
}

func main() {
	test_return(10)
	push("after-call")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"start", "in-if", "after-call"}, data)
	})

	t.Run("return-top-level", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return() {
	push("start")
	return
	push("unreachable")
}

func main() {
	test_return()
	push("done")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"start", "done"}, data)
	})

	t.Run("return-in-for", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return() {
	for i := 0; i < 5; i++ {
		push(i)
		if i == 2 {
			return
		}
	}
	push("end-of-func")
}

func main() {
	test_return()
	push("done")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"0", "1", "2", "done"}, data)
	})

	t.Run("return-in-nested", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return() {
	if true {
		for i := 0; i < 1; i++ {
			if true {
				push("deep")
				return
			}
		}
	}
	push("fail")
}

func main() {
	test_return()
	push("ok")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"deep", "ok"}, data)
	})

	t.Run("return-in-if-sibling", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return(a int) {
	if a > 0 {
		push("in-if")
		return
	}
	push("after-if-should-not-run-if-a-gt-0")
}

func main() {
	test_return(10)
	push("done")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"in-if", "done"}, data)
	})

	t.Run("return-in-if-else", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return(a int) {
	if a > 0 {
		if a > 5 {
			push("gt-5")
			return
		} else {
			push("le-5")
		}
		push("should-not-run-if-gt-5")
	} else {
		push("le-0")
	}
	push("end-of-func")
}

func main() {
	test_return(10)
	push("done")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"gt-5", "done"}, data)
	})

	t.Run("return-in-deep-if", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return() {
	if true {
		if true {
			if true {
				push("deep")
				return
			}
			push("fail-3")
		}
		push("fail-2")
	}
	push("fail-1")
}

func main() {
	test_return()
	push("done")
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"deep", "done"}, data)
	})

	t.Run("return-in-switch", func(t *testing.T) {
		data, err := utils.TestGoCode(`
func test_return(v int) {
	switch v {
	case 1:
		push("one")
		return
	case 2:
		push("two")
	}
	push("after-switch")
}

func main() {
	test_return(1)
	test_return(2)
}
`)
		assert.NoError(t, err)
		assert.Equal(t, []string{"one", "two", "after-switch"}, data)
	})
}
