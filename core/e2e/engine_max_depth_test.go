package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/runtimes"
)

func TestMaxStackDepth(t *testing.T) {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	code := `
		func main() {
			a := 1
			if a == 1 {
				if a == 1 {
					if a == 1 {
						if a == 1 {
							if a == 1 {
								if a == 1 {
									a = 2
								}
							}
						}
					}
				}
			}
		}
	`
	rt, err := executor.NewRuntimeByGoCode(code)
	if !assert.NoError(t, err) {
		return
	}

	// Set a very small stack limit (e.g. 3) to trigger the limit
	ctx := context.WithValue(context.Background(), runtime.ContextKeyMaxStackDepth, 3)
	err = rt.Execute(ctx)
	
	// Expect a stack overflow error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stack overflow")
}