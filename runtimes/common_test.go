package runtimes_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/runtimes"
)

func runTest(t *testing.T, code string) []string {
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

	var results []string
	executor.MustAddFunc("push", func(v any) {
		s := fmt.Sprintf("%v", v)
		s = strings.TrimPrefix(s, "&")
		s = strings.TrimPrefix(s, "{")
		s = strings.TrimSuffix(s, "}")
		results = append(results, s)
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
