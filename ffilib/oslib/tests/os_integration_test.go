package oslib_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/oslib"
)

func TestFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "os.txt")
	createdPath := filepath.Join(dir, "created.txt")
	openFilePath := filepath.Join(dir, "openfile.txt")
	t.Setenv("GO_MINI_TEST", "rocks")
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.FFISchema("os", oslib.OS_FFI_Schemas),
	}, []testutil.Case{
		{
			Name:    "files-env-and-constants",
			Imports: []string{"os", "io"},
			Body: fmt.Sprintf(`
if err := os.WriteFile(%q, []byte("hello")); err != nil {
	panic(err)
}
data, err := os.ReadFile(%q)
if err != nil {
	panic(err)
}
f, err := os.Open(%q)
if err != nil {
	panic(err)
}
opened, err := io.ReadAll(f)
if err != nil {
	panic(err)
}
if err = f.Close(); err != nil {
	panic(err)
}
created, err := os.Create(%q)
if err != nil {
	panic(err)
}
if err = created.Close(); err != nil {
	panic(err)
}
openedFile, err := os.OpenFile(%q, os.O_CREATE|os.O_RDWR, 0644)
if err != nil {
	panic(err)
}
if err = openedFile.Close(); err != nil {
	panic(err)
}
if err := os.Remove(%q); err != nil {
	panic(err)
}
if err := os.Remove(%q); err != nil {
	panic(err)
}
if err := os.Remove(%q); err != nil {
	panic(err)
}
test.OutBytes(data)
test.Out("|")
test.OutBytes(opened)
test.Out("|")
test.Out(os.Getenv("GO_MINI_TEST"))
test.Out("|")
test.OutBool((os.O_CREATE | os.O_RDWR) != 0)
`, path, path, path, createdPath, openFilePath, path, createdPath, openFilePath),
			Want:   "hello|hello|rocks|true",
			Covers: []string{"Open", "Create", "OpenFile", "ReadFile", "WriteFile", "Remove", "Getenv"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
