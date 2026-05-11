package tests

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
)

func TestDefaultIOModuleSchemaIsAvailableWithoutHostFileAccess(t *testing.T) {
	e := engine.NewMiniExecutor()
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
	e := engine.NewMiniExecutor()
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

func TestDefaultFileSystemAccessRemainsBlocked(t *testing.T) {
	e := engine.NewMiniExecutor()
	code := `package main
		import "os"
		func main() {
			_, _ = os.Open("test.txt")
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected os access to stay unavailable without InjectStandardLibraries")
	}
}

func TestDefaultIOFileHandleRemainsBlocked(t *testing.T) {
	e := engine.NewMiniExecutor()
	code := `package main
		import "io"
		func main() {
			var f io.File
			_ = f.Name()
		}`
	_, err := e.NewRuntimeByGoCode(code)
	if err == nil {
		t.Fatal("expected io.File to stay unavailable without InjectStandardLibraries")
	}
}

func TestInjectStandardLibrariesEnablesFileSystemSurface(t *testing.T) {
	e := engine.NewMiniExecutor()
	e.InjectStandardLibraries()

	code := `package main
		import "os"
		func main() {
			f, err := os.Open("test.txt")
			_, _ = f, err
		}`
	if _, err := e.NewRuntimeByGoCode(code); err != nil {
		t.Fatalf("expected InjectStandardLibraries to enable os/io file surface, got: %v", err)
	}
}
