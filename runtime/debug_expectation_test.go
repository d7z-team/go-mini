package runtime

import (
	"errors"
	"fmt"
	"strings"
)

// ExpectedEvent matches debugger events in runtime tests.
type ExpectedEvent struct {
	Kind               EventKind
	RunID              int64
	ExecutionContextID int64
	FunctionID         string
	PC                 int
	AnyPC              bool
	Line               int
	Column             int
	Locals             []ExpectedBinding
	Panic              *ExpectedValue
	Stack              []ExpectedFrame
}

type ExpectedFrame struct {
	ExecutionContextID int64
	FunctionID         string
	PC                 int
	AnyPC              bool
	Line               int
	Column             int
	Locals             []ExpectedBinding
}

type ExpectedBinding struct {
	ID    string
	Name  string
	Type  string
	Value ExpectedValue
}

type ExpectedValue struct {
	Kind   ValueKind
	Type   string
	Nil    bool
	Bool   *bool
	Int64  *int64
	String *string
	Text   string
}

func (e ExpectedEvent) Validate() error {
	switch e.Kind {
	case EventBreakpoint, EventStep, EventPause, EventPanic:
	default:
		return fmt.Errorf("unsupported expected event kind %q", e.Kind)
	}
	if e.RunID < 0 || e.ExecutionContextID < 0 {
		return errors.New("expected event ids must be non-negative")
	}
	if strings.TrimSpace(e.FunctionID) == "" {
		return errors.New("expected event function id is required")
	}
	if !e.AnyPC && e.PC < 0 {
		return errors.New("expected event pc must be non-negative")
	}
	if e.Line < 0 || e.Column < 0 {
		return errors.New("expected event location must be non-negative")
	}
	for i, frame := range e.Stack {
		if err := frame.Validate(); err != nil {
			return fmt.Errorf("expected event stack[%d]: %w", i, err)
		}
	}
	if e.Panic != nil {
		if err := e.Panic.Validate(); err != nil {
			return fmt.Errorf("expected event panic: %w", err)
		}
	}
	for i, local := range e.Locals {
		if err := local.Validate(); err != nil {
			return fmt.Errorf("expected event locals[%d]: %w", i, err)
		}
	}
	return nil
}

func (f ExpectedFrame) Validate() error {
	if f.ExecutionContextID < 0 {
		return errors.New("expected frame execution_context_id must be non-negative")
	}
	if strings.TrimSpace(f.FunctionID) == "" {
		return errors.New("expected frame function id is required")
	}
	if !f.AnyPC && f.PC < 0 {
		return errors.New("expected frame pc must be non-negative")
	}
	if f.Line < 0 || f.Column < 0 {
		return errors.New("expected frame location must be non-negative")
	}
	for i, local := range f.Locals {
		if err := local.Validate(); err != nil {
			return fmt.Errorf("expected frame locals[%d]: %w", i, err)
		}
	}
	return nil
}

func (b ExpectedBinding) Validate() error {
	if strings.TrimSpace(b.ID) == "" && strings.TrimSpace(b.Name) == "" {
		return errors.New("expected binding id or name is required")
	}
	return b.Value.Validate()
}

func (v ExpectedValue) Validate() error {
	switch v.Kind {
	case ValueNil, ValueBool, ValueInt64, ValueUint64, ValueFloat64, ValueComplex128, ValueString, ValueOpaque, "":
		return nil
	default:
		return fmt.Errorf("unsupported expected value kind %q", v.Kind)
	}
}

func (e ExpectedEvent) Match(event Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}
	if event.Kind != e.Kind {
		return fmt.Errorf("expected event kind %q, got %q", e.Kind, event.Kind)
	}
	if e.RunID > 0 && event.RunID != e.RunID {
		return fmt.Errorf("expected run_id %d, got %d", e.RunID, event.RunID)
	}
	if e.ExecutionContextID > 0 && event.ExecutionContextID != e.ExecutionContextID {
		return fmt.Errorf("expected execution_context_id %d, got %d", e.ExecutionContextID, event.ExecutionContextID)
	}
	if err := (ExpectedFrame{
		ExecutionContextID: e.ExecutionContextID, FunctionID: e.FunctionID,
		PC: e.PC, AnyPC: e.AnyPC, Line: e.Line, Column: e.Column, Locals: e.Locals,
	}).match("frame", event.Frame); err != nil {
		return err
	}
	if len(e.Stack) > 0 {
		if len(event.Stack) != len(e.Stack) {
			return fmt.Errorf("expected stack length %d, got %d", len(e.Stack), len(event.Stack))
		}
		for i, frame := range e.Stack {
			if err := frame.match(fmt.Sprintf("stack[%d]", i), event.Stack[i]); err != nil {
				return err
			}
		}
	}
	if e.Panic != nil {
		if event.Panic == nil {
			return errors.New("expected panic value")
		}
		if err := e.Panic.Match(*event.Panic); err != nil {
			return fmt.Errorf("panic value: %w", err)
		}
	}
	return nil
}

func (f ExpectedFrame) match(label string, frame Frame) error {
	if f.ExecutionContextID > 0 && frame.ExecutionContextID != f.ExecutionContextID {
		return fmt.Errorf("%s: expected execution_context_id %d, got %d", label, f.ExecutionContextID, frame.ExecutionContextID)
	}
	if frame.FunctionID != f.FunctionID {
		return fmt.Errorf("%s: expected function %q, got %q", label, f.FunctionID, frame.FunctionID)
	}
	if !f.AnyPC && frame.PC != f.PC {
		return fmt.Errorf("%s: expected pc %d, got %d", label, f.PC, frame.PC)
	}
	if f.Line > 0 && frame.Loc.Line != f.Line {
		return fmt.Errorf("%s: expected line %d, got %d", label, f.Line, frame.Loc.Line)
	}
	if f.Column > 0 && frame.Loc.Column != f.Column {
		return fmt.Errorf("%s: expected column %d, got %d", label, f.Column, frame.Loc.Column)
	}
	for i, expected := range f.Locals {
		if err := expected.Match(frame.Locals); err != nil {
			return fmt.Errorf("%s.locals[%d]: %w", label, i, err)
		}
	}
	return nil
}

func (b ExpectedBinding) Match(bindings []Binding) error {
	for _, binding := range bindings {
		if b.ID != "" && binding.ID != b.ID || b.Name != "" && binding.Name != b.Name {
			continue
		}
		if b.Type != "" && binding.Type != b.Type {
			return fmt.Errorf("expected binding type %q, got %q", b.Type, binding.Type)
		}
		return b.Value.Match(binding.Value)
	}
	return fmt.Errorf("expected binding id=%q name=%q", b.ID, b.Name)
}

func (v ExpectedValue) Match(value Value) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v.Kind != "" && value.Kind != v.Kind {
		return fmt.Errorf("expected value kind %q, got %q", v.Kind, value.Kind)
	}
	if v.Type != "" && value.Type != v.Type {
		return fmt.Errorf("expected value type %q, got %q", v.Type, value.Type)
	}
	if v.Text != "" && value.Text != v.Text {
		return fmt.Errorf("expected value text %q, got %q", v.Text, value.Text)
	}
	if v.Nil && !value.Nil {
		return errors.New("expected nil value")
	}
	if v.Bool != nil && (value.Bool == nil || *value.Bool != *v.Bool) {
		return fmt.Errorf("expected bool value %v, got %#v", *v.Bool, value.Bool)
	}
	if v.Int64 != nil && (value.Int64 == nil || *value.Int64 != *v.Int64) {
		return fmt.Errorf("expected int64 value %d, got %#v", *v.Int64, value.Int64)
	}
	if v.String != nil && (value.String == nil || *value.String != *v.String) {
		return fmt.Errorf("expected string value %q, got %#v", *v.String, value.String)
	}
	return nil
}

func ExpectedString(value string) ExpectedValue {
	return ExpectedValue{Kind: ValueString, String: &value}
}
