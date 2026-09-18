package e2e_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
)

func TestMethodValueExtractionPreservesReceiverBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "method_val.txt")

	testutil.RunCases(t, nil, []testutil.Case{
		{
			Name:    "file-write-method-value",
			Imports: []string{"os"},
			Body: fmt.Sprintf(`
f, err := os.Create(%q)
if err != nil {
	panic(err)
}
writeFn := f.Write
if _, err = writeFn([]byte("Hello Method Value")); err != nil {
	panic(err)
}
if err = f.Close(); err != nil {
	panic(err)
}
data, err := os.ReadFile(%q)
if err != nil {
	panic(err)
}
if err = os.Remove(%q); err != nil {
	panic(err)
}
test.OutBytes(data)
`, path, path, path),
			Want: "Hello Method Value",
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
