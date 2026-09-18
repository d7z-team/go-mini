package semantic

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) typeParameterIndexTypes(ref types.TypeRef) (types.TypeRef, types.TypeRef, bool) {
	terms, ok := a.typeParameterTerms(ref)
	if !ok {
		return types.TypeRef{}, types.TypeRef{}, false
	}
	var key, elem types.TypeRef
	for _, term := range terms {
		termKey, termElem, valid := a.indexTypes(term)
		if !valid {
			return types.TypeRef{}, types.TypeRef{}, false
		}
		if !key.Valid() {
			key, elem = termKey, termElem
			continue
		}
		if !a.info.Relations.Identical(key, termKey).OK || !a.info.Relations.Identical(elem, termElem).OK {
			return types.TypeRef{}, types.TypeRef{}, false
		}
	}
	return key, elem, key.Valid() && elem.Valid()
}

func (a *analyzer) indexTypes(ref types.TypeRef) (types.TypeRef, types.TypeRef, bool) {
	view := a.info.Relations.View(ref)
	if _, elem, ok := view.Array(); ok {
		return types.Builtin(types.PrimitiveInt), elem, true
	}
	if elem, ok := view.Elem(); ok && view.Shape() == types.Slice {
		return types.Builtin(types.PrimitiveInt), elem, true
	}
	if key, elem, ok := view.Map(); ok {
		return key, elem, true
	}
	if primitive, ok := view.Primitive(); ok && primitive == types.PrimitiveString {
		return types.Builtin(types.PrimitiveInt), types.Builtin(types.PrimitiveUint8), true
	}
	if elem, ok := view.Elem(); ok && view.Shape() == types.Pointer {
		if _, arrayElem, array := a.info.Relations.View(elem).Array(); array {
			return types.Builtin(types.PrimitiveInt), arrayElem, true
		}
	}
	return types.TypeRef{}, types.TypeRef{}, false
}

func (a *analyzer) typeParameterSliceable(ref types.TypeRef) bool {
	terms, ok := a.typeParameterTerms(ref)
	if !ok {
		return false
	}
	var elem types.TypeRef
	for _, term := range terms {
		_, current, valid := a.indexTypes(term)
		shape := a.info.Relations.View(term).Shape()
		if !valid || shape == types.Map {
			return false
		}
		if elem.Valid() && !a.info.Relations.Identical(elem, current).OK {
			return false
		}
		elem = current
	}
	return elem.Valid()
}

func (a *analyzer) typeParameterPointerElem(ref types.TypeRef) (types.TypeRef, bool) {
	terms, ok := a.typeParameterTerms(ref)
	if !ok {
		return types.TypeRef{}, false
	}
	var elem types.TypeRef
	for _, term := range terms {
		current, valid := a.info.Relations.View(term).Elem()
		if !valid || a.info.Relations.View(term).Shape() != types.Pointer ||
			elem.Valid() && !a.info.Relations.Identical(elem, current).OK {
			return types.TypeRef{}, false
		}
		elem = current
	}
	return elem, elem.Valid()
}

func (a *analyzer) typeParameterReceiveElem(ref types.TypeRef) (types.TypeRef, bool) {
	terms, ok := a.typeParameterTerms(ref)
	if !ok {
		return types.TypeRef{}, false
	}
	var elem types.TypeRef
	for _, term := range terms {
		direction, current, valid := a.info.Relations.View(term).Waitable()
		if !valid || direction == types.ChannelSend || elem.Valid() && !a.info.Relations.Identical(elem, current).OK {
			return types.TypeRef{}, false
		}
		elem = current
	}
	return elem, elem.Valid()
}

func (a *analyzer) validateTypeParameterSend(stmt *ast.Statement) {
	if len(stmt.Left) != 1 || len(stmt.Right) != 1 {
		return
	}
	channel := a.info.Exprs[stmt.Left[0].NodeID].Type
	if channel.Kind != types.TypeParameter || !a.typeParameterInScope(channel, stmt.Left[0].NodeID) {
		return
	}
	terms, ok := a.typeParameterTerms(channel)
	if !ok {
		if !a.deferTypeParameterCheck(channel) {
			a.addDiagnostic("semantic.generic.send_constraint", "type parameter constraint does not permit send", stmt.Span)
		}
		return
	}
	var elem types.TypeRef
	for _, term := range terms {
		direction, current, valid := a.info.Relations.View(term).Waitable()
		if !valid || direction == types.ChannelReceive || elem.Valid() && !a.info.Relations.Identical(elem, current).OK {
			a.addDiagnostic("semantic.generic.send_constraint", "type parameter constraint does not permit send", stmt.Span)
			return
		}
		elem = current
	}
	a.validateAssignments(stmt.Right, []types.TypeRef{elem})
}

func (a *analyzer) validateTypeParameterBuiltin(expr *ast.Expression, name string) {
	name = strings.TrimSpace(name)
	for i := range expr.Args {
		if !builtinTypeParameterPosition(name, i) {
			continue
		}
		ref := a.builtinArgumentType(expr.Args[i])
		if ref.Kind != types.TypeParameter || !a.typeParameterInScope(ref, expr.NodeID) {
			continue
		}
		terms, ok := a.typeParameterTerms(ref)
		if !ok && a.deferTypeParameterCheck(ref) {
			continue
		}
		if !ok || !a.typeParameterBuiltinAllowed(name, i, terms) {
			a.addDiagnostic("semantic.generic.builtin_constraint", "type parameter constraint does not permit "+name, expr.Args[i].Span)
			return
		}
	}
}

func builtinTypeParameterPosition(name string, index int) bool {
	switch name {
	case "len", "cap", "append", "clear", "close", "delete", "make":
		return index == 0
	case "copy", "complex", "min", "max":
		return true
	default:
		return false
	}
}

func (a *analyzer) typeParameterBuiltinAllowed(name string, index int, terms []types.TypeRef) bool {
	for _, term := range terms {
		view := a.info.Relations.View(term)
		shape := view.Shape()
		allowed := true
		switch name {
		case "len":
			_, primitive := view.Primitive()
			_, _, pointerArray := a.indexTypes(term)
			allowed = shape == types.Array || shape == types.Slice || shape == types.Map || shape == types.Waitable ||
				shape == types.Pointer && pointerArray || primitive && isStringType(view)
		case "cap":
			_, _, pointerArray := a.indexTypes(term)
			allowed = shape == types.Array || shape == types.Slice || shape == types.Waitable || shape == types.Pointer && pointerArray
		case "append":
			allowed = shape == types.Slice
		case "clear":
			allowed = shape == types.Slice || shape == types.Map
		case "close":
			direction, _, ok := view.Waitable()
			allowed = ok && direction != types.ChannelReceive
		case "delete":
			allowed = shape == types.Map
		case "copy":
			allowed = shape == types.Slice || index == 1 && isStringType(view)
		case "complex":
			allowed = false
		case "min", "max":
			allowed = view.Ordered()
		case "make":
			allowed = shape == types.Slice || shape == types.Map || shape == types.Waitable
		}
		if !allowed {
			return false
		}
	}
	return len(terms) != 0
}
