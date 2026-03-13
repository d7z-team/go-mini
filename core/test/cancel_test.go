package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.d7z.net/go-mini/core"
)

func TestContextCancellation(t *testing.T) {
	miniExecutor := engine.NewMiniExecutor()

	// A script with an infinite loop
	program, err := miniExecutor.NewRuntimeByGoExpr(`
for true {
	// spin
}
`)
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = program.Execute(ctx)
	duration := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	assert.Less(t, duration, 500*time.Millisecond, "Should have cancelled quickly")
}
