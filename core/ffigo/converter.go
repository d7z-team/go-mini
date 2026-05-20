package ffigo

import (
	"go/ast"
	"go/token"
	"hash/fnv"
	"reflect"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

type GoToASTConverter struct {
	fset       *token.FileSet
	imports    map[string]string // Alias -> Path
	interfaces map[string]*ast.InterfaceType
}

func (c *GoToASTConverter) genID(node ast.Node, meta string) string {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Ptr && reflect.ValueOf(node).IsNil()) {
		return "meta_" + meta
	}
	pos := c.fset.Position(node.Pos())
	h := fnv.New64a()
	// Using string concatenation is much faster than fmt.Fprintf for simple strings
	posStr := pos.Filename + ":" + strconv.Itoa(pos.Line) + ":" + strconv.Itoa(pos.Column) + ":" + meta
	h.Write([]byte(posStr))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *GoToASTConverter) extractLoc(node ast.Node) *miniast.Position {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Ptr && reflect.ValueOf(node).IsNil()) || c.fset == nil {
		return nil
	}
	start := c.fset.Position(node.Pos())
	if start.Line == 0 {
		return nil
	}
	end := c.fset.Position(node.End())
	return &miniast.Position{
		F:  start.Filename,
		L:  start.Line,
		C:  start.Column,
		EL: end.Line,
		EC: end.Column,
	}
}

func NewGoToASTConverter() *GoToASTConverter {
	return &GoToASTConverter{
		fset:       token.NewFileSet(),
		imports:    make(map[string]string),
		interfaces: make(map[string]*ast.InterfaceType),
	}
}
