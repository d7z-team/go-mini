package runtime

import "context"

type SlotFrame struct {
	Locals       []*Slot
	LocalNames   []string
	LocalIndex   map[string]int
	Upvalues     []*Slot
	UpvalueNames []string
	UpvalueIndex map[string]int
	Return       *Slot
	ReturnName   string
}

type Stack struct {
	Parent    *Stack
	MemoryPtr map[string]*Slot
	Symbols   map[string]SymbolRef
	Frame     *SlotFrame
	FrameBase *SlotFrame
	FrameSync int
	Scope     string
	interrupt string
	Depth     int

	// DeferOwner points at the function activation that owns function-scoped
	// defers. Lexical child scopes inherit the same owner so defer lifetime is
	// tied to the surrounding function instead of a transient block/loop scope.
	DeferOwner *Stack
	DeferStack []func()
}

func (s *Stack) CurrentDeferOwner() *Stack {
	if s == nil {
		return nil
	}
	if s.DeferOwner != nil {
		return s.DeferOwner
	}
	return s
}

func (s *Stack) AddDefer(fn func()) {
	owner := s.CurrentDeferOwner()
	if owner == nil {
		return
	}
	owner.DeferStack = append(owner.DeferStack, fn)
}

func (s *Stack) RunDefers() {
	// Defers append tasks onto TaskStack, so iterate forward here to preserve
	// source-level LIFO execution once the VM pops those tasks back off.
	for i := 0; i < len(s.DeferStack); i++ {
		s.DeferStack[i]()
	}
	s.DeferStack = nil
}

type StackContext struct {
	// Context is the host-provided context, strictly for FFI use.
	// VM kernel should check the run controller for pause/resume semantics.
	Context    context.Context
	Stack      *Stack
	Controller *RunController

	PanicVar       *Var         // 用于存储当前 VM 执行上下文中正在冒泡的 panic 对象
	PanicMessage   string       // 存储发生 panic 时的文本消息
	PanicTrace     []StackFrame // 存储发生 panic 时的原始堆栈信息，避免 unwind 期间 TaskStack 被清空导致丢失
	Executor       *Executor
	Shared         *SharedState
	ImportChain    map[string]bool
	OwnsSharedInit bool

	// 运行时状态 (Session State)

	StepCount int64
	StepLimit int64

	Debugger        Debugger
	DebugFrameDepth int

	// 迭代执行器状态 (Iterative Executor State)
	TaskStack   []Task
	ValueStack  *ValueStack
	LHSStack    *LHSStack
	CurrentTask *Task
	UnwindMode  UnwindMode

	// skippedScopeEnters tracks scope-enter tasks skipped during any unwind so
	// their orphaned scope-exit tasks are skipped as well.
	skippedScopeEnters int
}

func (ctx *StackContext) Done() <-chan struct{} {
	if ctx.Context != nil {
		return ctx.Context.Done()
	}
	return nil
}

func (ctx *StackContext) Err() error {
	if ctx == nil {
		return nil
	}
	if ctx.Context != nil {
		if err := ctx.Context.Err(); err != nil {
			return err
		}
	}
	if ctx.Controller != nil {
		return ctx.Controller.Err()
	}
	return nil
}

const (
	DefaultMaxStackDepth = 50000
)
