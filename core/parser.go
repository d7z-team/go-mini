package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
)

func unmarshalNode(data []byte) (ast.Node, error) {
	var typeInfo struct {
		ID      string `json:"id"`
		Meta    string `json:"meta"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &typeInfo); err != nil {
		return nil, fmt.Errorf("解析节点类型失败: %w", err)
	}
	if typeInfo.ID == "" {
		typeInfo.ID = fmt.Sprintf("rid_%d", time.Now().UnixNano())
	}
	node, err := unmarshalNodeData(ast.BaseNode{ID: typeInfo.ID, Meta: typeInfo.Meta, Message: typeInfo.Message}, data)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("解析节点%s(%s)失败: %s", typeInfo.Meta, typeInfo.ID, string(data)))
	}

	return node, nil
}

func parseExpr(data []byte) (ast.Expr, error) {
	if len(data) == 0 {
		return nil, nil
	}

	node, err := unmarshalNode(data)
	if err != nil {
		return nil, err
	}

	if expr, ok := node.(ast.Expr); ok {
		return expr, nil
	}

	return nil, fmt.Errorf("节点不是表达式类型: %T", node)
}

func Unmarshal(data []byte) (ast.Node, error) {
	if len(data) == 0 {
		return ast.NewBlock(nil), nil
	}

	if data[0] != '[' {
		node, err := unmarshalNode(data)
		if err != nil {
			return nil, err
		}

		if block, ok := node.(*ast.BlockStmt); ok {
			return block, nil
		}

		if prog, ok := node.(*ast.ProgramStmt); ok {
			return prog, nil
		}

		return ast.NewBlock(nil, node.(ast.Stmt)), nil
	}

	var rawNodes []json.RawMessage
	if err := json.Unmarshal(data, &rawNodes); err != nil {
		return nil, fmt.Errorf("解析节点数组失败: %w", err)
	}

	block := ast.NewBlock(nil)
	for i, raw := range rawNodes {
		node, err := unmarshalNode(raw)
		if err != nil {
			return nil, fmt.Errorf("解析节点[%d]失败: %w", i, err)
		}
		block.Children = append(block.Children, node.(ast.Stmt))
	}

	return block, nil
}

func ValidateAndOptimize(root ast.Node, call func(v *ast.ValidContext) error) (*ast.ProgramStmt, []ast.Logs, error) {
	return ValidateAndOptimizeWithLoader(root, nil, call)
}

func ValidateAndOptimizeWithLoader(root ast.Node, loader func(path string) (*ast.ProgramStmt, error), call func(v *ast.ValidContext) error) (*ast.ProgramStmt, []ast.Logs, error) {
	var ok bool
	var rootBlock *ast.ProgramStmt
	if rootBlock, ok = root.(*ast.ProgramStmt); !ok {
		rootBlock = &ast.ProgramStmt{
			BaseNode: ast.BaseNode{
				ID:   "boot",
				Meta: "boot",
				Type: "Void",
			},
			Constants: make(map[string]string),
			Variables: make(map[ast.Ident]ast.Expr),
			Structs:   make(map[ast.Ident]*ast.StructStmt),
			Functions: make(map[ast.Ident]*ast.FunctionStmt),
			Main:      make([]ast.Stmt, 0),
		}
		block, ok := root.(*ast.BlockStmt)
		if !ok {
			return rootBlock, []ast.Logs{{
				Path:    make([]string, 0),
				Level:   "error",
				Message: "无法解析对象",
			}}, errors.New("error")
		}
		rootBlock.Main = block.Children
	}
	ctx, err := ast.NewValidator(rootBlock)
	if err != nil {
		return nil, nil, err
	}
	ctx.SetLoader(loader)

	if err := call(ctx); err != nil {
		return nil, nil, err
	}

	// 自动加载并校验导入的包
	for _, imp := range rootBlock.Imports {
		if err := ctx.ImportPackage(imp.Path); err != nil {
			// 将加载错误记入日志
			ctx.AddErrorf("导入包 %s 失败: %v", imp.Path, err)
		}
	}

	optimized, ok := rootBlock.Validate(ctx)
	if !ok {
		return rootBlock, ctx.Logs(), errors.New("error")
	}
	opts := optimized.(*ast.ProgramStmt)

	// 合并导入的包符号到最终的程序中
	for k, v := range ctx.GetImportedFuncs() {
		opts.Functions[k] = v
	}
	for k, v := range ctx.GetImportedStructs() {
		opts.Structs[k] = v
	}
	for k, v := range ctx.GetImportedVars() {
		opts.Variables[k] = v
	}
	for k, v := range ctx.GetImportedConsts() {
		opts.Constants[k] = v
	}

	// 提取 main 函数作为入口
	if len(opts.Main) == 0 {
		if it, ok := opts.Functions["main"]; ok && len(it.Params) == 0 && it.Return == "Void" {
			delete(opts.Functions, "main")
			opts.Main = it.Body.Children
		}
	}
	return opts, ctx.Logs(), nil
}

func unmarshalNodeData(baseNode ast.BaseNode, data []byte) (ast.Node, error) {
	switch baseNode.Meta {
	case "boot":
		var raw struct {
			Package   string                     `json:"package,omitempty"`
			Imports   []ast.ImportSpec           `json:"imports,omitempty"`
			Constants map[string]string          `json:"constants"`
			Variables map[string]json.RawMessage `json:"variables"`
			Structs   map[string]json.RawMessage `json:"structs"`
			Functions map[string]json.RawMessage `json:"functions"`
			Main      []json.RawMessage          `json:"main"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		result := &ast.ProgramStmt{
			BaseNode:  baseNode,
			Package:   raw.Package,
			Imports:   raw.Imports,
			Constants: raw.Constants,
			Variables: make(map[ast.Ident]ast.Expr),
			Structs:   make(map[ast.Ident]*ast.StructStmt),
			Functions: make(map[ast.Ident]*ast.FunctionStmt),
		}
		if result.Package == "" {
			result.Package = "main"
		}
		for k, vData := range raw.Variables {
			vNode, err := parseExpr(vData)
			if err != nil {
				return nil, fmt.Errorf("解析变量[%s]失败: %w", k, err)
			}
			result.Variables[ast.Ident(k)] = vNode
		}
		for k, sData := range raw.Structs {
			sNode, err := unmarshalNode(sData)
			if err != nil {
				return nil, fmt.Errorf("解析结构体[%s]失败: %w", k, err)
			}
			result.Structs[ast.Ident(k)] = sNode.(*ast.StructStmt)
		}
		for k, fData := range raw.Functions {
			fNode, err := unmarshalNode(fData)
			if err != nil {
				return nil, fmt.Errorf("解析函数[%s]失败: %w", k, err)
			}
			result.Functions[ast.Ident(k)] = fNode.(*ast.FunctionStmt)
		}
		for i, mData := range raw.Main {
			mNode, err := unmarshalNode(mData)
			if err != nil {
				return nil, fmt.Errorf("解析main[%d]失败: %w", i, err)
			}
			result.Main = append(result.Main, mNode.(ast.Stmt))
		}
		return result, nil
	case "block":
		var raw struct {
			Children []json.RawMessage `json:"children"`
			Inner    bool              `json:"inner,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		result := &ast.BlockStmt{
			BaseNode: baseNode,
			Inner:    raw.Inner,
		}
		for _, childData := range raw.Children {
			child, err := unmarshalNode(childData)
			if err != nil {
				return nil, fmt.Errorf("解析block子节点失败: %w", err)
			}
			result.Children = append(result.Children, child.(ast.Stmt))
		}
		return result, nil

	case "if":
		var raw struct {
			Cond     json.RawMessage `json:"cond"`
			Body     json.RawMessage `json:"body"`
			ElseBody json.RawMessage `json:"else,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.IfStmt{
			BaseNode: baseNode,
		}
		cond, err := parseExpr(raw.Cond)
		if err != nil {
			return nil, fmt.Errorf("if.cond: %w", err)
		}
		n.Cond = cond

		if raw.Body != nil {
			node, err := unmarshalNode(raw.Body)
			if err != nil {
				return nil, fmt.Errorf("解析if body失败: %w", err)
			}
			if data, ok := node.(*ast.BlockStmt); ok {
				n.Body = data
			} else {
				n.Body = ast.NewBlock(data, data)
			}
		}

		if raw.ElseBody != nil {
			node, err := unmarshalNode(raw.ElseBody)
			if err != nil {
				return nil, fmt.Errorf("解析if else body失败: %w", err)
			}
			if data, ok := node.(*ast.BlockStmt); ok {
				n.ElseBody = data
			} else {
				n.ElseBody = ast.NewBlock(data, data)
			}
		}
		return n, nil

	case "for":
		var raw struct {
			Init   json.RawMessage `json:"init,omitempty"`
			Cond   json.RawMessage `json:"cond,omitempty"`
			Update json.RawMessage `json:"update,omitempty"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.ForStmt{BaseNode: baseNode}
		if raw.Init != nil {
			init, err := unmarshalNode(raw.Init)
			if err != nil {
				return nil, fmt.Errorf("解析for init失败: %w", err)
			}
			n.Init = init
		}

		if raw.Cond != nil {
			cond, err := parseExpr(raw.Cond)
			if err != nil {
				return nil, fmt.Errorf("解析for cond失败: %w", err)
			}
			n.Cond = cond
		}

		if raw.Update != nil {
			update, err := unmarshalNode(raw.Update)
			if err != nil {
				return nil, fmt.Errorf("解析for update失败: %w", err)
			}
			n.Update = update
		}

		if raw.Body != nil {
			body, err := unmarshalNode(raw.Body)
			if err != nil {
				return nil, fmt.Errorf("解析for body失败: %w", err)
			}
			n.Body = body
		}
		return n, nil

	case "switch":
		var raw struct {
			Cond    json.RawMessage   `json:"cond"`
			Cases   []json.RawMessage `json:"cases"`
			Default json.RawMessage   `json:"default,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.SwitchStmt{BaseNode: baseNode}
		cond, err := parseExpr(raw.Cond)
		if err != nil {
			return nil, fmt.Errorf("解析switch cond失败: %w", err)
		}
		n.Cond = cond

		n.Cases = make([]ast.SwitchCase, 0, len(raw.Cases))
		for _, caseData := range raw.Cases {
			var rawCase struct {
				Cond []json.RawMessage `json:"cond"`
				Body json.RawMessage   `json:"body"`
			}
			if err := json.Unmarshal(caseData, &rawCase); err != nil {
				return nil, err
			}

			caseItem := ast.SwitchCase{}
			for _, condData := range rawCase.Cond {
				cond, err := parseExpr(condData)
				if err != nil {
					return nil, fmt.Errorf("解析switch case cond失败: %w", err)
				}
				caseItem.Cond = append(caseItem.Cond, cond)
			}

			if rawCase.Body != nil {
				node, err := unmarshalNode(rawCase.Body)
				if err != nil {
					return nil, fmt.Errorf("解析switch case body失败: %w", err)
				}
				if block, ok := node.(*ast.BlockStmt); ok {
					caseItem.Body = block
				} else {
					return nil, errors.New("switch case body必须是block类型")
				}
			}
			n.Cases = append(n.Cases, caseItem)
		}

		if raw.Default != nil {
			var rawDefault struct {
				Body json.RawMessage `json:"body"`
			}
			if err := json.Unmarshal(raw.Default, &rawDefault); err != nil {
				return nil, err
			}

			defaultCase := &ast.SwitchCase{}
			if rawDefault.Body != nil {
				node, err := unmarshalNode(rawDefault.Body)
				if err != nil {
					return nil, fmt.Errorf("解析switch default body失败: %w", err)
				}
				if block, ok := node.(*ast.BlockStmt); ok {
					defaultCase.Body = block
				} else {
					return nil, errors.New("switch default body必须是block类型")
				}
			}
			n.Default = defaultCase
		}
		return n, nil

	case "return":
		var raw struct {
			Results []json.RawMessage `json:"results"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.ReturnStmt{BaseNode: baseNode}
		n.Results = make([]ast.Expr, len(raw.Results))
		for i, resultData := range raw.Results {
			result, err := parseExpr(resultData)
			if err != nil {
				return nil, fmt.Errorf("result[%d]: %w", i, err)
			}
			n.Results[i] = result
		}
		return n, nil

	case "function":
		var raw struct {
			Name   string `json:"name"`
			Scope  string `json:"scope"`
			Params []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"params,omitempty"`
			Return string          `json:"return,omitempty"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.FunctionStmt{
			BaseNode: baseNode,
			Scope:    ast.Ident(raw.Scope),
			FunctionType: ast.FunctionType{
				Params: make([]ast.FunctionParam, 0),
			},
			Body: ast.NewBlock(nil),
		}
		n.Name = ast.Ident(raw.Name)

		for _, param := range raw.Params {
			for _, fN := range n.Params {
				if fN.Name == ast.Ident(param.Name) {
					return nil, errors.New("变量名称重复")
				}
			}
			n.Params = append(n.Params, ast.FunctionParam{
				Name: ast.Ident(param.Name),
				Type: ast.OPSType(param.Type),
			})
		}
		n.Return = ast.OPSType(raw.Return)

		if raw.Body != nil {
			body, err := unmarshalNode(raw.Body)
			if err != nil {
				return nil, fmt.Errorf("解析function body失败: %w", err)
			}
			n.Body = body.(*ast.BlockStmt)
		}
		return n, nil
	case "call":
		var raw struct {
			Func json.RawMessage   `json:"func"`
			Args []json.RawMessage `json:"args,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.CallExprStmt{BaseNode: baseNode}
		fc, err := parseExpr(raw.Func)
		if err != nil {
			return nil, err
		}
		n.Func = fc
		for _, argData := range raw.Args {
			arg, err := parseExpr(argData)
			if err != nil {
				return nil, fmt.Errorf("解析call参数失败: %w", err)
			}
			n.Args = append(n.Args, arg)
		}
		return n, nil

	case "struct_call":
		var raw struct {
			Object json.RawMessage   `json:"object"`
			Name   string            `json:"name"`
			Args   []json.RawMessage `json:"args,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.StructCallExpr{
			BaseNode: baseNode,
			Name:     ast.Ident(raw.Name),
		}
		fc, err := parseExpr(raw.Object)
		if err != nil {
			return nil, err
		}
		n.Object = fc
		for _, argData := range raw.Args {
			arg, err := parseExpr(argData)
			if err != nil {
				return nil, fmt.Errorf("解析 struct_call 参数失败: %w", err)
			}
			n.Args = append(n.Args, arg)
		}
		return n, nil
	case "identifier":
		var raw struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		n := ast.IdentifierExpr{
			BaseNode: baseNode,
			Name:     ast.Ident(raw.Name),
		}
		return &n, nil
	case "assignment":
		var raw struct {
			Variable string          `json:"variable"`
			Value    json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.AssignmentStmt{
			BaseNode: baseNode,
			Variable: ast.Ident(raw.Variable),
		}
		value, err := parseExpr(raw.Value)
		if err != nil {
			return nil, fmt.Errorf("解析assignment value失败: %w", err)
		}
		n.Value = value
		return n, nil

	case "literal":
		var raw struct {
			Kind  string `json:"kind"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if raw.Kind == "" {
			raw.Kind = raw.Type
		}

		// 处理基础类型字面量转换
		switch raw.Kind {
		case "String":
			data := ast.NewMiniString(raw.Value)
			return &ast.LiteralExpr{
				BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: "String"},
				Value:    raw.Value,
				Data:     &data,
			}, nil
		case "Int64":
			val, _ := strconv.ParseInt(raw.Value, 10, 64)
			data := ast.NewMiniInt64(val)
			return &ast.LiteralExpr{
				BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: "Int64"},
				Value:    raw.Value,
				Data:     &data,
			}, nil
		case "Float64":
			val, _ := strconv.ParseFloat(raw.Value, 64)
			data := ast.NewMiniFloat64(val)
			return &ast.LiteralExpr{
				BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: "Float64"},
				Value:    raw.Value,
				Data:     &data,
			}, nil
		case "Bool":
			val, _ := strconv.ParseBool(raw.Value)
			data := ast.NewMiniBool(val)
			return &ast.LiteralExpr{
				BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: "Bool"},
				Value:    raw.Value,
				Data:     &data,
			}, nil
		case "Uint8":
			val, _ := strconv.ParseUint(raw.Value, 10, 8)
			data := ast.NewMiniUint8(byte(val))
			return &ast.LiteralExpr{
				BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: "Uint8"},
				Value:    raw.Value,
				Data:     &data,
			}, nil
		}

		return &ast.LiteralExpr{
			BaseNode: ast.BaseNode{
				ID:   baseNode.ID,
				Meta: baseNode.Meta,
				Type: ast.OPSType(raw.Kind),
			},
			Value: raw.Value,
		}, nil

	case "binary":
		var raw struct {
			Left     json.RawMessage `json:"left"`
			Operator string          `json:"operator"`
			Right    json.RawMessage `json:"right"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.BinaryExpr{
			BaseNode: baseNode,
			Operator: ast.Ident(raw.Operator),
		}

		left, err := parseExpr(raw.Left)
		if err != nil {
			return nil, fmt.Errorf("解析binary left失败: %w", err)
		}
		n.Left = left

		right, err := parseExpr(raw.Right)
		if err != nil {
			return nil, fmt.Errorf("解析binary right失败: %w", err)
		}
		n.Right = right
		return n, nil

	case "unary":
		var raw struct {
			Operator string          `json:"operator"`
			Operand  json.RawMessage `json:"operand"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.UnaryExpr{
			BaseNode: baseNode,
			Operator: ast.Ident(raw.Operator),
		}

		operand, err := parseExpr(raw.Operand)
		if err != nil {
			return nil, fmt.Errorf("解析unary operand失败: %w", err)
		}
		n.Operand = operand
		return n, nil

	case "member":
		var raw struct {
			Object   json.RawMessage `json:"object"`
			Property string          `json:"property"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.MemberExpr{
			BaseNode: baseNode,
			Property: ast.Ident(raw.Property),
		}

		object, err := parseExpr(raw.Object)
		if err != nil {
			return nil, fmt.Errorf("解析member object失败: %w", err)
		}
		n.Object = object
		return n, nil

	case "index":
		var raw struct {
			Object json.RawMessage `json:"object"`
			Index  json.RawMessage `json:"index"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.IndexExpr{BaseNode: baseNode}
		object, err := parseExpr(raw.Object)
		if err != nil {
			return nil, fmt.Errorf("解析index object失败: %w", err)
		}
		n.Object = object

		index, err := parseExpr(raw.Index)
		if err != nil {
			return nil, fmt.Errorf("解析index index失败: %w", err)
		}
		n.Index = index
		return n, nil

	case "composite":
		var raw struct {
			Kind   string `json:"kind"`
			Values []struct {
				Key   json.RawMessage `json:"key,omitempty"`
				Value json.RawMessage `json:"value"`
			} `json:"values,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.CompositeExpr{
			BaseNode: baseNode,
			Kind:     ast.Ident(raw.Kind),
			Values:   make([]ast.CompositeElement, len(raw.Values)),
		}

		// 设置类型
		if raw.Kind != "" {
			n.BaseNode.Type = ast.OPSType(raw.Kind)
		}

		for i, elem := range raw.Values {
			if elem.Key != nil {
				key, err := parseExpr(elem.Key)
				if err != nil {
					return nil, fmt.Errorf("element[%d].key: %w", i, err)
				}
				n.Values[i].Key = key
			}

			value, err := parseExpr(elem.Value)
			if err != nil {
				return nil, fmt.Errorf("element[%d].value: %w", i, err)
			}
			n.Values[i].Value = value
		}
		return n, nil

	case "interrupt":
		var raw struct {
			InterruptType string `json:"interrupt"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		return &ast.InterruptStmt{
			BaseNode:      baseNode,
			InterruptType: raw.InterruptType,
		}, nil

	case "defer":
		var raw struct {
			Call json.RawMessage `json:"call"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.DeferStmt{BaseNode: baseNode}
		call, err := parseExpr(raw.Call)
		if err != nil {
			return nil, fmt.Errorf("解析 defer call 失败: %w", err)
		}
		n.Call = call
		return n, nil

	case "struct":
		var raw struct {
			Name   string            `json:"name"`
			Fields map[string]string `json:"fields"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.StructStmt{
			BaseNode: baseNode,
			Name:     ast.Ident(raw.Name),
		}

		for key, field := range raw.Fields {
			n.Fields[ast.Ident(key)] = ast.OPSType(field)
		}
		return n, nil
	case "increment":
		var raw struct {
			Operator string          `json:"operator"`
			Operand  json.RawMessage `json:"operand"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.IncDecStmt{
			BaseNode: baseNode,
			Operator: ast.Ident(raw.Operator),
		}
		operand, err := parseExpr(raw.Operand)
		if err != nil {
			return nil, fmt.Errorf("解析unary operand失败: %w", err)
		}
		n.Operand = operand
		return n, nil

	case "new":
		var raw struct {
			TypeName string `json:"kind"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.NewExpr{
			BaseNode: baseNode,
			Kind:     ast.Ident(raw.TypeName),
		}
		return n, nil

	case "address":
		var raw struct {
			Operand json.RawMessage `json:"operand"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.AddressExpr{BaseNode: baseNode}
		operand, err := parseExpr(raw.Operand)
		if err != nil {
			return nil, fmt.Errorf("解析取地址操作数失败: %w", err)
		}
		n.Operand = operand
		return n, nil

	case "deref":
		var raw struct {
			Operand json.RawMessage `json:"operand"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}

		n := &ast.DerefExpr{BaseNode: baseNode}
		operand, err := parseExpr(raw.Operand)
		if err != nil {
			return nil, fmt.Errorf("解析解引用操作数失败: %w", err)
		}
		n.Operand = operand
		return n, nil
	case "const_ref":
		var raw struct {
			Operand string `json:"name"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		n := &ast.ConstRefExpr{BaseNode: baseNode, Name: ast.Ident(raw.Operand)}
		return n, nil
	default:
		return nil, errors.New("unknown type:" + baseNode.Meta)
	}
}
