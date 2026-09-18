package runtime

import (
	"errors"
	"fmt"
	"strings"
)

type EventKind string

const (
	EventBreakpoint EventKind = "breakpoint"
	EventStep       EventKind = "step"
	EventPause      EventKind = "pause"
	EventPanic      EventKind = "panic"
)

type ValueKind string

const (
	ValueNil        ValueKind = "nil"
	ValueBool       ValueKind = "bool"
	ValueInt64      ValueKind = "int64"
	ValueUint64     ValueKind = "uint64"
	ValueFloat64    ValueKind = "float64"
	ValueComplex128 ValueKind = "complex128"
	ValueString     ValueKind = "string"
	ValueArray      ValueKind = "array"
	ValueMap        ValueKind = "map"
	ValueStruct     ValueKind = "struct"
	ValueOpaque     ValueKind = "opaque"
)

type Location struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Value struct {
	Type       string           `json:"type,omitempty"`
	Kind       ValueKind        `json:"kind"`
	Text       string           `json:"text,omitempty"`
	Nil        bool             `json:"nil,omitempty"`
	Bool       *bool            `json:"bool,omitempty"`
	Int64      *int64           `json:"int64,omitempty"`
	Uint64     *uint64          `json:"uint64,omitempty"`
	Float64    *float64         `json:"float64,omitempty"`
	Complex128 *Complex128Value `json:"complex128,omitempty"`
	String     *string          `json:"string,omitempty"`
	Items      []Value          `json:"items,omitempty"`
	Entries    []MapEntry       `json:"entries,omitempty"`
	Fields     []Binding        `json:"fields,omitempty"`
}

type Complex128Value struct {
	Real float64 `json:"real"`
	Imag float64 `json:"imag"`
}

type MapEntry struct {
	Key   Value `json:"key"`
	Value Value `json:"value"`
}

type Binding struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
	Value Value  `json:"value"`
}

type Frame struct {
	Generation         uint64    `json:"generation,omitempty"`
	ProgramHash        string    `json:"program_hash,omitempty"`
	ExecutionContextID int64     `json:"execution_context_id,omitempty"`
	ModulePath         string    `json:"module_path,omitempty"`
	FunctionID         string    `json:"function_id"`
	PC                 int       `json:"pc"`
	Loc                Location  `json:"loc,omitempty"`
	Locals             []Binding `json:"locals,omitempty"`
	Upvalues           []Binding `json:"upvalues,omitempty"`
	Globals            []Binding `json:"globals,omitempty"`
}

type Event struct {
	Kind               EventKind `json:"kind"`
	RunID              int64     `json:"run_id"`
	Generation         uint64    `json:"generation,omitempty"`
	ProgramHash        string    `json:"program_hash,omitempty"`
	ExecutionContextID int64     `json:"execution_context_id"`
	Frame              Frame     `json:"frame"`
	Stack              []Frame   `json:"stack,omitempty"`
	Panic              *Value    `json:"panic,omitempty"`
}

func NewEvent(kind EventKind) Event { return Event{Kind: kind} }

func (e Event) Validate() error {
	switch e.Kind {
	case EventBreakpoint, EventStep, EventPause, EventPanic:
	default:
		return fmt.Errorf("unsupported debug event kind %q", e.Kind)
	}
	if e.RunID <= 0 {
		return errors.New("debug event run_id must be positive")
	}
	if e.ExecutionContextID <= 0 {
		return errors.New("debug event execution_context_id must be positive")
	}
	if err := e.Frame.Validate(); err != nil {
		return fmt.Errorf("frame: %w", err)
	}
	if e.Frame.ExecutionContextID != 0 && e.Frame.ExecutionContextID != e.ExecutionContextID {
		return errors.New("debug event frame execution_context_id must match event execution_context_id")
	}
	for i, frame := range e.Stack {
		if err := frame.Validate(); err != nil {
			return fmt.Errorf("stack[%d]: %w", i, err)
		}
	}
	if len(e.Stack) != 0 && !sameFrameIdentity(e.Frame, e.Stack[0]) {
		return errors.New("debug event frame must match stack[0]")
	}
	if e.Kind == EventPanic {
		if e.Panic == nil {
			return errors.New("panic debug event requires panic value")
		}
		if err := e.Panic.Validate(); err != nil {
			return fmt.Errorf("panic: %w", err)
		}
	}
	return nil
}

func (f Frame) Validate() error {
	if f.ExecutionContextID < 0 {
		return errors.New("execution_context_id must be non-negative")
	}
	if strings.TrimSpace(f.FunctionID) == "" {
		return errors.New("function_id is required")
	}
	if f.PC < 0 {
		return errors.New("pc must be non-negative")
	}
	if f.Loc.Line < 0 || f.Loc.Column < 0 {
		return errors.New("location line and column must be non-negative")
	}
	for i, local := range f.Locals {
		if err := local.Validate(); err != nil {
			return fmt.Errorf("locals[%d]: %w", i, err)
		}
	}
	for i, upvalue := range f.Upvalues {
		if err := upvalue.Validate(); err != nil {
			return fmt.Errorf("upvalues[%d]: %w", i, err)
		}
	}
	for i, global := range f.Globals {
		if err := global.Validate(); err != nil {
			return fmt.Errorf("globals[%d]: %w", i, err)
		}
	}
	return nil
}

func (b Binding) Validate() error {
	if strings.TrimSpace(b.ID) == "" && strings.TrimSpace(b.Name) == "" {
		return errors.New("binding id or name is required")
	}
	if strings.TrimSpace(b.Type) == "" && strings.TrimSpace(b.Value.Type) == "" {
		return errors.New("binding type is required")
	}
	return b.Value.Validate()
}

func (v Value) Validate() error {
	switch v.Kind {
	case ValueNil:
		if !v.Nil {
			return errors.New("nil value kind requires nil=true")
		}
	case ValueBool:
		if v.Bool == nil {
			return errors.New("bool value kind requires bool")
		}
	case ValueInt64:
		if v.Int64 == nil {
			return errors.New("int64 value kind requires int64")
		}
	case ValueUint64:
		if v.Uint64 == nil {
			return errors.New("uint64 value kind requires uint64")
		}
	case ValueFloat64:
		if v.Float64 == nil {
			return errors.New("float64 value kind requires float64")
		}
	case ValueComplex128:
		if v.Complex128 == nil {
			return errors.New("complex128 value kind requires complex128")
		}
	case ValueString:
		if v.String == nil {
			return errors.New("string value kind requires string")
		}
	case ValueArray:
		for i, item := range v.Items {
			if err := item.Validate(); err != nil {
				return fmt.Errorf("items[%d]: %w", i, err)
			}
		}
	case ValueMap:
		for i, entry := range v.Entries {
			if err := entry.Key.Validate(); err != nil {
				return fmt.Errorf("entries[%d].key: %w", i, err)
			}
			if err := entry.Value.Validate(); err != nil {
				return fmt.Errorf("entries[%d].value: %w", i, err)
			}
		}
	case ValueStruct:
		for i, field := range v.Fields {
			if err := field.Validate(); err != nil {
				return fmt.Errorf("fields[%d]: %w", i, err)
			}
		}
	case ValueOpaque:
	default:
		return fmt.Errorf("unsupported debug value kind %q", v.Kind)
	}
	return nil
}

func sameFrameIdentity(left, right Frame) bool {
	return left.Generation == right.Generation &&
		left.ProgramHash == right.ProgramHash &&
		left.ModulePath == right.ModulePath &&
		left.ExecutionContextID == right.ExecutionContextID &&
		left.FunctionID == right.FunctionID &&
		left.PC == right.PC &&
		left.Loc == right.Loc
}
