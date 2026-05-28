package runtime

import (
	"errors"
	"fmt"
	"strings"
)

const maxPreparedValidationDepth = 256

func ValidatePreparedProgram(plan *PreparedProgram) error {
	return validatePreparedProgram(plan, 0, nil)
}

func validatePreparedProgram(plan *PreparedProgram, depth int, stack map[*PreparedProgram]struct{}) error {
	if plan == nil {
		return errors.New("missing prepared program")
	}
	if depth > maxPreparedValidationDepth {
		return errors.New("prepared program module graph is too deeply nested")
	}
	if stack == nil {
		stack = make(map[*PreparedProgram]struct{})
	}
	if _, ok := stack[plan]; ok {
		return errors.New("prepared program module graph contains a cycle")
	}
	stack[plan] = struct{}{}
	defer delete(stack, plan)
	for name, typ := range plan.NamedTypes {
		if err := validateRuntimeType("named type "+name, typ); err != nil {
			return err
		}
	}
	for name, value := range plan.Constants {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("constant %s invalid: %w", name, err)
		}
		typ, err := ParseRuntimeType(value.Type)
		if err != nil {
			return fmt.Errorf("constant %s type: %w", name, err)
		}
		if err := validateRuntimeType("constant "+name+" type", typ); err != nil {
			return err
		}
		if !(typ.IsInt() || typ.Raw == SpecFloat64 || typ.IsBool() || typ.IsString()) {
			return fmt.Errorf("constant %s has unsupported type %s", name, typ.Raw)
		}
		if explicit, ok := plan.ConstantTypes[name]; ok && explicit.Raw != typ.Raw {
			return fmt.Errorf("constant %s type mismatch: %s vs %s", name, explicit.Raw, typ.Raw)
		}
	}
	for name, typ := range plan.ConstantTypes {
		if _, ok := plan.Constants[name]; !ok {
			return fmt.Errorf("constant type %s targets missing constant", name)
		}
		if err := validateRuntimeType("constant "+name+" type", typ); err != nil {
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
	for name, export := range plan.Exports {
		if err := validatePreparedExport(plan, name, export); err != nil {
			return err
		}
	}
	for path, module := range plan.Modules {
		if strings.TrimSpace(path) == "" {
			return errors.New("embedded module has empty path")
		}
		if module == nil {
			return fmt.Errorf("embedded module %s is nil", path)
		}
		if err := validatePreparedProgram(module, depth+1, stack); err != nil {
			return fmt.Errorf("embedded module %s invalid: %w", path, err)
		}
	}
	for path, hash := range plan.ModuleHashes {
		if strings.TrimSpace(path) == "" {
			return errors.New("embedded module hash has empty path")
		}
		if strings.TrimSpace(hash) == "" {
			return fmt.Errorf("embedded module %s has empty hash", path)
		}
		if plan.Modules[path] == nil {
			return fmt.Errorf("embedded module hash %s targets missing module", path)
		}
	}
	for i, req := range plan.ExternalRequirements {
		pkg := req.PackagePath
		member := req.MemberName
		if strings.TrimSpace(pkg) == "" {
			return fmt.Errorf("external requirement %d missing package", i)
		}
		if req.Kind != FFIMemberModule && strings.TrimSpace(member) == "" && req.TypeName == "" {
			return fmt.Errorf("external requirement %d missing member", i)
		}
		switch req.Kind {
		case FFIMemberFunc, FFIMemberConst, FFIMemberValue, FFIMemberType, FFIMemberModule:
		default:
			return fmt.Errorf("external requirement %d has invalid kind %s", i, req.Kind)
		}
		if !req.Type.IsEmpty() {
			if err := req.Type.ValidateCanonical(); err != nil {
				return fmt.Errorf("external requirement %d type: %w", i, err)
			}
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
	methodBindings := make(map[string]string)
	for name, fn := range plan.Functions {
		if fn == nil {
			return fmt.Errorf("function %s is nil", name)
		}
		if fn.Name == "" {
			return fmt.Errorf("function %s missing name", name)
		}
		if fn.Name != name {
			return fmt.Errorf("function %s name mismatch: %s", name, fn.Name)
		}
		if fn.FunctionSig == nil {
			return fmt.Errorf("function %s missing signature", name)
		}
		if err := validateRuntimeFuncSig("function "+name+" signature", fn.FunctionSig); err != nil {
			return err
		}
		if err := validatePreparedFunctionReceiver(name, fn); err != nil {
			return err
		}
		if err := validatePreparedMethodConflict(plan.Package, name, fn, methodBindings); err != nil {
			return err
		}
		if err := validatePreparedTaskPlan("function "+name, fn.BodyTasks, 0); err != nil {
			return err
		}
	}
	return validatePreparedTaskPlan("main", plan.MainTasks, 0)
}

func validatePreparedFunctionReceiver(name string, fn *PreparedFunction) error {
	if fn.Receiver.IsEmpty() {
		return nil
	}
	if !strings.Contains(name, ".") {
		return fmt.Errorf("function %s receiver requires method-qualified name", name)
	}
	receiver, err := ParseRuntimeType(fn.Receiver)
	if err != nil {
		return fmt.Errorf("function %s receiver: %w", name, err)
	}
	if receiver.Kind != RuntimeTypeNamed {
		return fmt.Errorf("function %s receiver must be a named type", name)
	}
	expected := receiver.Raw.String()
	if expected == "" || expected == "Any" {
		return fmt.Errorf("function %s receiver must be a concrete type", name)
	}
	nameReceiver := receiverNameFromFunctionName(name)
	if nameReceiver != expected {
		return fmt.Errorf("function %s receiver name %s does not match receiver %s", name, nameReceiver, expected)
	}
	if len(fn.FunctionSig.ParamTypes) == 0 {
		return fmt.Errorf("function %s receiver requires first parameter", name)
	}
	actual := fn.FunctionSig.ParamTypes[0]
	if actual.Kind == RuntimeTypeNamed && actual.Raw.String() == expected {
		return nil
	}
	if actual.Kind == RuntimeTypePointer && actual.Elem != nil && actual.Elem.Kind == RuntimeTypeNamed && actual.Elem.Raw.String() == expected {
		return nil
	}
	return fmt.Errorf("function %s receiver first parameter must be %s or Ptr<%s>, got %s", name, expected, expected, actual.Raw)
}

func validatePreparedMethodConflict(pkg, name string, fn *PreparedFunction, bindings map[string]string) error {
	if fn == nil || fn.Receiver.IsEmpty() {
		return nil
	}
	methodName := methodNameFromFunctionName(name)
	for _, receiverName := range methodFunctionReceiverKeys(pkg, fn.Receiver) {
		key := receiverName + "." + methodName
		if prev := bindings[key]; prev != "" && prev != name {
			return fmt.Errorf("method %s registered by both %s and %s", key, prev, name)
		}
		bindings[key] = name
	}
	return nil
}

func validatePreparedExport(plan *PreparedProgram, mapName string, export PreparedExport) error {
	if strings.TrimSpace(mapName) == "" {
		return errors.New("prepared export has empty map key")
	}
	if strings.TrimSpace(export.Name) == "" {
		return fmt.Errorf("prepared export %s missing name", mapName)
	}
	if export.Name != mapName {
		return fmt.Errorf("prepared export %s name mismatch: %s", mapName, export.Name)
	}
	target := export.TargetName
	if strings.TrimSpace(target) == "" {
		target = export.Name
	}
	if !export.Type.IsEmpty() {
		if err := validateRuntimeType("prepared export "+export.Name+" type", export.Type); err != nil {
			return err
		}
	}
	switch export.Kind {
	case PreparedExportFunc:
		if plan.Functions[target] == nil {
			return fmt.Errorf("prepared export %s targets missing function %s", export.Name, target)
		}
	case PreparedExportGlobal:
		if plan.Globals[target] == nil {
			return fmt.Errorf("prepared export %s targets missing global %s", export.Name, target)
		}
	case PreparedExportConst:
		if _, ok := plan.Constants[target]; !ok {
			return fmt.Errorf("prepared export %s targets missing constant %s", export.Name, target)
		}
		value := plan.Constants[target]
		typ, ok := plan.ConstantTypes[target]
		if !ok {
			typ, _ = ParseRuntimeType(value.Type)
		}
		if err := validateRuntimeType("prepared constant "+target+" type", typ); err != nil {
			return err
		}
	case PreparedExportType:
		if _, ok := plan.NamedTypes[target]; !ok {
			return fmt.Errorf("prepared export %s targets missing named type %s", export.Name, target)
		}
	case PreparedExportStruct:
		if plan.StructSchemas[target] == nil {
			return fmt.Errorf("prepared export %s targets missing struct schema %s", export.Name, target)
		}
	case PreparedExportInterface:
		if plan.InterfaceSchemas[target] == nil {
			return fmt.Errorf("prepared export %s targets missing interface schema %s", export.Name, target)
		}
	default:
		return fmt.Errorf("prepared export %s has invalid kind %s", export.Name, export.Kind)
	}
	return nil
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
	case OpApplyBinary, OpApplyUnary, OpIncDec, OpInterrupt, OpScopeEnter:
		value, ok := task.Data.(string)
		if !ok || value == "" {
			return fmt.Errorf("%s missing string payload", plan)
		}
		return nil
	case OpMember:
		data, ok := task.Data.(*MemberData)
		if !ok || data == nil || data.Property == "" {
			return fmt.Errorf("%s missing MemberData", plan)
		}
		if !data.ObjectType.IsEmpty() {
			return validateRuntimeType(plan+".object", data.ObjectType)
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
	case OpChanRecv:
		data, ok := task.Data.(*ChanRecvData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing ChanRecvData", plan)
		}
		return validateRuntimeType(plan+".result", data.ResultType)
	case OpChanSend:
		if task.Data != nil {
			return fmt.Errorf("%s must not carry payload", plan)
		}
		return nil
	case OpSelect:
		data, ok := task.Data.(*SelectData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing SelectData", plan)
		}
		return validateSelectData(plan, data, depth)
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
		if !data.ReceiverType.IsEmpty() {
			return validateRuntimeType(plan+".receiver", data.ReceiverType)
		}
		return nil
	case OpComposite:
		data, ok := task.Data.(*CompositeData)
		if !ok || data == nil {
			return fmt.Errorf("%s missing CompositeData", plan)
		}
		return validateRuntimeType(plan+".type", data.Type)
	case OpAddressOf:
		if task.Data != nil {
			return fmt.Errorf("%s must not carry payload", plan)
		}
		return nil
	case OpAddressAlloc:
		data, ok := task.Data.(RuntimeType)
		if !ok {
			return fmt.Errorf("%s missing RuntimeType", plan)
		}
		return validateRuntimeType(plan+".type", data)
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
	case OpLoadConst:
		name, ok := task.Data.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s missing constant name", plan)
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

func validateSelectData(plan string, data *SelectData, depth int) error {
	defaults := 0
	for i, c := range data.Cases {
		switch c.Kind {
		case SelectCommDefault:
			defaults++
			if defaults > 1 {
				return fmt.Errorf("%s has multiple default cases", plan)
			}
		case SelectCommRecv:
			if !c.RecvType.IsEmpty() {
				if err := validateRuntimeType(fmt.Sprintf("%s.case_%d_recv", plan, i), c.RecvType); err != nil {
					return err
				}
			}
			if !c.OKType.IsEmpty() {
				if err := validateRuntimeType(fmt.Sprintf("%s.case_%d_ok", plan, i), c.OKType); err != nil {
					return err
				}
			}
			if err := validateSymbolRef(fmt.Sprintf("%s.case_%d_recv_sym", plan, i), c.RecvSym); err != nil {
				return err
			}
			if err := validateSymbolRef(fmt.Sprintf("%s.case_%d_ok_sym", plan, i), c.RecvOKSym); err != nil {
				return err
			}
		case SelectCommSend:
		default:
			return fmt.Errorf("%s.case_%d has unknown select comm kind %q", plan, i, c.Kind)
		}
		if err := validatePreparedTaskPlan(fmt.Sprintf("%s.case_%d_body", plan, i), c.Body, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeType(plan string, typ RuntimeType) error {
	if typ.Kind == RuntimeTypeInvalid && typ.Raw.IsEmpty() {
		return fmt.Errorf("%s missing runtime type", plan)
	}
	if typ.Raw.IsEmpty() {
		return fmt.Errorf("%s missing raw runtime type", plan)
	}
	if _, err := ParseRuntimeType(typ.Raw); err != nil {
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
	if _, err := ParseRuntimeFuncSig(sig.Spec); err != nil {
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
		if _, err := ParseRuntimeType(field.Type); err != nil {
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
	case OpScopeEnter, OpForScopeEnter, OpRangeScopeEnter, OpSelectScopeEnter, OpCatchScopeEnter:
		return true
	default:
		return false
	}
}

func isScopeExitOp(op OpCode) bool {
	return op == OpScopeExit || op == OpForScopeExit
}
