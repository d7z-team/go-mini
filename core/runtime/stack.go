package runtime

import (
	"context"
	"sync/atomic"

	"gopkg.d7z.net/go-mini/core/debugger"
)

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
	// VM kernel should check 'status' instead of Context.Err() for performance.
	Context context.Context
	Stack   *Stack

	// status represents the execution state (Fake Context)
	// 0: Running, 1: Aborted/Cancelled, 2: Paused
	status int32

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

	Debugger *debugger.Session

	// 迭代执行器状态 (Iterative Executor State)
	TaskStack  []Task
	ValueStack *ValueStack
	LHSStack   *LHSStack
	UnwindMode UnwindMode

	// skippedScopeEnters tracks scope-enter tasks skipped during any unwind so
	// their orphaned scope-exit tasks are skipped as well.
	skippedScopeEnters int

	// resumeSignal is used to unblock the execution loop after a pause.
	resumeSignal chan struct{}
}

func (ctx *StackContext) Abort() {
	atomic.StoreInt32(&ctx.status, 1)
}

func (ctx *StackContext) Aborted() bool {
	return atomic.LoadInt32(&ctx.status) == 1
}

func (ctx *StackContext) Pause() {
	atomic.CompareAndSwapInt32(&ctx.status, 0, 2)
}

func (ctx *StackContext) Resume() {
	if atomic.CompareAndSwapInt32(&ctx.status, 2, 0) {
		select {
		case ctx.resumeSignal <- struct{}{}:
		default:
		}
	}
}

func (ctx *StackContext) IsPaused() bool {
	return atomic.LoadInt32(&ctx.status) == 2
}

func (ctx *StackContext) Done() <-chan struct{} {
	if ctx.Context != nil {
		return ctx.Context.Done()
	}
	return nil
}

func (ctx *StackContext) Err() error {
	if ctx.Aborted() {
		if ctx.Context != nil {
			return ctx.Context.Err()
		}
		return context.Canceled
	}
	return nil
}

const (
	DefaultMaxStackDepth = 50000
)
