package filepathlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/testutil"
	"gopkg.d7z.net/go-mini/ffilib"
	"gopkg.d7z.net/go-mini/ffilib/filepathlib"
)

func TestFilepath(t *testing.T) {
	testutil.RunCases(t, []testutil.MethodSchema{
		testutil.SurfaceFFISchema("filepath", filepathlib.SurfaceFilepath(&filepathlib.FilepathHost{})),
	}, []testutil.Case{
		{
			Name:    "path-helpers",
			Imports: []string{"filepath"},
			Body: `
p := filepath.Join("a", "b", "c.txt")
dir, file := filepath.Split(p)
matched, err := filepath.Match("*.txt", file)
if err != nil {
	panic(err)
}
rel, err := filepath.Rel("a", p)
if err != nil {
	panic(err)
}
test.Out(filepath.Base(p))
test.Out("|")
test.Out(filepath.Clean("a/../a/b/"))
test.Out("|")
test.Out(filepath.Dir(p))
test.Out("|")
test.Out(filepath.Ext(p))
test.Out("|")
test.OutBool(filepath.IsAbs(p))
test.Out("|")
test.OutBool(matched)
test.Out("|")
test.Out(rel)
test.Out("|")
test.Out(dir)
test.Out("|")
test.Out(file)
test.Out("|")
test.Out(filepath.ToSlash(filepath.FromSlash("a/b")))
test.Out("|")
test.Out(filepath.VolumeName(p))
`,
			Want:   "c.txt|a/b|a/b|.txt|false|true|b/c.txt|a/b/|c.txt|a/b|",
			Covers: []string{"Base", "Clean", "Dir", "Ext", "IsAbs", "Join", "Match", "Rel", "Split", "ToSlash", "FromSlash", "VolumeName"},
		},
	}, testutil.WithSurface(ffilib.Surface()))
}
