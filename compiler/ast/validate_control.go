package ast

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/source"
)

type branchLabelInfo struct {
	path       []int
	pos        int
	targetKind StmtKind
	span       source.Span
	used       bool
}

type branchVariableInfo struct {
	path []int
	pos  int
}

type branchGotoInfo struct {
	label string
	path  []int
	pos   int
	span  source.Span
}

type branchValidation struct {
	nextScope  int
	nextPos    int
	labels     map[string]branchLabelInfo
	labelOrder []string
	variables  []branchVariableInfo
	gotos      []branchGotoInfo
}

type branchControl struct {
	kind  StmtKind
	label string
}

type branchCaseContext struct {
	typeSwitch bool
	final      bool
}

func validateBranchTargets(block BlockStmt, add func(string, string, source.Span)) {
	state := branchValidation{labels: map[string]branchLabelInfo{}}
	state.collectBlock(block, nil, add)
	for _, jump := range state.gotos {
		target, ok := state.labels[jump.label]
		if !ok {
			add("ast.branch.goto.label.unknown", "goto statement targets an undefined label", jump.span)
			continue
		}
		target.used = true
		state.labels[jump.label] = target
		if !branchPathPrefix(target.path, jump.path) {
			add("ast.branch.goto.scope", "goto statement cannot jump into a deeper block", jump.span)
			continue
		}
		if !branchPathEqual(target.path, jump.path) {
			continue
		}
		for _, variable := range state.variables {
			if !branchPathEqual(variable.path, jump.path) || variable.pos <= jump.pos || variable.pos >= target.pos {
				continue
			}
			add("ast.branch.goto.decl", "goto statement jumps over a variable declaration", jump.span)
			break
		}
	}
	state.validateControlFlow(block, nil, nil, add)
	for _, label := range state.labelOrder {
		info := state.labels[label]
		if info.used {
			continue
		}
		add("ast.stmt.label.unused", "label declared and not used: "+label, info.span)
	}
}

func (v *branchValidation) collectBlock(block BlockStmt, path []int, add func(string, string, source.Span)) {
	for _, stmt := range block.Stmts {
		pos := v.nextPos
		v.nextPos++
		switch stmt.Kind {
		case StmtLabel:
			label := strings.TrimSpace(stmt.Label)
			if label != "" {
				if _, exists := v.labels[label]; exists {
					add("ast.stmt.label.duplicate", "duplicate statement label", stmt.Span)
				} else {
					targetKind := StmtKind("")
					if len(stmt.Body.Stmts) == 1 {
						targetKind = stmt.Body.Stmts[0].Kind
					}
					v.labels[label] = branchLabelInfo{path: append([]int(nil), path...), pos: pos, targetKind: targetKind, span: stmt.Span}
					v.labelOrder = append(v.labelOrder, label)
				}
			}
			// A label does not introduce a lexical block. The parser wraps
			// its following statement in a block only to keep the AST uniform.
			v.collectBlock(stmt.Body, path, add)
		case StmtBranch:
			if strings.TrimSpace(stmt.Op) == "goto" {
				label := strings.TrimSpace(stmt.Label)
				if label == "" {
					add("ast.branch.goto.label.missing", "goto statement requires a label", stmt.Span)
				} else {
					v.gotos = append(v.gotos, branchGotoInfo{label: label, path: append([]int(nil), path...), pos: pos, span: stmt.Span})
				}
			}
		case StmtDecl:
			for _, decl := range statementDecls(stmt) {
				if decl.Kind == DeclVar {
					v.variables = append(v.variables, branchVariableInfo{path: append([]int(nil), path...), pos: pos})
					break
				}
			}
		case StmtAssign:
			if strings.TrimSpace(stmt.Op) == ":=" {
				v.variables = append(v.variables, branchVariableInfo{path: append([]int(nil), path...), pos: pos})
			}
		}
		v.collectNested(stmt, path, add)
	}
}

func (v *branchValidation) validateControlFlow(block BlockStmt, controls []branchControl, caseContext *branchCaseContext, add func(string, string, source.Span)) {
	for index, stmt := range block.Stmts {
		last := caseContext != nil && index == len(block.Stmts)-1
		v.validateControlStatement(stmt, controls, caseContext, last, add)
	}
}

func (v *branchValidation) validateControlStatement(stmt Statement, controls []branchControl, caseContext *branchCaseContext, last bool, add func(string, string, source.Span)) {
	switch stmt.Kind {
	case StmtBranch:
		v.validateBranchStatement(stmt, controls, caseContext, last, add)
	case StmtLabel:
		if len(stmt.Body.Stmts) == 1 && isBreakableStatement(stmt.Body.Stmts[0].Kind) {
			control := branchControl{kind: stmt.Body.Stmts[0].Kind, label: strings.TrimSpace(stmt.Label)}
			v.validateControlStatement(stmt.Body.Stmts[0], append(controls, control), caseContext, last, add)
			return
		}
		v.validateControlFlow(stmt.Body, controls, nil, add)
	case StmtBlock:
		v.validateControlFlow(stmt.Body, controls, nil, add)
	case StmtIf:
		if stmt.Init != nil {
			v.validateControlStatement(*stmt.Init, controls, nil, false, add)
		}
		v.validateControlFlow(stmt.Body, controls, nil, add)
		if stmt.Else != nil {
			v.validateControlStatement(*stmt.Else, controls, nil, false, add)
		}
	case StmtFor:
		if stmt.Init != nil {
			v.validateControlStatement(*stmt.Init, controls, nil, false, add)
		}
		v.validateControlFlow(stmt.Body, append(controls, branchControl{kind: StmtFor}), nil, add)
		if stmt.Post != nil {
			v.validateControlStatement(*stmt.Post, controls, nil, false, add)
		}
	case StmtRange:
		v.validateControlFlow(stmt.Body, append(controls, branchControl{kind: StmtRange}), nil, add)
	case StmtSwitch:
		control := append([]branchControl(nil), controls...)
		control = append(control, branchControl{kind: StmtSwitch})
		for index, clause := range stmt.Cases {
			if clause.Comm != nil {
				v.validateControlStatement(*clause.Comm, control, nil, false, add)
			}
			v.validateControlFlow(clause.Body, control, &branchCaseContext{typeSwitch: stmt.TypeSwitch, final: index == len(stmt.Cases)-1}, add)
		}
	case StmtSelect:
		control := append([]branchControl(nil), controls...)
		control = append(control, branchControl{kind: StmtSelect})
		for _, clause := range stmt.Cases {
			if clause.Comm != nil {
				v.validateControlStatement(*clause.Comm, control, nil, false, add)
			}
			v.validateControlFlow(clause.Body, control, nil, add)
		}
	}
}

func (v *branchValidation) validateBranchStatement(stmt Statement, controls []branchControl, caseContext *branchCaseContext, last bool, add func(string, string, source.Span)) {
	op := strings.TrimSpace(stmt.Op)
	if op == "goto" {
		return
	}
	label := strings.TrimSpace(stmt.Label)
	switch op {
	case "break":
		if label == "" {
			if !hasBreakControl(controls) {
				add("ast.branch.break.outside", "break statement is not inside a loop, switch, or select", stmt.Span)
			}
			return
		}
		v.validateLabeledBranch(label, controls, true, stmt.Span, add)
	case "continue":
		if label == "" {
			if !hasLoopControl(controls) {
				add("ast.branch.continue.outside", "continue statement is not inside a loop", stmt.Span)
			}
			return
		}
		v.validateLabeledBranch(label, controls, false, stmt.Span, add)
	case "fallthrough":
		if caseContext == nil {
			add("ast.branch.fallthrough.context", "fallthrough statement is not inside a switch case", stmt.Span)
		} else if caseContext.typeSwitch {
			add("ast.branch.fallthrough.typeswitch", "fallthrough is not allowed in a type switch", stmt.Span)
		} else if caseContext.final {
			add("ast.branch.fallthrough.final", "fallthrough cannot appear in the final switch case", stmt.Span)
		} else if !last {
			add("ast.branch.fallthrough.position", "fallthrough must be the final statement in a switch case", stmt.Span)
		}
	}
}

func (v *branchValidation) validateLabeledBranch(label string, controls []branchControl, breakTarget bool, span source.Span, add func(string, string, source.Span)) {
	info, exists := v.labels[label]
	if !exists {
		if breakTarget {
			add("ast.branch.break.label.unknown", "break statement targets an undefined label", span)
		} else {
			add("ast.branch.continue.label.unknown", "continue statement targets an undefined label", span)
		}
		return
	}
	info.used = true
	v.labels[label] = info
	var target *branchControl
	for index := len(controls) - 1; index >= 0; index-- {
		if controls[index].label == label {
			target = &controls[index]
			break
		}
	}
	if target == nil {
		if breakTarget && !isBreakableStatement(info.targetKind) {
			add("ast.branch.break.label.target", "break label does not target a breakable statement", span)
			return
		}
		if !breakTarget && info.targetKind != StmtFor && info.targetKind != StmtRange {
			add("ast.branch.continue.label.target", "continue label does not target a loop", span)
			return
		}
		if breakTarget {
			add("ast.branch.break.label.scope", "break label does not target an enclosing control statement", span)
		} else {
			add("ast.branch.continue.label.scope", "continue label does not target an enclosing control statement", span)
		}
		return
	}
	if breakTarget {
		if !isBreakableStatement(target.kind) {
			add("ast.branch.break.label.target", "break label does not target a breakable statement", span)
		}
	} else if target.kind != StmtFor && target.kind != StmtRange {
		add("ast.branch.continue.label.target", "continue label does not target a loop", span)
	}
}

func hasBreakControl(controls []branchControl) bool {
	for index := len(controls) - 1; index >= 0; index-- {
		if isBreakableStatement(controls[index].kind) {
			return true
		}
	}
	return false
}

func hasLoopControl(controls []branchControl) bool {
	for index := len(controls) - 1; index >= 0; index-- {
		if controls[index].kind == StmtFor || controls[index].kind == StmtRange {
			return true
		}
	}
	return false
}

func (v *branchValidation) collectNested(stmt Statement, path []int, add func(string, string, source.Span)) {
	switch stmt.Kind {
	case StmtIf:
		if stmt.Init != nil {
			v.collectBlock(BlockStmt{Stmts: []Statement{*stmt.Init}}, v.newPath(path), add)
		}
		v.collectBlock(stmt.Body, v.newPath(path), add)
		if stmt.Else != nil {
			v.collectBlock(BlockStmt{Stmts: []Statement{*stmt.Else}}, v.newPath(path), add)
		}
	case StmtFor:
		loopPath := v.newPath(path)
		if stmt.Init != nil {
			v.collectBlock(BlockStmt{Stmts: []Statement{*stmt.Init}}, loopPath, add)
		}
		v.collectBlock(stmt.Body, loopPath, add)
		if stmt.Post != nil {
			v.collectBlock(BlockStmt{Stmts: []Statement{*stmt.Post}}, loopPath, add)
		}
	case StmtRange:
		v.collectBlock(stmt.Body, v.newPath(path), add)
	case StmtSwitch, StmtSelect:
		for _, clause := range stmt.Cases {
			clausePath := v.newPath(path)
			if clause.Comm != nil {
				v.collectBlock(BlockStmt{Stmts: []Statement{*clause.Comm}}, clausePath, add)
			}
			v.collectBlock(clause.Body, clausePath, add)
		}
	case StmtBlock:
		v.collectBlock(stmt.Body, v.newPath(path), add)
	}
}

func (v *branchValidation) newPath(parent []int) []int {
	v.nextScope++
	path := append([]int(nil), parent...)
	return append(path, v.nextScope)
}

func branchPathEqual(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func branchPathPrefix(prefix, path []int) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i := range prefix {
		if prefix[i] != path[i] {
			return false
		}
	}
	return true
}

func blockTerminates(block BlockStmt) bool {
	for _, stmt := range block.Stmts {
		if statementTerminates(stmt) {
			return true
		}
	}
	return false
}

func statementTerminates(stmt Statement) bool {
	return statementTerminatesWithLoopLabel(stmt, "")
}

func statementTerminatesWithLoopLabel(stmt Statement, loopLabel string) bool {
	switch stmt.Kind {
	case StmtReturn, StmtPanic:
		return true
	case StmtBlock:
		return blockTerminates(stmt.Body)
	case StmtLabel:
		if len(stmt.Body.Stmts) == 1 && isBreakableStatement(stmt.Body.Stmts[0].Kind) {
			return statementTerminatesWithLoopLabel(stmt.Body.Stmts[0], strings.TrimSpace(stmt.Label))
		}
		return blockTerminates(stmt.Body)
	case StmtIf:
		return blockTerminates(stmt.Body) && stmt.Else != nil && statementTerminates(*stmt.Else)
	case StmtFor:
		return isAlwaysTrue(stmt.Cond) && !containsLoopBreak(stmt.Body, loopLabel)
	case StmtSwitch:
		return switchTerminates(stmt.Cases)
	case StmtSelect:
		if len(stmt.Cases) == 0 {
			return true
		}
		for _, clause := range stmt.Cases {
			if !blockTerminates(clause.Body) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func switchTerminates(cases []CaseClause) bool {
	if len(cases) == 0 {
		return false
	}
	terminates := make([]bool, len(cases))
	for i := len(cases) - 1; i >= 0; i-- {
		if blockTerminates(cases[i].Body) {
			terminates[i] = true
			continue
		}
		if caseEndsWithFallthrough(cases[i].Body) && i+1 < len(cases) {
			terminates[i] = terminates[i+1]
		}
	}
	for _, clause := range cases {
		if clause.Default {
			for _, terminatesCase := range terminates {
				if !terminatesCase {
					return false
				}
			}
			return true
		}
	}
	return false
}

func caseEndsWithFallthrough(block BlockStmt) bool {
	if len(block.Stmts) == 0 {
		return false
	}
	last := block.Stmts[len(block.Stmts)-1]
	return last.Kind == StmtBranch && strings.TrimSpace(last.Op) == "fallthrough"
}

func isAlwaysTrue(expr *Expression) bool {
	if expr == nil {
		return true
	}
	if expr.Kind == ExprLiteral && strings.TrimSpace(expr.Literal) == "true" {
		return true
	}
	return expr.Kind == ExprIdent && strings.TrimSpace(expr.Name) == "true"
}

func isBreakableStatement(kind StmtKind) bool {
	switch kind {
	case StmtFor, StmtRange, StmtSwitch, StmtSelect:
		return true
	default:
		return false
	}
}

func containsLoopBreak(block BlockStmt, loopLabel string) bool {
	for _, stmt := range block.Stmts {
		if statementContainsLoopBreak(stmt, loopLabel) {
			return true
		}
	}
	return false
}

func statementContainsLoopBreak(stmt Statement, loopLabel string) bool {
	if stmt.Kind == StmtBranch && strings.TrimSpace(stmt.Op) == "break" {
		label := strings.TrimSpace(stmt.Label)
		return label == "" || (loopLabel != "" && label == loopLabel)
	}
	switch stmt.Kind {
	case StmtBlock, StmtLabel:
		return containsLoopBreak(stmt.Body, loopLabel)
	case StmtIf:
		return containsLoopBreak(stmt.Body, loopLabel) || (stmt.Else != nil && statementContainsLoopBreak(*stmt.Else, loopLabel))
	case StmtFor, StmtRange:
		return loopLabel != "" && containsNamedBreak(stmt.Body, loopLabel)
	case StmtSwitch, StmtSelect:
		if loopLabel == "" {
			return false
		}
		for _, clause := range stmt.Cases {
			if clause.Comm != nil && statementContainsNamedBreak(*clause.Comm, loopLabel) {
				return true
			}
			if containsNamedBreak(clause.Body, loopLabel) {
				return true
			}
		}
		return false
	default:
		// A nested loop, switch, or select consumes its own unlabeled break.
		return false
	}
}

func containsNamedBreak(block BlockStmt, label string) bool {
	for _, stmt := range block.Stmts {
		if statementContainsNamedBreak(stmt, label) {
			return true
		}
	}
	return false
}

func statementContainsNamedBreak(stmt Statement, label string) bool {
	if stmt.Kind == StmtBranch && strings.TrimSpace(stmt.Op) == "break" {
		return strings.TrimSpace(stmt.Label) == label
	}
	switch stmt.Kind {
	case StmtBlock, StmtLabel:
		return containsNamedBreak(stmt.Body, label)
	case StmtIf:
		return containsNamedBreak(stmt.Body, label) || (stmt.Else != nil && statementContainsNamedBreak(*stmt.Else, label))
	case StmtFor, StmtRange:
		return containsNamedBreak(stmt.Body, label)
	case StmtSwitch, StmtSelect:
		for _, clause := range stmt.Cases {
			if clause.Comm != nil && statementContainsNamedBreak(*clause.Comm, label) {
				return true
			}
			if containsNamedBreak(clause.Body, label) {
				return true
			}
		}
	}
	return false
}
