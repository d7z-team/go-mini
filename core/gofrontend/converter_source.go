package gofrontend

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

func (c *Converter) ConvertSource(filename, code string) (miniast.Node, error) {
	node, errs := c.convert(filename, code, false)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return node, nil
}

func (c *Converter) ConvertSourceTolerant(filename, code string) (miniast.Node, []error) {
	return c.convert(filename, code, true)
}

func (c *Converter) convert(filename, code string, tolerant bool) (miniast.Node, []error) {
	c.reset()
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

	type convertedImport struct {
		spec miniast.ImportSpec
		loc  *miniast.Position
		node ast.Node
	}

	var miniImports []miniast.ImportSpec
	var importDecls []convertedImport
	topLevelDecls := make(map[string]string)
	declareTopLevel := func(name, kind string, node ast.Node) bool {
		if name == "" || name == "_" {
			return true
		}
		if existing, ok := topLevelDecls[name]; ok {
			c.addError(node, fmt.Sprintf("duplicate top-level %s %s conflicts with existing %s", kind, name, existing))
			return false
		}
		topLevelDecls[name] = kind
		return true
	}
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
			if !declareTopLevel(alias, "import", imp) {
				continue
			}
			c.imports[alias] = path
			spec := miniast.ImportSpec{
				Alias: alias,
				Path:  path,
				File:  filename,
			}
			loc := c.extractLoc(imp)
			if imp.Name != nil {
				loc = c.extractLoc(imp.Name)
			}
			miniImports = append(miniImports, spec)
			importDecls = append(importDecls, convertedImport{spec: spec, loc: loc, node: imp})
		}
	}

	program := &miniast.ProgramStmt{
		BaseNode:      miniast.BaseNode{ID: c.genID(f, "boot"), Meta: "boot", Type: "Void", Loc: c.extractLoc(f)},
		Constants:     make(map[string]string),
		ConstantTypes: make(map[string]miniast.GoMiniType),
		ConstantLocs:  make(map[string]*miniast.Position),
		Variables:     make(map[miniast.Ident]miniast.Expr),
		Types:         make(map[miniast.Ident]miniast.GoMiniType),
		TypeLocs:      make(map[miniast.Ident]*miniast.Position),
		Structs:       make(map[miniast.Ident]*miniast.StructStmt),
		Interfaces:    make(map[miniast.Ident]*miniast.InterfaceStmt),
		ImportLocs:    make(map[string]*miniast.Position),
		Functions:     make(map[miniast.Ident]*miniast.FunctionStmt),
		Imports:       miniImports,
	}
	if f != nil {
		program.Package = f.Name.Name
		program.ModulePath = program.Package
	}

	for _, imported := range importDecls {
		imp := imported.spec
		loc := imported.loc
		program.ImportLocs[imp.Alias] = loc
		program.ImportLocs[miniast.ImportLocationKey(filename, imp.Alias)] = loc
		program.Variables[miniast.Ident(imp.Alias)] = &miniast.ImportExpr{
			BaseNode: miniast.BaseNode{ID: c.genID(imported.node, "import"), Meta: "import", Type: miniast.TypeModule, Loc: loc},
			Path:     imp.Path,
		}
	}

	if f != nil {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				fn := c.convertFunc(d)
				if fn.ReceiverType == "" && !declareTopLevel(string(fn.Name), "function", d.Name) {
					continue
				}
				program.Functions[fn.RegistryName()] = fn
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !declareTopLevel(s.Name.Name, "type", s.Name) {
							continue
						}
						program.TypeLocs[miniast.Ident(s.Name.Name)] = c.extractLoc(s.Name)
						if st, ok := s.Type.(*ast.StructType); ok {
							var doc string
							if s.Doc != nil {
								doc = s.Doc.Text()
							} else if d.Doc != nil {
								doc = d.Doc.Text()
							}
							program.Structs[miniast.Ident(s.Name.Name)] = c.convertStruct(s.Name.Name, st, c.extractLoc(s.Name), doc)
						} else if it, ok := s.Type.(*ast.InterfaceType); ok {
							c.interfaces[s.Name.Name] = it // 记录接口定义
							program.Interfaces[miniast.Ident(s.Name.Name)] = &miniast.InterfaceStmt{
								BaseNode: miniast.BaseNode{
									ID:   c.genID(s, "interface"),
									Meta: "interface",
									Type: "Void",
									Loc:  c.extractLoc(s.Name),
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
								if !declareTopLevel(name.Name, "constant", name) {
									continue
								}
								if i < len(s.Values) {
									switch lit := s.Values[i].(type) {
									case *ast.BasicLit:
										val := lit.Value
										switch lit.Kind {
										case token.STRING:
											program.ConstantTypes[name.Name] = miniast.TypeString
											if unquoted, err := strconv.Unquote(val); err == nil {
												val = unquoted
											} else if len(val) >= 2 {
												val = val[1 : len(val)-1]
											}
										case token.INT:
											program.ConstantTypes[name.Name] = miniast.TypeInt64
										case token.CHAR:
											program.ConstantTypes[name.Name] = miniast.TypeInt64
											if unquoted, _, _, err := strconv.UnquoteChar(strings.Trim(lit.Value, "'"), '\''); err == nil {
												val = strconv.FormatInt(int64(unquoted), 10)
											}
										case token.FLOAT:
											program.ConstantTypes[name.Name] = miniast.TypeFloat64
										}
										program.Constants[name.Name] = val
										program.ConstantLocs[name.Name] = c.extractLoc(name)
									case *ast.Ident:
										if lit.Name == "true" || lit.Name == "false" {
											program.Constants[name.Name] = lit.Name
											program.ConstantTypes[name.Name] = miniast.TypeBool
											program.ConstantLocs[name.Name] = c.extractLoc(name)
										}
									}
								}
							}
						case token.VAR:
							decl := c.convertValueSpecDecl(s)
							for i, name := range s.Names {
								if !declareTopLevel(name.Name, "variable", name) {
									continue
								}
								program.Variables[miniast.Ident(name.Name)] = declValueForBinding(decl, i)
							}
							program.Main = append(program.Main, decl)
						}
					}
				}
			}
		}
	}

	errs = append(errs, c.errs...)
	return program, errs
}

func (c *Converter) ConvertExprSource(code string) (miniast.Expr, error) {
	c.reset()
	e, err := parser.ParseExpr(code)
	if err != nil {
		return nil, err
	}
	expr := c.convertExpr(e)
	if len(c.errs) > 0 {
		return nil, c.errs[0]
	}
	return expr, nil
}

func (c *Converter) ConvertStmtsSource(code string) ([]miniast.Stmt, error) {
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
