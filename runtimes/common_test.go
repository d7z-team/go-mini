package runtimes_test

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/utils"
	"gopkg.d7z.net/go-mini/runtimes"
)

func runTest(t *testing.T, code string) []string {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		results = append(results, utils.FormatValue(v))
	})

	rt, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("failed to parse/validate: %v", err)
	}

	err = rt.Execute(context.Background())
	if err != nil {
		t.Fatalf("failed to execute: %v", err)
	}

	return results
}
