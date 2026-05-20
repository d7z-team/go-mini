package runtime

import (
	"fmt"
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

// VMError is the unified error type for all go-mini runtime failures and panics.
type VMError struct {
	Message string          `json:"message"`
	Value   *Var            `json:"value,omitempty"` // Present if it's a panic(value)
	Frames  []StackFrame    `json:"frames"`
	IsPanic bool            `json:"is_panic"`
	Cause   error           `json:"-"` // Underlying Go error if any
	Handle  uint32          `json:"handle,omitempty"`
	Bridge  ffigo.FFIBridge `json:"-"`
}

func (e *VMError) Error() string {
	var sb strings.Builder
	if e.IsPanic {
		sb.WriteString("panic: ")
	}
	sb.WriteString(e.Message)
	if len(e.Frames) > 0 {
		sb.WriteString("\n\nvm execution context (mini) [running]:")
		for _, f := range e.Frames {
			// VSCode 终端匹配模式： path:line:col
			fmt.Fprintf(&sb, "\n%s()\n\t%s:%d:%d", f.Function, f.Filename, f.Line, f.Column)
		}
	}
	return sb.String()
}

func (e *VMError) Unwrap() error {
	return e.Cause
}
