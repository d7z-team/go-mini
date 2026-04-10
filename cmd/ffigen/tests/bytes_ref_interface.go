//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg tests -out bytes_ref_ffigen_test.go bytes_ref_interface.go
package tests

import (
	"bytes"
	"context"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ffigo"
)

// ffigen:module copyback
type BytesRefAPI interface {
	Mutate(buf *ffigo.BytesRef) int64
}

type BytesRefHost struct{}

func (h *BytesRefHost) Mutate(buf *ffigo.BytesRef) int64 {
	if buf == nil {
		return 0
	}
	buf.Value = append(bytes.ToUpper(buf.Value), '!')
	return int64(len(buf.Value))
}

func TestGeneratedBytesRefCopyBack(t *testing.T) {
	executor := engine.NewMiniExecutor()
	RegisterBytesRefAPI(executor, &BytesRefHost{}, executor.HandleRegistry())

	code := `
	package main
	import "copyback"
	func main() {
		buf := []byte("mini")
		n := copyback.Mutate(buf)
		if n != 5 { panic("mutate len") }
		if String(buf) != "MINI!" { panic("mutate copy-back") }
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
