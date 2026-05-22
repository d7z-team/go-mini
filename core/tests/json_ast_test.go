package engine_test

import (
	"context"
	"strings"
	"testing"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
)

func TestJSONASTComprehensive(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()

	// 构造一个包含多种复杂节点的 JSON AST
	// 涵盖 switch, slice, map 字面量, 逻辑运算等
	jsonPayload := `
{
  "meta": "boot",
  "variables": {
    "output": { "meta": "literal", "type": "String", "value": "" },
    "m": {
        "meta": "composite",
        "type": "Map<String, Int64>",
        "values": []
    },
    "s": {
        "meta": "composite",
        "type": "Array<Int64>",
        "values": [
          { "value": { "meta": "literal", "type": "Int64", "value": "10" } },
          { "value": { "meta": "literal", "type": "Int64", "value": "20" } }
        ]
    },
    "slice_val": {
        "meta": "composite",
        "type": "Array<Int64>",
        "values": []
    }
  },
  "main": [
    {
      "meta": "assignment",
      "kind": "=",
      "lhs": { "meta": "identifier", "name": "m" },
      "value": {
        "meta": "composite",
        "type": "Map<String, Int64>",
        "values": [
          { "key": { "meta": "literal", "type": "String", "value": "a" }, "value": { "meta": "literal", "type": "Int64", "value": "1" } }
        ]
      }
    },
    {
      "meta": "switch",
      "tag": { "meta": "literal", "type": "Int64", "value": "1" },
      "body": {
        "meta": "block",
        "children": [
          {
            "meta": "case",
            "list": [ { "meta": "literal", "type": "Int64", "value": "1" } ],
            "body": [
              {
                "meta": "assignment",
                "kind": "=",
                "lhs": { "meta": "identifier", "name": "output" },
                "value": { "meta": "literal", "type": "String", "value": "matched" }
              }
            ]
          }
        ]
      }
    },
    {
      "meta": "assignment",
      "kind": "=",
      "lhs": { "meta": "identifier", "name": "slice_val" },
      "value": {
        "meta": "slice",
        "x": { "meta": "identifier", "name": "s" },
        "low": { "meta": "literal", "type": "Int64", "value": "0" },
        "high": { "meta": "literal", "type": "Int64", "value": "1" }
      }
    }
  ]
}
`
	decodedNode, err := engine.Unmarshal([]byte(jsonPayload))
	if err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	validatedProgram, _, err := engine.ValidateAndOptimize(decodedNode, func(v *ast.ValidContext) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	compiled, err := testExecutor.CompileProgram(validatedProgram)
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	testProgram, err := testExecutor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("NewRuntimeByCompiled failed: %v", err)
	}

	err = testProgram.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestJSONASTPreservesLSPDeclarationMetadata(t *testing.T) {
	node, err := engine.Unmarshal([]byte(`{
		"meta":"boot",
		"imports":[{"alias":"fmt","path":"fmt"}],
		"import_locs":{"fmt":{"f":"snippet","l":2,"c":8,"el":2,"ec":13}},
		"constants":{"Version":"1"},
		"constant_locs":{"Version":{"f":"snippet","l":3,"c":7,"el":3,"ec":14}},
		"variables":{},
		"types":{"MyInt":"Int64"},
		"type_locs":{"MyInt":{"f":"snippet","l":4,"c":6,"el":4,"ec":11}},
		"structs":{
			"Point":{
				"meta":"struct",
				"name":"Point",
				"fields":{"X":"Int64"},
				"field_names":["X"],
				"field_locs":{"X":{"f":"snippet","l":6,"c":2,"el":6,"ec":3}}
			}
		},
		"interfaces":{
			"Reader":{
				"meta":"interface",
				"name":"Reader",
				"type":"interface{Read() String;}",
				"loc":{"f":"snippet","l":5,"c":6,"el":5,"ec":12}
			}
		},
		"functions":{},
		"main":[]
	}`))
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	prog := node.(*ast.ProgramStmt)
	if prog.ImportLocs["fmt"].C != 8 || prog.ConstantLocs["Version"].C != 7 || prog.TypeLocs["MyInt"].C != 6 {
		t.Fatalf("expected top-level declaration locations, got imports=%+v constants=%+v types=%+v", prog.ImportLocs, prog.ConstantLocs, prog.TypeLocs)
	}
	if prog.Interfaces["Reader"].GetBase().Loc.C != 6 {
		t.Fatalf("expected interface location, got %+v", prog.Interfaces["Reader"].GetBase().Loc)
	}
	if prog.Structs["Point"].FieldLocs["X"].C != 2 {
		t.Fatalf("expected struct field location, got %+v", prog.Structs["Point"].FieldLocs)
	}
}

func TestCompileProgramSupportsValidatedAST(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	artifact, err := testExecutor.CompileProgram(&ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "test"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Main:       []ast.Stmt{},
	})
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	if artifact == nil || artifact.Bytecode == nil || artifact.Bytecode.Executable == nil {
		t.Fatal("expected executable artifact")
	}
}

func TestCompileProgramRejectsNonCanonicalHandwrittenAST(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	_, err := testExecutor.CompileProgram(&ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "test"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions:  map[ast.Ident]*ast.FunctionStmt{},
		Main: []ast.Stmt{&ast.GenDeclStmt{
			BaseNode: ast.BaseNode{ID: "decl", Meta: "decl"},
			Bindings: []ast.VarBinding{{
				Name: "items",
				Kind: "[]Int64",
			}},
		}},
	})
	if err == nil {
		t.Fatal("expected non-canonical handwritten AST type to be rejected")
	}
}

func TestValidatedASTRejectsAssignmentWithoutKind(t *testing.T) {
	program := &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{ID: "test"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"main": {
				BaseNode: ast.BaseNode{Meta: "function"},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block"},
					Children: []ast.Stmt{
						&ast.GenDeclStmt{
							BaseNode: ast.BaseNode{Meta: "decl"},
							Bindings: []ast.VarBinding{{
								Name: "x",
								Kind: "Int64",
							}},
						},
						&ast.AssignmentStmt{
							BaseNode: ast.BaseNode{Meta: "assignment"},
							LHS:      &ast.IdentifierExpr{BaseNode: ast.BaseNode{Meta: "identifier"}, Name: "x"},
							Value:    &ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64"}, Value: "1"},
						},
					},
				},
			},
		},
	}

	validator, _ := ast.NewValidator(program, nil, nil, true)
	err := program.Check(ast.NewSemanticContext(validator))
	if err == nil {
		t.Fatal("expected semantic error for missing assignment kind")
	}
	if !strings.Contains(err.Error(), "assignment missing assignment kind") {
		t.Fatalf("unexpected semantic error: %v", err)
	}
}

func TestJSONDeclRejectsLegacyShape(t *testing.T) {
	_, err := engine.Unmarshal([]byte(`{"meta":"decl","name":"x","kind":"Int64"}`))
	if err == nil {
		t.Fatal("expected legacy decl JSON to fail")
	}
	if !strings.Contains(err.Error(), "bindings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJSONMultiAssignmentRejectsLegacyValueShape(t *testing.T) {
	_, err := engine.Unmarshal([]byte(`{
		"meta":"multi_assignment",
		"kind":":=",
		"lhs":[{"meta":"identifier","name":"a"},{"meta":"identifier","name":"b"}],
		"value":{"meta":"identifier","name":"pair"}
	}`))
	if err == nil {
		t.Fatal("expected legacy multi_assignment JSON to fail")
	}
	if !strings.Contains(err.Error(), "values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJSONInferredDeclRequiresInitializer(t *testing.T) {
	node, err := engine.Unmarshal([]byte(`{
		"meta":"boot",
		"constants":{},
		"variables":{},
		"types":{},
		"structs":{},
		"functions":{},
		"main":[
			{"meta":"decl","bindings":[{"name":"x","inferred":true}]}
		]
	}`))
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	_, _, err = engine.ValidateAndOptimize(node, func(v *ast.ValidContext) error { return nil })
	if err == nil {
		t.Fatal("expected inferred decl without initializer to fail")
	}
	if !strings.Contains(err.Error(), "cannot infer type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExplicitTupleDeclarationAcceptsTupleValue(t *testing.T) {
	testExecutor := engine.NewMiniExecutor()
	program := &ast.ProgramStmt{
		BaseNode:   ast.BaseNode{Meta: "boot"},
		Package:    "main",
		Constants:  map[string]string{},
		Variables:  map[ast.Ident]ast.Expr{},
		Types:      map[ast.Ident]ast.GoMiniType{},
		Structs:    map[ast.Ident]*ast.StructStmt{},
		Interfaces: map[ast.Ident]*ast.InterfaceStmt{},
		Functions: map[ast.Ident]*ast.FunctionStmt{
			"pair": {
				BaseNode: ast.BaseNode{Meta: "function"},
				Name:     "pair",
				FunctionType: ast.FunctionType{
					Return: "tuple(Int64, String)",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block"},
					Inner:    true,
					Children: []ast.Stmt{
						&ast.ReturnStmt{
							BaseNode: ast.BaseNode{Meta: "return"},
							Results: []ast.Expr{
								&ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "Int64"}, Value: "1"},
								&ast.LiteralExpr{BaseNode: ast.BaseNode{Meta: "literal", Type: "String"}, Value: "go"},
							},
						},
					},
				},
			},
			"main": {
				BaseNode: ast.BaseNode{Meta: "function"},
				Name:     "main",
				FunctionType: ast.FunctionType{
					Return: "Void",
				},
				Body: &ast.BlockStmt{
					BaseNode: ast.BaseNode{Meta: "block"},
					Inner:    true,
					Children: []ast.Stmt{
						&ast.GenDeclStmt{
							BaseNode: ast.BaseNode{Meta: "decl"},
							Bindings: []ast.VarBinding{{
								Name: "t",
								Kind: "tuple(Int64, String)",
							}},
							Values: []ast.Expr{
								&ast.CallExprStmt{
									BaseNode: ast.BaseNode{Meta: "call"},
									Func:     &ast.ConstRefExpr{BaseNode: ast.BaseNode{Meta: "const_ref"}, Name: "pair"},
								},
							},
						},
					},
				},
			},
		},
	}

	validatedProgram, _, err := engine.ValidateAndOptimize(program, func(v *ast.ValidContext) error { return nil })
	if err != nil {
		t.Fatalf("validation failed: %v", err)
	}
	compiled, err := testExecutor.CompileProgram(validatedProgram)
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	testProgram, err := testExecutor.NewRuntimeByCompiled(compiled)
	if err != nil {
		t.Fatalf("NewRuntimeByCompiled failed: %v", err)
	}
	if err := testProgram.Execute(context.Background()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}
