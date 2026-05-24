package iolib_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/iolib"
)

func TestIO(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "io.txt")
	secondPath := filepath.Join(dir, "io-second.txt")

	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("io", iolib.SurfaceIO(&iolib.IOHost{})),
		testutil.SurfaceFFISchema("io.File", iolib.SurfaceFile()),
	}, []testutil.Case{
		{
			Name:    "module-and-file-operations",
			Imports: []string{"io", "os"},
			Body: fmt.Sprintf(`
f, err := os.Create(%q)
if err != nil {
	panic(err)
}
n, err := f.Write([]byte("hello world"))
if err != nil {
	panic(err)
}
if _, err = f.WriteAt([]byte("MINI"), 6); err != nil {
	panic(err)
}
if _, err = f.Seek(0, io.SeekStart); err != nil {
	panic(err)
}
buf := []byte(".....")
readN, err := f.Read(buf)
if err != nil {
	panic(err)
}
atBuf := []byte("....")
readAtN, err := f.ReadAt(atBuf, 6)
if err != nil {
	panic(err)
}
if err = f.Truncate(10); err != nil {
	panic(err)
}
if err = f.Sync(); err != nil {
	panic(err)
}
if err = f.Close(); err != nil {
	panic(err)
}

src, err := os.Open(%q)
if err != nil {
	panic(err)
}
all, err := io.ReadAll(src)
if err != nil {
	panic(err)
}
if err = src.Close(); err != nil {
	panic(err)
}

dst, err := os.Create(%q)
if err != nil {
	panic(err)
}
copySrc, err := os.Open(%q)
if err != nil {
	panic(err)
}
copied, err := io.Copy(dst, copySrc)
if err != nil {
	panic(err)
}
if err = copySrc.Close(); err != nil {
	panic(err)
}
written, err := io.WriteString(dst, "!")
if err != nil {
	panic(err)
}
fileWritten, err := dst.WriteString("?")
if err != nil {
	panic(err)
}
nativeWritten, err := dst.WriteNative([]byte("."))
if err != nil {
	panic(err)
}
name := dst.Name()
if err = dst.Close(); err != nil {
	panic(err)
}

test.OutInt(n)
test.Out("|")
test.OutInt(readN)
test.Out("|")
test.OutBytes(buf)
test.Out("|")
test.OutInt(readAtN)
test.Out("|")
test.OutBytes(atBuf)
test.Out("|")
test.OutBytes(all)
test.Out("|")
test.OutInt(copied)
test.Out("|")
test.OutInt(written)
test.Out("|")
test.OutInt(fileWritten)
test.Out("|")
test.OutInt(nativeWritten)
test.Out("|")
test.OutBool(name != "")
`, filePath, filePath, secondPath, filePath),
			Want:   "11|5|hello|4|MINI|hello MINI|10|1|1|1|true",
			Covers: []string{"ReadAll", "Copy", "WriteString", "Write", "Read", "WriteAt", "ReadAt", "Seek", "Close", "Sync", "Truncate", "Name", "WriteNative"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
