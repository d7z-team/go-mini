package ast

import (
	"testing"
)

type dummyExpr struct {
	BaseNode
	dummyType OPSType
}

func (d *dummyExpr) GetBase() *BaseNode { return &d.BaseNode }
func (d *dummyExpr) exprNode()          {}
func (d *dummyExpr) Validate(ctx *ValidContext) (Node, bool) {
	d.Type = d.dummyType
	return d, true
}

func TestCallExprArrayDeduction(t *testing.T) {
	program := &ProgramStmt{}
	v, err := NewValidator(program)
	if err != nil {
		t.Fatalf("NewValidator error: %v", err)
	}

	tests := []struct {
		name         string
		funcType     OPSType
		argTypes     []OPSType
		expectedPass bool
		expectedType OPSType
	}{
		{
			name:         "Single element perfect match",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"Array<String>"},
			expectedPass: true,
			expectedType: "Void",
		},
		{
			name:         "Multiple same types deduced to Array",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"String", "String"},
			expectedPass: true,
			expectedType: "Void",
		},
		{
			name:         "Multiple types deduced to Array<Any> passing to Array<Any>",
			funcType:     "function(Array<Any>) Void",
			argTypes:     []OPSType{"String", "Int64"},
			expectedPass: true,
			expectedType: "Void",
		},
		{
			name:         "Multiple types deduced to Array<Any> failing to pass to Array<String>",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"String", "Int64"},
			expectedPass: false,
		},
		{
			name:         "Array mismatch with exact array types",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"Array<Int64>"},
			expectedPass: false,
		},
		{
			name:         "Single arg not implicitly matched",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"Int64"},
			expectedPass: false,
		},
		{
			name:         "Single arg implicitly matched as Array item",
			funcType:     "function(Array<String>) Void",
			argTypes:     []OPSType{"String"},
			expectedPass: true,
			expectedType: "Void",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := make([]Expr, 0, len(tt.argTypes))
			for _, argType := range tt.argTypes {
				args = append(args, &dummyExpr{
					BaseNode:  BaseNode{Type: argType},
					dummyType: argType,
				})
			}

			callExpr := &CallExprStmt{
				Func: &dummyExpr{
					BaseNode:  BaseNode{Type: tt.funcType},
					dummyType: tt.funcType,
				},
				Args: args,
			}

			// Clear errors for new test
			v.root.logs = nil

			node, ok := callExpr.Validate(v)
			if ok != tt.expectedPass {
				t.Errorf("expected pass: %v, got: %v", tt.expectedPass, ok)
				if !tt.expectedPass && len(v.root.logs) > 0 {
					t.Logf("Errors: %v", v.root.logs)
				}
			}
			if tt.expectedPass {
				if node.GetBase().Type != tt.expectedType {
					t.Errorf("expected type: %v, got: %v", tt.expectedType, node.GetBase().Type)
				}
			} else {
				if len(v.root.logs) == 0 {
					t.Errorf("Expected failure but got no errors logged")
				} else {
					t.Logf("Expected failure got logs: %v", v.root.logs)
				}
			}
		})
	}
}
