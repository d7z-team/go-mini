package runtime

import (
	"fmt"
	"runtime"
	"sync/atomic"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) evalFFI(session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) (*Var, error) {
	if route.MethodID == 0 && route.FuncSig == nil {
		return nil, fmt.Errorf("ffi route %s uses Invoke without schema", route.Name)
	}
	buf := ffigo.GetBuffer()
	defer ffigo.ReleaseBuffer(buf)

	funcSig := route.FuncSig
	copyBackTargets, err := e.resolveFFICopyBackTargets(session, funcSig, args, argLHS)
	if err != nil {
		return nil, err
	}

	// 序列化参数
	if funcSig != nil && funcSig.Variadic {
		// 1. 序列化常规参数
		numNormal := len(funcSig.ParamTypes) - 1
		for i := 0; i < numNormal; i++ {
			arg := &Var{VType: TypeAny} // 默认
			if i < len(args) {
				arg = args[i]
			}
			if err := e.serializeRuntimeTypeForBridge(buf, arg, funcSig.ParamTypes[i], route.Bridge); err != nil {
				return nil, err
			}
		}

		// 2. 序列化变长参数部分：[Count (Uvarint)] [Item1] [Item2]...
		numVariadic := 0
		if len(args) > numNormal {
			numVariadic = len(args) - numNormal
		}
		buf.WriteUvarint(uint64(numVariadic))
		itemType := funcSig.ParamTypes[numNormal]
		if numVariadic > 0 {
			for i := 0; i < numVariadic; i++ {
				if err := e.serializeRuntimeTypeForBridge(buf, args[numNormal+i], itemType, route.Bridge); err != nil {
					return nil, err
				}
			}
		}
	} else {
		// 普通非变长函数序列化
		for i, arg := range args {
			argType := RuntimeType{Kind: RuntimeTypeAny, Raw: SpecAny, TypeID: CanonicalTypeID(string(SpecAny))}
			if funcSig != nil && i < len(funcSig.ParamTypes) {
				argType = funcSig.ParamTypes[i]
			}
			if err := e.serializeRuntimeTypeForBridge(buf, arg, argType, route.Bridge); err != nil {
				return nil, err
			}
		}
	}

	ownedArgs := append([]byte(nil), buf.Bytes()...)
	var ret ffigo.FFIReturn
	err = nil
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("FFI panic: %v", r)
				err = e.newPanicError(session, msg)
			}
		}()
		if route.Bridge == nil {
			err = fmt.Errorf("ffi route %s has no bridge", route.Name)
			return
		}
		req := &ffigo.FFICallRequest{
			MethodID: route.MethodID,
			Method:   route.Name,
			Args:     ownedArgs,
			Channels: e.channelRegistry(),
		}
		if route.MethodID == 0 {
			ret, err = route.Bridge.Invoke(session.Context, req)
		} else {
			ret, err = route.Bridge.Call(session.Context, req)
		}
		runtime.KeepAlive(args) // 关键：确保参数在调用期间不被回收
	}()

	if err != nil {
		return nil, e.wrapFFIError(session, err)
	}

	switch v := ret.(type) {
	case nil:
		return e.finishFFI(session, route, copyBackTargets, nil, nil)
	case []byte:
		return e.finishFFI(session, route, copyBackTargets, v, nil)
	case ffigo.AsyncCall:
		scheduler := e.currentScheduler()
		if scheduler == nil || scheduler.Current() == nil {
			return nil, fmt.Errorf("ffi route %s suspended without active VM scheduler", route.Name)
		}
		run := e.currentRun()
		if run == nil || run.Events == nil {
			return nil, fmt.Errorf("ffi route %s suspended without active VM event loop", route.Name)
		}
		resumeData := &ResumeFFIData{
			Route:           route,
			CopyBackTargets: copyBackTargets,
		}
		token, err := scheduler.PrepareFFI(Task{Op: OpResumeFFI, Data: resumeData})
		if err != nil {
			return nil, err
		}
		sink := &ffiEventCompletionSink{events: run.Events, token: token}
		wait, err := v.StartWire(session.Context, sink)
		if err != nil {
			scheduler.AbortFFI(token)
			return nil, e.wrapFFIError(session, err)
		}
		if wait == nil && !sink.Completed() {
			scheduler.AbortFFI(token)
			return nil, fmt.Errorf("async FFI route %s returned no wait handle", route.Name)
		}
		if err := scheduler.CommitFFI(token, wait); err != nil {
			if wait != nil {
				wait.Cancel()
			}
			scheduler.AbortFFI(token)
			return nil, err
		}
		return nil, errExecutionContextSuspend
	default:
		return nil, fmt.Errorf("ffi route %s returned unsupported payload %T", route.Name, ret)
	}
}

type ffiEventCompletionSink struct {
	events    *VMEventLoop
	token     uint64
	completed atomic.Bool
}

func (s *ffiEventCompletionSink) CompleteWire(ret []byte, err error) bool {
	if s.events == nil {
		return false
	}
	owned := append([]byte(nil), ret...)
	if err := s.events.Post(VMEvent{Kind: VMEventAsyncFFIComplete, Data: &VMAsyncFFICompleteEvent{
		Token: s.token,
		Ret:   owned,
		Err:   err,
	}}); err != nil {
		return false
	}
	s.completed.Store(true)
	return true
}

func (s *ffiEventCompletionSink) Completed() bool {
	return s != nil && s.completed.Load()
}

func (e *Executor) wrapFFIError(session *StackContext, err error) error {
	if vme, ok := err.(*VMError); ok {
		vme.IsPanic = true
		return vme
	}
	var frames []StackFrame
	if session != nil {
		frames = session.GenerateStackTrace(nil)
	}
	stackErr := wrapErrorWithStack(err, frames)
	return &VMError{
		Message: stackErr.Error(),
		Value:   newErrorVar(stackErr),
		Frames:  frames,
		IsPanic: true,
		Cause:   err,
	}
}

func (e *Executor) finishFFI(session *StackContext, route FFIRoute, copyBackTargets []ffiCopyBackTarget, retData []byte, callErr error) (*Var, error) {
	if callErr != nil {
		return nil, e.wrapFFIError(session, callErr)
	}
	funcSig := route.FuncSig
	if len(retData) == 0 {
		if len(copyBackTargets) > 0 {
			return nil, fmt.Errorf("ffi route %s returned empty payload for inout copy-back", route.Name)
		}
		return nil, nil
	}

	reader := ffigo.NewReader(retData)
	if len(copyBackTargets) > 0 {
		copyBackCount, err := reader.ReadCount(ffigo.MaxWireCollectionItems, "copy-back")
		if err != nil {
			return nil, fmt.Errorf("ffi route %s returned invalid payload: %w", route.Name, err)
		}
		if copyBackCount != len(copyBackTargets) {
			return nil, fmt.Errorf("ffi route %s returned %d copy-back values, want %d", route.Name, copyBackCount, len(copyBackTargets))
		}
		for i, target := range copyBackTargets {
			if err := e.applyFFICopyBack(session, route.Bridge, target, reader); err != nil {
				return nil, fmt.Errorf("ffi route %s copy-back[%d]: %w", route.Name, i, err)
			}
		}
	}
	var (
		res *Var
		err error
	)
	if funcSig != nil {
		res, err = e.deserializeRuntimeType(session, reader, funcSig.ReturnType, route.Bridge)
	} else {
		res, err = e.deserializeRuntimeType(session, reader, RuntimeType{Kind: RuntimeTypeAny, Raw: "Any"}, route.Bridge)
	}
	if err != nil {
		if readErr := reader.Err(); readErr != nil {
			return nil, fmt.Errorf("ffi route %s returned invalid payload: %w", route.Name, readErr)
		}
		return nil, err
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("ffi route %s returned invalid payload: %w", route.Name, err)
	}
	return res, nil
}
