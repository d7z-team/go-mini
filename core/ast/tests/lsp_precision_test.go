package ast_test

import (
	"strings"
	"testing"

	"gopkg.d7z.net/go-mini/core/ast"
)

var testExternalSpecs = map[ast.Ident]ast.GoMiniType{
	"sink": ast.CreateFunctionType([]ast.FunctionParam{{Type: ast.TypeAny}}, ast.TypeVoid, true),
}

func newTestSemanticContext(t *testing.T) *ast.SemanticContext {
	t.Helper()
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
	}
	validator, err := ast.NewValidator(prog, testExternalSpecs, nil, true)
	if err != nil {
		t.Fatalf("new validator failed: %v", err)
	}
	return ast.NewSemanticContext(validator)
}

func TestBadExprCallReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 6}},
							Func: &ast.BadExpr{
								BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 4}},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise inference error, got: %v", err)
	}
}

func TestBadExprMemberReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 10}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 6}},
								Name:     "sink",
							},
							Args: []ast.Expr{
								&ast.MemberExpr{
									BaseNode: ast.BaseNode{Meta: "member", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 10}},
									Object: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 8}},
									},
									Property: "X",
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "member access object has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise member inference error, got: %v", err)
	}
}

func TestBadExprIndexReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 1, C: 1, EL: 4, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 1, C: 12, EL: 4, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 12}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 2, C: 2, EL: 2, EC: 6}},
								Name:     "sink",
							},
							Args: []ast.Expr{
								&ast.IndexExpr{
									BaseNode: ast.BaseNode{Meta: "index", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 12}},
									Object: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 2, C: 8, EL: 2, EC: 8}},
									},
									Index: &ast.LiteralExpr{
										BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 2, C: 10, EL: 2, EC: 10}},
										Value:    "0",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "index object has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise index inference error, got: %v", err)
	}
}

func TestBadExprIndexOperandReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:   "main",
		Constants: map[string]string{},
		Variables: map[ast.Ident]ast.Expr{
			"arr": &ast.CompositeExpr{
				BaseNode: ast.BaseNode{Meta: "composite", Loc: &ast.Position{L: 1, C: 11, EL: 1, EC: 19}},
				Kind:     "Array<Int64>",
				Values: []ast.CompositeElement{
					{Value: &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 1, C: 17, EL: 1, EC: 17}}, Value: "1"}},
				},
			},
		},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 14}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 6}},
								Name:     "sink",
							},
							Args: []ast.Expr{
								&ast.IndexExpr{
									BaseNode: ast.BaseNode{Meta: "index", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 14}},
									Object: &ast.IdentifierExpr{
										BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 10}},
										Name:     "arr",
									},
									Index: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 12, EL: 3, EC: 12}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "previous bad_expr has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise index operand error, got: %v", err)
	}
}

func TestBadExprSliceReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:   "main",
		Constants: map[string]string{},
		Variables: map[ast.Ident]ast.Expr{
			"arr": &ast.CompositeExpr{
				BaseNode: ast.BaseNode{Meta: "composite", Loc: &ast.Position{L: 1, C: 11, EL: 1, EC: 21}},
				Kind:     "Array<Int64>",
				Values: []ast.CompositeElement{
					{Value: &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 1, C: 17, EL: 1, EC: 17}}, Value: "1"}},
					{Value: &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 1, C: 20, EL: 1, EC: 20}}, Value: "2"}},
				},
			},
		},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 16}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 6}},
								Name:     "sink",
							},
							Args: []ast.Expr{
								&ast.SliceExpr{
									BaseNode: ast.BaseNode{Meta: "slice", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 16}},
									X: &ast.IdentifierExpr{
										BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 10}},
										Name:     "arr",
									},
									Low: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 12, EL: 3, EC: 12}},
									},
									High: &ast.LiteralExpr{
										BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 3, C: 14, EL: 3, EC: 14}},
										Value:    "1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "previous bad_expr has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise slice inference error, got: %v", err)
	}
}

func TestBadExprStringSliceReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:   "main",
		Constants: map[string]string{},
		Variables: map[ast.Ident]ast.Expr{
			"s": &ast.LiteralExpr{
				BaseNode: ast.BaseNode{Meta: "literal", Type: "String", Loc: &ast.Position{L: 1, C: 9, EL: 1, EC: 15}},
				Value:    "hello",
			},
		},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.CallExprStmt{
							BaseNode: ast.BaseNode{Meta: "call", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 14}},
							Func: &ast.ConstRefExpr{
								BaseNode: ast.BaseNode{Meta: "const_ref", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 6}},
								Name:     "sink",
							},
							Args: []ast.Expr{
								&ast.SliceExpr{
									BaseNode: ast.BaseNode{Meta: "slice", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 14}},
									X: &ast.IdentifierExpr{
										BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 8, EL: 3, EC: 8}},
										Name:     "s",
									},
									Low: &ast.BadExpr{
										BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 10, EL: 3, EC: 10}},
									},
									High: &ast.LiteralExpr{
										BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 3, C: 12, EL: 3, EC: 12}},
										Value:    "1",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "previous bad_expr has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise string-slice inference error, got: %v", err)
	}
}

func TestInvalidCompositeMemberReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.AssignmentStmt{
							BaseNode: ast.BaseNode{Meta: "assign", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 14}},
							Kind:     ast.AssignSet,
							LHS: &ast.IdentifierExpr{
								BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 2}},
								Name:     "x",
							},
							Value: &ast.MemberExpr{
								BaseNode: ast.BaseNode{Meta: "member", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 14}},
								Object: &ast.CompositeExpr{
									BaseNode: ast.BaseNode{Meta: "composite", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 12}},
									Values: []ast.CompositeElement{
										{
											Key:   &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "String", Loc: &ast.Position{L: 3, C: 7, EL: 3, EC: 7}}, Value: "a"},
											Value: &ast.BadExpr{BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 10, EL: 3, EC: 10}}},
										},
									},
								},
								Property: "a",
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "member access object has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise invalid composite member error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "composite literal element 1 value has errors") {
		t.Fatalf("expected composite source detail, got: %v", err)
	}
}

func TestInvalidCompositeIndexReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.AssignmentStmt{
							BaseNode: ast.BaseNode{Meta: "assign", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 14}},
							Kind:     ast.AssignSet,
							LHS: &ast.IdentifierExpr{
								BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 2}},
								Name:     "x",
							},
							Value: &ast.IndexExpr{
								BaseNode: ast.BaseNode{Meta: "index", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 14}},
								Object: &ast.CompositeExpr{
									BaseNode: ast.BaseNode{Meta: "composite", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 10}},
									Values: []ast.CompositeElement{
										{Value: &ast.BadExpr{BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 7, EL: 3, EC: 7}}}},
									},
								},
								Index: &ast.LiteralExpr{
									BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 3, C: 10, EL: 3, EC: 10}},
									Value:    "0",
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "index object has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise invalid composite index error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "composite literal element 1 value has errors") {
		t.Fatalf("expected composite source detail, got: %v", err)
	}
}

func TestInvalidCompositeSliceReportsPreciseInferenceError(t *testing.T) {
	prog := &ast.ProgramStmt{
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function", Loc: &ast.Position{L: 2, C: 1, EL: 5, EC: 1}},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block", Loc: &ast.Position{L: 2, C: 12, EL: 5, EC: 1}},
					Children: []ast.Stmt{
						&ast.AssignmentStmt{
							BaseNode: ast.BaseNode{Meta: "assign", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 16}},
							Kind:     ast.AssignSet,
							LHS: &ast.IdentifierExpr{
								BaseNode: ast.BaseNode{Meta: "ident", Loc: &ast.Position{L: 3, C: 2, EL: 3, EC: 2}},
								Name:     "x",
							},
							Value: &ast.SliceExpr{
								BaseNode: ast.BaseNode{Meta: "slice", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 16}},
								X: &ast.CompositeExpr{
									BaseNode: ast.BaseNode{Meta: "composite", Loc: &ast.Position{L: 3, C: 6, EL: 3, EC: 10}},
									Values: []ast.CompositeElement{
										{Value: &ast.BadExpr{BaseNode: ast.BaseNode{Meta: "bad_expr", Loc: &ast.Position{L: 3, C: 7, EL: 3, EC: 7}}}},
									},
								},
								Low: &ast.LiteralExpr{
									BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 3, C: 10, EL: 3, EC: 10}},
									Value:    "0",
								},
								High: &ast.LiteralExpr{
									BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64", Loc: &ast.Position{L: 3, C: 12, EL: 3, EC: 12}},
									Value:    "1",
								},
							},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(prog, testExternalSpecs, nil, true)
	err := prog.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error")
	}
	if !strings.Contains(err.Error(), "slice object has errors") && !strings.Contains(err.Error(), "cannot infer precisely") {
		t.Fatalf("expected precise invalid composite slice error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "composite literal element 1 value has errors") {
		t.Fatalf("expected composite source detail, got: %v", err)
	}
}

func TestStarExprRejectsOpaqueHostRefType(t *testing.T) {
	ctx := newTestSemanticContext(t)
	ctx.AddVariable("h", "HostRef<demo.Handle>")

	expr := &ast.StarExpr{
		BaseNode: ast.BaseNode{Meta: "star"},
		X: &ast.IdentifierExpr{
			BaseNode: ast.BaseNode{Meta: "ident"},
			Name:     "h",
		},
	}

	err := expr.Check(ctx)
	if err == nil {
		t.Fatal("expected dereference of HostRef to fail")
	}
	if !strings.Contains(err.Error(), "cannot dereference opaque host reference") {
		t.Fatalf("unexpected dereference error: %v", err)
	}
}

func TestConcreteArrayIndexRejectsAnyIndexType(t *testing.T) {
	ctx := newTestSemanticContext(t)
	ctx.AddVariable("arr", "Array<Int64>")
	ctx.AddVariable("idx", "Any")

	expr := &ast.IndexExpr{
		BaseNode: ast.BaseNode{Meta: "index"},
		Object: &ast.IdentifierExpr{
			BaseNode: ast.BaseNode{Meta: "ident"},
			Name:     "arr",
		},
		Index: &ast.IdentifierExpr{
			BaseNode: ast.BaseNode{Meta: "ident"},
			Name:     "idx",
		},
	}

	err := expr.Check(ctx)
	if err == nil {
		t.Fatal("expected concrete array Any-index to fail")
	}
	if !strings.Contains(err.Error(), "array index must be Int64") {
		t.Fatalf("unexpected array index error: %v", err)
	}
}

func TestConcreteMapIndexRejectsAnyKeyType(t *testing.T) {
	ctx := newTestSemanticContext(t)
	ctx.AddVariable("m", "Map<String, Int64>")
	ctx.AddVariable("k", "Any")

	expr := &ast.IndexExpr{
		BaseNode: ast.BaseNode{Meta: "index"},
		Object: &ast.IdentifierExpr{
			BaseNode: ast.BaseNode{Meta: "ident"},
			Name:     "m",
		},
		Index: &ast.IdentifierExpr{
			BaseNode: ast.BaseNode{Meta: "ident"},
			Name:     "k",
		},
	}

	err := expr.Check(ctx)
	if err == nil {
		t.Fatal("expected concrete map Any-key to fail")
	}
	if !strings.Contains(err.Error(), "map key type mismatch") {
		t.Fatalf("unexpected map index error: %v", err)
	}
}

func TestCompositeExprSkipsInvalidArrayChildren(t *testing.T) {
	ctx := newTestSemanticContext(t)
	expr := &ast.CompositeExpr{
		BaseNode: ast.BaseNode{Meta: "composite"},
		Values: []ast.CompositeElement{
			{Value: &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64"}, Value: "1"}},
			{Value: &ast.BadExpr{BaseNode: ast.BaseNode{Meta: "bad_expr"}}},
		},
	}

	if err := expr.Check(ctx); err != nil {
		t.Fatalf("composite check failed: %v", err)
	}
	if expr.Type != "Array<Int64>" {
		t.Fatalf("expected Array<Int64>, got %s", expr.Type)
	}
	if !expr.IsInvalid() || !strings.Contains(expr.InvalidCause, "composite literal element 2 value has errors") {
		t.Fatalf("expected precise invalid cause, got %q", expr.InvalidCause)
	}
}

func TestCompositeExprIgnoresInvalidChildrenForMapInference(t *testing.T) {
	ctx := newTestSemanticContext(t)
	expr := &ast.CompositeExpr{
		BaseNode: ast.BaseNode{Meta: "composite"},
		Values: []ast.CompositeElement{
			{
				Key:   &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "String"}, Value: "a"},
				Value: &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64"}, Value: "1"},
			},
			{
				Key:   &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "String"}, Value: "b"},
				Value: &ast.BadExpr{BaseNode: ast.BaseNode{Meta: "bad_expr"}},
			},
		},
	}

	if err := expr.Check(ctx); err != nil {
		t.Fatalf("composite check failed: %v", err)
	}
	if expr.Type != "Map<String, Int64>" {
		t.Fatalf("expected Map<String, Int64>, got %s", expr.Type)
	}
	if !expr.IsInvalid() || !strings.Contains(expr.InvalidCause, "composite literal element 2 value has errors") {
		t.Fatalf("expected precise invalid cause, got %q", expr.InvalidCause)
	}
}
