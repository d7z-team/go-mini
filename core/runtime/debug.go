package runtime

import "context"

type DebugPosition struct {
	ModulePath string
	F          string
	L          int
	C          int
	EL         int
	EC         int
}

type DebugStopReason string

const (
	DebugStopBreakpoint DebugStopReason = "breakpoint"
	DebugStopStep       DebugStopReason = "step"
)

type DebugEvent struct {
	RunID              uint64
	ExecutionContextID uint32
	Loc                *DebugPosition
	Reason             DebugStopReason
	FrameDepth         int
	Variables          map[string]string
}

type DebugBreakpoint struct {
	ModulePath string
	File       string
	Line       int
}

type DebugStepMode string

const (
	DebugStepInto DebugStepMode = "into"
	DebugStepOver DebugStepMode = "over"
)

type DebugStepRequest struct {
	RunID uint64
	Mode  DebugStepMode
}

type DebugPoint struct {
	RunID              uint64
	ExecutionContextID uint32
	Loc                DebugPosition
	FrameDepth         int
}

type DebugDecision struct {
	Stop      bool
	Reason    DebugStopReason
	ClearStep bool
}

type Debugger interface {
	Checkpoint(point DebugPoint) DebugDecision
	Publish(event *DebugEvent)
	RequestStep(req DebugStepRequest) error
	ClearPause(runID uint64)
	ClearStep(runID uint64)
	ClearRun(runID uint64)
}

type debuggerContextKey struct{}

func ContextWithDebugger(ctx context.Context, dbg Debugger) context.Context {
	if dbg == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, debuggerContextKey{}, dbg)
}

func DebuggerFromContext(ctx context.Context) Debugger {
	if ctx == nil {
		return nil
	}
	debugger, _ := ctx.Value(debuggerContextKey{}).(Debugger)
	return debugger
}
