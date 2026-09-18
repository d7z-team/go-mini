package lower

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	ir "github.com/d7z-team/mini-go/compiler/hir"
)

func (l *lowerer) lowerIf(stmt ast.Statement, scope *funcScope) ([]ir.Statement, bool) {
	if stmt.Cond == nil {
		l.add("hirgen.if.cond.missing", "missing if condition", stmt.Span)
		return nil, false
	}
	ifScope := l.childScope(scope)
	thenLabel := l.newLabel("if.then")
	afterElseLabel := l.newLabel("if.after_else")
	endLabel := l.newLabel("if.end")
	var out []ir.Statement
	if stmt.Init != nil {
		init, ok := l.lowerStatement(*stmt.Init, ifScope)
		if !ok {
			return nil, false
		}
		out = append(out, init...)
	}
	cond, ok := l.lowerExpression(*stmt.Cond, ifScope)
	if !ok {
		return nil, false
	}
	out = append(out,
		ir.Statement{Kind: ir.StmtJumpIf, Expr: cond, Label: thenLabel},
	)
	if stmt.Else != nil {
		elseBody, ok := l.lowerStatement(*stmt.Else, ifScope)
		if !ok {
			return nil, false
		}
		out = append(out, elseBody...)
	}
	out = append(out,
		ir.Statement{Kind: ir.StmtLabel, Label: afterElseLabel},
		ir.Statement{Kind: ir.StmtJump, Label: endLabel},
		ir.Statement{Kind: ir.StmtLabel, Label: thenLabel},
	)
	body, ok := l.lowerBlock(stmt.Body, ifScope)
	if !ok {
		return nil, false
	}
	out = append(out, body...)
	out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: endLabel})
	return out, true
}

func (l *lowerer) lowerForWithLabel(stmt ast.Statement, scope *funcScope, userLabel string) ([]ir.Statement, bool) {
	loopScope := l.childScope(scope)
	condLabel := l.newLabel("for.cond")
	bodyLabel := l.newLabel("for.body")
	postLabel := l.newLabel("for.post")
	endLabel := l.newLabel("for.end")
	var out []ir.Statement
	if stmt.Init != nil {
		init, ok := l.lowerStatement(*stmt.Init, loopScope)
		if !ok {
			return nil, false
		}
		out = append(out, init...)
	}
	out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: condLabel})
	if stmt.Cond != nil {
		cond, ok := l.lowerExpression(*stmt.Cond, loopScope)
		if !ok {
			return nil, false
		}
		out = append(out,
			ir.Statement{Kind: ir.StmtJumpIf, Expr: cond, Label: bodyLabel},
			ir.Statement{Kind: ir.StmtJump, Label: endLabel},
			ir.Statement{Kind: ir.StmtLabel, Label: bodyLabel},
		)
	}
	l.pushBranchWithUserLabel(userLabel, endLabel, postLabel)
	body, ok := l.lowerBlock(stmt.Body, loopScope)
	l.popBranch()
	if !ok {
		return nil, false
	}
	out = append(out, body...)
	out = append(out, ir.Statement{Kind: ir.StmtLabel, Label: postLabel})
	if stmt.Post != nil {
		post, ok := l.lowerStatement(*stmt.Post, loopScope)
		if !ok {
			return nil, false
		}
		out = append(out, post...)
	}
	out = append(out,
		ir.Statement{Kind: ir.StmtJump, Label: condLabel},
		ir.Statement{Kind: ir.StmtLabel, Label: endLabel},
	)
	return out, true
}
