package engine

import (
	"encoding/json"
	"errors"
	"fmt"
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
			BaseNode:  ast.BaseNode{ID: "boot", Meta: "boot", Type: "Void"},
			Constants: make(map[string]string),
			Variables: make(map[ast.Ident]ast.Expr),
			Structs:   make(map[ast.Ident]*ast.StructStmt),
			Functions: make(map[ast.Ident]*ast.FunctionStmt),
			Main:      make([]ast.Stmt, 0),
		}
		if block, ok := root.(*ast.BlockStmt); ok {
			rootBlock.Main = block.Children
		}
	}
	ctx, err := ast.NewValidator(rootBlock)
	if err != nil {
		return nil, nil, err
	}
	ctx.SetLoader(loader)
	if err := call(ctx); err != nil {
		return nil, nil, err
	}

	if err := rootBlock.Check(ast.NewSemanticContext(ctx)); err != nil {
		return rootBlock, ctx.Logs(), err
	}
	optimized := rootBlock.Optimize(ast.NewOptimizeContext(ctx))
	opts := optimized.(*ast.ProgramStmt)

	// 提取 main 函数作为入口
	if len(opts.Main) == 0 {
		if it, ok := opts.Functions["main"]; ok {
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
		for k, vData := range raw.Variables {
			vNode, _ := parseExpr(vData)
			result.Variables[ast.Ident(k)] = vNode
		}
		for k, sData := range raw.Structs {
			sNode, err := unmarshalNode(sData)
			if err != nil {
				return nil, err
			}
			result.Structs[ast.Ident(k)] = sNode.(*ast.StructStmt)
		}
		for k, fData := range raw.Functions {
			fNode, err := unmarshalNode(fData)
			if err != nil {
				return nil, err
			}
			result.Functions[ast.Ident(k)] = fNode.(*ast.FunctionStmt)
		}
		for _, mData := range raw.Main {
			mNode, err := unmarshalNode(mData)
			if err != nil {
				return nil, err
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
		result := &ast.BlockStmt{BaseNode: baseNode, Inner: raw.Inner}
		for _, childData := range raw.Children {
			child, _ := unmarshalNode(childData)
			result.Children = append(result.Children, child.(ast.Stmt))
		}
		return result, nil
	case "if":
		var raw struct {
			Cond json.RawMessage `json:"cond"`
			Body json.RawMessage `json:"body"`
			Else json.RawMessage `json:"else,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.IfStmt{BaseNode: baseNode}
		n.Cond, _ = parseExpr(raw.Cond)
		if raw.Body != nil {
			node, _ := unmarshalNode(raw.Body)
			if block, ok := node.(*ast.BlockStmt); ok {
				n.Body = block
			} else {
				n.Body = ast.NewBlock(nil, node.(ast.Stmt))
			}
		}
		if raw.Else != nil {
			node, _ := unmarshalNode(raw.Else)
			if block, ok := node.(*ast.BlockStmt); ok {
				n.ElseBody = block
			} else {
				n.ElseBody = ast.NewBlock(nil, node.(ast.Stmt))
			}
		}
		return n, nil
	case "for":
		var raw struct {
			Init json.RawMessage `json:"init,omitempty"`
			Cond json.RawMessage `json:"cond,omitempty"`
			Upd  json.RawMessage `json:"update,omitempty"`
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.ForStmt{BaseNode: baseNode}
		if raw.Init != nil {
			n.Init, _ = unmarshalNode(raw.Init)
		}
		if raw.Cond != nil {
			n.Cond, _ = parseExpr(raw.Cond)
		}
		if raw.Upd != nil {
			n.Update, _ = unmarshalNode(raw.Upd)
		}
		if raw.Body != nil {
			n.Body, _ = unmarshalNode(raw.Body)
		}
		return n, nil
	case "return":
		var raw struct {
			Results []json.RawMessage `json:"results"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.ReturnStmt{BaseNode: baseNode}
		for _, rData := range raw.Results {
			r, _ := parseExpr(rData)
			n.Results = append(n.Results, r)
		}
		return n, nil
	case "function":
		var raw struct {
			Name   string `json:"name"`
			Params []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"params,omitempty"`
			Ret  string          `json:"return,omitempty"`
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.FunctionStmt{BaseNode: baseNode, FunctionType: ast.FunctionType{Return: ast.GoMiniType(raw.Ret)}}
		n.Name = ast.Ident(raw.Name)
		for _, p := range raw.Params {
			n.Params = append(n.Params, ast.FunctionParam{Name: ast.Ident(p.Name), Type: ast.GoMiniType(p.Type)})
		}
		if raw.Body != nil {
			b, _ := unmarshalNode(raw.Body)
			n.Body = b.(*ast.BlockStmt)
		}
		return n, nil
	case "call":
		var raw struct {
			Func json.RawMessage   `json:"func"`
			Args []json.RawMessage `json:"args,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.CallExprStmt{BaseNode: baseNode}
		n.Func, _ = parseExpr(raw.Func)
		for _, aData := range raw.Args {
			arg, _ := parseExpr(aData)
			n.Args = append(n.Args, arg)
		}
		return n, nil
	case "struct":
		var raw struct {
			Name       string            `json:"name"`
			Fields     map[string]string `json:"fields"`
			FieldNames []string          `json:"field_names,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.StructStmt{BaseNode: baseNode, Name: ast.Ident(raw.Name), Fields: make(map[ast.Ident]ast.GoMiniType)}
		for k, v := range raw.Fields {
			n.Fields[ast.Ident(k)] = ast.GoMiniType(v)
		}
		for _, v := range raw.FieldNames {
			n.FieldNames = append(n.FieldNames, ast.Ident(v))
		}
		return n, nil
	case "range":
		var raw struct {
			Key    string          `json:"key"`
			Value  string          `json:"value"`
			X      json.RawMessage `json:"x"`
			Body   json.RawMessage `json:"body"`
			Define bool            `json:"define"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.RangeStmt{BaseNode: baseNode, Key: ast.Ident(raw.Key), Value: ast.Ident(raw.Value), Define: raw.Define}
		n.X, _ = parseExpr(raw.X)
		if raw.Body != nil {
			node, _ := unmarshalNode(raw.Body)
			n.Body = node.(*ast.BlockStmt)
		}
		return n, nil
	case "switch":
		var raw struct {
			Init json.RawMessage `json:"init,omitempty"`
			Tag  json.RawMessage `json:"tag,omitempty"`
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.SwitchStmt{BaseNode: baseNode}
		if raw.Init != nil {
			node, _ := unmarshalNode(raw.Init)
			n.Init = node.(ast.Stmt)
		}
		if raw.Tag != nil {
			n.Tag, _ = parseExpr(raw.Tag)
		}
		if raw.Body != nil {
			node, _ := unmarshalNode(raw.Body)
			n.Body = node.(*ast.BlockStmt)
		}
		return n, nil
	case "case":
		var raw struct {
			List []json.RawMessage `json:"list,omitempty"`
			Body []json.RawMessage `json:"body,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.CaseClause{BaseNode: baseNode}
		for _, eData := range raw.List {
			expr, _ := parseExpr(eData)
			n.List = append(n.List, expr)
		}
		for _, sData := range raw.Body {
			stmt, _ := unmarshalNode(sData)
			n.Body = append(n.Body, stmt.(ast.Stmt))
		}
		return n, nil
	case "defer":
		var raw struct {
			Call json.RawMessage `json:"call"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.DeferStmt{BaseNode: baseNode}
		n.Call, _ = parseExpr(raw.Call)
		return n, nil
	case "decl":
		var raw struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(data, &raw)
		return &ast.GenDeclStmt{BaseNode: baseNode, Name: ast.Ident(raw.Name), Kind: ast.GoMiniType(raw.Kind)}, nil
	case "increment":
		var raw struct {
			Operand  json.RawMessage `json:"operand"`
			Operator string          `json:"operator"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.IncDecStmt{BaseNode: baseNode, Operator: ast.Ident(raw.Operator)}
		n.Operand, _ = parseExpr(raw.Operand)
		return n, nil
	case "interrupt":
		var raw struct {
			InterruptType string `json:"interrupt_type"`
		}
		_ = json.Unmarshal(data, &raw)
		return &ast.InterruptStmt{BaseNode: baseNode, InterruptType: raw.InterruptType}, nil
	case "try":
		var raw struct {
			Body    json.RawMessage `json:"body"`
			Catch   json.RawMessage `json:"catch,omitempty"`
			Finally json.RawMessage `json:"finally,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.TryStmt{BaseNode: baseNode}
		if raw.Body != nil {
			node, _ := unmarshalNode(raw.Body)
			n.Body = node.(*ast.BlockStmt)
		}
		if raw.Catch != nil {
			node, _ := unmarshalNode(raw.Catch)
			n.Catch = node.(*ast.CatchClause)
		}
		if raw.Finally != nil {
			node, _ := unmarshalNode(raw.Finally)
			n.Finally = node.(*ast.BlockStmt)
		}
		return n, nil
	case "catch":
		var raw struct {
			VarName string          `json:"var_name,omitempty"`
			Body    json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.CatchClause{BaseNode: baseNode, VarName: ast.Ident(raw.VarName)}
		if raw.Body != nil {
			node, _ := unmarshalNode(raw.Body)
			n.Body = node.(*ast.BlockStmt)
		}
		return n, nil
	case "member":
		var raw struct {
			Object   json.RawMessage `json:"object"`
			Property string          `json:"property"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.MemberExpr{BaseNode: baseNode, Property: ast.Ident(raw.Property)}
		n.Object, _ = parseExpr(raw.Object)
		return n, nil
	case "index":
		var raw struct {
			Object json.RawMessage `json:"object"`
			Index  json.RawMessage `json:"index"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.IndexExpr{BaseNode: baseNode}
		n.Object, _ = parseExpr(raw.Object)
		n.Index, _ = parseExpr(raw.Index)
		return n, nil
	case "slice":
		var raw struct {
			X    json.RawMessage `json:"x"`
			Low  json.RawMessage `json:"low,omitempty"`
			High json.RawMessage `json:"high,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.SliceExpr{BaseNode: baseNode}
		n.X, _ = parseExpr(raw.X)
		if raw.Low != nil {
			n.Low, _ = parseExpr(raw.Low)
		}
		if raw.High != nil {
			n.High, _ = parseExpr(raw.High)
		}
		return n, nil
	case "composite":
		var raw struct {
			Kind   string `json:"type"`
			Values []struct {
				Key   json.RawMessage `json:"key,omitempty"`
				Value json.RawMessage `json:"value"`
			} `json:"values,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.CompositeExpr{BaseNode: baseNode, Kind: ast.Ident(raw.Kind)}
		for _, v := range raw.Values {
			elem := ast.CompositeElement{}
			if v.Key != nil {
				elem.Key, _ = parseExpr(v.Key)
			}
			elem.Value, _ = parseExpr(v.Value)
			n.Values = append(n.Values, elem)
		}
		return n, nil
	case "identifier":
		var raw struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(data, &raw)
		return &ast.IdentifierExpr{BaseNode: baseNode, Name: ast.Ident(raw.Name)}, nil
	case "assignment":
		var raw struct {
			LHS json.RawMessage `json:"lhs"`
			Val json.RawMessage `json:"value"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.AssignmentStmt{BaseNode: baseNode}
		n.LHS, _ = parseExpr(raw.LHS)
		n.Value, _ = parseExpr(raw.Val)
		return n, nil
	case "multi_assignment":
		var raw struct {
			LHS []json.RawMessage `json:"lhs"`
			Val json.RawMessage   `json:"value"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.MultiAssignmentStmt{BaseNode: baseNode}
		for _, l := range raw.LHS {
			if e, err := parseExpr(l); err == nil {
				n.LHS = append(n.LHS, e)
			}
		}
		n.Value, _ = parseExpr(raw.Val)
		return n, nil
	case "literal":
		var raw struct {
			Kind  string `json:"kind"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}
		_ = json.Unmarshal(data, &raw)
		if raw.Kind == "" {
			raw.Kind = raw.Type
		}
		// 隔离架构下直接保留字面量，不包装 Data
		return &ast.LiteralExpr{BaseNode: ast.BaseNode{ID: baseNode.ID, Meta: baseNode.Meta, Type: ast.GoMiniType(raw.Kind)}, Value: raw.Value}, nil
	case "binary":
		var raw struct {
			L  json.RawMessage `json:"left"`
			Op string          `json:"operator"`
			R  json.RawMessage `json:"right"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.BinaryExpr{BaseNode: baseNode, Operator: ast.Ident(raw.Op)}
		n.Left, _ = parseExpr(raw.L)
		n.Right, _ = parseExpr(raw.R)
		return n, nil
	case "unary":
		var raw struct {
			Op  string          `json:"operator"`
			Val json.RawMessage `json:"operand"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.UnaryExpr{BaseNode: baseNode, Operator: ast.Ident(raw.Op)}
		n.Operand, _ = parseExpr(raw.Val)
		return n, nil
	case "const_ref":
		var raw struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(data, &raw)
		return &ast.ConstRefExpr{BaseNode: baseNode, Name: ast.Ident(raw.Name)}, nil
	case "func_lit":
		var raw struct {
			Params       []ast.FunctionParam `json:"params,omitempty"`
			Return       ast.GoMiniType      `json:"return,omitempty"`
			Body         json.RawMessage     `json:"body"`
			CaptureNames []string            `json:"capture_names,omitempty"`
		}
		_ = json.Unmarshal(data, &raw)
		n := &ast.FuncLitExpr{
			BaseNode:     baseNode,
			FunctionType: ast.FunctionType{Params: raw.Params, Return: raw.Return},
			CaptureNames: raw.CaptureNames,
		}
		if raw.Body != nil {
			bodyNode, _ := unmarshalNode(raw.Body)
			n.Body = bodyNode.(*ast.BlockStmt)
		}
		return n, nil
	default:
		return nil, errors.New("unknown meta: " + baseNode.Meta)
	}
}
