package compiler

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/bytecode"
	"gopkg.d7z.net/go-mini/core/runtime"
)

const (
	// Pseudo ops are bytecode-only annotations. They are not executable runtime IR.
	pseudoOpLoadConst = "PSEUDO_LOAD_CONST"
)

func buildBytecode(program *ast.ProgramStmt, globalInitOrder []ast.Ident) *bytecode.Program {
	if program == nil {
		return nil
	}

	builder := &bytecodeBuilder{program: program}
	bc := bytecode.NewProgram()
	prepared, err := runtime.PrepareProgram(program)
	if err != nil {
		return nil
	}
	bc.Executable = prepared

	for _, name := range globalInitOrder {
		expr, ok := program.Variables[name]
		if !ok || expr == nil {
			continue
		}
		code, ok := builder.compileExpr(expr)
		if !ok {
			return nil
		}
		bc.Globals = append(bc.Globals, bytecode.Global{Name: string(name), Instructions: code})
	}

	entry, ok := builder.compileStatements(program.Main)
	if !ok {
		return nil
	}
	bc.Entry = entry

	keys := make([]string, 0, len(program.Functions))
	for name := range program.Functions {
		keys = append(keys, string(name))
	}
	sort.Strings(keys)

	for _, name := range keys {
		fn := program.Functions[ast.Ident(name)]
		if fn == nil {
			continue
		}
		code, ok := builder.compileStatements([]ast.Stmt{fn.Body})
		if !ok {
			return nil
		}
		bc.Functions = append(bc.Functions, bytecode.Function{
			Name:         name,
			Signature:    formatFunctionSignature(fn),
			Instructions: code,
		})
	}

	return bc
}

type bytecodeBuilder struct {
	program *ast.ProgramStmt
}

func (b *bytecodeBuilder) compileStatements(stmts []ast.Stmt) ([]bytecode.Instruction, bool) {
	out := make([]bytecode.Instruction, 0)
	for _, stmt := range stmts {
		code, ok := b.compileStmt(stmt)
		if !ok {
			return nil, false
		}
		out = append(out, code...)
	}
	return out, true
}

func (b *bytecodeBuilder) compileStmt(stmt ast.Stmt) ([]bytecode.Instruction, bool) {
	switch n := stmt.(type) {
	case nil:
		return nil, true
	case *ast.BlockStmt:
		return b.compileStatements(n.Children)
	case *ast.GenDeclStmt:
		return nil, true
	case *ast.AssignmentStmt:
		lhs, ok := b.compileLHS(n.LHS)
		if !ok {
			return nil, false
		}
		rhs, ok := b.compileExpr(n.Value)
		if !ok {
			return nil, false
		}
		assign := []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpAssign, "", "Assignment")}
		return append(append(lhs, rhs...), assign...), true
	case *ast.MultiAssignmentStmt:
		out := make([]bytecode.Instruction, 0)
		for _, lhs := range n.LHS {
			code, ok := b.compileLHS(lhs)
			if !ok {
				return nil, false
			}
			out = append(out, code...)
		}
		val, ok := b.compileExpr(n.Value)
		if !ok {
			return nil, false
		}
		out = append(out, val...)
		out = append(out, b.runtimeInstruction(n, runtime.OpMultiAssign, fmt.Sprintf("%d", len(n.LHS)), "Multiple assignment"))
		return out, true
	case *ast.IncDecStmt:
		lhs, ok := b.compileLHS(n.Operand)
		if !ok {
			return nil, false
		}
		return append(lhs, b.runtimeInstruction(n, runtime.OpIncDec, string(n.Operator), "Inc/Dec")), true
	case *ast.ReturnStmt:
		out := make([]bytecode.Instruction, 0)
		for _, expr := range n.Results {
			code, ok := b.compileExpr(expr)
			if !ok {
				return nil, false
			}
			out = append(out, code...)
		}
		out = append(out, b.runtimeInstruction(n, runtime.OpReturn, fmt.Sprintf("%d", len(n.Results)), fmt.Sprintf("Return %d values", len(n.Results))))
		return out, true
	case *ast.IfStmt:
		cond, ok := b.compileExpr(n.Cond)
		if !ok {
			return nil, false
		}
		out := append([]bytecode.Instruction{}, cond...)
		comment := "Branch if false to END"
		if n.ElseBody != nil {
			comment = "Branch if false to ELSE"
		}
		out = append(out, b.runtimeInstruction(n, runtime.OpBranchIf, "", comment))
		body, ok := b.compileStmt(n.Body)
		if !ok {
			return nil, false
		}
		out = append(out, body...)
		if n.ElseBody != nil {
			elseCode, ok := b.compileStmt(n.ElseBody)
			if !ok {
				return nil, false
			}
			out = append(out, elseCode...)
		}
		return out, true
	case *ast.ExpressionStmt:
		return b.compileExpr(n.X)
	case *ast.CallExprStmt:
		return b.compileExpr(n)
	case *ast.InterruptStmt:
		return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpInterrupt, n.InterruptType, "Interrupt")}, true
	default:
		return nil, false
	}
}

func (b *bytecodeBuilder) compileExpr(expr ast.Expr) ([]bytecode.Instruction, bool) {
	switch n := expr.(type) {
	case nil:
		return nil, true
	case *ast.LiteralExpr:
		return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpPush, formatLiteral(n.Value), "literal")}, true
	case *ast.IdentifierExpr:
		return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpLoadVar, string(n.Name), "identifier")}, true
	case *ast.ConstRefExpr:
		if val, ok := b.program.Constants[string(n.Name)]; ok {
			return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpPush, formatLiteral(val), "const")}, true
		}
		return []bytecode.Instruction{b.instruction(n, pseudoOpLoadConst, string(n.Name), "const")}, true
	case *ast.BinaryExpr:
		left, ok := b.compileExpr(n.Left)
		if !ok {
			return nil, false
		}
		right, ok := b.compileExpr(n.Right)
		if !ok {
			return nil, false
		}
		out := append([]bytecode.Instruction{}, left...)
		out = append(out, right...)
		out = append(out, b.runtimeInstruction(n, runtime.OpApplyBinary, string(n.Operator), ""))
		return out, true
	case *ast.UnaryExpr:
		operand, ok := b.compileExpr(n.Operand)
		if !ok {
			return nil, false
		}
		return append(operand, b.runtimeInstruction(n, runtime.OpApplyUnary, string(n.Operator), "")), true
	case *ast.CallExprStmt:
		out := make([]bytecode.Instruction, 0)
		if member, ok := n.Func.(*ast.MemberExpr); ok {
			recv, ok := b.compileExpr(member.Object)
			if !ok {
				return nil, false
			}
			out = append(out, recv...)
		} else if _, ok := n.Func.(*ast.IdentifierExpr); !ok {
			if _, ok := n.Func.(*ast.ConstRefExpr); !ok {
				fn, ok := b.compileExpr(n.Func)
				if !ok {
					return nil, false
				}
				out = append(out, fn...)
			}
		}

		for _, arg := range n.Args {
			code, ok := b.compileExpr(arg)
			if !ok {
				return nil, false
			}
			out = append(out, code...)
		}

		out = append(out, b.runtimeInstruction(n, runtime.OpCall, b.callName(n.Func), "Call "+b.callName(n.Func)))
		return out, true
	case *ast.MemberExpr:
		obj, ok := b.compileExpr(n.Object)
		if !ok {
			return nil, false
		}
		return append(obj, b.runtimeInstruction(n, runtime.OpMember, string(n.Property), "member access")), true
	case *ast.IndexExpr:
		obj, ok := b.compileExpr(n.Object)
		if !ok {
			return nil, false
		}
		idx, ok := b.compileExpr(n.Index)
		if !ok {
			return nil, false
		}
		out := append([]bytecode.Instruction{}, obj...)
		out = append(out, idx...)
		out = append(out, b.runtimeInstruction(n, runtime.OpIndex, "", "index access"))
		return out, true
	case *ast.SliceExpr:
		out, ok := b.compileExpr(n.X)
		if !ok {
			return nil, false
		}
		if n.Low != nil {
			code, ok := b.compileExpr(n.Low)
			if !ok {
				return nil, false
			}
			out = append(out, code...)
		}
		if n.High != nil {
			code, ok := b.compileExpr(n.High)
			if !ok {
				return nil, false
			}
			out = append(out, code...)
		}
		out = append(out, b.runtimeInstruction(n, runtime.OpSlice, "", "slice"))
		return out, true
	case *ast.TypeAssertExpr:
		out, ok := b.compileExpr(n.X)
		if !ok {
			return nil, false
		}
		return append(out, b.runtimeInstruction(n, runtime.OpAssert, string(n.Type), "type assertion")), true
	case *ast.StarExpr:
		out, ok := b.compileExpr(n.X)
		if !ok {
			return nil, false
		}
		return append(out, b.runtimeInstruction(n, runtime.OpApplyUnary, "Dereference", "")), true
	case *ast.ImportExpr:
		return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpImportInit, n.Path, "import")}, true
	default:
		return nil, false
	}
}

func (b *bytecodeBuilder) compileLHS(expr ast.Expr) ([]bytecode.Instruction, bool) {
	switch n := expr.(type) {
	case *ast.IdentifierExpr:
		return []bytecode.Instruction{b.runtimeInstruction(n, runtime.OpEvalLHS, string(n.Name), "identifier")}, true
	case *ast.MemberExpr:
		obj, ok := b.compileExpr(n.Object)
		if !ok {
			return nil, false
		}
		return append(obj, b.runtimeInstruction(n, runtime.OpEvalLHS, string(n.Property), "member target")), true
	case *ast.IndexExpr:
		obj, ok := b.compileExpr(n.Object)
		if !ok {
			return nil, false
		}
		idx, ok := b.compileExpr(n.Index)
		if !ok {
			return nil, false
		}
		out := append([]bytecode.Instruction{}, obj...)
		out = append(out, idx...)
		out = append(out, b.runtimeInstruction(n, runtime.OpEvalLHS, "[]", "index target"))
		return out, true
	case *ast.StarExpr:
		out, ok := b.compileExpr(n.X)
		if !ok {
			return nil, false
		}
		return append(out, b.runtimeInstruction(n, runtime.OpEvalLHS, "*", "deref target")), true
	default:
		return nil, false
	}
}

func (b *bytecodeBuilder) runtimeInstruction(node ast.Node, op runtime.OpCode, operand, comment string) bytecode.Instruction {
	return b.instruction(node, op.String(), operand, comment)
}

func (b *bytecodeBuilder) instruction(node ast.Node, op, operand, comment string) bytecode.Instruction {
	inst := bytecode.Instruction{
		Op:      op,
		Operand: operand,
		Comment: comment,
	}
	if node != nil {
		base := node.GetBase()
		inst.NodeID = base.ID
		if base.Loc != nil {
			inst.Loc = &bytecode.Location{
				File:   base.Loc.F,
				Line:   base.Loc.L,
				Column: base.Loc.C,
			}
		}
	}
	return inst
}

func (b *bytecodeBuilder) callName(expr ast.Expr) string {
	switch fn := expr.(type) {
	case *ast.IdentifierExpr:
		return string(fn.Name)
	case *ast.ConstRefExpr:
		return string(fn.Name)
	case *ast.MemberExpr:
		return string(fn.Property)
	default:
		return "anonymous"
	}
}

func formatFunctionSignature(fn *ast.FunctionStmt) string {
	if fn == nil {
		return ":"
	}
	params := make([]string, 0, len(fn.Params))
	for i, p := range fn.Params {
		prefix := ""
		if fn.Variadic && i == len(fn.Params)-1 {
			prefix = "..."
		}
		params = append(params, prefix+string(p.Type))
	}
	return fmt.Sprintf("(%s) %s", strings.Join(params, ","), fn.Return)
}

func formatLiteral(raw string) string {
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		return raw
	}
	if strings.ContainsAny(raw, " \t") {
		return fmt.Sprintf("%q", raw)
	}
	return raw
}
