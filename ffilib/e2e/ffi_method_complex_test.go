package e2e_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
)

func TestOSFileMethodsKeepHandleIsolation(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "complex_1.txt")
	secondPath := filepath.Join(dir, "complex_2.txt")
	leakPath := filepath.Join(dir, "leak_test.txt")

	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "multiple-file-methods",
			Imports: []string{"os"},
			Body: fmt.Sprintf(`
f1, err := os.Create(%q)
if err != nil {
	panic(err)
}
f2, err := os.Create(%q)
if err != nil {
	panic(err)
}

if _, err = f1.Write([]byte("file 1 content")); err != nil {
	panic(err)
}
if _, err = f2.Write([]byte("file 2 content")); err != nil {
	panic(err)
}
if err = f1.Close(); err != nil {
	panic(err)
}
if err = f2.Close(); err != nil {
	panic(err)
}

data1, err := os.ReadFile(%q)
if err != nil {
	panic(err)
}
data2, err := os.ReadFile(%q)
if err != nil {
	panic(err)
}
if err = os.Remove(%q); err != nil {
	panic(err)
}
if err = os.Remove(%q); err != nil {
	panic(err)
}
test.OutBytes(data1)
test.Out("|")
test.OutBytes(data2)
`, firstPath, secondPath, firstPath, secondPath, firstPath, secondPath),
			Want: "file 1 content|file 2 content",
		},
		{
			Name:           "nil-file-method-rejected",
			Imports:        []string{"os"},
			Body:           "var f *os.File\nf.Close()\n",
			WantCompileErr: "<any>",
		},
		{
			Name:    "unclosed-handles-do-not-break-cleanup",
			Imports: []string{"os"},
			Body: fmt.Sprintf(`
for i := 0; i < 10; i++ {
	_, err := os.Create(%q)
	if err != nil {
		panic(err)
	}
	if err = os.Remove(%q); err != nil {
		panic(err)
	}
}
test.Out("ok")
`, leakPath, leakPath),
			Want: "ok",
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
