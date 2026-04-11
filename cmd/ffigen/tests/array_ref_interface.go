//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out array_ref_ffigen_test.go array_ref_interface.go
package tests

import (
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// ffigen:module copyback
type ArrayRefAPI interface {
	Rewrite(nums *ffigo.ArrayRef[int64]) int64
}

type ArrayRefHost struct{}

func (h *ArrayRefHost) Rewrite(nums *ffigo.ArrayRef[int64]) int64 {
	if nums == nil {
		return 0
	}
	next := make([]int64, 0, len(nums.Value)+1)
	for _, item := range nums.Value {
		next = append(next, item*10)
	}
	next = append(next, 99)
	nums.Value = next
	return int64(len(nums.Value))
}

func TestGeneratedArrayRefCopyBack(t *testing.T) {
	executor := engine.NewMiniExecutor()
	RegisterArrayRefAPI(executor, &ArrayRefHost{}, executor.HandleRegistry())

	code := `
	package main
	import "copyback"
	func main() {
		arr := []int64{1, 2, 3}
		n := copyback.Rewrite(arr)
		if n != 4 { panic("rewrite len") }
		if len(arr) != 4 { panic("rewrite copy-back len") }
		if arr[0] != 10 || arr[1] != 20 || arr[2] != 30 || arr[3] != 99 { panic("rewrite copy-back data") }
	}
	`
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
}

func TestGeneratedArrayRefProxyRoundTrip(t *testing.T) {
	registry := ffigo.NewHandleRegistry()
	bridge := &ArrayRefAPI_Bridge{Impl: &ArrayRefHost{}, Registry: registry}
	proxy := NewArrayRefAPIProxy(bridge, registry)

	nums := &ffigo.ArrayRef[int64]{Value: []int64{4, 5}}
	n := proxy.Rewrite(nums)
	if n != 3 {
		t.Fatalf("unexpected len: %d", n)
	}
	if len(nums.Value) != 3 || nums.Value[0] != 40 || nums.Value[1] != 50 || nums.Value[2] != 99 {
		t.Fatalf("unexpected proxy copy-back: %#v", nums.Value)
	}
}
