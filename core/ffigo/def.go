package ffigo

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"strconv"
	"strings"

	spec "gopkg.d7z.net/go-mini/core/ast"
)

// GoToASTConverter 将Go代码转换为自定义AST
type GoToASTConverter struct {
	fset           *token.FileSet
	nodeID         int
	imports        map[string]string // 包别名映射
	structTypes    map[string]string // Go类型到OPSType的映射
	commentsByLine map[int]string    // 行号到注释内容的映射
	originalSource []byte            // 原始源代码
}

// NewGoToASTConverter 创建新的转换器
func NewGoToASTConverter() *GoToASTConverter {
	return &GoToASTConverter{
		fset:           token.NewFileSet(),
		nodeID:         0,
		imports:        make(map[string]string),
		structTypes:    make(map[string]string),
		commentsByLine: make(map[int]string),
	}
}

// ConvertFile 转换Go源文件
func (c *GoToASTConverter) ConvertFile(filename string) (*spec.ProgramStmt, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取Go文件失败: %v", err)
	}
	c.originalSource = src
	node, err := parser.ParseFile(c.fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析Go文件失败: %v", err)
	}
	return c.convertFile(node)
}

// ConvertSource 转换Go源代码字符串
func (c *GoToASTConverter) ConvertSource(src string) (*spec.ProgramStmt, error) {
	c.originalSource = []byte(src)
	node, err := parser.ParseFile(c.fset, "", src, parser.ParseComments)
	if err != nil {
		var scanErr scanner.ErrorList
		if errors.As(err, &scanErr) {
			return nil, errors.Join(err, &spec.MiniCodeError{
				Line:    scanErr[0].Pos.Line,
				Message: "",
			})
		}
		return nil, err
	}
	return c.convertFile(node)
}

func (c *GoToASTConverter) convertFile(file *ast.File) (*spec.ProgramStmt, error) {
	c.imports = make(map[string]string)
	c.commentsByLine = make(map[int]string)

	// 处理所有注释，按行存储
	for _, group := range file.Comments {
		pos := c.fset.Position(group.Pos())
		text := strings.TrimSpace(group.Text())
		if existing, ok := c.commentsByLine[pos.Line]; ok {
			c.commentsByLine[pos.Line] = existing + " " + text
		} else {
			c.commentsByLine[pos.Line] = text
		}
	}

	var imports []spec.ImportSpec
	// 处理导入
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if imp.Name != nil {
			c.imports[path] = imp.Name.Name
			imports = append(imports, spec.ImportSpec{
				Alias: imp.Name.Name,
				Path:  path,
			})
		} else {
			// 提取包名
			parts := strings.Split(path, "/")
			name := parts[len(parts)-1]
			c.imports[path] = name
			imports = append(imports, spec.ImportSpec{
				Alias: "",
				Path:  path,
			})
		}
	}

	// 转换所有声明
	children := make([]spec.Stmt, 0)

	for _, decl := range file.Decls {
		node, err := c.convertDecl(decl)
		if err != nil {
			return nil, err
		}
		if node != nil {
			children = append(children, node)
		}
	}

	pkgName := "main"
	if file.Name != nil {
		pkgName = file.Name.Name
	}

	root := &spec.ProgramStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(file),
			Meta: "boot",
			Type: "Void",
		},
		Package:   pkgName,
		Imports:   imports,
		Constants: make(map[string]string),
		Variables: make(map[spec.Ident]spec.Expr),
		Structs:   make(map[spec.Ident]*spec.StructStmt),
		Functions: make(map[spec.Ident]*spec.FunctionStmt),
		Main:      children,
	}
	c.setNodeMessage(root, file)
	return root, nil
}

func (c *GoToASTConverter) convertDecl(decl ast.Decl) (spec.Stmt, error) {
	var res spec.Stmt
	var err error
	switch d := decl.(type) {
	case *ast.FuncDecl:
		res, err = c.convertFuncDecl(d)
	case *ast.GenDecl:
		res, err = c.convertGenDecl(d)
	default:
		return nil, fmt.Errorf("未知的声明类型: %T", decl)
	}
	if err == nil && res != nil {
		c.setNodeMessage(res, decl)
	}
	return res, err
}

func (c *GoToASTConverter) convertFuncDecl(fd *ast.FuncDecl) (spec.Stmt, error) {
	funcStmt := &spec.FunctionStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(fd),
			Meta: "function",
			Type: "Void",
		},
		Name: spec.Ident(fd.Name.Name),
	}

	// 处理接收器
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recv := fd.Recv.List[0]
		if len(recv.Names) > 0 {
			// 有接收器的方法
			recvType, err := c.convertType(recv.Type)
			if err != nil {
				return nil, err
			}
			// 提取类型名
			scope := string(recvType)
			if recvType.IsPtr() {
				elemType, _ := recvType.GetPtrElementType()
				scope = string(elemType)
			}
			funcStmt.Scope = spec.Ident(scope)
		}
	}

	// 处理参数
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		for _, field := range fd.Recv.List {
			paramType, err := c.convertType(field.Type)
			if err != nil {
				return nil, err
			}
			for _, name := range field.Names {
				funcStmt.Params = append(funcStmt.Params, spec.FunctionParam{
					Name: spec.Ident(name.Name),
					Type: paramType,
				})
			}
		}
	}
	if fd.Type.Params != nil {
		for _, field := range fd.Type.Params.List {
			paramType, err := c.convertType(field.Type)
			if err != nil {
				return nil, err
			}
			for _, name := range field.Names {
				funcStmt.Params = append(funcStmt.Params, spec.FunctionParam{
					Name: spec.Ident(name.Name),
					Type: paramType,
				})
			}
		}
	}

	// 处理返回值
	if fd.Type.Results != nil {
		var returnTypes []spec.OPSType
		for _, field := range fd.Type.Results.List {
			returnType, err := c.convertType(field.Type)
			if err != nil {
				return nil, err
			}
			if field.Names != nil {
				// 命名返回值，暂时忽略名称
				for range field.Names {
					returnTypes = append(returnTypes, returnType)
				}
			} else {
				returnTypes = append(returnTypes, returnType)
			}
		}
		funcStmt.Return = spec.CreateTupleType(returnTypes...)
	} else {
		funcStmt.Return = "Void"
	}

	// 处理函数体
	if fd.Body != nil {
		body, err := c.convertBlockStmt(fd.Body)
		if err != nil {
			return nil, err
		}
		funcStmt.Body = body
	} else {
		// 外部声明函数
		funcStmt.Body = spec.NewBlock(nil)
	}

	return funcStmt, nil
}

func (c *GoToASTConverter) convertGenDecl(gd *ast.GenDecl) (spec.Stmt, error) {
	switch gd.Tok {
	case token.IMPORT:
		return nil, nil // Imports are handled in convertFile
	case token.TYPE:
		return c.convertTypeDecl(gd)
	case token.VAR, token.CONST:
		return c.convertVarDecl(gd)
	default:
		return nil, fmt.Errorf("不支持的定义类型: %v", gd.Tok)
	}
}

func (c *GoToASTConverter) convertTypeDecl(gd *ast.GenDecl) (spec.Stmt, error) {
	for _, s := range gd.Specs {
		ts, ok := s.(*ast.TypeSpec)
		if !ok {
			continue
		}

		// 只处理结构体类型
		structType, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}

		fields := make(map[spec.Ident]spec.OPSType)

		if structType.Fields != nil {
			for _, field := range structType.Fields.List {
				fieldType, err := c.convertType(field.Type)
				if err != nil {
					return nil, err
				}
				if field.Names != nil {
					for _, name := range field.Names {
						fields[spec.Ident(name.Name)] = fieldType
					}
				} else {
					// 嵌入字段
					fields[spec.Ident(fieldType)] = fieldType
				}
			}
		}

		// 注册结构体类型映射
		c.structTypes[ts.Name.Name] = ts.Name.Name

		return &spec.StructStmt{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(ts),
				Meta: "struct",
				Type: "Void",
			},
			Name:   spec.Ident(ts.Name.Name),
			Fields: fields,
		}, nil
	}
	return nil, nil
}

func (c *GoToASTConverter) convertVarDecl(gd *ast.GenDecl) (spec.Stmt, error) {
	var stmts []spec.Stmt

	for _, s := range gd.Specs {
		vs, ok := s.(*ast.ValueSpec)
		if !ok {
			continue
		}

		for i, name := range vs.Names {
			var value spec.Expr

			if vs.Values != nil && i < len(vs.Values) {
				val, err := c.convertExpr(vs.Values[i])
				if err != nil {
					return nil, err
				}
				value = val
			} else if vs.Type != nil {
				// 只有类型没有值，创建默认值
				varType, err := c.convertType(vs.Type)
				if err != nil {
					return nil, err
				}
				value = c.createDefaultValue(varType)
			}

			if value != nil {
				stmt := &spec.AssignmentStmt{
					BaseNode: spec.BaseNode{
						ID:   c.nextID(name),
						Meta: "assignment",
						Type: "Void",
					},
					Variable: spec.Ident(name.Name),
					Value:    value,
				}
				c.setNodeMessage(stmt, name)
				stmts = append(stmts, stmt)
			}
		}
	}

	if len(stmts) == 1 {
		return stmts[0], nil
	}

	return &spec.BlockStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(gd),
			Meta: "block",
			Type: "Void",
		},
		Children: stmts,
	}, nil
}

func (c *GoToASTConverter) convertStmt(stmt ast.Stmt) (spec.Stmt, error) {
	var res spec.Stmt
	var err error
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		res, err = c.convertBlockStmt(s)
	case *ast.IfStmt:
		res, err = c.convertIfStmt(s)
	case *ast.ForStmt:
		res, err = c.convertForStmt(s)
	case *ast.ReturnStmt:
		res, err = c.convertReturnStmt(s)
	case *ast.AssignStmt:
		res, err = c.convertAssignStmt(s)
	case *ast.DeclStmt:
		res, err = c.convertDecl(s.Decl)
	case *ast.ExprStmt:
		expr, err2 := c.convertExpr(s.X)
		if err2 != nil {
			return nil, err2
		}
		// 表达式语句转换为调用表达式
		res = expr.(spec.Stmt)
	case *ast.IncDecStmt:
		res, err = c.convertIncDecStmt(s)
	case *ast.SwitchStmt:
		res, err = c.convertSwitchStmt(s)
	case *ast.BranchStmt:
		res, err = c.convertBranchStmt(s)
	case *ast.RangeStmt:
		res, err = c.convertRangeStmt(s)
	case *ast.DeferStmt:
		res, err = c.convertDeferStmt(s)
	default:
		return nil, fmt.Errorf("不支持的语句类型: %T", stmt)
	}
	if err == nil && res != nil {
		c.setNodeMessage(res, stmt)
	}
	return res, err
}

func (c *GoToASTConverter) convertBlockStmt(block *ast.BlockStmt) (*spec.BlockStmt, error) {
	children := make([]spec.Stmt, 0, len(block.List))

	for _, stmt := range block.List {
		node, err := c.convertStmt(stmt)
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}

	res := &spec.BlockStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(block),
			Meta: "block",
			Type: "Void",
		},
		Children: children,
	}
	c.setNodeMessage(res, block)
	return res, nil
}

func (c *GoToASTConverter) convertIfStmt(ifStmt *ast.IfStmt) (spec.Stmt, error) {
	// 准备真实的 if 节点
	actualIf := &spec.IfStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(ifStmt),
			Meta: "if",
			Type: "Void",
		},
	}

	// 转换条件
	cond, err := c.convertExpr(ifStmt.Cond)
	if err != nil {
		return nil, err
	}
	actualIf.Cond = cond

	// 转换主体
	body, err := c.convertBlockStmt(ifStmt.Body)
	if err != nil {
		return nil, err
	}
	actualIf.Body = body

	// 转换 Else
	if ifStmt.Else != nil {
		elseNode, err := c.convertStmt(ifStmt.Else)
		if err != nil {
			return nil, err
		}
		if i, ok := elseNode.(*spec.IfStmt); ok {
			actualIf.ElseBody = spec.NewBlock(i, i)
		} else if b, ok := elseNode.(*spec.BlockStmt); ok {
			actualIf.ElseBody = b
		} else {
			// 如果 else 是其他类型的语句（虽然 Go 语法通常不允许，除非是嵌套 if），包装成 Block
			actualIf.ElseBody = &spec.BlockStmt{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(ifStmt.Else),
					Meta: "block",
					Type: "Void",
				},
				Children: []spec.Stmt{elseNode},
			}
		}
	}

	// 如果有初始化语句，包装在 Block 中
	if ifStmt.Init != nil {
		initStmt, err := c.convertStmt(ifStmt.Init)
		if err != nil {
			return nil, err
		}

		wrapperBlock := &spec.BlockStmt{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(ifStmt),
				Meta: "block", // 标记为隐式块作用域
				Type: "Void",
			},
			Children: []spec.Stmt{initStmt, actualIf},
		}
		return wrapperBlock, nil
	}

	return actualIf, nil
}

func (c *GoToASTConverter) convertForStmt(forStmt *ast.ForStmt) (*spec.ForStmt, error) {
	result := &spec.ForStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(forStmt),
			Meta: "for",
			Type: "Void",
		},
	}

	// 处理初始化
	if forStmt.Init != nil {
		init, err := c.convertStmt(forStmt.Init)
		if err != nil {
			return nil, err
		}
		result.Init = init
	}

	// 处理条件
	if forStmt.Cond != nil {
		cond, err := c.convertExpr(forStmt.Cond)
		if err != nil {
			return nil, err
		}
		result.Cond = cond
	} else {
		// 默认为 true
		result.Cond = &spec.LiteralExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(nil),
				Meta: "literal",
				Type: "Bool",
			},
			Value: "true",
		}
	}

	// 处理更新
	if forStmt.Post != nil {
		post, err := c.convertStmt(forStmt.Post)
		if err != nil {
			return nil, err
		}
		result.Update = post
	}

	// 处理循环体
	if forStmt.Body != nil {
		body, err := c.convertBlockStmt(forStmt.Body)
		if err != nil {
			return nil, err
		}
		result.Body = body
	}

	return result, nil
}

func (c *GoToASTConverter) convertReturnStmt(ret *ast.ReturnStmt) (*spec.ReturnStmt, error) {
	result := &spec.ReturnStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(ret),
			Meta: "return",
			Type: "Void",
		},
	}

	for _, expr := range ret.Results {
		converted, err := c.convertExpr(expr)
		if err != nil {
			return nil, err
		}
		result.Results = append(result.Results, converted)
	}

	return result, nil
}

func (c *GoToASTConverter) convertAssignStmt(assign *ast.AssignStmt) (spec.Stmt, error) {
	// 处理多重赋值
	if len(assign.Lhs) > 1 || len(assign.Rhs) > 1 {
		return c.convertMultiAssign(assign)
	}

	lhs := assign.Lhs[0]
	rhs := assign.Rhs[0]

	// 处理数组/Map索引赋值 arr[i] = v -> arr.set(i, v) 或 map[k] = v -> map.put(k, v)
	if indexExpr, ok := lhs.(*ast.IndexExpr); ok {
		object, err := c.convertExpr(indexExpr.X)
		if err != nil {
			return nil, err
		}
		index, err := c.convertExpr(indexExpr.Index)
		if err != nil {
			return nil, err
		}
		value, err := c.convertExpr(rhs)
		if err != nil {
			return nil, err
		}

		// Go 中 map[k]=v 和 arr[i]=v 语法一样。
		// executor.go 中 resolveArrayMapMethod 可以处理方法分发。
		return &spec.StructCallExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(assign),
				Meta: "struct_call",
			},
			Object: object,
			Name:   "set",
			Args:   []spec.Expr{index, value},
		}, nil
	}

	// 处理解引用赋值 *p = v
	if starExpr, ok := lhs.(*ast.StarExpr); ok {
		object, err := c.convertExpr(starExpr.X)
		if err != nil {
			return nil, err
		}
		value, err := c.convertExpr(rhs)
		if err != nil {
			return nil, err
		}
		return &spec.DerefAssignmentStmt{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(assign),
				Meta: "deref_assignment",
				Type: "Void",
			},
			Object: object,
			Value:  value,
		}, nil
	}

	// 简单赋值
	// 获取左侧变量名
	var varName spec.Ident
	switch expr := lhs.(type) {
	case *ast.Ident:
		varName = spec.Ident(expr.Name)
	case *ast.SelectorExpr:
		// 成员访问，暂时简化为变量
		varName = spec.Ident(fmt.Sprintf("%s.%s", expr.X, expr.Sel.Name))
	default:
		return nil, fmt.Errorf("不支持的赋值左侧: %T", lhs)
	}

	value, err := c.convertExpr(rhs)
	if err != nil {
		return nil, err
	}

	return &spec.AssignmentStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(assign),
			Meta: "assignment",
			Type: "Void",
		},
		Variable: varName,
		Value:    value,
	}, nil
}

func (c *GoToASTConverter) convertMultiAssign(assign *ast.AssignStmt) (spec.Stmt, error) {
	// 简化处理：转换为多个单赋值语句
	var stmts []spec.Stmt

	for i, lhs := range assign.Lhs {
		if i >= len(assign.Rhs) {
			break
		}

		var varName spec.Ident
		switch expr := lhs.(type) {
		case *ast.Ident:
			varName = spec.Ident(expr.Name)
		default:
			return nil, fmt.Errorf("不支持的赋值左侧: %T", lhs)
		}

		value, err := c.convertExpr(assign.Rhs[i])
		if err != nil {
			return nil, err
		}

		stmt := &spec.AssignmentStmt{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(lhs),
				Meta: "assignment",
				Type: "Void",
			},
			Variable: varName,
			Value:    value,
		}
		c.setNodeMessage(stmt, lhs)
		stmts = append(stmts, stmt)
	}
	return &spec.BlockStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(assign),
			Meta: "block",
			Type: "Void",
		},
		Children: stmts,
		Inner:    true,
	}, nil
}

func (c *GoToASTConverter) convertIncDecStmt(stmt *ast.IncDecStmt) (*spec.IncDecStmt, error) {
	operand, err := c.convertExpr(stmt.X)
	if err != nil {
		return nil, err
	}

	op := "++"
	if stmt.Tok == token.DEC {
		op = "--"
	}

	return &spec.IncDecStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(stmt),
			Meta: "increment",
			Type: "Void",
		},
		Operand:  operand,
		Operator: spec.Ident(op),
	}, nil
}

func (c *GoToASTConverter) convertSwitchStmt(switchStmt *ast.SwitchStmt) (*spec.SwitchStmt, error) {
	result := &spec.SwitchStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(switchStmt),
			Meta: "switch",
			Type: "Void",
		},
	}

	// 处理标签
	if switchStmt.Tag != nil {
		tag, err := c.convertExpr(switchStmt.Tag)
		if err != nil {
			return nil, err
		}
		result.Cond = tag
	}

	// 处理case
	for _, stmt := range switchStmt.Body.List {
		caseClause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}

		switchCase := spec.SwitchCase{}

		// 处理条件
		for _, expr := range caseClause.List {
			cond, err := c.convertExpr(expr)
			if err != nil {
				return nil, err
			}
			switchCase.Cond = append(switchCase.Cond, cond)
		}

		// 处理主体
		if len(caseClause.Body) > 0 {
			bodyStmts := make([]spec.Stmt, 0, len(caseClause.Body))
			for _, bodyStmt := range caseClause.Body {
				converted, err := c.convertStmt(bodyStmt)
				if err != nil {
					return nil, err
				}
				bodyStmts = append(bodyStmts, converted)
			}
			switchCase.Body = &spec.BlockStmt{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(caseClause),
					Meta: "block",
					Type: "Void",
				},
				Children: bodyStmts,
			}
			c.setNodeMessage(switchCase.Body, caseClause)
		}

		if caseClause.List == nil {
			// default case
			result.Default = &switchCase
		} else {
			result.Cases = append(result.Cases, switchCase)
		}
	}

	return result, nil
}

func (c *GoToASTConverter) convertDeferStmt(deferStmt *ast.DeferStmt) (*spec.DeferStmt, error) {
	call, err := c.convertExpr(deferStmt.Call)
	if err != nil {
		return nil, err
	}

	return &spec.DeferStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(deferStmt),
			Meta: "defer",
			Type: "Void",
		},
		Call: call,
	}, nil
}

func (c *GoToASTConverter) convertBranchStmt(branch *ast.BranchStmt) (*spec.InterruptStmt, error) {
	var interruptType string

	switch branch.Tok {
	case token.BREAK:
		interruptType = "break"
	case token.CONTINUE:
		interruptType = "continue"
	default:
		return nil, fmt.Errorf("不支持的分支语句: %v", branch.Tok)
	}

	return &spec.InterruptStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(branch),
			Meta: "interrupt",
			Type: "Void",
		},
		InterruptType: interruptType,
	}, nil
}

func (c *GoToASTConverter) convertExpr(expr ast.Expr) (spec.Expr, error) {
	var res spec.Expr
	var err error
	switch e := expr.(type) {
	case *ast.Ident:
		res = c.convertIdent(e)
	case *ast.BasicLit:
		res, err = c.convertBasicLit(e)
	case *ast.BinaryExpr:
		res, err = c.convertBinaryExpr(e)
	case *ast.UnaryExpr:
		res, err = c.convertUnaryExpr(e)
	case *ast.CallExpr:
		res, err = c.convertCallExpr(e)
	case *ast.SelectorExpr:
		res, err = c.convertSelectorExpr(e)
	case *ast.CompositeLit:
		res, err = c.convertCompositeLit(e)
	case *ast.IndexExpr:
		res, err = c.convertIndexExpr(e)
	case *ast.StarExpr:
		res, err = c.convertStarExpr(e)
	case *ast.ParenExpr:
		res, err = c.convertExpr(e.X)
	case *ast.ArrayType:
		// 当类型作为表达式出现时（如 make 参数）
		var t spec.OPSType
		t, err = c.convertType(e)
		if err == nil {
			return &spec.LiteralExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(e),
					Meta: "type_expr",
					Type: t,
				},
				Value: string(t),
			}, nil
		}
	default:
		return nil, fmt.Errorf("不支持的表达式类型: %T", expr)
	}
	if err == nil && res != nil {
		c.setNodeMessage(res, expr)
	}
	return res, err
}

func (c *GoToASTConverter) convertIdent(ident *ast.Ident) spec.Expr {
	if ident.Name == "true" || ident.Name == "false" {
		return &spec.LiteralExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(ident),
				Meta: "literal",
				Type: "Bool",
			},
			Value: ident.Name,
		}
	}
	if ident.Name == "nil" {
		return &spec.LiteralExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(ident),
				Meta: "literal",
				Type: "Ptr<Any>",
			},
			Value: "",
		}
	}
	return &spec.IdentifierExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(ident),
			Meta: "identifier",
		},
		Name: spec.Ident(ident.Name),
	}
}

func (c *GoToASTConverter) convertBasicLit(lit *ast.BasicLit) (*spec.LiteralExpr, error) {
	var typ spec.OPSType
	var value string

	switch lit.Kind {
	case token.INT:
		typ = "Int64"
		value = lit.Value
	case token.FLOAT:
		typ = "Float64"
		value = lit.Value
	case token.CHAR, token.STRING:
		typ = "String"
		// 去除引号
		value, _ = strconv.Unquote(lit.Value)
	default:
		return nil, fmt.Errorf("不支持的字面量类型: %v", lit.Kind)
	}

	return &spec.LiteralExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(lit),
			Meta: "literal",
			Type: typ,
		},
		Value: value,
	}, nil
}

func (c *GoToASTConverter) convertBinaryExpr(bin *ast.BinaryExpr) (*spec.BinaryExpr, error) {
	left, err := c.convertExpr(bin.X)
	if err != nil {
		return nil, err
	}

	right, err := c.convertExpr(bin.Y)
	if err != nil {
		return nil, err
	}

	var operator spec.Ident
	switch bin.Op {
	case token.ADD:
		operator = "+"
	case token.SUB:
		operator = "-"
	case token.MUL:
		operator = "*"
	case token.QUO:
		operator = "/"
	case token.REM:
		operator = "%"
	case token.EQL:
		operator = "=="
	case token.NEQ:
		operator = "!="
	case token.LSS:
		operator = "<"
	case token.GTR:
		operator = ">"
	case token.LEQ:
		operator = "<="
	case token.GEQ:
		operator = ">="
	case token.LAND:
		operator = "&&"
	case token.LOR:
		operator = "||"
	default:
		return nil, fmt.Errorf("不支持的二元操作符: %v", bin.Op)
	}

	return &spec.BinaryExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(bin),
			Meta: "binary",
		},
		Left:     left,
		Operator: operator,
		Right:    right,
	}, nil
}

func (c *GoToASTConverter) convertUnaryExpr(unary *ast.UnaryExpr) (spec.Expr, error) {
	operand, err := c.convertExpr(unary.X)
	if err != nil {
		return nil, err
	}

	var operator spec.Ident
	switch unary.Op {
	case token.SUB:
		operator = "-"
	case token.NOT:
		operator = "!"
	case token.AND:
		// 取地址操作
		return &spec.AddressExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(unary),
				Meta: "address",
			},
			Operand: operand,
		}, nil
	default:
		return nil, fmt.Errorf("不支持的一元操作符: %v", unary.Op)
	}

	return &spec.UnaryExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(unary),
			Meta: "unary",
		},
		Operator: operator,
		Operand:  operand,
	}, nil
}

func (c *GoToASTConverter) convertCallExpr(call *ast.CallExpr) (spec.Expr, error) {
	args := make([]spec.Expr, 0, len(call.Args))
	for _, arg := range call.Args {
		converted, err := c.convertExpr(arg)
		if err != nil {
			return nil, err
		}
		args = append(args, converted)
	}

	switch item := call.Fun.(type) {
	case *ast.Ident:
		funcRef := &spec.ConstRefExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(item),
				Meta: "const_ref",
			},
			Name: spec.Ident(item.Name),
		}
		c.setNodeMessage(funcRef, item)
		return &spec.CallExprStmt{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(call),
				Meta: "call",
			},
			Func: funcRef,
			Args: args,
		}, nil
	case *ast.SelectorExpr:
		switch itemX := item.X.(type) {
		case *ast.Ident:
			obj := &spec.IdentifierExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(itemX),
					Meta: "identifier",
				},
				Name: spec.Ident(itemX.Name),
			}
			c.setNodeMessage(obj, itemX)
			return &spec.StructCallExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(call),
					Meta: "struct_call",
				},
				Object: obj,
				Name:   spec.Ident(item.Sel.Name),
				Args:   args,
			}, nil
		case *ast.CallExpr:
			object, err := c.convertExpr(itemX)
			if err != nil {
				return nil, err
			}
			return &spec.StructCallExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(call),
					Meta: "struct_call",
				},
				Object: object,
				Name:   spec.Ident(item.Sel.Name),
				Args:   args,
			}, nil
		default:
			object, err := c.convertExpr(itemX)
			if err != nil {
				return nil, err
			}
			return &spec.StructCallExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(call),
					Meta: "struct_call",
				},
				Object: object,
				Name:   spec.Ident(item.Sel.Name),
				Args:   args,
			}, nil
		}
	default:
		return nil, errors.New("unknown call function")
	}
}

func (c *GoToASTConverter) convertSelectorExpr(sel *ast.SelectorExpr) (*spec.MemberExpr, error) {
	object, err := c.convertExpr(sel.X)
	if err != nil {
		return nil, err
	}

	return &spec.MemberExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(sel),
			Meta: "member",
		},
		Object:   object,
		Property: spec.Ident(sel.Sel.Name),
	}, nil
}

func (c *GoToASTConverter) convertCompositeLit(comp *ast.CompositeLit) (*spec.CompositeExpr, error) {
	typ, err := c.convertType(comp.Type)
	if err != nil {
		return nil, err
	}
	values := make([]spec.CompositeElement, 0, len(comp.Elts))
	for _, elem := range comp.Elts {
		// 处理键值对
		kv, ok := elem.(*ast.KeyValueExpr)
		if ok {
			value, err := c.convertExpr(kv.Value)
			if err != nil {
				return nil, err
			}
			var key spec.Expr
			if typ.IsMap() {
				key, err = c.convertExpr(kv.Key)
				if err != nil {
					return nil, err
				}
			} else {
				// 默认为结构体，键必须是标识符
				ident, ok := kv.Key.(*ast.Ident)
				if !ok {
					// 尝试处理字面量键（JSON风格）
					if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						val, _ := strconv.Unquote(lit.Value)
						keyNode := &spec.LiteralExpr{
							BaseNode: spec.BaseNode{
								ID:   c.nextID(lit),
								Meta: "literal",
								Type: "String",
							},
							Value: val,
						}
						c.setNodeMessage(keyNode, lit)
						key = keyNode
					} else {
						return nil, fmt.Errorf("struct key must be identifier or string literal: %T", kv.Key)
					}
				} else {
					keyNode := &spec.LiteralExpr{
						BaseNode: spec.BaseNode{
							ID:   c.nextID(ident),
							Meta: "literal",
							Type: "String",
						},
						Value: ident.Name,
					}
					c.setNodeMessage(keyNode, ident)
					key = keyNode
				}
			}

			values = append(values, spec.CompositeElement{
				Key:   key,
				Value: value,
			})
		} else {
			// 只有值
			value, err := c.convertExpr(elem)
			if err != nil {
				return nil, err
			}
			values = append(values, spec.CompositeElement{
				Value: value,
			})
		}
	}

	return &spec.CompositeExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(comp),
			Meta: "composite",
			Type: typ,
		},
		Kind:   spec.Ident(typ),
		Values: values,
	}, nil
}

func (c *GoToASTConverter) convertIndexExpr(idx *ast.IndexExpr) (*spec.IndexExpr, error) {
	object, err := c.convertExpr(idx.X)
	if err != nil {
		return nil, err
	}

	index, err := c.convertExpr(idx.Index)
	if err != nil {
		return nil, err
	}

	return &spec.IndexExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(idx),
			Meta: "index",
		},
		Object: object,
		Index:  index,
	}, nil
}

func (c *GoToASTConverter) convertStarExpr(star *ast.StarExpr) (*spec.DerefExpr, error) {
	operand, err := c.convertExpr(star.X)
	if err != nil {
		return nil, err
	}

	return &spec.DerefExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(star),
			Meta: "deref",
		},
		Operand: operand,
	}, nil
}

func (c *GoToASTConverter) convertType(typ ast.Expr) (spec.OPSType, error) {
	switch t := typ.(type) {
	case *ast.Ident:
		return c.convertBasicType(t.Name)
	case *ast.StarExpr:
		// 指针类型
		elemType, err := c.convertType(t.X)
		if err != nil {
			return "", err
		}
		return elemType.ToPtr(), nil
	case *ast.ArrayType:
		// 数组类型
		elemType, err := c.convertType(t.Elt)
		if err != nil {
			return "", err
		}
		return spec.CreateArrayType(elemType), nil
	case *ast.MapType:
		keyType, err := c.convertType(t.Key)
		if err != nil {
			return "", err
		}
		valType, err := c.convertType(t.Value)
		if err != nil {
			return "", err
		}
		return spec.CreateMapType(keyType, valType), nil
	case *ast.SelectorExpr:
		// 限定标识符，如 pkg.Type
		return spec.OPSType(fmt.Sprintf("%s.%s", t.X, t.Sel.Name)), nil
	default:
		return "", fmt.Errorf("不支持的类型: %T", typ)
	}
}

func (c *GoToASTConverter) convertBasicType(typeName string) (spec.OPSType, error) {
	// 映射Go基本类型到OPSType
	switch typeName {
	case "byte", "uint8":
		return "Uint8", nil
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint16", "uint32", "uint64":
		return "Int64", nil
	case "float32", "float64":
		return "Float64", nil
	case "string":
		return "String", nil
	case "bool":
		return "Bool", nil
	default:
		// 检查是否是已注册的结构体类型
		if mapped, ok := c.structTypes[typeName]; ok {
			return spec.OPSType(mapped), nil
		}
		// 默认作为自定义类型
		return spec.OPSType(typeName), nil
	}
}

func (c *GoToASTConverter) createDefaultValue(typ spec.OPSType) *spec.LiteralExpr {
	var value string

	switch typ {
	case "Int64":
		value = "0"
	case "Float64":
		value = "0.0"
	case "String":
		value = ""
	case "Bool":
		value = "false"
	default:
		value = ""
	}

	return &spec.LiteralExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(nil),
			Meta: "literal",
			Type: typ,
		},
		Value: value,
	}
}

func (c *GoToASTConverter) setNodeMessage(specNode spec.Node, goNode ast.Node) {
	if specNode == nil || goNode == nil {
		return
	}
	pos := c.fset.Position(goNode.Pos())
	// 获取节点结束所在的行 (针对行尾注释)
	endPos := c.fset.Position(goNode.End())
	if msg, ok := c.commentsByLine[endPos.Line]; ok {
		specNode.GetBase().Message = msg
	} else {
		// 尝试获取节点开始所在的行 (针对行首注释或单行定义)
		if msg, ok = c.commentsByLine[pos.Line]; ok {
			specNode.GetBase().Message = msg
		} else {
			// 默认填充正在执行的行数
			specNode.GetBase().Message = fmt.Sprintf("正在执行第 L%d 行", pos.Line)
		}
	}
}

func (c *GoToASTConverter) nextID(node ast.Node) string {
	c.nodeID++
	if node != nil {
		pos := c.fset.Position(node.Pos())
		return fmt.Sprintf("go_ast_L%d_%d", pos.Line, c.nodeID)
	}
	return fmt.Sprintf("go_ast_%d", c.nodeID)
}

// ConvertGoToAST 便捷函数：转换Go源代码到AST
func ConvertGoToAST(src string) (*spec.ProgramStmt, error) {
	converter := NewGoToASTConverter()
	return converter.ConvertSource(src)
}

func (c *GoToASTConverter) convertRangeStmt(rangeStmt *ast.RangeStmt) (spec.Stmt, error) {
	// 1. Evaluate X -> rangeObj
	xExpr, err := c.convertExpr(rangeStmt.X)
	if err != nil {
		return nil, err
	}

	rangeObjID := c.nextID(rangeStmt)
	rangeObjName := spec.Ident("__range_" + rangeObjID)
	indexName := spec.Ident("__index_" + c.nextID(rangeStmt))

	// __range_obj := X
	initRangeObj := &spec.AssignmentStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "assignment",
			Type: "Void",
		},
		Variable: rangeObjName,
		Value:    xExpr,
	}

	// 2. Index := 0
	initIndex := &spec.AssignmentStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "assignment",
			Type: "Void",
		},
		Variable: indexName,
		Value: &spec.LiteralExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(rangeStmt),
				Meta: "literal",
				Type: "Int64",
			},
			Value: "0",
		},
	}

	// 3. Cond: index < rangeObj.Len() / rangeObj.Size()
	// rangeObj.Len() or rangeObj.Size()
	methodName := "length"
	if xExpr.GetBase().Type.IsMap() {
		methodName = "size"
	}

	lenCall := &spec.StructCallExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "struct_call",
		},
		Object: &spec.IdentifierExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(rangeStmt),
				Meta: "identifier",
			},
			Name: rangeObjName,
		},
		Name: spec.Ident(methodName),
	}

	cond := &spec.BinaryExpr{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "binary",
		},
		Left: &spec.IdentifierExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(rangeStmt),
				Meta: "identifier",
			},
			Name: indexName,
		},
		Operator: "<",
		Right:    lenCall,
	}

	// 4. Update: index++
	update := &spec.IncDecStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "increment",
			Type: "Void",
		},
		Operand: &spec.IdentifierExpr{
			BaseNode: spec.BaseNode{
				ID:   c.nextID(rangeStmt),
				Meta: "identifier",
			},
			Name: indexName,
		},
		Operator: "++",
	}

	// 5. Body assignments
	var assignments []spec.Stmt

	// Key = index
	if rangeStmt.Key != nil {
		if id, ok := rangeStmt.Key.(*ast.Ident); !ok || id.Name != "_" {
			// Convert Key to variable name
			var keyName spec.Ident
			if id, ok := rangeStmt.Key.(*ast.Ident); ok {
				keyName = spec.Ident(id.Name)
			} else {
				return nil, errors.New("range key must be identifier")
			}

			// Then assign the index
			storeKey := &spec.AssignmentStmt{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(rangeStmt.Key),
					Meta: "assignment",
					Type: "Void",
				},
				Variable: keyName,
				Value: &spec.IdentifierExpr{
					BaseNode: spec.BaseNode{
						ID:   c.nextID(rangeStmt.Key),
						Meta: "identifier",
					},
					Name: indexName,
				},
			}
			assignments = append(assignments, storeKey)
		}
	}

	// Value = rangeObj[index]
	if rangeStmt.Value != nil {
		if id, ok := rangeStmt.Value.(*ast.Ident); !ok || id.Name != "_" {
			var valName spec.Ident
			if id, ok := rangeStmt.Value.(*ast.Ident); ok {
				valName = spec.Ident(id.Name)
			} else {
				return nil, errors.New("range value must be identifier")
			}

			indexExpr := &spec.IndexExpr{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(rangeStmt.Value),
					Meta: "index",
				},
				Object: &spec.IdentifierExpr{
					BaseNode: spec.BaseNode{
						ID:   c.nextID(rangeStmt.Value),
						Meta: "identifier",
					},
					Name: rangeObjName,
				},
				Index: &spec.IdentifierExpr{
					BaseNode: spec.BaseNode{
						ID:   c.nextID(rangeStmt.Value),
						Meta: "identifier",
					},
					Name: indexName,
				},
			}

			assignVal := &spec.AssignmentStmt{
				BaseNode: spec.BaseNode{
					ID:   c.nextID(rangeStmt.Value),
					Meta: "assignment",
					Type: "Void",
				},
				Variable: valName,
				Value:    indexExpr,
			}
			assignments = append(assignments, assignVal)
		}
	}

	// 6. Body
	bodyBlock, err := c.convertBlockStmt(rangeStmt.Body)
	if err != nil {
		return nil, err
	}

	// Prepend assignments
	bodyBlock.Children = append(assignments, bodyBlock.Children...)

	forStmt := &spec.ForStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "for",
			Type: "Void",
		},
		Init:   initIndex,
		Cond:   cond,
		Update: update,
		Body:   bodyBlock,
	}

	// Wrap in block
	return &spec.BlockStmt{
		BaseNode: spec.BaseNode{
			ID:   c.nextID(rangeStmt),
			Meta: "block",
			Type: "Void",
		},
		Children: []spec.Stmt{initRangeObj, forStmt},
	}, nil
}
