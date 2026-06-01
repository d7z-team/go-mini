package runtime

import (
	"errors"
	"fmt"
)

func pruneRangeContinueResidualTasks(session *StackContext) {
	for i := len(session.TaskStack) - 1; i >= 0; i-- {
		task := session.TaskStack[i]
		if task.Op == OpLoopBoundary && task.Data == nil {
			session.TaskStack = session.TaskStack[:i+1]
			return
		}
	}
}

func (e *Executor) declareInitVars(session *StackContext, data *VarDeclData) error {
	if data == nil {
		return errors.New("missing var declaration data")
	}

	values := make([]*Var, 0, len(data.Bindings))
	switch data.Mode {
	case "", VarDeclInitZero:
		if data.ValueCount != 0 {
			return fmt.Errorf("var declaration zero-init expected no values, got %d", data.ValueCount)
		}
	case VarDeclInitDestructure:
		if data.ValueCount != 1 {
			return fmt.Errorf("var declaration expected single expandable value, got %d", data.ValueCount)
		}
		value := session.ValueStack.Pop()
		if value == nil {
			return errors.New("var declaration: RHS evaluated to nil")
		}
		value = e.unwrapValue(value)
		if value == nil {
			return errors.New("var declaration: RHS evaluated to nil")
		}
		if value.VType != TypeArray {
			return &VMError{Message: fmt.Sprintf("cannot destructure type %v", value.VType), IsPanic: true}
		}
		raw := arrayRef(value).Snapshot()
		if len(raw) != len(data.Bindings) {
			return &VMError{Message: fmt.Sprintf("var declaration: destructure count mismatch (need %d, got %d)", len(data.Bindings), len(raw)), IsPanic: true}
		}
		values = make([]*Var, len(raw))
		for i, item := range raw {
			values[i] = cloneVarForAssign(item)
		}
	case VarDeclInitPerBinding:
		if data.ValueCount != len(data.Bindings) {
			return fmt.Errorf("var declaration count mismatch: %d names = %d values", len(data.Bindings), data.ValueCount)
		}
		values = make([]*Var, data.ValueCount)
		for i := data.ValueCount - 1; i >= 0; i-- {
			values[i] = session.ValueStack.Pop()
		}
	default:
		return fmt.Errorf("unknown var declaration init mode: %s", data.Mode)
	}

	for i, binding := range data.Bindings {
		if binding.Name == "" || binding.Name == "_" {
			continue
		}
		var value *Var
		if len(values) > 0 {
			value = values[i]
		}
		switch binding.Sym.Kind {
		case SymbolLocal:
			if err := session.DeclareSymbol(binding.Sym, binding.Kind); err != nil {
				return err
			}
			if len(values) > 0 {
				if err := session.StoreSymbol(binding.Sym, value); err != nil {
					return err
				}
			}
		case SymbolUpvalue:
			if len(values) > 0 {
				if err := session.StoreSymbol(binding.Sym, value); err != nil {
					return err
				}
			}
		default:
			if err := session.InitGlobal(binding.Name, binding.Kind, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Executor) scheduleForBody(session *StackContext, data *ForData) {
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Data: data})
	if len(data.Update) > 0 {
		session.TaskStack = append(session.TaskStack, data.Update...)
	}
	session.TaskStack = append(session.TaskStack, Task{Op: OpLoopContinue})
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeExit})
	session.TaskStack = append(session.TaskStack, data.Body...)
	session.TaskStack = append(session.TaskStack, Task{Op: OpForScopeEnter})
}

func (e *Executor) switchCaseTasks(plan *SwitchData, tag *Var, body []Task, scopeName string) []Task {
	out := make([]Task, 0, len(body)+4)
	if plan.IsType {
		out = append(out, Task{Op: OpScopeExit})
	}
	out = append(out, body...)
	if plan.HasAssign {
		assignValue := cloneVarForAssign(e.unwrapValue(tag))
		if assignValue == nil {
			assignValue = cloneVarForAssign(tag)
		}
		out = append(out, Task{Op: OpAssign})
		out = append(out, Task{Op: OpPush, Data: assignValue})
		out = append(out, plan.AssignLHS...)
	}
	if plan.IsType {
		out = append(out, Task{Op: OpScopeEnter, Data: scopeName})
	}
	return out
}

func (e *Executor) switchTypeCaseMatches(tag *Var, targets []RuntimeType) bool {
	tag = e.unwrapValue(tag)
	actual := runtimeTypeForAssignment(tag)
	for _, targetType := range targets {
		if targetType.IsEmpty() {
			continue
		}
		if tag == nil || (tag.VType == TypeAny && tag.Ref == nil) {
			raw := targetType.Raw
			if raw == "nil" || raw == "Any" {
				return true
			}
			continue
		}
		if targetType.Raw == "Any" {
			return true
		}
		if targetType.IsInterface() {
			if _, err := e.CheckSatisfaction(tag, targetType.Raw.String()); err == nil {
				return true
			}
			continue
		}
		if _, ok := e.resolveInterfaceSpec(targetType.Raw); ok {
			if _, err := e.CheckSatisfaction(tag, targetType.Raw.String()); err == nil {
				return true
			}
			continue
		}
		if !actual.IsEmpty() && actual.Raw.Equals(targetType.Raw) {
			return true
		}
	}
	return false
}
