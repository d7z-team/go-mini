package specialize

import (
	"strings"
	"testing"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	check "github.com/d7z-team/mini-go/compiler/semantic"
)

func TestGenericSpecializationKeyPreservesCompleteTypeIdentity(t *testing.T) {
	intType := ast.TypeExpr{Kind: ast.TypeName, Name: "int"}
	stringType := ast.TypeExpr{Kind: ast.TypeName, Name: "string"}
	functionInt := ast.TypeExpr{Kind: ast.TypeFunc, Params: []ast.Field{{Type: intType}}, Results: []ast.Field{{Type: intType}}}
	functionString := ast.TypeExpr{Kind: ast.TypeFunc, Params: []ast.Field{{Type: stringType}}, Results: []ast.Field{{Type: stringType}}}
	_, intFunctionName := specializationNames("function", "Use", []ast.TypeExpr{functionInt})
	_, stringFunctionName := specializationNames("function", "Use", []ast.TypeExpr{functionString})
	if intFunctionName == stringFunctionName {
		t.Fatalf("different function type arguments produced the same specialization %q", intFunctionName)
	}

	arrayTwo := ast.TypeExpr{Kind: ast.TypeArray, Elem: &intType, Len: &ast.Expression{Kind: ast.ExprLiteral, Literal: "2"}}
	arrayThree := ast.TypeExpr{Kind: ast.TypeArray, Elem: &intType, Len: &ast.Expression{Kind: ast.ExprLiteral, Literal: "3"}}
	_, arrayTwoName := specializationNames("type", "Box", []ast.TypeExpr{arrayTwo})
	_, arrayThreeName := specializationNames("type", "Box", []ast.TypeExpr{arrayThree})
	if arrayTwoName == arrayThreeName {
		t.Fatalf("different array lengths produced the same specialization %q", arrayTwoName)
	}
}

func TestSpecializationRewritesTypeParameterConversion(t *testing.T) {
	parsed := parser.ParseSource("example/generic", "conversion.mgo", `package generic

type Integer interface { ~int | ~int64 }
func Convert[T Integer](value int) T { return T(value) }
var _ = Convert[int64](1)
`)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse source: %#v", parsed.Diagnostics)
	}
	program, diagnostics, err := Apply(check.Check(parsed.Program), nil)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("specialize source: err=%v diagnostics=%#v", err, diagnostics)
	}
	for _, decl := range program.Files[0].Decls {
		if decl.Kind != ast.DeclFunc || !strings.HasPrefix(decl.Func.Name, "generic_Convert_") {
			continue
		}
		result := decl.Func.Body.Stmts[0].Results[0]
		if result.Kind != ast.ExprConvert || result.Type.Kind != ast.TypeName || result.Type.Name != "Int64" {
			t.Fatalf("conversion was not concretized: %+v", result)
		}
		return
	}
	t.Fatalf("generated Convert specialization not found: %#v", program.Files[0].Decls)
}

func TestSpecializationPropagatesGenericResultTypeToLocalInference(t *testing.T) {
	parsed := parser.ParseSource("example/generic", "local.mgo", `package generic

type Named []int
type Seq2[K, V any] func(func(K, V) bool)

func Clone[S ~[]E, E any](value S) S { return value }
func All[S ~[]E, E any](value S) Seq2[int, E] {
	return func(yield func(int, E) bool) {}
}
func Collect[K comparable, V any](seq Seq2[K, V]) map[K]V { return make(map[K]V) }

func Use() {
	values := Clone(Named{1, 2})
	sequence := All(values)
	mapping := Collect(sequence)
	_ = mapping[0] == 0
}
`)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse source: %#v", parsed.Diagnostics)
	}
	program, diagnostics, err := Apply(check.Check(parsed.Program), nil)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("specialize source: err=%v diagnostics=%#v", err, diagnostics)
	}
	if checked := check.Check(program); len(checked.Info.Diagnostics) != 0 {
		t.Fatalf("specialized semantic diagnostics: %#v", checked.Info.Diagnostics)
	}
}

func TestSpecializationInfersRemainingTypeArgumentsFromExplicitNilCall(t *testing.T) {
	parsed := parser.ParseSource("example/generic", "nil.mgo", `package generic

type Named []int
func Clone[S ~[]E, E any](value S) S { return value }
func Use() Named { return Clone[Named](nil) }
`)
	if len(parsed.Diagnostics) != 0 {
		t.Fatalf("parse source: %#v", parsed.Diagnostics)
	}
	program, diagnostics, err := Apply(check.Check(parsed.Program), nil)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("specialize source: err=%v diagnostics=%#v", err, diagnostics)
	}
	if checked := check.Check(program); len(checked.Info.Diagnostics) != 0 {
		t.Fatalf("specialized semantic diagnostics: %#v", checked.Info.Diagnostics)
	}
}
