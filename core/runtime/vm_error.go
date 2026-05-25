package runtime

import (
	"errors"
	"fmt"
	"reflect"
	goruntime "runtime"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

// StackFrame represents a single frame in the virtual machine's stack trace.
type StackFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// VMError is the execution-level envelope for runtime failures and panics.
// Script-visible Error values are ordinary Go errors stored in TypeError vars.
type VMError struct {
	Message string       `json:"message"`
	Value   *Var         `json:"value,omitempty"` // Present if it's a panic(value)
	Frames  []StackFrame `json:"frames"`
	IsPanic bool         `json:"is_panic"`
	Cause   error        `json:"-"` // Underlying Go error if any
}

func (e *VMError) Error() string {
	var sb strings.Builder
	if e.IsPanic {
		sb.WriteString("panic: ")
	}
	sb.WriteString(e.Message)
	sb.WriteString(formatStackFrames(e.Frames))
	return sb.String()
}

func (e *VMError) Unwrap() error {
	return e.Cause
}

// VMStackError wraps a Go error created by VM code with the VM stack captured
// at the creation site. It deliberately keeps Go's normal errors.Is/As chain.
type VMStackError struct {
	Err    error        `json:"-"`
	Frames []StackFrame `json:"frames,omitempty"`
}

func (e *VMStackError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *VMStackError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *VMStackError) StackString() string {
	if e == nil {
		return ""
	}
	return strings.TrimPrefix(formatStackFrames(e.Frames), "\n\n")
}

// VMHostError is the VM-visible wrapper for an error that originated in FFI.
// Handle/Bridge preserve host identity while Err, when available, preserves the
// original Go errors.Is/As chain through Unwrap.
type VMHostError struct {
	Message string          `json:"message"`
	Handle  uint32          `json:"handle,omitempty"`
	Bridge  ffigo.FFIBridge `json:"-"`
	Err     error           `json:"-"`
}

func (e *VMHostError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *VMHostError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *VMHostError) Is(target error) bool {
	if e == nil || target == nil || e.Handle == 0 {
		return false
	}
	var host *VMHostError
	if errors.As(target, &host) && host != nil {
		return e.Handle == host.Handle && sameRuntimeBridge(e.Bridge, host.Bridge)
	}
	return false
}

func newErrorVar(err error) *Var {
	if err == nil {
		return nil
	}
	res := &Var{VType: TypeError, Ref: err}
	res.SetRawType("Error")
	if host := hostErrorFromError(err); host != nil {
		res.Handle = host.Handle
		res.Bridge = host.Bridge
	}
	return res
}

func goErrorFromVar(v *Var) error {
	if v == nil {
		return nil
	}
	if v.VType == TypeAny {
		if inner, ok := v.Ref.(*Var); ok {
			return goErrorFromVar(inner)
		}
	}
	if v.VType != TypeError {
		return nil
	}
	if err, ok := v.Ref.(error); ok {
		return err
	}
	return nil
}

func hostErrorFromError(err error) *VMHostError {
	if err == nil {
		return nil
	}
	var host *VMHostError
	if errors.As(err, &host) {
		return host
	}
	return nil
}

func sameGoError(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	ah := hostErrorFromError(a)
	bh := hostErrorFromError(b)
	if ah != nil || bh != nil {
		return ah != nil && bh != nil && ah.Handle != 0 && ah.Handle == bh.Handle && sameRuntimeBridge(ah.Bridge, bh.Bridge)
	}
	at := reflect.TypeOf(a)
	if at == nil || at != reflect.TypeOf(b) || !at.Comparable() {
		return false
	}
	return a == b
}

func newHostErrorVar(data ffigo.ErrorData, bridge ffigo.FFIBridge) *Var {
	if data.Message == "" && data.Handle == 0 {
		return nil
	}
	host := &VMHostError{
		Message: data.Message,
		Handle:  data.Handle,
		Bridge:  bridge,
		Err:     lookupHostError(bridge, data.Handle),
	}
	if host.Handle != 0 && host.Bridge != nil {
		goruntime.SetFinalizer(host, func(h *VMHostError) {
			_ = h.Bridge.DestroyHandle(h.Handle)
		})
	}
	return newErrorVar(host)
}

func lookupHostError(bridge ffigo.FFIBridge, handle uint32) error {
	if handle == 0 {
		return nil
	}
	if router, ok := bridge.(*ffigo.RouterBridge); ok && router.Registry != nil {
		if obj, ok := router.Registry.Get(handle); ok {
			if err, ok := obj.(error); ok {
				return err
			}
		}
	}
	return nil
}

func wrapErrorWithStack(err error, frames []StackFrame) error {
	if err == nil {
		return nil
	}
	var stackErr *VMStackError
	if errors.As(err, &stackErr) && stackErr != nil && len(stackErr.Frames) > 0 {
		return err
	}
	return &VMStackError{Err: err, Frames: append([]StackFrame(nil), frames...)}
}

func formatStackFrames(frames []StackFrame) string {
	if len(frames) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nvm execution context (mini) [running]:")
	for _, f := range frames {
		// VSCode terminal matcher: path:line:col
		fmt.Fprintf(&sb, "\n%s()\n\t%s:%d:%d", f.Function, f.Filename, f.Line, f.Column)
	}
	return sb.String()
}
