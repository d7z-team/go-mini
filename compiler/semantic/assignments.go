package semantic

import (
	"fmt"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

// validateAssignments compares resolved source values. Untyped constants are
// concretized separately; unresolved generic results are checked after specialization.
func (a *analyzer) validateAssignments(values []ast.Expression, targets []types.TypeRef) {
	sources := a.assignmentExpressionTypes(values, len(targets))
	if len(sources) != len(targets) {
		return
	}
	for i, target := range targets {
		expr := values[0]
		if len(values) == len(targets) {
			expr = values[i]
		}
		info := a.info.Exprs[expr.NodeID]
		if a.validateConstantTarget(expr, target, false) || info.Untyped || !a.info.TypeExact(sources[i]) || !a.info.TypeExact(target) {
			continue
		}
		if !a.info.Relations.Assignable(sources[i], target).OK {
			a.addDiagnostic("semantic.assign.type", fmt.Sprintf("cannot assign %s to %s",
				types.FormatWithTable(a.info.TypeTable, a.info.Relations.ResolveAlias(sources[i])), types.FormatWithTable(a.info.TypeTable, a.info.Relations.ResolveAlias(target))), expr.Span)
		}
	}
}

func (a *analyzer) validateShortDeclaration(left []ast.Expression, scope ScopeID) {
	seen := make(map[string]bool, len(left))
	fresh := false
	for _, expr := range left {
		if expr.Kind != ast.ExprIdent {
			a.addDiagnostic("semantic.short.target", "short declaration target must be an identifier", expr.Span)
			continue
		}
		if expr.Name == "_" {
			continue
		}
		if seen[expr.Name] {
			a.addDiagnostic("semantic.short.duplicate", "short declaration repeats identifier "+expr.Name, expr.Span)
		}
		seen[expr.Name] = true
		if id, exists := a.info.Scopes[scope].Objects[expr.Name]; exists {
			if a.info.Objects[id].Kind != ObjectVar {
				a.addDiagnostic("semantic.short.variable", "short declaration can only reassign variables", expr.Span)
			}
		} else {
			fresh = true
		}
	}
	if !fresh && len(left) != 0 {
		a.addDiagnostic("semantic.short.no_new", "short declaration requires at least one new variable", left[0].Span)
	}
}
