package runtime

import (
	"gopkg.d7z.net/go-mini/core/ast"
)

type OpCode int

const (
	OpExec OpCode = iota
	OpEval
	OpEvalLHS
	OpEvalLHSIndex
	OpEvalLHSMember
	OpApplyBinary
	OpApplyUnary
	OpPop
	OpScopeEnter
	OpScopeExit
	OpAssign
	OpMultiAssign
	OpIncDec
	OpRunDefers
	OpFinally
	OpCatchBoundary
	OpLoopBoundary
	OpLoopContinue
	OpForCond
	OpForScopeEnter
	OpForScopeExit
	OpRangeInit
	OpRangeIter
	OpRangeScopeEnter
	OpCallBoundary
	OpJumpIf
	OpBranchIf
	OpDoCall
	OpCall
	OpReturn
	OpInterrupt
	OpComposite
	OpIndex
	OpSlice
	OpMember
	OpLoadVar
	OpResumeUnwind
	OpImportInit
	OpImportDone
	OpSwitchTag
	OpSwitchNextCase
	OpSwitchMatchCase
	OpCatchScopeEnter
	OpInitVar
)

type UnwindMode int

const (
	UnwindNone UnwindMode = iota
	UnwindPanic
	UnwindReturn
	UnwindBreak
	UnwindContinue
)

type Task struct {
	Op   OpCode
	Node ast.Node
	Data interface{}
}

type RangeData struct {
	Stmt   *ast.RangeStmt
	Obj    *Var
	Keys   []string // For map
	Length int      // For array
	Index  int
}

type ImportData struct {
	Path        string
	OldExecutor interface{}
	OldStack    *Stack
	OldTaskStack []Task
	OldValueStack *ValueStack
	ModSession  *StackContext
}

// ValueStack represents a stack of values for expression evaluation
type ValueStack struct {
	data []*Var
}

func (vs *ValueStack) Push(v *Var) {
	vs.data = append(vs.data, v)
}

func (vs *ValueStack) Pop() *Var {
	if len(vs.data) == 0 {
		return nil
	}
	v := vs.data[len(vs.data)-1]
	vs.data = vs.data[:len(vs.data)-1]
	return v
}

func (vs *ValueStack) Peek() *Var {
	if len(vs.data) == 0 {
		return nil
	}
	return vs.data[len(vs.data)-1]
}

func (vs *ValueStack) Len() int {
	return len(vs.data)
}

func (vs *ValueStack) Clear() {
	vs.data = vs.data[:0]
}

// Data structures for Task Data field

// LHS Descriptors
type LHS_Env struct {
	Name string
}

type LHS_Index struct {
	Obj   *Var
	Index *Var
}

type LHS_Member struct {
	Obj      *Var
	Property string
}

// Assignment Data
type AssignData struct {
	LHSCount int
	RHSCount int
}
