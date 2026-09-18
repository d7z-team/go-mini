package ast

import "testing"

func TestCloneProgramDoesNotShareMutableNodes(t *testing.T) {
	program := Program{Files: []File{{Decls: []Decl{{Kind: DeclFunc, Func: FuncDecl{
		Name:   "F",
		Params: []Field{{Name: "values", Type: TypeExpr{Kind: TypeSlice, Elem: &TypeExpr{Kind: TypeName, Name: "int"}}}},
		Body: BlockStmt{Stmts: []Statement{{Kind: StmtReturn, Results: []Expression{{
			Kind:     ExprComposite,
			Elements: []Expression{{Kind: ExprLiteral, Literal: "1"}},
		}}}}},
	}}}}}}

	clone := CloneProgram(program)
	clone.Files[0].Decls[0].Func.Params[0].Type.Elem.Name = "string"
	clone.Files[0].Decls[0].Func.Body.Stmts[0].Results[0].Elements[0].Literal = "2"

	function := program.Files[0].Decls[0].Func
	if function.Params[0].Type.Elem.Name != "int" {
		t.Fatalf("source parameter type changed to %q", function.Params[0].Type.Elem.Name)
	}
	if literal := function.Body.Stmts[0].Results[0].Elements[0].Literal; literal != "1" {
		t.Fatalf("source literal changed to %q", literal)
	}
}
