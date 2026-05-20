package runtime

import (
	"errors"
	"fmt"
)

const maxPreparedValidationDepth = 256

func ValidatePreparedProgram(plan *PreparedProgram) error {
	if plan == nil {
		return errors.New("missing prepared program")
	}
	for name, typ := range plan.NamedTypes {
		if err := validateRuntimeType("named type "+name, typ); err != nil {
			return err
		}
	}
	for name, spec := range plan.StructSchemas {
		if err := validateRuntimeStructSpec("struct schema "+name, spec); err != nil {
			return err
		}
	}
	for name, spec := range plan.InterfaceSchemas {
		if err := validateRuntimeInterfaceSpec("interface schema "+name, spec); err != nil {
			return err
		}
	}
	for i, group := range plan.GlobalInitGroups {
		if group == nil {
			return fmt.Errorf("global init group %d is nil", i)
		}
		if err := validatePreparedTaskPlan(fmt.Sprintf("global init group %d", i), group.InitPlan, 0); err != nil {
			return err
		}
	}
	for name, global := range plan.Globals {
		if global == nil {
			return fmt.Errorf("global %s is nil", name)
		}
		if global.Name == "" {
			return fmt.Errorf("global %s missing name", name)
		}
		if err := validateRuntimeType("global "+name+" kind", global.Kind); err != nil {
			return err
		}
		if err := validatePreparedTaskPlan(fmt.Sprintf("global %s init", name), global.InitPlan, 0); err != nil {
			return err
		}
	}
	for name, fn := range plan.Functions {
		if fn == nil {
			return fmt.Errorf("function %s is nil", name)
		}
		if fn.Name == "" {
			return fmt.Errorf("function %s missing name", name)
		}
		if fn.FunctionSig == nil {
			return fmt.Errorf("function %s missing signature", name)
		}
		if err := validateRuntimeFuncSig("function "+name+" signature", fn.FunctionSig); err != nil {
			return err
		}
		if err := validatePreparedTaskPlan("function "+name, fn.BodyTasks, 0); err != nil {
			return err
		}
	}
	return validatePreparedTaskPlan("main", plan.MainTasks, 0)
}

func validatePreparedTaskPlan(path string, tasks []Task, depth int) error {
	if depth > maxPreparedValidationDepth {
		return fmt.Errorf("%s: prepared task plan is too deeply nested", path)
	}
	for i := range tasks {
		if err := validatePreparedTaskPayload(path, i, tasks[i], depth); err != nil {
			return err
		}
	}
	return validatePreparedScopeFlow(path, tasks)
}

func validatePreparedScopeFlow(path string, tasks []Task) error {
	scopeDepth := 0
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		if startsDirectUnwind(task.Op) {
			if err := validatePreparedUnwindSuffix(path, tasks[:i], scopeDepth, i); err != nil {
				return err
			}
		}
		switch {
		case isScopeEnterOp(task.Op):
			scopeDepth++
		case isScopeExitOp(task.Op):
			if scopeDepth == 0 {
				return fmt.Errorf("%s: %s at task %d exits without matching scope enter", path, task.Op.String(), i)
			}
			scopeDepth--
		}
	}
	if scopeDepth != 0 {
		return fmt.Errorf("%s: task plan leaves %d scope(s) open", path, scopeDepth)
	}
	return nil
}

func validatePreparedUnwindSuffix(path string, suffix []Task, currentDepth, from int) error {
	skippedEnters := 0
	scopeDepth := currentDepth
	for i := len(suffix) - 1; i >= 0; i-- {
		task := suffix[i]
		switch {
		case isScopeEnterOp(task.Op):
			skippedEnters++
		case isScopeExitOp(task.Op):
			if skippedEnters > 0 {
				skippedEnters--
				continue
			}
			if scopeDepth == 0 {
				return fmt.Errorf("%s: unwind from task %d reaches orphan %s at task %d", path, from, task.Op.String(), i)
			}
			scopeDepth--
		}
	}
	return nil
}

func validatePreparedTaskPayload(path string, index int, task Task, depth int) error {
	plan := fmt.Sprintf("%s task %d (%s)", path, index, task.Op.String())
	switch task.Op {
	case OpLineStep, OpAssign, OpPop, OpScopeExit:
		if task.Data != nil {
			return fmt.Errorf("%s must not carry payload", plan)
		}
		return nil
	case OpDeclareInitVars:
		data, ok := task.Data.(*VarDeclData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing VarDeclData", plan)
		}
		return validateVarDeclData(plan, data)
	case OpApplyBinary, OpApplyUnary, OpIncDec, OpInterrupt, OpMember, OpScopeEnter:
		value, ok := task.Data.(string)
		if !ok || value == "" {
			return fmt.Errorf("%s missing string payload", plan)
		}
		return nil
	case OpMultiAssign:
		data, ok := task.Data.(*MultiAssignData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing MultiAssignData", plan)
		}
		return validateMultiAssignData(plan, data)
	case OpReturn:
		count, ok := task.Data.(int)
		if !ok || count < 0 {
			return fmt.Errorf("%s missing return count", plan)
		}
		return nil
	case OpScheduleDefer:
		data, ok := task.Data.(*DeferData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing DeferData", plan)
		}
		return validatePreparedTaskPlan(plan+".defer", data.Tasks, depth+1)
	case OpFinally:
		data, ok := task.Data.(*FinallyData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing FinallyData", plan)
		}
		return validatePreparedTaskPlan(plan+".finally", data.Body, depth+1)
	case OpCatchBoundary:
		data, ok := task.Data.(*CatchData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing CatchData", plan)
		}
		return validatePreparedTaskPlan(plan+".catch", data.Body, depth+1)
	case OpForStart:
		data, ok := task.Data.(*ForData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing ForData", plan)
		}
		return validateForData(plan, data, depth)
	case OpRangeInit:
		data, ok := task.Data.(*RangeData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing RangeData", plan)
		}
		if data.Obj != nil || data.Keys != nil || data.Length != 0 || data.Index != 0 {
			return fmt.Errorf("%s carries runtime range state", plan)
		}
		return validatePreparedTaskPlan(plan+".range_body", data.Body, depth+1)
	case OpJumpIf:
		data, ok := task.Data.(*JumpData)
		if !ok || data == nil || data.Operator == "" {
			return fmt.Errorf("%s missing JumpData", plan)
		}
		return validatePreparedTaskPlan(plan+".right", data.Right, depth+1)
	case OpBranchIf:
		data, ok := task.Data.(*BranchData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing BranchData", plan)
		}
		if err := validatePreparedTaskPlan(plan+".then", data.Then, depth+1); err != nil {
			return err
		}
		return validatePreparedTaskPlan(plan+".else", data.Else, depth+1)
	case OpCall, OpGo:
		data, ok := task.Data.(*CallData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing CallData", plan)
		}
		if data.ArgCount < 0 {
			return fmt.Errorf("%s has negative arg count", plan)
		}
		return nil
	case OpComposite:
		data, ok := task.Data.(*CompositeData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing CompositeData", plan)
		}
		return validateRuntimeType(plan+".type", data.Type)
	case OpIndex:
		data, ok := task.Data.(*IndexData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing IndexData", plan)
		}
		return validateRuntimeType(plan+".result", data.ResultType)
	case OpSlice:
		data, ok := task.Data.(*SliceData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing SliceData", plan)
		}
		return nil
	case OpLoadVar:
		data, ok := task.Data.(*LoadVarData)
		if !ok || data == nil || data.Name == "" {
			return fmt.Errorf("%s missing LoadVarData", plan)
		}
		return nil
	case OpLoadLocal, OpStoreLocal:
		sym, ok := task.Data.(SymbolRef)
		if !ok || sym.Kind != SymbolLocal || sym.Slot < 0 {
			return fmt.Errorf("%s missing local SymbolRef", plan)
		}
		return nil
	case OpLoadUpvalue, OpStoreUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok || sym.Kind != SymbolUpvalue || sym.Slot < 0 {
			return fmt.Errorf("%s missing upvalue SymbolRef", plan)
		}
		return nil
	case OpImportInit:
		data, ok := task.Data.(*ImportInitData)
		if !ok || data == nil || data.Path == "" {
			return fmt.Errorf("%s missing ImportInitData", plan)
		}
		return nil
	case OpSwitchStart:
		data, ok := task.Data.(*SwitchData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing SwitchData", plan)
		}
		return validateSwitchData(plan, data, depth)
	case OpAssert:
		data, ok := task.Data.(*AssertData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing AssertData", plan)
		}
		if err := validateRuntimeType(plan+".target", data.TargetType); err != nil {
			return err
		}
		return validateRuntimeType(plan+".result", data.ResultType)
	case OpPush:
		if task.Data == nil {
			return nil
		}
		value, ok := task.Data.(*Var)
		if !ok {
			return fmt.Errorf("%s payload must be *Var", plan)
		}
		return validateRuntimeType(plan+".value", value.RuntimeType())
	case OpMakeClosure:
		data, ok := task.Data.(*ClosureData)
		if !ok || data == nil || data.FunctionSig == nil {
			return fmt.Errorf("%s missing ClosureData", plan)
		}
		if err := validateRuntimeFuncSig(plan+".signature", data.FunctionSig); err != nil {
			return err
		}
		return validatePreparedTaskPlan(plan+".closure", data.BodyTasks, depth+1)
	case OpEvalLHS:
		if task.Data == nil {
			return nil
		}
		if _, ok := task.Data.(*LHSData); !ok {
			return fmt.Errorf("%s payload must be LHSData", plan)
		}
		return nil
	default:
		return fmt.Errorf("%s is not valid in prepared executable bytecode", plan)
	}
}

func validateVarDeclData(plan string, data *VarDeclData) error {
	if len(data.Bindings) == 0 {
		return fmt.Errorf("%s has no bindings", plan)
	}
	switch data.Mode {
	case "", VarDeclInitZero:
		if data.ValueCount != 0 {
			return fmt.Errorf("%s zero init has %d value(s)", plan, data.ValueCount)
		}
	case VarDeclInitPerBinding:
		if data.ValueCount != len(data.Bindings) {
			return fmt.Errorf("%s count mismatch: %d bindings, %d values", plan, len(data.Bindings), data.ValueCount)
		}
	case VarDeclInitDestructure:
		if data.ValueCount != 1 {
			return fmt.Errorf("%s destructure requires one value", plan)
		}
	default:
		return fmt.Errorf("%s has unknown init mode %q", plan, data.Mode)
	}
	for i, binding := range data.Bindings {
		if binding.Name == "" && binding.Sym.Kind != SymbolUnknown {
			return fmt.Errorf("%s binding %d has unnamed symbol", plan, i)
		}
		if err := validateRuntimeType(fmt.Sprintf("%s binding %d kind", plan, i), binding.Kind); err != nil {
			return err
		}
		if err := validateSymbolRef(fmt.Sprintf("%s binding %d", plan, i), binding.Sym); err != nil {
			return err
		}
	}
	return nil
}

func validateMultiAssignData(plan string, data *MultiAssignData) error {
	if data.LHSCount <= 0 {
		return fmt.Errorf("%s has no lhs values", plan)
	}
	switch data.Mode {
	case MultiAssignPerBinding:
		if data.ValueCount != data.LHSCount {
			return fmt.Errorf("%s count mismatch: %d lhs, %d values", plan, data.LHSCount, data.ValueCount)
		}
	case MultiAssignDestructure:
		if data.ValueCount != 1 {
			return fmt.Errorf("%s destructure requires one value", plan)
		}
	default:
		return fmt.Errorf("%s has unknown mode %q", plan, data.Mode)
	}
	return nil
}

func validateForData(plan string, data *ForData, depth int) error {
	if err := validatePreparedTaskPlan(plan+".for_cond", data.Cond, depth+1); err != nil {
		return err
	}
	if err := validatePreparedTaskPlan(plan+".for_body", data.Body, depth+1); err != nil {
		return err
	}
	return validatePreparedTaskPlan(plan+".for_update", data.Update, depth+1)
}

func validateSwitchData(plan string, data *SwitchData, depth int) error {
	if err := validatePreparedTaskPlan(plan+".switch_init", data.Init, depth+1); err != nil {
		return err
	}
	if err := validatePreparedTaskPlan(plan+".switch_tag", data.Tag, depth+1); err != nil {
		return err
	}
	if err := validatePreparedTaskPlan(plan+".switch_assign", data.AssignLHS, depth+1); err != nil {
		return err
	}
	for i, c := range data.Cases {
		for j, typ := range c.TypeNames {
			if err := validateRuntimeType(fmt.Sprintf("%s.case_%d_type_%d", plan, i, j), typ); err != nil {
				return err
			}
		}
		for j, expr := range c.Exprs {
			if err := validatePreparedTaskPlan(fmt.Sprintf("%s.case_%d_expr_%d", plan, i, j), expr, depth+1); err != nil {
				return err
			}
		}
		if err := validatePreparedTaskPlan(fmt.Sprintf("%s.case_%d_body", plan, i), c.Body, depth+1); err != nil {
			return err
		}
	}
	return validatePreparedTaskPlan(plan+".switch_default", data.DefaultBody, depth+1)
}

func validateRuntimeType(plan string, typ RuntimeType) error {
	if typ.Kind == RuntimeTypeInvalid && typ.Raw.IsEmpty() {
		return fmt.Errorf("%s missing runtime type", plan)
	}
	if typ.Raw.IsEmpty() {
		return fmt.Errorf("%s missing raw runtime type", plan)
	}
	if _, err := ParseRuntimeType(typ.Raw.Ast()); err != nil {
		return fmt.Errorf("%s: %w", plan, err)
	}
	return nil
}

func validateRuntimeFuncSig(plan string, sig *RuntimeFuncSig) error {
	if sig == nil {
		return fmt.Errorf("%s missing function signature", plan)
	}
	if sig.Spec.IsEmpty() {
		return fmt.Errorf("%s missing function spec", plan)
	}
	if _, err := ParseRuntimeFuncSig(sig.Spec.Ast()); err != nil {
		return fmt.Errorf("%s: %w", plan, err)
	}
	for i, param := range sig.ParamTypes {
		if err := validateRuntimeType(fmt.Sprintf("%s param %d", plan, i), param); err != nil {
			return err
		}
	}
	return validateRuntimeType(plan+" return", sig.ReturnType)
}

func validateRuntimeStructSpec(plan string, spec *RuntimeStructSpec) error {
	if spec == nil {
		return fmt.Errorf("%s is nil", plan)
	}
	if err := validateRuntimeType(plan+".type", spec.TypeInfo); err != nil {
		return err
	}
	for i, field := range spec.Fields {
		if field.Type.IsEmpty() {
			return fmt.Errorf("%s field %d missing type", plan, i)
		}
		if _, err := ParseRuntimeType(field.Type.Ast()); err != nil {
			return fmt.Errorf("%s field %d: %w", plan, i, err)
		}
		if err := validateRuntimeType(fmt.Sprintf("%s field %d info", plan, i), field.TypeInfo); err != nil {
			return err
		}
	}
	for _, method := range spec.Methods {
		if err := validateRuntimeFuncSig(plan+" method "+method.Name, method.Spec); err != nil {
			return err
		}
	}
	for name, sig := range spec.ByMethod {
		if err := validateRuntimeFuncSig(plan+" method "+name, sig); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeInterfaceSpec(plan string, spec *RuntimeInterfaceSpec) error {
	if spec == nil {
		return fmt.Errorf("%s is nil", plan)
	}
	if err := validateRuntimeType(plan+".type", spec.TypeInfo); err != nil {
		return err
	}
	for _, method := range spec.Methods {
		if err := validateRuntimeFuncSig(plan+" method "+method.Name, method.Spec); err != nil {
			return err
		}
	}
	for name, sig := range spec.ByName {
		if err := validateRuntimeFuncSig(plan+" method "+name, sig); err != nil {
			return err
		}
	}
	return nil
}

func validateSymbolRef(plan string, sym SymbolRef) error {
	switch sym.Kind {
	case SymbolUnknown, SymbolGlobal, SymbolBuiltin:
		return nil
	case SymbolLocal, SymbolUpvalue:
		if sym.Slot < 0 {
			return fmt.Errorf("%s has negative local slot", plan)
		}
		return nil
	default:
		return fmt.Errorf("%s has invalid symbol kind %d", plan, sym.Kind)
	}
}

func startsDirectUnwind(op OpCode) bool {
	return op == OpReturn || op == OpInterrupt
}

func isScopeEnterOp(op OpCode) bool {
	switch op {
	case OpScopeEnter, OpForScopeEnter, OpRangeScopeEnter, OpCatchScopeEnter:
		return true
	default:
		return false
	}
}

func isScopeExitOp(op OpCode) bool {
	return op == OpScopeExit || op == OpForScopeExit
}
