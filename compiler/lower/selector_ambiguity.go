package lower

import (
	"fmt"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
)

type selectorCandidate struct {
	depth int
	kind  string
	owner string
}

func (l *lowerer) rejectAmbiguousSelector(expr ast.Expression, scope *funcScope, code string) bool {
	if expr.Kind != ast.ExprSelector || expr.Operand == nil || strings.TrimSpace(expr.Field) == "" {
		return false
	}
	if _, ok := l.selectorModulePath(expr, scope); ok {
		return false
	}
	if _, ok := l.methodExpressionReceiverType(*expr.Operand, scope); ok {
		return false
	}
	receiverType := l.expressionType(*expr.Operand, scope)
	if !l.selectorAmbiguous(receiverType, expr.Field) {
		return false
	}
	l.add(code, fmt.Sprintf("ambiguous selector %q", strings.TrimSpace(expr.Field)), expr.Span)
	return true
}

func (l *lowerer) selectorAmbiguous(receiverType, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	base := l.selectorBaseType(receiverType)
	if base == "" {
		return false
	}
	if l.directSelectorCandidate(base, name) {
		return false
	}
	candidates := l.promotedSelectorCandidates(base, name)
	if len(candidates) < 2 {
		return false
	}
	minDepth := candidates[0].depth
	count := 0
	for _, candidate := range candidates {
		if candidate.depth < minDepth {
			minDepth = candidate.depth
			count = 1
			continue
		}
		if candidate.depth == minDepth {
			count++
		}
	}
	return count > 1
}

func (l *lowerer) selectorBaseType(typ string) string {
	typ = l.resolveType(strings.TrimSpace(typ))
	if typ == "" {
		return ""
	}
	if elem, ok := l.pointerElementType(typ); ok {
		typ = l.resolveType(elem)
	}
	return strings.TrimSpace(typ)
}

func (l *lowerer) directSelectorCandidate(base, name string) bool {
	if l.directStructField(base, name) {
		return true
	}
	if l.directMethod(base, name) {
		return true
	}
	return false
}

func (l *lowerer) directStructField(base, name string) bool {
	decl, ok := l.typeDeclFor(base)
	if !ok || decl.Kind != ast.TypeStruct {
		return false
	}
	for _, field := range decl.Fields {
		if l.structFieldName(field) == name {
			return true
		}
	}
	return false
}

func (l *lowerer) directMethod(receiverType, name string) bool {
	receiverType = strings.TrimSpace(receiverType)
	name = strings.TrimSpace(name)
	if receiverType == "" || name == "" {
		return false
	}
	if _, ok := l.methods[receiverType+"."+name]; ok {
		return true
	}
	if _, ok := l.methods["Ptr<"+receiverType+">."+name]; ok {
		return true
	}
	return false
}

func (l *lowerer) promotedSelectorCandidates(base, name string) []selectorCandidate {
	var out []selectorCandidate
	l.collectPromotedSelectorCandidates(base, name, 1, map[string]struct{}{}, &out)
	return out
}

func (l *lowerer) collectPromotedSelectorCandidates(base, name string, depth int, seen map[string]struct{}, out *[]selectorCandidate) {
	base = l.selectorBaseType(base)
	if base == "" {
		return
	}
	seenKey := fmt.Sprintf("%s@%d", base, depth)
	if _, recursive := seen[seenKey]; recursive {
		return
	}
	seen[seenKey] = struct{}{}
	defer delete(seen, seenKey)

	decl, ok := l.typeDeclFor(base)
	if !ok || decl.Kind != ast.TypeStruct {
		return
	}
	for _, field := range decl.Fields {
		if strings.TrimSpace(field.Name) != "" {
			continue
		}
		fieldType := l.selectorBaseType(l.resolveSourceType(field.Type))
		if fieldType == "" {
			continue
		}
		l.collectDirectSelectorCandidates(fieldType, name, depth, out)
		l.collectPromotedSelectorCandidates(fieldType, name, depth+1, seen, out)
	}
}

func (l *lowerer) collectDirectSelectorCandidates(base, name string, depth int, out *[]selectorCandidate) {
	decl, ok := l.typeDeclFor(base)
	if ok && decl.Kind == ast.TypeStruct {
		for _, field := range decl.Fields {
			if l.structFieldName(field) == name {
				*out = append(*out, selectorCandidate{depth: depth, kind: "field", owner: base})
			}
		}
	}
	for _, receiver := range l.promotedReceiverTypes(base) {
		if _, ok := l.methods[receiver+"."+name]; ok {
			*out = append(*out, selectorCandidate{depth: depth, kind: "method", owner: receiver})
		}
	}
}
