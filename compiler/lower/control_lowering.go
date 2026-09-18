package lower

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
)

func (l *lowerer) lowerBranch(stmt ast.Statement) ([]ir.Statement, bool) {
	if stmt.Op == "goto" {
		label := userLabel(stmt.Label)
		if label == "" {
			l.add("hirgen.branch.goto.label", "goto statement requires a label", stmt.Span)
			return nil, false
		}
		return []ir.Statement{
			{Kind: ir.StmtJump, Label: label},
			{Kind: ir.StmtLabel, Label: l.newLabel("branch.after")},
		}, true
	}
	if strings.TrimSpace(stmt.Label) != "" {
		return l.lowerLabeledBranch(stmt)
	}
	if len(l.branches) == 0 {
		l.add("hirgen.branch.outside", "branch statement outside control flow", stmt.Span)
		return nil, false
	}
	target := l.branches[len(l.branches)-1]
	var label string
	switch stmt.Op {
	case "break":
		label = target.breakLabel
	case "continue":
		label = l.innermostContinueLabel()
		if label == "" {
			l.add("hirgen.branch.continue", "continue statement outside loop", stmt.Span)
			return nil, false
		}
	case "fallthrough":
		label = target.fallthroughLabel
		if label == "" {
			l.add("hirgen.branch.fallthrough", "fallthrough statement has no target case", stmt.Span)
			return nil, false
		}
	default:
		l.add("hirgen.branch.op", "unsupported branch statement", stmt.Span)
		return nil, false
	}
	return []ir.Statement{
		{Kind: ir.StmtJump, Label: label},
		{Kind: ir.StmtLabel, Label: l.newLabel("branch.after")},
	}, true
}

func (l *lowerer) innermostContinueLabel() string {
	for i := len(l.branches) - 1; i >= 0; i-- {
		if label := strings.TrimSpace(l.branches[i].continueLabel); label != "" {
			return label
		}
	}
	return ""
}

func (l *lowerer) lowerLabeledBranch(stmt ast.Statement) ([]ir.Statement, bool) {
	target, ok := l.lookupLabeledBranch(stmt.Label)
	if !ok {
		l.add("hirgen.branch.label.unknown", "branch label does not target an enclosing control statement", stmt.Span)
		return nil, false
	}
	var label string
	switch stmt.Op {
	case "break":
		label = target.breakLabel
	case "continue":
		label = target.continueLabel
		if label == "" {
			l.add("hirgen.branch.continue.label", "continue label does not target a loop", stmt.Span)
			return nil, false
		}
	default:
		l.add("hirgen.branch.label.op", "only break and continue support labels", stmt.Span)
		return nil, false
	}
	return []ir.Statement{
		{Kind: ir.StmtJump, Label: label},
		{Kind: ir.StmtLabel, Label: l.newLabel("branch.after")},
	}, true
}

func (l *lowerer) lowerLabel(stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	label := userLabel(stmt.Label)
	if label == "" {
		l.add("hirgen.label.missing", "labeled statement requires a label", stmt.Span)
		return nil, false
	}
	out := []ir.Statement{{Kind: ir.StmtLabel, Label: label}}
	if len(stmt.Body.Stmts) == 1 {
		nested := stmt.Body.Stmts[0]
		switch nested.Kind {
		case ast.StmtFor, ast.StmtRange, ast.StmtSwitch, ast.StmtSelect:
			control, ok := l.lowerLabeledControl(stmt.Label, nested, scope)
			if !ok {
				return nil, false
			}
			return append(out, control...), true
		}
	}
	for _, nested := range stmt.Body.Stmts {
		lowered, ok := l.lowerStatement(nested, scope)
		if !ok {
			return nil, false
		}
		if loc := hirLocationPtr(nested.Span); loc != nil && len(lowered) != 0 && len(lowered[0].SourcePoints) == 0 {
			lowered[0].SourcePoints = []ir.Location{*loc}
		}
		out = append(out, lowered...)
	}
	return out, true
}

func (l *lowerer) lowerLabeledControl(label string, stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	switch stmt.Kind {
	case ast.StmtFor:
		return l.lowerForWithLabel(stmt, scope, label)
	case ast.StmtRange:
		return l.lowerRangeWithLabel(stmt, scope, label)
	case ast.StmtSwitch:
		return l.lowerSwitchWithLabel(stmt, scope, label)
	case ast.StmtSelect:
		return l.lowerSelectWithLabel(stmt, scope, label)
	default:
		l.add("hirgen.label.control", "label does not target a supported control statement", stmt.Span)
		return nil, false
	}
}
