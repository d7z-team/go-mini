package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestDeferRecover(t *testing.T) {
	executor := engine.MustNewMiniExecutor()
	code := `
package main

var res = "initial"

func test() {
	defer func() {
		if r := recover(); r != nil {
			res = "recovered: " + string(r)
		}
	}()
	panic("boom")
}

func main() {
	test()
	if res != "recovered: boom" {
		panic("unexpected res: " + res)
	}
}
`
	runtime, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
}
