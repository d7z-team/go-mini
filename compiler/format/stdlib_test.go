package format_test

import (
	"testing"

	miniformat "github.com/d7z-team/mini-go/compiler/format"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
	"github.com/d7z-team/mini-go/stdlib"
)

func TestStandardLibraryFormatsSafely(t *testing.T) {
	sources, err := workspace.StandardLibrary(stdlib.Open())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := sources.PackagePaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, modulePath := range paths {
		pkg, ok, err := sources.Package(modulePath)
		if err != nil || !ok {
			t.Fatalf("load %s: ok=%v err=%v", modulePath, ok, err)
		}
		allFiles := append(append([]source.File(nil), pkg.Files...), pkg.TestFiles...)
		for _, file := range allFiles {
			file := file
			t.Run(modulePath+"/"+file.Path, func(t *testing.T) {
				result := miniformat.Source(modulePath, file.Path, file.Text)
				if len(result.Diagnostics) != 0 {
					t.Fatalf("format diagnostics: %+v", result.Diagnostics)
				}
				second := miniformat.Source(modulePath, file.Path, result.Text)
				if len(second.Diagnostics) != 0 || second.Text != result.Text {
					t.Fatalf("format is not stable: diagnostics=%+v", second.Diagnostics)
				}
			})
		}
	}
}
