package semantic

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) validateGenericConversion(expr *ast.Expression, target types.TypeRef) {
	var operand *ast.Expression
	if expr.Kind == ast.ExprConvert {
		operand = expr.Operand
	} else if expr.Kind == ast.ExprCall && len(expr.Args) == 1 {
		operand = &expr.Args[0]
	}
	if operand == nil {
		return
	}
	a.validateConstantTarget(*operand, target, true)
	source := a.info.Exprs[operand.NodeID].Type
	sourceParameter := source.Kind == types.TypeParameter && a.typeParameterInScope(source, expr.NodeID)
	targetParameter := target.Kind == types.TypeParameter && a.typeParameterInScope(target, expr.NodeID)
	if !sourceParameter && !targetParameter {
		return
	}
	if sourceParameter && !targetParameter {
		if constraint, ok := a.info.Relations.View(source).Constraint(); ok && a.info.Relations.Assignable(constraint, target).OK {
			return
		}
	}
	sourceTerms := []types.TypeRef{source}
	if sourceParameter {
		var ok bool
		sourceTerms, ok = a.typeParameterTerms(source)
		if !ok {
			if a.deferTypeParameterCheck(source) {
				return
			}
			a.addDiagnostic("semantic.generic.conversion", "type parameter constraint does not permit conversion", expr.Span)
			return
		}
	}
	targetTerms := []types.TypeRef{target}
	if targetParameter {
		var ok bool
		targetTerms, ok = a.typeParameterTerms(target)
		if !ok {
			if a.deferTypeParameterCheck(target) {
				return
			}
			a.addDiagnostic("semantic.generic.conversion", "type parameter constraint does not permit conversion", expr.Span)
			return
		}
	}
	for _, sourceTerm := range sourceTerms {
		for _, targetTerm := range targetTerms {
			if !a.typesConvertible(sourceTerm, targetTerm) {
				a.addDiagnostic("semantic.generic.conversion", "type parameter constraint does not permit conversion", expr.Span)
				return
			}
		}
	}
}

func (a *analyzer) typesConvertible(source, target types.TypeRef) bool {
	if a.info.Relations.Assignable(source, target).OK || a.info.Relations.UnderlyingIdentical(source, target).OK {
		return true
	}
	sourceView := a.info.Relations.View(source)
	targetView := a.info.Relations.View(target)
	sourceNumeric, sourceIsNumeric := sourceView.NumericInfo()
	_, targetIsNumeric := targetView.NumericInfo()
	if sourceIsNumeric && targetIsNumeric {
		return true
	}
	if numericInteger(sourceNumeric, sourceIsNumeric) && isStringType(targetView) {
		return true
	}
	if isStringType(sourceView) && targetView.Shape() == types.Slice || sourceView.Shape() == types.Slice && isStringType(targetView) {
		elem, ok := targetView.Elem()
		if isStringType(targetView) {
			elem, ok = sourceView.Elem()
		}
		if ok {
			primitive, primitiveOK := a.info.Relations.View(elem).Primitive()
			return primitiveOK && (primitive == types.PrimitiveUint8 || primitive == types.PrimitiveInt32)
		}
	}
	if sourceView.Shape() == types.Slice {
		sourceElem, sourceOK := sourceView.Elem()
		_, targetElem, targetArray := targetView.Array()
		if targetView.Shape() == types.Pointer {
			if elem, ok := targetView.Elem(); ok {
				_, targetElem, targetArray = a.info.Relations.View(elem).Array()
			}
		}
		return sourceOK && targetArray && a.info.Relations.Identical(sourceElem, targetElem).OK
	}
	sourceDirection, sourceElem, sourceChannel := sourceView.Waitable()
	targetDirection, targetElem, targetChannel := targetView.Waitable()
	return sourceChannel && targetChannel && sourceDirection == types.ChannelBoth && targetDirection != types.ChannelInvalid &&
		a.info.Relations.Identical(sourceElem, targetElem).OK
}
