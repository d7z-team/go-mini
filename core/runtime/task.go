package runtime

type OpCode int

const (
	OpDeclareVar OpCode = iota
	OpEvalLHS
	OpApplyBinary
	OpApplyUnary
	OpPop
	OpScopeEnter
	OpScopeExit
	OpAssign
	OpMultiAssign
	OpIncDec
	OpRunDefers
	OpScheduleDefer
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
	OpLoadLocal
	OpLoadUpvalue
	OpStoreLocal
	OpStoreUpvalue
	OpResumeUnwind
	OpImportInit
	OpImportDone
	OpSwitchTag
	OpSwitchNextCase
	OpSwitchMatchCase
	OpCatchScopeEnter
	OpInitVar
	OpAssert
	OpPush
	OpMakeClosure
	OpLineStep
)

func (op OpCode) String() string {
	switch op {
	case OpDeclareVar:
		return "DECLARE_VAR"
	case OpEvalLHS:
		return "EVAL_LHS"
	case OpApplyBinary:
		return "BINARY_OP"
	case OpApplyUnary:
		return "UNARY_OP"
	case OpPop:
		return "POP"
	case OpScopeEnter:
		return "SCOPE_ENTER"
	case OpScopeExit:
		return "SCOPE_EXIT"
	case OpAssign:
		return "ASSIGN"
	case OpMultiAssign:
		return "MULTI_ASSIGN"
	case OpIncDec:
		return "INC_DEC"
	case OpRunDefers:
		return "RUN_DEFERS"
	case OpScheduleDefer:
		return "SCHEDULE_DEFER"
	case OpFinally:
		return "FINALLY"
	case OpCatchBoundary:
		return "CATCH_BOUNDARY"
	case OpLoopBoundary:
		return "LOOP_BOUNDARY"
	case OpLoopContinue:
		return "LOOP_CONTINUE"
	case OpForCond:
		return "FOR_COND"
	case OpForScopeEnter:
		return "FOR_SCOPE_ENTER"
	case OpForScopeExit:
		return "FOR_SCOPE_EXIT"
	case OpRangeInit:
		return "RANGE_INIT"
	case OpRangeIter:
		return "RANGE_ITER"
	case OpRangeScopeEnter:
		return "RANGE_SCOPE_ENTER"
	case OpCallBoundary:
		return "CALL_BOUNDARY"
	case OpJumpIf:
		return "JUMP_IF"
	case OpBranchIf:
		return "BRANCH_IF"
	case OpDoCall:
		return "DO_CALL"
	case OpCall:
		return "CALL"
	case OpReturn:
		return "RETURN"
	case OpInterrupt:
		return "INTERRUPT"
	case OpComposite:
		return "COMPOSITE"
	case OpIndex:
		return "INDEX"
	case OpSlice:
		return "SLICE"
	case OpMember:
		return "MEMBER"
	case OpLoadVar:
		return "LOAD_VAR"
	case OpLoadLocal:
		return "LOAD_LOCAL"
	case OpLoadUpvalue:
		return "LOAD_UPVALUE"
	case OpStoreLocal:
		return "STORE_LOCAL"
	case OpStoreUpvalue:
		return "STORE_UPVALUE"
	case OpResumeUnwind:
		return "RESUME_UNWIND"
	case OpImportInit:
		return "IMPORT_INIT"
	case OpImportDone:
		return "IMPORT_DONE"
	case OpSwitchTag:
		return "SWITCH_TAG"
	case OpSwitchNextCase:
		return "SWITCH_NEXT_CASE"
	case OpSwitchMatchCase:
		return "SWITCH_MATCH_CASE"
	case OpCatchScopeEnter:
		return "CATCH_SCOPE_ENTER"
	case OpInitVar:
		return "INIT_VAR"
	case OpAssert:
		return "ASSERT"
	case OpPush:
		return "PUSH"
	case OpMakeClosure:
		return "MAKE_CLOSURE"
	case OpLineStep:
		return "LINE_STEP"
	default:
		return "UNKNOWN"
	}
}

type UnwindMode int

const (
	UnwindNone UnwindMode = iota
	UnwindPanic
	UnwindReturn
	UnwindBreak
	UnwindContinue
)

type SourceRef struct {
	ID          string
	Meta        string
	File        string
	Line        int
	Col         int
	IsStmtStart bool
}

type Task struct {
	Op     OpCode
	Source *SourceRef
	Data   interface{}
}

type DeclareVarData struct {
	Name string
	Kind RuntimeType
	Sym  SymbolRef
}

type SymbolKind uint8

const (
	SymbolUnknown SymbolKind = iota
	SymbolGlobal
	SymbolLocal
	SymbolUpvalue
	SymbolBuiltin
)

type SymbolRef struct {
	Name string
	Kind SymbolKind
	Slot int
}

type BranchData struct {
	Then []Task
	Else []Task
}

type DeferData struct {
	Tasks     []Task
	PopResult bool
}

type ForData struct {
	Cond   []Task
	Body   []Task
	Update []Task
}

type JumpData struct {
	Operator string
	Right    []Task
}

type IndexData struct {
	Multi      bool
	ResultType RuntimeType
}

type SliceData struct {
	HasLow  bool
	HasHigh bool
}

type AssertData struct {
	TargetType RuntimeType
	Multi      bool
	ResultType RuntimeType
}

type ImportInitData struct {
	Path string
}

type SwitchCaseData struct {
	Exprs     [][]Task
	TypeNames []RuntimeType
	Body      []Task
}

type SwitchData struct {
	IsType      bool
	HasTag      bool
	HasAssign   bool
	Init        []Task
	Tag         []Task
	AssignLHS   []Task
	Cases       []SwitchCaseData
	DefaultBody []Task
}

type SwitchState struct {
	Plan   *SwitchData
	Tag    *Var
	Index  int
	ExprIx int
}

type FinallyData struct {
	Body []Task
}

type CatchData struct {
	VarName string
	Sym     SymbolRef
	Body    []Task
}

type CompositeEntryData struct {
	IdentKey   string
	HasExprKey bool
}

type CompositeData struct {
	Type    RuntimeType
	Entries []CompositeEntryData
}

type CallMode int

const (
	CallByValue CallMode = iota
	CallByName
	CallByMember
)

type CallData struct {
	Mode          CallMode
	Name          string
	ArgCount      int
	Ellipsis      bool
	Sym           SymbolRef
	CaptureArgLHS bool
}

type DoCallData struct {
	Name         string
	FunctionSig  *RuntimeFuncSig
	BodyTasks    []Task
	Args         []*Var
}

type CallBoundaryData struct {
	Name      string
	OldStack  *Stack
	OldExec   *Executor
	OldShared *SharedState
	HasReturn bool
	ValueBase int
	LHSBase   int
}

type CatchScopeData struct {
	Catch *CatchData
	Panic *Var
}

type RangeData struct {
	Key    string
	Value  string
	KeySym SymbolRef
	ValSym SymbolRef
	Define bool
	Body   []Task
	Obj    *Var
	Keys   []string // For map
	Length int      // For array
	Index  int
}

type ImportData struct {
	Path          string
	OldExecutor   *Executor
	OldStack      *Stack
	OldTaskStack  []Task
	OldValueStack *ValueStack
	OldLHSStack   *LHSStack
	ModSession    *StackContext
}

type RangeScopeData struct {
	Range *RangeData
	Key   *Var
	Val   *Var
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

func (vs *ValueStack) Truncate(n int) {
	if n < 0 {
		n = 0
	}
	if n > len(vs.data) {
		return
	}
	vs.data = vs.data[:n]
}

// LHSStack stores assignment targets separately from expression values.
type LHSStack struct {
	data []LHSValue
}

func (ls *LHSStack) Push(v LHSValue) {
	ls.data = append(ls.data, v)
}

func (ls *LHSStack) Pop() LHSValue {
	if len(ls.data) == 0 {
		return nil
	}
	v := ls.data[len(ls.data)-1]
	ls.data = ls.data[:len(ls.data)-1]
	return v
}

func (ls *LHSStack) Peek() LHSValue {
	if len(ls.data) == 0 {
		return nil
	}
	return ls.data[len(ls.data)-1]
}

func (ls *LHSStack) Len() int {
	return len(ls.data)
}

func (ls *LHSStack) Clear() {
	ls.data = ls.data[:0]
}

func (ls *LHSStack) Truncate(n int) {
	if n < 0 {
		n = 0
	}
	if n > len(ls.data) {
		return
	}
	ls.data = ls.data[:n]
}

// Data structures for Task Data field

// LHS resolution type
type LHSType int

const (
	LHSTypeEnv LHSType = iota
	LHSTypeIndex
	LHSTypeMember
	LHSTypeStar
	LHSTypeNone
)

// LHS Descriptors
type LHSEnv struct {
	Name string
	Sym  SymbolRef
}

type LHSValue interface {
	isLHSValue()
}

func (*LHSEnv) isLHSValue()    {}
func (*LHSIndex) isLHSValue()  {}
func (*LHSMember) isLHSValue() {}
func (*LHSDeref) isLHSValue()  {}

type LHSData struct {
	Kind     LHSType
	Name     string
	Property string
	Sym      SymbolRef
}

type ClosureData struct {
	FunctionSig  *RuntimeFuncSig
	BodyTasks    []Task
	CaptureNames []string
	CaptureRefs  []SymbolRef
}

type LoadVarData struct {
	Name string
	Sym  SymbolRef
}

type LHSIndex struct {
	Obj   *Var
	Index *Var
}

type LHSMember struct {
	Obj      *Var
	Property string
}

type LHSDeref struct {
	Target *Var
}

// Assignment Data
type AssignData struct {
	LHSCount int
	RHSCount int
}
