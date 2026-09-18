package runtime

import (
	"errors"
	"fmt"
	"strings"
)

type ErrorKind string

const (
	ErrorRuntime   ErrorKind = "runtime"
	ErrorPanic     ErrorKind = "panic"
	ErrorBlocked   ErrorKind = "blocked"
	ErrorStepLimit ErrorKind = "step_limit"
	ErrorInternal  ErrorKind = "internal"
)

// ExecutionError is the protocol-neutral failure boundary for an artifact run.
// It contains only stable data; host errors and runtime-only values stay behind
// the runtime projection.
type ExecutionError struct {
	Kind               ErrorKind `json:"kind"`
	Code               string    `json:"code"`
	Message            string    `json:"message"`
	Generation         uint64    `json:"generation,omitempty"`
	ProgramHash        string    `json:"program_hash,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	RouteID            string    `json:"route_id,omitempty"`
	ModulePath         string    `json:"module_path,omitempty"`
	FunctionID         string    `json:"function_id,omitempty"`
	PC                 int       `json:"pc,omitempty"`
	Op                 string    `json:"op,omitempty"`
	ExecutionContextID int64     `json:"execution_context_id,omitempty"`
	Loc                *Location `json:"loc,omitempty"`
	Stack              []Frame   `json:"stack,omitempty"`
	Panic              *Value    `json:"panic,omitempty"`
}

func NewExecutionError(kind ErrorKind, code, message string) ExecutionError {
	return ExecutionError{
		Kind: kind, Code: strings.TrimSpace(code), Message: strings.TrimSpace(message),
	}
}

func (e ExecutionError) Validate() error {
	switch e.Kind {
	case ErrorRuntime, ErrorPanic, ErrorBlocked, ErrorStepLimit, ErrorInternal:
	default:
		return fmt.Errorf("unsupported execution error kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Code) == "" {
		return errors.New("execution error code is required")
	}
	if strings.TrimSpace(e.Message) == "" {
		return errors.New("execution error message is required")
	}
	if e.PC < 0 {
		return errors.New("execution error pc must be non-negative")
	}
	if e.ExecutionContextID < 0 {
		return errors.New("execution error execution_context_id must be non-negative")
	}
	if e.Loc != nil && (e.Loc.Line < 0 || e.Loc.Column < 0) {
		return errors.New("execution error location line and column must be non-negative")
	}
	if e.Kind == ErrorPanic {
		if e.Panic == nil {
			return errors.New("panic execution error requires panic value")
		}
		if err := e.Panic.Validate(); err != nil {
			return fmt.Errorf("panic: %w", err)
		}
	} else if e.Panic != nil {
		return errors.New("non-panic execution error must not carry panic value")
	}
	for i, frame := range e.Stack {
		if err := frame.Validate(); err != nil {
			return fmt.Errorf("stack[%d]: %w", i, err)
		}
	}
	return nil
}
