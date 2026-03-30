package ast

import (
	"fmt"
	"testing"
)

func TestFindCompletionsAt_ConstantsAndGlobals(t *testing.T) {
	// 模拟一个导入的子模块
	subRoot := &ValidRoot{
		Global: &ValidStruct{
			Fields:  make(map[Ident]GoMiniType),
			Methods: make(map[Ident]CallFunctionType),
		},
		program: &ProgramStmt{
			BaseNode: BaseNode{ID: "sub", Meta: "boot", Type: "Void", Loc: &Position{L: 1, C: 1}},
			Constants: map[string]string{
				"SubConst": "999",
			},
			Variables: map[Ident]Expr{
				"SubVar": &LiteralExpr{BaseNode: BaseNode{Type: "Int64"}, Value: "888"},
			},
		},
		vars: make(map[Ident]GoMiniType),
	}
	subRoot.vars["SubVar"] = "Int64"

	// 模拟一个 ProgramStmt
	prog := &ProgramStmt{
		BaseNode: BaseNode{ID: "root", Meta: "boot", Type: "Void", Loc: &Position{L: 1, C: 1}},
		Imports: []ImportSpec{
			{Alias: "pkg", Path: "pkg"},
		},
		Constants: map[string]string{
			"MyConst": "123",
		},
		Variables: map[Ident]Expr{
			"MyGlobalVar": &LiteralExpr{BaseNode: BaseNode{Type: "Int64"}, Value: "456"},
		},
		Functions: map[Ident]*FunctionStmt{
			"main": {
				BaseNode: BaseNode{ID: "main_func", Meta: "function", Loc: &Position{L: 1, C: 1, EL: 10, EC: 1}},
				Name:     "main",
				Body: &BlockStmt{
					BaseNode: BaseNode{ID: "main_body", Meta: "block", Loc: &Position{L: 2, C: 1, EL: 9, EC: 1}},
					Children: []Stmt{
						&ExpressionStmt{
							BaseNode: BaseNode{ID: "expr", Meta: "expr_stmt", Loc: &Position{L: 3, C: 1, EL: 3, EC: 10}},
							X: &IdentifierExpr{
								BaseNode: BaseNode{ID: "ident", Meta: "identifier", Loc: &Position{L: 3, C: 1, EL: 3, EC: 5}},
								Name:     "My", // 用于测试 FindCompletionsAt
							},
						},
						&ExpressionStmt{
							BaseNode: BaseNode{ID: "expr2", Meta: "expr_stmt", Loc: &Position{L: 4, C: 1, EL: 4, EC: 10}},
							X: &MemberExpr{
								BaseNode: BaseNode{ID: "sel", Meta: "member", Loc: &Position{L: 4, C: 1, EL: 4, EC: 8}},
								Object: &IdentifierExpr{
									BaseNode: BaseNode{ID: "pkg_ident", Meta: "identifier", Loc: &Position{L: 4, C: 1, EL: 4, EC: 4}},
									Name:     "pkg",
								},
								Property: "Sub", // 用于测试成员补全
							},
						},
					},
				},
			},
		},
	}

	// 初始化 Validator 以构建 Scope
	v, err := NewValidator(prog, nil, nil, true)
	if err != nil {
		t.Fatalf("NewValidator failed: %v", err)
	}
	
	v.root.ImportedRoots["pkg"] = subRoot
	v.root.Imports["pkg"] = "pkg"

	// 注入 pkg 到 vars 以便 MemberExpr 识别 (模拟已加载包)
	v.root.vars["pkg"] = "Package"

	semCtx := NewSemanticContext(v)
	// 注意：由于是手动构造的部分 AST，Check 可能会报错，只要构建了 Scope 即可
	_ = prog.Check(semCtx)

	pMap := BuildParentMap(prog)

	// 1. 测试 Completion (当前文件)
	completions := FindCompletionsAt(prog, 3, 1)

	foundConst := false
	foundGlobal := false
	for _, item := range completions {
		if item.Label == "MyConst" {
			foundConst = true
			if item.Kind != "constant" {
				t.Errorf("Expected kind 'constant' for MyConst, got %s", item.Kind)
			}
		}
		if item.Label == "MyGlobalVar" {
			foundGlobal = true
		}
	}

	if !foundGlobal {
		t.Errorf("Global variables NOT found in completions")
	}
	if !foundConst {
		t.Errorf("Constants NOT found in completions")
	}

	// 2. 测试 Member Completion (跨包)
	// 光标在 "pkg." (L:4, C:5) 之后
	memberCompletions := FindCompletionsAt(prog, 4, 5) 
	foundSubConst := false
	foundSubVar := false
	for _, item := range memberCompletions {
		if item.Label == "SubConst" {
			foundSubConst = true
			if item.Kind != "constant" {
				t.Errorf("Expected kind 'constant' for SubConst, got %s", item.Kind)
			}
		}
		if item.Label == "SubVar" {
			foundSubVar = true
		}
	}
	if !foundSubVar {
		t.Errorf("SubVar NOT found in member completions")
	}
	if !foundSubConst {
		t.Errorf("SubConst NOT found in member completions")
	}

	// 3. 测试 Definition / Hover
	identNode := FindNodeAt(prog, 3, 1)
	if identNode != nil {
		idExpr := identNode.(*IdentifierExpr)
		
		// 测试常量
		idExpr.Name = "MyConst"
		defConst := FindDefinition(prog, idExpr, pMap)
		if defConst != nil {
			fmt.Printf("Definition for 'MyConst' found: %T\n", defConst)
			if lit, ok := defConst.(*LiteralExpr); ok {
				if lit.Value != "123" {
					t.Errorf("Expected constant value 123, got %s", lit.Value)
				}
			} else {
				t.Errorf("Expected *LiteralExpr for constant definition, got %T", defConst)
			}
		} else {
			t.Errorf("Definition for 'MyConst' NOT found")
		}

		// 测试 Hover
		info := FindHoverInfo(prog, identNode, pMap)
		if info != nil {
			if info.Type != "Constant" {
				t.Errorf("Expected hover type 'Constant', got %s", info.Type)
			}
		} else {
			t.Errorf("Hover info for 'MyConst' NOT found")
		}
	}
}
