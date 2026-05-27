package runtime

import "context"

type DebugPosition struct {
	F  string
	L  int
	C  int
	EL int
	EC int
}

type DebugEvent struct {
	RunID              uint64
	ExecutionContextID uint32
	Loc                *DebugPosition
	Variables          map[string]string
}

type Debugger interface {
	ShouldTrigger(runID uint64, line int) bool
	Publish(event *DebugEvent)
	RequestStep(runID uint64)
	ClearStep(runID uint64)
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
