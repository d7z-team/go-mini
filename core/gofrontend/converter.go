package gofrontend

import (
	"fmt"
	"go/ast"
	"go/token"
	"hash/fnv"
	"reflect"
	"strconv"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

type Converter struct {
	fset       *token.FileSet
	imports    map[string]string // Alias -> Path
	interfaces map[string]*ast.InterfaceType
	errs       []error
}

type ConvertError struct {
	Pos     *miniast.Position
	Message string
}

func (e *ConvertError) Error() string {
	if e == nil {
		return ""
	}
	if e.Pos != nil && e.Pos.L > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Pos.F, e.Pos.L, e.Pos.C, e.Message)
	}
	return e.Message
}

func (c *Converter) reset() {
	c.fset = token.NewFileSet()
	c.imports = make(map[string]string)
	c.interfaces = make(map[string]*ast.InterfaceType)
	c.errs = nil
}

func (c *Converter) addError(node ast.Node, message string) {
	c.errs = append(c.errs, &ConvertError{Pos: c.extractLoc(node), Message: message})
}

func (c *Converter) badExpr(node ast.Node, message string) miniast.Expr {
	c.addError(node, message)
	return &miniast.BadExpr{
		BaseNode: miniast.BaseNode{ID: c.genID(node, "bad_expr"), Meta: "bad_expr", Loc: c.extractLoc(node), InvalidCause: message},
		RawText:  message,
	}
}

func (c *Converter) badStmt(node ast.Node, message string) miniast.Stmt {
	c.addError(node, message)
	return &miniast.BadStmt{
		BaseNode: miniast.BaseNode{ID: c.genID(node, "bad_stmt"), Meta: "bad_stmt", Loc: c.extractLoc(node), InvalidCause: message},
		RawText:  message,
	}
}

func (c *Converter) genID(node ast.Node, meta string) string {
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

func (c *Converter) extractLoc(node ast.Node) *miniast.Position {
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

func NewConverter() *Converter {
	return &Converter{
		fset:       token.NewFileSet(),
		imports:    make(map[string]string),
		interfaces: make(map[string]*ast.InterfaceType),
	}
}
