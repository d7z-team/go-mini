package e2e_test

import (
	"testing"
)

func TestDefaultIOModuleSchemaNoHostAccess(t *testing.T) {
	e := newStdExecutor()
	code := `package main
		import "io"
		func takeReader(r io.Reader) int64 {
			_ = io.SeekStart
			return 0
		}
		func takeWriter(w io.Writer) int64 {
			_ = io.SeekEnd
			return 0
		}
		func main() {}`
	if _, err := e.NewRuntimeByGoCode(code); err != nil {
		t.Fatalf("expected safe io schema to be available by default, got: %v", err)
	}
}

func TestDefaultImageModuleIsAllowed(t *testing.T) {
	e := newStdExecutor()
	code := `package main
		import "image"
		func main() {
			img := image.NewRGBA(2, 3)
			_, _ = img.Size()
		}`
	if _, err := e.NewRuntimeByGoCode(code); err != nil {
		t.Fatalf("expected image module to be available by default, got: %v", err)
	}
}

func TestSurfaceEnablesFileSystemSurface(t *testing.T) {
	e := newStdExecutor()

	code := `package main
		import "os"
		func main() {
			f, err := os.Open("test.txt")
			_, _ = f, err
		}`
	if _, err := e.NewRuntimeByGoCode(code); err != nil {
		t.Fatalf("expected ffilib.Surface to enable os/io file surface, got: %v", err)
	}
}
