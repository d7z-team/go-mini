package ast

import (
	"fmt"
	"reflect"
)

func RewriteOperatorOverloads(program *ProgramStmt) bool {
	if program == nil {
		return false
	}
	r := operatorRewriter{}
	for name, expr := range program.Variables {
		program.Variables[name] = r.rewriteExpr(expr)
	}
	for i, stmt := range program.Main {
		program.Main[i] = r.rewriteStmt(stmt)
	}
	for _, fn := range program.Functions {
		if fn != nil && fn.Body != nil {
			fn.Body = r.rewriteBlock(fn.Body)
		}
	}
	return r.changed
}

func AssertNoResidualOperatorOverloads(program *ProgramStmt) error {
	if node := residualOperatorOverload(reflect.ValueOf(program), map[uintptr]bool{}); node != nil {
		return fmt.Errorf("operator overload rewrite left residual %T", node)
	}
	return nil
}

type operatorRewriter struct {
	changed bool
}

func (r *operatorRewriter) rewriteBlock(block *BlockStmt) *BlockStmt {
	if block == nil {
		return nil
	}
	for i, stmt := range block.Children {
		block.Children[i] = r.rewriteStmt(stmt)
	}
	return block
}

func (r *operatorRewriter) rewriteStmt(stmt Stmt) Stmt {
	switch n := stmt.(type) {
	case nil:
		return nil
	case *BlockStmt:
		return r.rewriteBlock(n)
	case *IfStmt:
		n.Cond = r.rewriteExpr(n.Cond)
		n.Body = r.rewriteBlock(n.Body)
		n.ElseBody = r.rewriteBlock(n.ElseBody)
	case *ForStmt:
		n.Init = r.rewriteNode(n.Init)
		n.Cond = r.rewriteExpr(n.Cond)
		n.Update = r.rewriteNode(n.Update)
		n.Body = r.rewriteNode(n.Body)
	case *ReturnStmt:
		for i, result := range n.Results {
			n.Results[i] = r.rewriteExpr(result)
		}
	case *DeferStmt:
		n.Call = r.rewriteExpr(n.Call)
	case *GoStmt:
		n.Call = r.rewriteExpr(n.Call)
	case *SendStmt:
		n.Channel = r.rewriteExpr(n.Channel)
		n.Value = r.rewriteExpr(n.Value)
	case *SelectStmt:
		for i := range n.Cases {
			n.Cases[i].Comm = r.rewriteStmt(n.Cases[i].Comm)
			for j, bodyStmt := range n.Cases[i].Body {
				n.Cases[i].Body[j] = r.rewriteStmt(bodyStmt)
			}
		}
	case *RangeStmt:
		n.X = r.rewriteExpr(n.X)
		n.Body = r.rewriteBlock(n.Body)
	case *SwitchStmt:
		n.Init = r.rewriteStmt(n.Init)
		n.Assign = r.rewriteStmt(n.Assign)
		n.Tag = r.rewriteExpr(n.Tag)
		n.Body = r.rewriteBlock(n.Body)
	case *CaseClause:
		for i, expr := range n.List {
			n.List[i] = r.rewriteExpr(expr)
		}
		for i, bodyStmt := range n.Body {
			n.Body[i] = r.rewriteStmt(bodyStmt)
		}
	case *FunctionStmt:
		n.Body = r.rewriteBlock(n.Body)
	case *MultiAssignmentStmt:
		for i, lhs := range n.LHS {
			n.LHS[i] = r.rewriteExpr(lhs)
		}
		for i, value := range n.Values {
			n.Values[i] = r.rewriteExpr(value)
		}
	case *GenDeclStmt:
		for i, value := range n.Values {
			n.Values[i] = r.rewriteExpr(value)
		}
	case *AssignmentStmt:
		n.LHS = r.rewriteExpr(n.LHS)
		n.Value = r.rewriteExpr(n.Value)
	case *TryStmt:
		n.Body = r.rewriteBlock(n.Body)
		if n.Catch != nil {
			n.Catch.Body = r.rewriteBlock(n.Catch.Body)
		}
		n.Finally = r.rewriteBlock(n.Finally)
	case *IncDecStmt:
		n.Operand = r.rewriteExpr(n.Operand)
	case *ExpressionStmt:
		n.X = r.rewriteExpr(n.X)
	case *CallExprStmt:
		return r.rewriteExpr(n).(Stmt)
	}
	return stmt
}

func (r *operatorRewriter) rewriteNode(node Node) Node {
	switch n := node.(type) {
	case nil:
		return nil
	case Expr:
		return r.rewriteExpr(n)
	case Stmt:
		return r.rewriteStmt(n)
	default:
		return n
	}
}

func (r *operatorRewriter) rewriteExpr(expr Expr) Expr {
	switch n := expr.(type) {
	case nil:
		return nil
	case *BinaryExpr:
		n.Left = r.rewriteExpr(n.Left)
		n.Right = r.rewriteExpr(n.Right)
		if n.OperatorResolution != nil {
			r.changed = true
			return operatorCall(n.BaseNode, n.Left, n.OperatorResolution.MethodName, []Expr{n.Right})
		}
	case *UnaryExpr:
		n.Operand = r.rewriteExpr(n.Operand)
		if n.OperatorResolution != nil {
			r.changed = true
			return operatorCall(n.BaseNode, n.Operand, n.OperatorResolution.MethodName, nil)
		}
	case *StarExpr:
		n.X = r.rewriteExpr(n.X)
	case *AddressExpr:
		n.Target = r.rewriteExpr(n.Target)
	case *TypeAssertExpr:
		n.X = r.rewriteExpr(n.X)
	case *ReceiveExpr:
		n.Channel = r.rewriteExpr(n.Channel)
	case *CallExprStmt:
		n.Func = r.rewriteExpr(n.Func)
		for i, arg := range n.Args {
			n.Args[i] = r.rewriteExpr(arg)
		}
	case *MemberExpr:
		n.Object = r.rewriteExpr(n.Object)
	case *CompositeExpr:
		for i := range n.Values {
			n.Values[i].Key = r.rewriteExpr(n.Values[i].Key)
			n.Values[i].Value = r.rewriteExpr(n.Values[i].Value)
		}
	case *IndexExpr:
		n.Object = r.rewriteExpr(n.Object)
		n.Index = r.rewriteExpr(n.Index)
	case *SliceExpr:
		n.X = r.rewriteExpr(n.X)
		n.Low = r.rewriteExpr(n.Low)
		n.High = r.rewriteExpr(n.High)
	case *FuncLitExpr:
		n.Body = r.rewriteBlock(n.Body)
	}
	return expr
}

func operatorCall(base BaseNode, receiver Expr, method Ident, args []Expr) *CallExprStmt {
	callBase := base
	callBase.Meta = "call"
	return &CallExprStmt{
		BaseNode: callBase,
		Func: &MemberExpr{
			BaseNode: BaseNode{
				ID:   base.ID + "_operator",
				Meta: "member",
				Loc:  base.Loc,
			},
			Object:   receiver,
			Property: method,
		},
		Args: args,
	}
}

func residualOperatorOverload(v reflect.Value, seen map[uintptr]bool) Node {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return residualOperatorOverload(v.Elem(), seen)
	case reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		ptr := v.Pointer()
		if seen[ptr] {
			return nil
		}
		seen[ptr] = true
		if v.CanInterface() {
			switch n := v.Interface().(type) {
			case *BinaryExpr:
				if n.OperatorResolution != nil {
					return n
				}
			case *UnaryExpr:
				if n.OperatorResolution != nil {
					return n
				}
			}
		}
		return residualOperatorOverload(v.Elem(), seen)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if field.PkgPath != "" || field.Name == "Scope" {
				continue
			}
			if node := residualOperatorOverload(v.Field(i), seen); node != nil {
				return node
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if node := residualOperatorOverload(v.Index(i), seen); node != nil {
				return node
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if node := residualOperatorOverload(iter.Value(), seen); node != nil {
				return node
			}
		}
	}
	return nil
}
