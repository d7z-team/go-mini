package runtime

import (
	"fmt"
	"runtime"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

func (e *Executor) evalFFI(session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) (*Var, error) {
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
			if err := e.serializeRuntimeType(buf, arg, funcSig.ParamTypes[i]); err != nil {
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
				if err := e.serializeRuntimeType(buf, args[numNormal+i], itemType); err != nil {
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
			if err := e.serializeRuntimeType(buf, arg, argType); err != nil {
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
				err = &VMError{Value: NewString(msg), Message: msg, IsPanic: true}
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
		if e.scheduler == nil || e.scheduler.Current() == nil {
			return nil, fmt.Errorf("ffi route %s suspended without active VM scheduler", route.Name)
		}
		resumeData := &ResumeFFIData{
			Route:           route,
			CopyBackTargets: copyBackTargets,
		}
		token, sink, err := e.scheduler.PrepareFFI(Task{Op: OpResumeFFI, Data: resumeData})
		if err != nil {
			return nil, err
		}
		cancel, err := v.StartWire(session.Context, sink)
		if err != nil {
			e.scheduler.AbortFFI(token)
			return nil, e.wrapFFIError(session, err)
		}
		if err := e.scheduler.CommitFFI(token, cancel); err != nil {
			if cancel != nil {
				cancel()
			}
			e.scheduler.AbortFFI(token)
			return nil, err
		}
		return nil, errExecutionContextSuspend
	default:
		return nil, fmt.Errorf("ffi route %s returned unsupported payload %T", route.Name, ret)
	}
}

func (e *Executor) wrapFFIError(session *StackContext, err error) error {
	if vme, ok := err.(*VMError); ok {
		vme.IsPanic = true
		return vme
	}
	frames := session.GenerateStackTrace(nil)
	var stackStr strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&stackStr, "\n  #%d %s (%s:%d:%d)", i, f.Function, f.Filename, f.Line, f.Column)
	}
	return &VMError{
		Message: fmt.Sprintf("%v\n\nVM Stack Trace:%s", err.Error(), stackStr.String()),
		Value:   NewString(err.Error()),
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
		copyBackCount := int(reader.ReadUvarint())
		if copyBackCount != len(copyBackTargets) {
			return nil, fmt.Errorf("ffi route %s returned %d copy-back values, want %d", route.Name, copyBackCount, len(copyBackTargets))
		}
		for i, target := range copyBackTargets {
			if err := e.applyFFICopyBack(session, route.Bridge, target, reader); err != nil {
				return nil, fmt.Errorf("ffi route %s copy-back[%d]: %w", route.Name, i, err)
			}
		}
	}
	if funcSig != nil {
		return e.deserializeRuntimeType(session, reader, funcSig.ReturnType, route.Bridge)
	}
	return e.deserializeRuntimeType(session, reader, RuntimeType{Kind: RuntimeTypeAny, Raw: "Any"}, route.Bridge)
}
