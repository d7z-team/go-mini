package ffigo

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	miniast "gopkg.d7z.net/go-mini/core/ast"
)

func (c *GoToASTConverter) ConvertSource(filename, code string) (miniast.Node, error) {
	node, errs := c.convert(filename, code, false)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return node, nil
}

func (c *GoToASTConverter) ConvertSourceTolerant(filename, code string) (miniast.Node, []error) {
	return c.convert(filename, code, true)
}

func (c *GoToASTConverter) convert(filename, code string, tolerant bool) (miniast.Node, []error) {
	mode := parser.ParseComments
	if tolerant {
		mode |= parser.AllErrors
	}
	f, err := parser.ParseFile(c.fset, filename, code, mode)
	var errs []error
	if err != nil {
		if f == nil && !tolerant {
			return nil, []error{err}
		}
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				errs = append(errs, e)
			}
		} else {
			errs = append(errs, err)
		}
	}

	// 记录导入
	c.imports = make(map[string]string)
	var miniImports []miniast.ImportSpec
	if f != nil {
		for _, imp := range f.Imports {
			if len(imp.Path.Value) < 2 {
				continue
			}
			path := imp.Path.Value
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			} else {
				path = path[1 : len(path)-1]
			}
			var alias string
			if imp.Name != nil {
				alias = imp.Name.Name
			} else {
				parts := strings.Split(path, "/")
				alias = parts[len(parts)-1]
			}
			c.imports[alias] = path
			miniImports = append(miniImports, miniast.ImportSpec{
				Alias: alias,
				Path:  path,
			})
		}
	}

	program := &miniast.ProgramStmt{
		BaseNode:   miniast.BaseNode{ID: c.genID(f, "boot"), Meta: "boot", Type: "Void", Loc: c.extractLoc(f)},
		Constants:  make(map[string]string),
		Variables:  make(map[miniast.Ident]miniast.Expr),
		Types:      make(map[miniast.Ident]miniast.GoMiniType),
		Structs:    make(map[miniast.Ident]*miniast.StructStmt),
		Interfaces: make(map[miniast.Ident]*miniast.InterfaceStmt),
		Functions:  make(map[miniast.Ident]*miniast.FunctionStmt),
		Imports:    miniImports,
	}
	if f != nil {
		program.Package = f.Name.Name
	}

	for i, imp := range miniImports {
		program.Variables[miniast.Ident(imp.Alias)] = &miniast.ImportExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(f.Imports[i], "import"), Meta: "import", Type: miniast.TypeModule},
			Path:     imp.Path,
		}
	}

	if f != nil {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				fn := c.convertFunc(d)
				key := fn.Name
				if fn.ReceiverType != "" {
					key = miniast.Ident(string(fn.ReceiverType) + "." + string(fn.Name))
				}
				program.Functions[key] = fn
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if st, ok := s.Type.(*ast.StructType); ok {
							var doc string
							if s.Doc != nil {
								doc = s.Doc.Text()
							} else if d.Doc != nil {
								doc = d.Doc.Text()
							}
							program.Structs[miniast.Ident(s.Name.Name)] = c.convertStruct(s.Name.Name, st, doc)
						} else if it, ok := s.Type.(*ast.InterfaceType); ok {
							c.interfaces[s.Name.Name] = it // 记录接口定义
							program.Interfaces[miniast.Ident(s.Name.Name)] = &miniast.InterfaceStmt{
								BaseNode: miniast.BaseNode{
									ID:   c.genID(s, "interface"),
									Meta: "interface",
									Type: "Void",
									Loc:  c.extractLoc(s),
								},
								Name: miniast.Ident(s.Name.Name),
								Type: miniast.GoMiniType(c.typeToString(it)),
							}
						} else {
							// 基础类型别名 (type MyInt int64)
							program.Types[miniast.Ident(s.Name.Name)] = miniast.GoMiniType(c.typeToString(s.Type))
						}
					case *ast.ValueSpec:
						switch d.Tok {
						case token.CONST:
							for i, name := range s.Names {
								if i < len(s.Values) {
									if lit, ok := s.Values[i].(*ast.BasicLit); ok {
										val := lit.Value
										if lit.Kind == token.STRING && len(val) >= 2 {
											if unquoted, err := strconv.Unquote(val); err == nil {
												val = unquoted
											} else {
												val = val[1 : len(val)-1]
											}
										}
										program.Constants[name.Name] = val
									}
								}
							}
						case token.VAR:
							decl := c.convertValueSpecDecl(s)
							for i, name := range s.Names {
								program.Variables[miniast.Ident(name.Name)] = declValueForBinding(decl, i)
							}
							program.Main = append(program.Main, decl)
						}
					}
				}
			}
		}
	}

	return program, errs
}

func (c *GoToASTConverter) ConvertExprSource(code string) (miniast.Expr, error) {
	e, err := parser.ParseExpr(code)
	if err != nil {
		return nil, err
	}
	return c.convertExpr(e), nil
}

func (c *GoToASTConverter) ConvertStmtsSource(code string) ([]miniast.Stmt, error) {
	wrapper := fmt.Sprintf("package main\nfunc main() {\n%s\n}", code)
	node, err := c.ConvertSource("snippet", wrapper)
	if err != nil {
		return nil, err
	}
	prog := node.(*miniast.ProgramStmt)
	if len(prog.Main) > 0 {
		return prog.Main, nil
	}
	if mainFunc, ok := prog.Functions["main"]; ok && mainFunc.Body != nil {
		return mainFunc.Body.Children, nil
	}
	return nil, nil
}
