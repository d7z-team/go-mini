package semantic

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) analyzeCaseClauses(stmt *ast.Statement, scope ScopeID) {
	control := SwitchInfo{}
	if stmt.Kind == ast.StmtSwitch {
		control.Kind = SwitchExpression
		if stmt.Expr != nil {
			control.Tag = a.info.Exprs[stmt.Expr.NodeID].Type
			control.Comparable = control.Tag.Valid() && a.info.Relations.Comparable(control.Tag).OK
		} else {
			control.Comparable = true
		}
		if stmt.TypeSwitch {
			control.Kind = SwitchType
			if stmt.Expr != nil {
				control.Subject = a.info.Exprs[stmt.Expr.NodeID].Type
				control.Interface = control.Subject.Kind == types.Any
				if _, _, typeSet, isInterface := a.info.Relations.View(control.Subject).Interface(); isInterface {
					control.Interface = true
					control.TypeSet = typeSet
				}
			}
		}
	}

	for i := range stmt.Cases {
		clause := &stmt.Cases[i]
		clauseScope := a.newScope(ScopeClause, clause.NodeID, scope)
		caseInfo := SwitchCaseInfo{Node: clause.NodeID, Nil: clause.Nil, Default: clause.Default}
		for j := range clause.Values {
			a.analyzeExpr(&clause.Values[j], clauseScope)
			if control.Kind == SwitchExpression {
				target := control.Tag
				if stmt.Expr == nil {
					target = types.Builtin(types.PrimitiveBool)
				}
				a.validateAssignments([]ast.Expression{clause.Values[j]}, []types.TypeRef{target})
			}
		}
		for j := range clause.Types {
			a.analyzeType(&clause.Types[j], clauseScope)
			typ := a.resolvedType(clause.Types[j])
			caseInfo.Types = append(caseInfo.Types, typ)
			caseInfo.Implements = append(caseInfo.Implements, typ.Valid() && control.Subject.Valid() && a.info.Relations.Implements(typ, control.Subject).OK)
			_, _, typeSet, _ := a.info.Relations.View(typ).Interface()
			caseInfo.TypeSets = append(caseInfo.TypeSets, typeSet)
		}
		if stmt.TypeSwitch && strings.TrimSpace(stmt.TypeSwitchName) != "" {
			caseInfo.Binding = control.Subject
			if !clause.Nil && !clause.Default && len(caseInfo.Types) == 1 {
				caseInfo.Binding = caseInfo.Types[0]
			}
			a.declare(clauseScope, ObjectVar, stmt.TypeSwitchName, clause.NodeID, caseInfo.Binding, true, false)
		}
		if clause.Comm != nil {
			a.analyzeStmt(clause.Comm, clauseScope)
		}
		a.analyzeBlock(&clause.Body, clauseScope, false)
		control.Cases = append(control.Cases, caseInfo)
	}
	if control.Kind != SwitchInvalid {
		a.info.Switches[stmt.NodeID] = control
	}
}
