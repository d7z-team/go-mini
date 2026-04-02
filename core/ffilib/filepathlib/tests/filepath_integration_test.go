package filepathlib_test

import (
	"testing"

	"gopkg.d7z.net/go-mini/core/ffilib/internal/testutil"
)

func TestJoinAndSplit(t *testing.T) {
	testutil.Run(t, `
package main
import "filepath"

func main() {
	p := filepath.Join("a", "b", "c.txt")
	dir, file := filepath.Split(p)
	if filepath.Base(p) != "c.txt" || file != "c.txt" || dir != filepath.Join("a", "b") + "/" {
		panic("filepath failed")
	}
}
`)
}
