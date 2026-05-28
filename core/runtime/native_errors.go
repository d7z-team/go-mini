package runtime

import (
	goerrors "errors"
	"fmt"
)

func NativeErrorsNew(e *Executor, session *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	text := ""
	if len(args) > 0 && args[0] != nil {
		text = nativeStringArg(e, args[0])
	}
	return e.newStackErrorVar(session, goerrors.New(text)), nil
}

func NativeErrorsIs(_ *Executor, _ *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	var err, target error
	if len(args) > 0 {
		err = goErrorFromVar(args[0])
	}
	if len(args) > 1 {
		target = goErrorFromVar(args[1])
	}
	if sameGoError(err, target) {
		return NewBool(true), nil
	}
	if host := hostErrorFromError(target); host != nil && host.Err != nil {
		target = host.Err
	}
	return NewBool(goerrors.Is(err, target)), nil
}

func NativeErrorsAs(e *Executor, session *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	var err error
	if len(args) > 0 {
		err = goErrorFromVar(args[0])
	}
	if len(args) < 2 || args[1] == nil {
		return nil, e.newPanicError(session, "errors.As target must be a non-nil pointer")
	}
	target := e.unwrapValue(args[1])
	slot, ok := e.slotPointerSlot(target)
	if !ok || slot == nil {
		return nil, e.newPanicError(session, "errors.As target must be a non-nil pointer")
	}
	if err == nil {
		return NewBool(false), nil
	}
	if !slot.Decl.IsEmpty() && !slot.Decl.IsAny() && slot.Decl.Raw != SpecError {
		return nil, e.newPanicError(session, "errors.As target must point to Error or Any")
	}
	value := newErrorVar(err)
	if slot.Decl.IsAny() {
		value = e.wrapAnyVar(value)
	}
	if err := session.Assign(slot, value); err != nil {
		return nil, e.newPanicError(session, err.Error())
	}
	return NewBool(true), nil
}

func NativeErrorsUnwrap(_ *Executor, _ *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	if len(args) == 0 {
		return nil, nil
	}
	err := goErrorFromVar(args[0])
	if err == nil {
		return nil, nil
	}
	if next := goerrors.Unwrap(err); next != nil {
		return newErrorVar(next), nil
	}
	return nil, nil
}

func NativeErrorsStack(_ *Executor, _ *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	if len(args) == 0 {
		return NewString(""), nil
	}
	var stackErr *VMStackError
	if goerrors.As(goErrorFromVar(args[0]), &stackErr) && stackErr != nil {
		return NewString(stackErr.StackString()), nil
	}
	return NewString(""), nil
}

func NativeFmtErrorf(e *Executor, session *StackContext, _ FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	if len(args) == 0 {
		return nil, e.newPanicError(session, "fmt.Errorf requires a format string")
	}
	formatArg := e.unwrapValue(args[0])
	if formatArg == nil || formatArg.VType != TypeString {
		return nil, e.newPanicError(session, "fmt.Errorf requires a format string")
	}
	goArgs := make([]any, 0, len(args)-1)
	for _, arg := range args[1:] {
		goArgs = append(goArgs, nativeFormatArg(e, arg))
	}
	return e.newStackErrorVar(session, fmt.Errorf(formatArg.Str, goArgs...)), nil
}

func (e *Executor) evalRoute(session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) (*Var, error) {
	if route.Native != nil {
		return route.Native(e, session, route, args, argLHS)
	}
	return e.evalFFI(session, route, args, argLHS)
}

func (e *Executor) newStackErrorVar(session *StackContext, err error) *Var {
	return newErrorVar(wrapErrorWithStack(err, e.currentStackFrames(session)))
}

func (e *Executor) errorVarForPanic(session *StackContext, val *Var) *Var {
	if err := goErrorFromVar(e.unwrapValue(val)); err != nil {
		return newErrorVar(wrapErrorWithStack(err, e.currentStackFrames(session)))
	}
	text := "panic"
	if val != nil {
		if msg, err := val.ToError(); err == nil && msg != "" {
			text = msg
		}
	}
	return e.newStackErrorVar(session, goerrors.New(text))
}

func (e *Executor) newPanicError(session *StackContext, message string) *VMError {
	errVar := e.newStackErrorVar(session, goerrors.New(message))
	return &VMError{Message: message, Value: errVar, IsPanic: true}
}

func (e *Executor) currentStackFrames(session *StackContext) []StackFrame {
	if session == nil {
		return nil
	}
	return session.GenerateStackTrace(session.CurrentTask)
}

func nativeStringArg(e *Executor, v *Var) string {
	v = e.unwrapValue(v)
	if v == nil {
		return ""
	}
	if v.VType == TypeString {
		return v.Str
	}
	if text, err := v.ToError(); err == nil {
		return text
	}
	return fmt.Sprint(v.Interface())
}

func nativeFormatArg(e *Executor, v *Var) any {
	v = e.unwrapValue(v)
	if v == nil {
		return nil
	}
	switch v.VType {
	case TypeError:
		return goErrorFromVar(v)
	case TypeInt:
		return v.I64
	case TypeFloat:
		return v.F64
	case TypeString:
		return v.Str
	case TypeBytes:
		return v.B
	case TypeBool:
		return v.Bool
	default:
		return v.Interface()
	}
}
