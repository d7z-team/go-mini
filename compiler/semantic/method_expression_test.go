package semantic

import (
	"testing"

	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/types"
)

func TestMethodExpressionReceiverSets(t *testing.T) {
	for _, test := range []struct {
		expression string
		valid      bool
	}{
		{"T.Value", true},
		{"(*T).Value", true},
		{"(*T).Change", true},
		{"T.Change", false},
		{"(*Box).Change", true},
		{"Box.Change", false},
		{"Box.Value", true},
	} {
		t.Run(test.expression, func(t *testing.T) {
			parsed := parser.ParseSource("example", "method.mgo", `package example
type T struct { N int }
func (t T) Value() int { return t.N }
func (t *T) Change() {}
type Box struct { T }
var F = `+test.expression)
			checked := WithOptions(parsed.Program, AnalyzeOptions{})
			if test.valid {
				if len(checked.Info.Diagnostics) != 0 {
					t.Fatal(checked.Info.Diagnostics)
				}
				for _, selection := range checked.Info.Selections {
					if selection.Kind == SelectionMethodExpression && !types.NewRelations(checked.Info.TypeTable).Identical(selection.Signature.Params[0].Type, selection.Receiver).OK {
						t.Fatal("method expression parameter differs from selected receiver")
					}
				}
			} else {
				for _, diag := range checked.Info.Diagnostics {
					if diag.Code == "semantic.method_expression.method" {
						return
					}
				}
				t.Fatalf("expected method set diagnostic: %v", checked.Info.Diagnostics)
			}
		})
	}
}
