package lower

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
)

func (l *lowerer) lowerSelectWithLabel(stmt ast.Statement, scope *funcScope, userLabel string) ([]ir.Statement, bool) {
	endLabel := l.newLabel("select.end")
	if len(stmt.Cases) == 0 {
		waitSet := ir.Expression{Kind: ir.ExprMakeWaitSet}
		park := ir.Expression{Kind: ir.ExprWaitSetPark, WaitSet: &waitSet}
		return []ir.Statement{
			{Kind: ir.StmtExpr, Expr: park},
			{Kind: ir.StmtLabel, Label: endLabel},
		}, true
	}

	defaultIndex := -1
	hasComm := false
	for i, clause := range stmt.Cases {
		if clause.Default && clause.Comm == nil {
			if defaultIndex >= 0 {
				l.add("hirgen.select.default.duplicate", "select has multiple default clauses", clause.Span)
				return nil, false
			}
			defaultIndex = i
			continue
		}
		hasComm = true
	}
	if hasComm {
		return l.lowerSelectCommunication(stmt, defaultIndex, endLabel, scope, userLabel)
	}
	if defaultIndex < 0 {
		l.add("hirgen.select.default.missing", "select emit requires a default clause or communication clauses", stmt.Span)
		return nil, false
	}

	l.pushBranchWithUserLabel(userLabel, endLabel, "")
	body, ok := l.lowerBlock(stmt.Cases[defaultIndex].Body, scope)
	l.popBranch()
	if !ok {
		return nil, false
	}
	out := append([]ir.Statement(nil), body...)
	out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: endLabel})
	return out, true
}

func (l *lowerer) lowerSelectCommunication(stmt ast.Statement, defaultIndex int, endLabel string, scope *funcScope, userLabel string) ([]ir.Statement, bool) {
	caseLabels := make([]string, len(stmt.Cases))
	caseScopes := make([]*funcScope, len(stmt.Cases))
	for i := range stmt.Cases {
		caseLabels[i] = l.newLabel("select.case")
		caseScopes[i] = l.childScope(scope)
	}
	var out []ir.Statement
	waitSetLocal := l.newSyntheticLocal(scope, "select.waitset", "WaitSet")
	waitSetRef := ir.Expression{Kind: ir.ExprLocal, Local: waitSetLocal}
	commCaseIndexes := make([]int, 0, len(stmt.Cases))
	bindings := make([]selectCommBinding, len(stmt.Cases))
	for i, clause := range stmt.Cases {
		if i == defaultIndex || clause.Comm == nil {
			continue
		}
		if !l.declareSelectShortReceiveTargets(*clause.Comm, caseScopes[i]) {
			return nil, false
		}
		binding, bindingStmts, ok := l.lowerSelectCommBinding(*clause.Comm, caseScopes[i], scope)
		if !ok {
			return nil, false
		}
		bindings[i] = binding
		out = append(out, bindingStmts...)
	}
	out = append(out, ir.Statement{Kind: ir.StmtStoreLocal, Local: waitSetLocal, Expr: ir.Expression{Kind: ir.ExprMakeWaitSet}})
	for i, clause := range stmt.Cases {
		if i == defaultIndex {
			continue
		}
		if clause.Comm == nil {
			l.add("hirgen.select.comm.missing", "select communication clause is missing communication statement", clause.Span)
			return nil, false
		}
		tokenLocal := l.newSyntheticLocal(scope, "select.token", "WaitToken")
		tokenRef := ir.Expression{Kind: ir.ExprLocal, Local: tokenLocal}
		add := ir.Expression{Kind: ir.ExprWaitSetAdd, WaitSet: &waitSetRef, Token: &tokenRef}
		out = append(out,
			ir.Statement{Kind: ir.StmtStoreLocal, Local: tokenLocal, Expr: ir.Expression{Kind: ir.ExprMakeWaitToken}},
			ir.Statement{Kind: ir.StmtStoreLocal, Local: waitSetLocal, Expr: add},
		)
		out = append(out, l.lowerSelectWaitRegistration(*clause.Comm, bindings[i], tokenRef)...)
		commCaseIndexes = append(commCaseIndexes, i)
	}
	selectedLocal := l.newSyntheticLocal(scope, "select.index", "Int")
	selectedRef := ir.Expression{Kind: ir.ExprLocal, Local: selectedLocal}
	selectOperation := ir.ExprWaitSetPark
	if defaultIndex >= 0 {
		selectOperation = ir.ExprWaitSetPoll
	}
	out = append(out, ir.Statement{Kind: ir.StmtStoreLocal, Local: selectedLocal, Expr: ir.Expression{Kind: selectOperation, WaitSet: &waitSetRef}})
	for tokenIndex, caseIndex := range commCaseIndexes {
		index := ir.Expression{Kind: ir.ExprLiteral, Type: l.hirType("Int"), Value: json.RawMessage(strconv.FormatInt(int64(tokenIndex), 10))}
		cond := ir.Expression{Kind: ir.ExprBinary, Operator: "==", Left: &selectedRef, Right: &index}
		out = append(out, ir.Statement{Kind: ir.StmtJumpIf, Expr: cond, Label: caseLabels[caseIndex]})
	}
	if defaultIndex >= 0 {
		out = append(out, ir.Statement{Kind: ir.StmtJump, Label: caseLabels[defaultIndex]})
	} else {
		out = append(out, ir.Statement{Kind: ir.StmtJump, Label: endLabel})
	}
	for i, clause := range stmt.Cases {
		out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: caseLabels[i]})
		l.pushBranchWithUserLabel(userLabel, endLabel, "")
		if i != defaultIndex && clause.Comm != nil {
			commit, ok := l.lowerSelectCommit(*clause.Comm, caseScopes[i], bindings[i])
			if !ok {
				l.popBranch()
				return nil, false
			}
			out = append(out, commit...)
		}
		body, ok := l.lowerBlock(clause.Body, caseScopes[i])
		l.popBranch()
		if !ok {
			return nil, false
		}
		out = append(out, body...)
		out = append(out, ir.Statement{Kind: ir.StmtJump, Label: endLabel})
	}
	out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: endLabel})
	out = append(out, ir.Statement{Kind: ir.StmtWaitSetCancel, Expr: ir.Expression{Kind: ir.ExprLocal, Local: waitSetLocal}})
	return out, true
}

func (l *lowerer) declareSelectShortReceiveTargets(comm ast.Statement, scope *funcScope) bool {
	if comm.Kind != ast.StmtAssign || comm.Op != ":=" {
		return true
	}
	if len(comm.Right) != 1 || comm.Right[0].Kind != ast.ExprReceive || comm.Right[0].Operand == nil {
		l.add("hirgen.select.short.receive", "select short declaration currently requires receive expression", comm.Span)
		return false
	}
	if len(comm.Left) != 1 && len(comm.Left) != 2 {
		l.add("hirgen.select.short.targets", "select receive short declaration requires one or two targets", comm.Span)
		return false
	}
	valueType := l.channelElementType(l.expressionType(*comm.Right[0].Operand, scope))
	targetTypes := []string{valueType, "Bool"}
	seen := map[string]struct{}{}
	newName := false
	for i, target := range comm.Left {
		if target.Kind != ast.ExprIdent {
			l.add("hirgen.select.short.target", "select short declaration requires identifier targets", target.Span)
			return false
		}
		name := strings.TrimSpace(target.Name)
		if name == "" || name == "_" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			l.add("hirgen.select.short.duplicate", "select short declaration has duplicate target name", target.Span)
			return false
		}
		seen[name] = struct{}{}
		if _, exists := scope.constants[name]; exists {
			l.add("hirgen.select.short.constant", "select short declaration cannot redeclare a constant", target.Span)
			return false
		}
		if _, exists := l.currentLocal(name, scope); exists {
			continue
		}
		typ := "Any"
		if i < len(targetTypes) && strings.TrimSpace(targetTypes[i]) != "" {
			typ = targetTypes[i]
		}
		id := l.newLocalID(scope.function, name)
		scope.function.Locals = append(scope.function.Locals, ir.Local{
			ID: id, Name: name, Type: l.hirType(typ), Scope: scope.debugScope, Declaration: hirLocationPtr(target.Span),
		})
		scope.locals[name] = id
		scope.localTypes[name] = l.hirType(typ)
		newName = true
	}
	if !newName {
		l.add("hirgen.select.short.no_new", "select short declaration requires at least one new variable", comm.Span)
		return false
	}
	return true
}

type selectCommBinding struct {
	Channel   *ir.Expression
	SendValue *ir.Expression
}

func (l *lowerer) lowerSelectCommBinding(comm ast.Statement, caseScope, ownerScope *funcScope) (selectCommBinding, []ir.Statement, bool) {
	if recv, ok := selectReceiveExpression(comm); ok {
		channel, ok := l.lowerExpression(*recv.Operand, caseScope)
		if !ok {
			return selectCommBinding{}, nil, false
		}
		channelLocal := l.newSyntheticLocal(ownerScope, "select.chan", l.expressionType(*recv.Operand, caseScope))
		channelRef := ir.Expression{Kind: ir.ExprLocal, Local: channelLocal}
		return selectCommBinding{Channel: &channelRef}, []ir.Statement{{Kind: ir.StmtStoreLocal, Local: channelLocal, Expr: channel}}, true
	}
	if send, ok := selectSendStatement(comm); ok {
		valueType, ok := l.sendChannelElementType(send.Left[0], caseScope, "hirgen.select.comm.send.direction")
		if !ok {
			return selectCommBinding{}, nil, false
		}
		channel, ok := l.lowerExpression(send.Left[0], caseScope)
		if !ok {
			return selectCommBinding{}, nil, false
		}
		channelLocal := l.newSyntheticLocal(ownerScope, "select.chan", l.expressionType(send.Left[0], caseScope))
		channelRef := ir.Expression{Kind: ir.ExprLocal, Local: channelLocal}
		value, ok := l.lowerExpressionInType(send.Right[0], valueType, caseScope)
		if !ok {
			return selectCommBinding{}, nil, false
		}
		valueLocal := l.newSyntheticLocal(ownerScope, "select.send", firstNonEmpty(valueType, l.expressionType(send.Right[0], caseScope)))
		valueRef := ir.Expression{Kind: ir.ExprLocal, Local: valueLocal}
		return selectCommBinding{Channel: &channelRef, SendValue: &valueRef}, []ir.Statement{
			{Kind: ir.StmtStoreLocal, Local: channelLocal, Expr: channel},
			{Kind: ir.StmtStoreLocal, Local: valueLocal, Expr: value},
		}, true
	}
	return selectCommBinding{}, nil, true
}

func (l *lowerer) lowerSelectWaitRegistration(comm ast.Statement, binding selectCommBinding, token ir.Expression) []ir.Statement {
	if binding.Channel == nil {
		return nil
	}
	if _, ok := selectReceiveExpression(comm); ok {
		wait := ir.Expression{Kind: ir.ExprChanSubscribeRecv, Operand: binding.Channel, Token: &token}
		return []ir.Statement{{Kind: ir.StmtExpr, Expr: wait}}
	}
	if _, ok := selectSendStatement(comm); ok {
		wait := ir.Expression{Kind: ir.ExprChanSubscribeSend, Operand: binding.Channel, Token: &token}
		return []ir.Statement{{Kind: ir.StmtExpr, Expr: wait}}
	}
	return nil
}

func selectReceiveExpression(comm ast.Statement) (*ast.Expression, bool) {
	switch comm.Kind {
	case ast.StmtExpr:
		if comm.Expr != nil && comm.Expr.Kind == ast.ExprReceive && comm.Expr.Operand != nil {
			return comm.Expr, true
		}
	case ast.StmtAssign:
		if len(comm.Right) == 1 && comm.Right[0].Kind == ast.ExprReceive && comm.Right[0].Operand != nil {
			return &comm.Right[0], true
		}
	}
	return nil, false
}

func selectSendStatement(comm ast.Statement) (*ast.Statement, bool) {
	if comm.Kind != ast.StmtSend || len(comm.Left) != 1 || len(comm.Right) != 1 {
		return nil, false
	}
	return &comm, true
}

func (l *lowerer) lowerSelectCommit(comm ast.Statement, scope *funcScope, binding selectCommBinding) ([]ir.Statement, bool) {
	switch comm.Kind {
	case ast.StmtExpr:
		if comm.Expr == nil || comm.Expr.Kind != ast.ExprReceive {
			l.add("hirgen.select.comm.receive", "select expression communication currently requires receive expression", comm.Span)
			return nil, false
		}
		recv, ok := l.lowerReceiveCommitExpression(*comm.Expr, scope, binding, false)
		if !ok {
			return nil, false
		}
		return []ir.Statement{{Kind: ir.StmtExpr, Expr: recv}}, true
	case ast.StmtAssign:
		if len(comm.Right) != 1 || comm.Right[0].Kind != ast.ExprReceive {
			l.add("hirgen.select.comm.receive", "select assignment communication currently requires receive expression", comm.Span)
			return nil, false
		}
		if len(comm.Left) != 1 && len(comm.Left) != 2 {
			l.add("hirgen.select.comm.targets", "select receive assignment requires one or two targets", comm.Span)
			return nil, false
		}
		recv, ok := l.lowerReceiveCommitExpression(comm.Right[0], scope, binding, len(comm.Left) == 2)
		if !ok {
			return nil, false
		}
		return l.lowerSelectReceiveAssignment(comm.Left, recv, scope)
	case ast.StmtSend:
		if len(comm.Left) != 1 || len(comm.Right) != 1 {
			l.add("hirgen.select.comm.send.shape", "select send communication requires channel and value expressions", comm.Span)
			return nil, false
		}
		valueType, ok := l.sendChannelElementType(comm.Left[0], scope, "hirgen.select.comm.send.direction")
		if !ok {
			return nil, false
		}
		channel := binding.Channel
		if channel == nil {
			lowered, ok := l.lowerExpression(comm.Left[0], scope)
			if !ok {
				return nil, false
			}
			channel = &lowered
		}
		value := binding.SendValue
		if value == nil {
			lowered, ok := l.lowerExpressionInType(comm.Right[0], valueType, scope)
			if !ok {
				return nil, false
			}
			value = &lowered
		}
		return []ir.Statement{{Kind: ir.StmtChanSend, Object: *channel, Expr: *value}}, true
	default:
		l.add("hirgen.select.comm.unsupported", "unsupported select communication statement", comm.Span)
		return nil, false
	}
}

func (l *lowerer) lowerSelectReceiveAssignment(targets []ast.Expression, recv ir.Expression, scope *funcScope) ([]ir.Statement, bool) {
	plans, ok := l.planLValues(targets, scope)
	if !ok {
		return nil, false
	}
	tempTargets, tempRefs := l.lvalueResultTemps(plans, scope)
	out := []ir.Statement{{Kind: ir.StmtStoreResults, Expr: recv, Targets: tempTargets}}
	out = appendLValuePreludes(out, plans)
	for i, plan := range plans {
		store := plan.store(tempRefs[i])
		if store.Kind != "" {
			out = append(out, store)
		}
	}
	return out, true
}

func (l *lowerer) lowerReceiveCommitExpression(expr ast.Expression, scope *funcScope, binding selectCommBinding, twoValue bool) (ir.Expression, bool) {
	if binding.Channel != nil {
		if twoValue {
			return ir.Expression{Kind: ir.ExprChanRecvOK, Operand: binding.Channel}, true
		}
		return ir.Expression{Kind: ir.ExprChanRecv, Operand: binding.Channel}, true
	}
	return l.lowerReceiveExpression(expr, scope, twoValue)
}
