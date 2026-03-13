package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

type FileInfo struct {
	Name string
	Size int64
}

func (f *FileInfo) OPSType() ast.Ident {
	return "fs.FileInfo"
}

func (f *FileInfo) GetName() *ast.MiniString {
	s := ast.NewMiniString(f.Name)
	return &s
}

func TestNamespaceInjection(t *testing.T) {
	e := engine.NewMiniExecutor()

	// Inject a function into the "fs" package
	e.MustAddFunc("fs.ReadFile", func(path *ast.MiniString) *ast.MiniString {
		s := ast.NewMiniString("content of " + path.GoString())
		return &s
	})

	// Inject a struct into the "fs" package
	e.AddNativeStruct((*FileInfo)(nil))

	// Also add a method that returns this struct
	e.MustAddFunc("fs.Stat", func(path *ast.MiniString) *FileInfo {
		return &FileInfo{Name: path.GoString(), Size: 1024}
	})

	// Add basic assertions or just print to test
	var results []string
	e.MustAddFunc("push", func(s *ast.MiniString) {
		results = append(results, s.GoString())
	})

	// 确认 GenDecl 的 Mangling

	libCode := `
package mylib
var X = "hello"
func GetX() string { return X }
`
	prog, err := e.NewRuntimeByGoCode(libCode)
	assert.NoError(t, err)

	libAst := prog.GetProgram()

	// 检查变量名是否被转义
	foundMangled := false
	for i, child := range libAst.Main {
		t.Logf("Main[%d]: %T", i, child)
		if gen, ok := child.(*ast.GenDeclStmt); ok {
			t.Logf("Found GenDeclStmt: %s", gen.Name)
			if gen.Name == "mylib.X" {
				foundMangled = true
			}
		} else if block, ok := child.(*ast.BlockStmt); ok {
			for j, c := range block.Children {
				t.Logf("  Block[%d]: %T", j, c)
				if gen, ok := c.(*ast.GenDeclStmt); ok {
					t.Logf("  Found GenDeclStmt in block: %s", gen.Name)
					if gen.Name == "mylib.X" {
						foundMangled = true
					}
				}
			}
		}
	}
	assert.True(t, foundMangled, "Global variable X should be mangled to mylib.X")
}
