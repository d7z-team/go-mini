package tests

import (
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func compileASTRuntimeProgram(t *testing.T, program *ast.ProgramStmt) *engine.ExecutableProgram {
	t.Helper()

	executor := engine.MustNewMiniExecutor()
	compiled, err := executor.CompileAST(program)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := executor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func tryProgram(vars map[ast.Ident]ast.Expr, functions map[ast.Ident]*ast.FunctionStmt, main ...ast.Stmt) *ast.ProgramStmt {
	if vars == nil {
		vars = map[ast.Ident]ast.Expr{}
	}
	if functions == nil {
		functions = map[ast.Ident]*ast.FunctionStmt{}
	}
	return &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "boot", Meta: "boot", Type: ast.TypeVoid},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  vars,
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  functions,
		Main:       main,
	}
}

func block(stmts ...ast.Stmt) *ast.BlockStmt {
	return ast.NewBlock(nil, stmts...)
}

func fn(name ast.Ident, params []ast.FunctionParam, stmts ...ast.Stmt) *ast.FunctionStmt {
	return &ast.FunctionStmt{
		BaseNode: ast.BaseNode{Meta: "function", Type: ast.TypeVoid},
		Name:     name,
		FunctionType: ast.FunctionType{
			Params: params,
			Return: ast.TypeVoid,
		},
		Body: block(stmts...),
	}
}

func ident(name ast.Ident) *ast.IdentifierExpr {
	return &ast.IdentifierExpr{BaseNode: ast.BaseNode{Meta: "identifier"}, Name: name}
}

func constRef(name ast.Ident) *ast.ConstRefExpr {
	return &ast.ConstRefExpr{BaseNode: ast.BaseNode{Meta: "const_ref"}, Name: name}
}

func stringLit(value string) *ast.LiteralExpr {
	return &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: ast.TypeString}, Value: value}
}

func call(name ast.Ident, args ...ast.Expr) *ast.CallExprStmt {
	return &ast.CallExprStmt{BaseNode: ast.BaseNode{Meta: "call"}, Func: constRef(name), Args: args}
}

func binary(op ast.Ident, left, right ast.Expr) *ast.BinaryExpr {
	return &ast.BinaryExpr{BaseNode: ast.BaseNode{Meta: "binary"}, Operator: op, Left: left, Right: right}
}

func assign(name ast.Ident, value ast.Expr) *ast.AssignmentStmt {
	return &ast.AssignmentStmt{
		BaseNode: ast.BaseNode{Meta: "assignment", Type: ast.TypeVoid},
		Kind:     ast.AssignSet,
		LHS:      ident(name),
		Value:    value,
	}
}

func ifStmt(cond ast.Expr, body ...ast.Stmt) *ast.IfStmt {
	return &ast.IfStmt{BaseNode: ast.BaseNode{Meta: "if", Type: ast.TypeVoid}, Cond: cond, Body: block(body...)}
}

func tryStmt(body *ast.BlockStmt, catch *ast.CatchClause, finally *ast.BlockStmt) *ast.TryStmt {
	return &ast.TryStmt{BaseNode: ast.BaseNode{Meta: "try", Type: ast.TypeVoid}, Body: body, Catch: catch, Finally: finally}
}

func catchStmt(name ast.Ident, body ...ast.Stmt) *ast.CatchClause {
	return &ast.CatchClause{BaseNode: ast.BaseNode{Meta: "catch", Type: ast.TypeVoid}, VarName: name, Body: block(body...)}
}

func deferCall(name ast.Ident, args ...ast.Expr) *ast.DeferStmt {
	return &ast.DeferStmt{BaseNode: ast.BaseNode{Meta: "defer", Type: ast.TypeVoid}, Call: call(name, args...)}
}

func appendTextFunction() *ast.FunctionStmt {
	return fn("appendText", []ast.FunctionParam{{Name: "s", Type: ast.TypeString}},
		assign("res", binary("Plus", ident("res"), ident("s"))),
	)
}

func unexpectedResultGuard(want string) ast.Stmt {
	return ifStmt(
		binary("Neq", ident("res"), stringLit(want)),
		call("panic", binary("Plus", stringLit("unexpected res: "), ident("res"))),
	)
}
