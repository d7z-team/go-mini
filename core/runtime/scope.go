package runtime

import (
	"errors"
	"fmt"
)

func (s *Stack) DumpVariables() map[string]string {
	result := make(map[string]string)
	curr := s
	for curr != nil {
		if curr.Frame != nil {
			for name, sym := range curr.Symbols {
				if name == "" {
					continue
				}
				if _, exists := result[name]; exists {
					continue
				}
				if slot := lookupFrameSlotBySymbol(curr.Frame, sym); slot != nil && slot.Value != nil {
					result[name] = fmt.Sprintf("%v", slot.Value.Interface())
				}
			}
			if curr.Frame.Return != nil && curr.Frame.ReturnName != "" && curr.Frame.Return.Value != nil {
				if _, exists := result[curr.Frame.ReturnName]; !exists {
					result[curr.Frame.ReturnName] = fmt.Sprintf("%v", curr.Frame.Return.Value.Interface())
				}
			}
		}
		for name, slot := range curr.MemoryPtr {
			if _, exists := result[name]; !exists && slot != nil && slot.Value != nil {
				result[name] = fmt.Sprintf("%v", slot.Value.Interface())
			}
		}
		curr = curr.Parent
	}
	return result
}

func (f *SlotFrame) ensureLocalSlot(slot int, name string) {
	if f == nil || slot < 0 {
		return
	}
	if f.LocalIndex == nil {
		f.LocalIndex = make(map[string]int)
	}
	for len(f.Locals) <= slot {
		f.Locals = append(f.Locals, nil)
	}
	for len(f.LocalNames) <= slot {
		f.LocalNames = append(f.LocalNames, "")
	}
	if name != "" {
		if f.LocalNames[slot] == "" {
			f.LocalNames[slot] = name
		}
		f.LocalIndex[name] = slot
	}
}

func (f *SlotFrame) ensureUpvalueSlot(slot int, name string) {
	if f == nil || slot < 0 {
		return
	}
	if f.UpvalueIndex == nil {
		f.UpvalueIndex = make(map[string]int)
	}
	for len(f.Upvalues) <= slot {
		f.Upvalues = append(f.Upvalues, nil)
	}
	for len(f.UpvalueNames) <= slot {
		f.UpvalueNames = append(f.UpvalueNames, "")
	}
	if name != "" {
		if f.UpvalueNames[slot] == "" {
			f.UpvalueNames[slot] = name
		}
		f.UpvalueIndex[name] = slot
	}
}

func lookupFrameVarByName(frame *SlotFrame, name string) *Var {
	slot := lookupFrameSlotByName(frame, name)
	if slot == nil {
		return nil
	}
	return slot.Value
}

func lookupFrameSlotBySymbol(frame *SlotFrame, sym SymbolRef) *Slot {
	if frame == nil {
		return nil
	}
	switch sym.Kind {
	case SymbolLocal:
		if sym.Slot >= 0 && sym.Slot < len(frame.Locals) {
			return frame.Locals[sym.Slot]
		}
	case SymbolUpvalue:
		if sym.Slot >= 0 && sym.Slot < len(frame.Upvalues) {
			return frame.Upvalues[sym.Slot]
		}
	}
	return nil
}

func lookupStackSymbolByName(stack *Stack, name string) (SymbolRef, bool) {
	if stack == nil || name == "" {
		return SymbolRef{}, false
	}
	if sym, ok := stack.Symbols[name]; ok {
		return sym, true
	}
	return SymbolRef{}, false
}

func lookupFrameSlotByName(frame *SlotFrame, name string) *Slot {
	if frame == nil || name == "" {
		return nil
	}
	if slot, ok := frame.LocalIndex[name]; ok && slot >= 0 && slot < len(frame.Locals) {
		return frame.Locals[slot]
	}
	if slot, ok := frame.UpvalueIndex[name]; ok && slot >= 0 && slot < len(frame.Upvalues) {
		return frame.Upvalues[slot]
	}
	if frame.ReturnName == name {
		return frame.Return
	}
	return nil
}

func lookupFrameSymbolByName(frame *SlotFrame, name string) (SymbolRef, bool) {
	if frame == nil || name == "" {
		return SymbolRef{}, false
	}
	if slot, ok := frame.LocalIndex[name]; ok {
		return SymbolRef{Name: name, Kind: SymbolLocal, Slot: slot}, true
	}
	if slot, ok := frame.UpvalueIndex[name]; ok {
		return SymbolRef{Name: name, Kind: SymbolUpvalue, Slot: slot}, true
	}
	return SymbolRef{}, false
}

func loadVarFromScope(exec *Executor, shared *SharedState, stack *Stack, variable string) (*Var, error) {
	if variable == "nil" {
		return nil, nil
	}
	s := stack
	for s != nil {
		if sym, ok := lookupStackSymbolByName(s, variable); ok {
			if slot := lookupFrameSlotBySymbol(s.Frame, sym); slot != nil {
				return slot.Value, nil
			}
		}
		if slot, ok := s.MemoryPtr[variable]; ok {
			if slot == nil {
				return nil, nil
			}
			return slot.Value, nil
		}
		if v := lookupFrameVarByName(s.Frame, variable); v != nil {
			return v, nil
		}
		s = s.Parent
	}
	if shared != nil {
		if v, ok := shared.LoadGlobal(variable); ok {
			return v, nil
		}
	}
	if exec != nil {
		exec.mu.RLock()
		defer exec.mu.RUnlock()
		if fn, ok := exec.functions[variable]; ok {
			return &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
					BodyTasks:   cloneTasks(fn.BodyTasks),
					Context:     &LexicalContext{Executor: exec, Shared: shared, Stack: stack},
				},
				TypeInfo: MustParseRuntimeType(SpecClosure),
			}, nil
		}
		if route, ok := exec.routes[variable]; ok {
			return &Var{
				VType:    TypeAny,
				Ref:      route,
				TypeInfo: MustParseRuntimeType(SpecClosure),
			}, nil
		}
	}
	return nil, fmt.Errorf("undefined: %s", variable)
}

func storeVarToScope(exec *Executor, shared *SharedState, stack *Stack, variable string, expr *Var) error {
	if variable == "nil" {
		return nil
	}
	s := stack
	for s != nil {
		if sym, ok := lookupStackSymbolByName(s, variable); ok {
			ctx := &StackContext{Executor: exec, Shared: shared, Stack: s}
			return ctx.StoreSymbol(sym, expr)
		}
		if slot, ok := s.MemoryPtr[variable]; ok {
			ctx := &StackContext{Executor: exec, Shared: shared, Stack: s}
			return ctx.Assign(slot, expr)
		}
		s = s.Parent
	}
	if shared != nil && shared.HasGlobal(variable) {
		ctx := &StackContext{Executor: exec, Shared: shared, Stack: stack}
		return ctx.StoreSymbol(SymbolRef{Name: variable, Kind: SymbolGlobal, Slot: -1}, expr)
	}
	if stack == nil {
		return errors.New("missing lexical stack")
	}
	return fmt.Errorf("undefined: %s", variable)
}

func (ctx *StackContext) ScopeApply(scope string) error {
	if ctx == nil {
		return errors.New("missing stack context")
	}
	if ctx.Stack == nil {
		return errors.New("scope enter without active scope")
	}
	newDepth := 1
	var frame *SlotFrame
	if ctx.Stack != nil {
		newDepth = ctx.Stack.Depth + 1
		frame = ctx.Stack.Frame
	}
	if newDepth > DefaultMaxStackDepth {
		return errors.New("stack overflow")
	}
	ctx.Stack = &Stack{
		Parent:    ctx.Stack,
		MemoryPtr: make(map[string]*Slot),
		Symbols:   make(map[string]SymbolRef),
		Frame:     frame,
		FrameBase: frame,
		Scope:     scope,
		Depth:     newDepth,
		DeferOwner: func() *Stack {
			if ctx.Stack == nil {
				return nil
			}
			return ctx.Stack.CurrentDeferOwner()
		}(),
	}
	return nil
}

func cloneSlotFrame(frame *SlotFrame) *SlotFrame {
	if frame == nil {
		return &SlotFrame{}
	}
	cloned := &SlotFrame{
		Locals:       append([]*Slot(nil), frame.Locals...),
		LocalNames:   append([]string(nil), frame.LocalNames...),
		Upvalues:     append([]*Slot(nil), frame.Upvalues...),
		UpvalueNames: append([]string(nil), frame.UpvalueNames...),
		Return:       frame.Return,
		ReturnName:   frame.ReturnName,
	}
	if len(frame.LocalIndex) > 0 {
		cloned.LocalIndex = make(map[string]int, len(frame.LocalIndex))
		for k, v := range frame.LocalIndex {
			cloned.LocalIndex[k] = v
		}
	}
	if len(frame.UpvalueIndex) > 0 {
		cloned.UpvalueIndex = make(map[string]int, len(frame.UpvalueIndex))
		for k, v := range frame.UpvalueIndex {
			cloned.UpvalueIndex[k] = v
		}
	}
	return cloned
}

func (ctx *StackContext) ScopeApplyLoopBody(scope string) error {
	if ctx == nil {
		return errors.New("missing stack context")
	}
	if ctx.Stack == nil {
		return errors.New("loop scope enter without active scope")
	}
	newDepth := 1
	var parentFrame *SlotFrame
	if ctx.Stack != nil {
		newDepth = ctx.Stack.Depth + 1
		parentFrame = ctx.Stack.Frame
	}
	if newDepth > DefaultMaxStackDepth {
		return errors.New("stack overflow")
	}
	clonedFrame := cloneSlotFrame(parentFrame)
	syncLimit := 0
	if parentFrame != nil {
		syncLimit = len(parentFrame.Locals)
	}
	ctx.Stack = &Stack{
		Parent:    ctx.Stack,
		MemoryPtr: make(map[string]*Slot),
		Symbols:   make(map[string]SymbolRef),
		Frame:     clonedFrame,
		FrameBase: parentFrame,
		FrameSync: syncLimit,
		Scope:     scope,
		Depth:     newDepth,
		DeferOwner: func() *Stack {
			if ctx.Stack == nil {
				return nil
			}
			return ctx.Stack.CurrentDeferOwner()
		}(),
	}
	return nil
}

func (ctx *StackContext) SyncLoopScope() {
	if ctx.Stack == nil || ctx.Stack.FrameBase == nil || ctx.Stack.Frame == nil {
		return
	}
	base := ctx.Stack.FrameBase
	loop := ctx.Stack.Frame
	limit := ctx.Stack.FrameSync
	if limit > len(loop.Locals) {
		limit = len(loop.Locals)
	}
	for i := 0; i < limit; i++ {
		src := loop.Locals[i]
		if src == nil {
			continue
		}
		base.ensureLocalSlot(i, "")
		dst := base.Locals[i]
		if dst == nil {
			base.Locals[i] = src
			continue
		}
		_ = ctx.Assign(dst, src.Value)
	}
}

func (ctx *StackContext) WithScope(sType string, child func(ctx *StackContext)) error {
	if err := ctx.ScopeApply(sType); err != nil {
		return err
	}
	defer func() { _ = ctx.ScopeExit() }()
	child(ctx)
	return nil
}

func (ctx *StackContext) ScopeExit() error {
	if ctx == nil {
		return errors.New("missing stack context")
	}
	if ctx.Stack == nil {
		return errors.New("scope exit without active scope")
	}
	if ctx.Stack.Parent == nil {
		return errors.New("scope exit would leave root scope")
	}
	ctx.Stack = ctx.Stack.Parent
	return nil
}

func (ctx *StackContext) Store(variable string, expr *Var) error {
	if ctx.Stack != nil {
		if sym, ok := lookupStackSymbolByName(ctx.Stack, variable); ok {
			return ctx.StoreSymbol(sym, expr)
		}
	}
	return storeVarToScope(ctx.Executor, ctx.Shared, ctx.Stack, variable, expr)
}

func (ctx *StackContext) AddVariable(name string, v *Var) error {
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		ctx.Shared.StoreGlobal(name, cloneVarForAssign(v))
		return nil
	}
	ctx.Stack.MemoryPtr[name] = NewSlot(v.RuntimeType(), cloneVarForAssign(v))
	return nil
}

func (ctx *StackContext) DeclareSymbol(sym SymbolRef, kind RuntimeType) error {
	if sym.Kind != SymbolLocal || ctx.Stack == nil {
		return ctx.NewVar(sym.Name, kind)
	}
	if ctx.Stack.Frame == nil {
		ctx.Stack.Frame = &SlotFrame{}
	}
	ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
	if sym.Name != "" && sym.Name != "_" {
		if ctx.Stack.Symbols == nil {
			ctx.Stack.Symbols = make(map[string]SymbolRef)
		}
		ctx.Stack.Symbols[sym.Name] = sym
	}
	var v *Var
	if ctx.Executor != nil {
		var err error
		v, err = ctx.Executor.initializeType(ctx, kind, 0)
		if err != nil {
			return err
		}
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	ctx.Stack.Frame.Locals[sym.Slot] = NewSlot(kind, v)
	return nil
}

func (ctx *StackContext) Load(name string) (*Var, error) {
	if name == "nil" {
		return nil, nil
	}
	if ctx.Stack != nil {
		if sym, ok := lookupStackSymbolByName(ctx.Stack, name); ok {
			return ctx.LoadSymbol(sym)
		}
	}
	v, err := loadVarFromScope(ctx.Executor, ctx.Shared, ctx.Stack, name)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (ctx *StackContext) LoadSymbol(sym SymbolRef) (*Var, error) {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Locals) {
			if v := ctx.Stack.Frame.Locals[sym.Slot]; v != nil {
				return v.Value, nil
			}
		}
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Upvalues) {
			if v := ctx.Stack.Frame.Upvalues[sym.Slot]; v != nil {
				return v.Value, nil
			}
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			if v, ok := ctx.Shared.LoadGlobal(sym.Name); ok {
				return v, nil
			}
		}
	case SymbolBuiltin, SymbolUnknown:
	}
	return ctx.Load(sym.Name)
}

func (ctx *StackContext) CaptureVar(name string) (*Slot, error) {
	if ctx.Stack != nil {
		if sym, ok := lookupStackSymbolByName(ctx.Stack, name); ok {
			return ctx.CaptureSymbol(sym)
		}
	}
	s := ctx.Stack
	for s != nil {
		v, ok := s.MemoryPtr[name]
		if !ok {
			if sym, symOK := lookupStackSymbolByName(s, name); symOK {
				v = lookupFrameSlotBySymbol(s.Frame, sym)
			} else {
				v = lookupFrameSlotByName(s.Frame, name)
			}
			ok = v != nil
		}
		if ok {
			return v, nil
		}
		s = s.Parent
	}

	if ctx.Shared != nil {
		if v, ok := ctx.Shared.CaptureGlobalSlot(name); ok {
			return v, nil
		}
	}

	// 检查全局函数定义 (命名函数作为值被捕获)
	if ctx.Executor != nil {
		exec := ctx.Executor
		exec.mu.RLock()
		defer exec.mu.RUnlock()

		// 1. 尝试查找脚本定义的函数
		if fn, ok := exec.functions[name]; ok {
			return NewSlot(MustParseRuntimeType(SpecClosure), &Var{
				VType: TypeClosure,
				Ref: &VMClosure{
					FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
					BodyTasks:   cloneTasks(fn.BodyTasks),
					Context:     &LexicalContext{Executor: ctx.Executor, Shared: ctx.Shared, Stack: ctx.Stack},
				},
				TypeInfo: MustParseRuntimeType(SpecClosure),
			}), nil
		}

		// 2. 尝试查找 FFI 路由
		if route, ok := exec.routes[name]; ok {
			return NewSlot(MustParseRuntimeType(SpecClosure), &Var{
				VType:    TypeAny,
				Ref:      route,
				TypeInfo: MustParseRuntimeType(SpecClosure),
			}), nil
		}
	}

	return nil, fmt.Errorf("undefined capture: %s", name)
}

func (ctx *StackContext) CaptureSymbol(sym SymbolRef) (*Slot, error) {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 {
			ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
			v := ctx.Stack.Frame.Locals[sym.Slot]
			if v == nil {
				return nil, fmt.Errorf("undefined local capture: %s", sym.Name)
			}
			return v, nil
		}
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil && sym.Slot >= 0 && sym.Slot < len(ctx.Stack.Frame.Upvalues) {
			if v := ctx.Stack.Frame.Upvalues[sym.Slot]; v != nil {
				return v, nil
			}
			return nil, fmt.Errorf("undefined upvalue capture: %s", sym.Name)
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			if v, ok := ctx.Shared.CaptureGlobalSlot(sym.Name); ok {
				return v, nil
			}
		}
	}
	return ctx.CaptureVar(sym.Name)
}

func (ctx *StackContext) Interrupt() bool {
	return ctx.Stack != nil && ctx.Stack.interrupt != ""
}

func (ctx *StackContext) SetInterrupt(scopeName, interruptType string) error {
	s := ctx.Stack
	for s != nil {
		s.interrupt = interruptType
		if s.Scope == scopeName {
			return nil
		}
		s = s.Parent
	}
	return fmt.Errorf("scope %s not found", scopeName)
}

func (ctx *StackContext) NewVar(name string, kind RuntimeType) error {
	if ctx.Stack != nil {
		if _, ok := lookupStackSymbolByName(ctx.Stack, name); ok {
			return nil
		}
	}
	if _, ok := ctx.Stack.MemoryPtr[name]; ok {
		return nil
	}
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		if _, ok := ctx.Shared.LoadGlobal(name); ok {
			return nil
		}
	}
	// 确保变量被正确初始化为零值
	var v *Var
	if ctx.Executor != nil {
		var err error
		v, err = ctx.Executor.initializeType(ctx, kind, 0)
		if err != nil {
			return err
		}
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	if ctx.Stack != nil && ctx.Stack.Depth == 1 && ctx.Stack.Scope == "global" && ctx.Shared != nil {
		ctx.Shared.StoreGlobal(name, v)
		return nil
	}
	ctx.Stack.MemoryPtr[name] = NewSlot(kind, v)
	return nil
}

func (ctx *StackContext) InitReturn(kind RuntimeType) error {
	if ctx.Stack == nil {
		return errors.New("missing stack for return slot")
	}
	if ctx.Stack.Frame == nil {
		ctx.Stack.Frame = &SlotFrame{}
	}
	if ctx.Stack.Frame.Return != nil {
		return nil
	}
	var v *Var
	if ctx.Executor != nil {
		var err error
		v, err = ctx.Executor.initializeType(ctx, kind, 0)
		if err != nil {
			return err
		}
	} else {
		v = NewVarWithRuntimeType(kind, TypeAny)
	}
	ctx.Stack.Frame.Return = NewSlot(kind, v)
	ctx.Stack.Frame.ReturnName = "__return__"
	return nil
}

func (ctx *StackContext) LoadReturn() (*Var, error) {
	if ctx.Stack != nil && ctx.Stack.Frame != nil && ctx.Stack.Frame.Return != nil {
		return ctx.Stack.Frame.Return.Value, nil
	}
	return nil, errors.New("missing return slot")
}

func (ctx *StackContext) StoreReturn(expr *Var) error {
	if ctx.Stack == nil || ctx.Stack.Frame == nil || ctx.Stack.Frame.Return == nil {
		return errors.New("missing return slot")
	}
	return ctx.Assign(ctx.Stack.Frame.Return, expr)
}

func (ctx *StackContext) Assign(slot *Slot, expr *Var) error {
	if slot == nil {
		return errors.New("missing assignment slot")
	}
	value, err := ctx.prepareAssignedValue(slot.Decl, expr)
	if err != nil {
		return err
	}
	slot.Value = value
	return nil
}

func (ctx *StackContext) prepareAssignedValue(target RuntimeType, expr *Var) (*Var, error) {
	if target.IsEmpty() {
		if expr != nil && !expr.RuntimeType().IsEmpty() {
			target = expr.RuntimeType()
		} else {
			target = MustParseRuntimeType("Any")
		}
	}
	if expr == nil {
		return nilValueForType(target)
	}
	if !target.IsEmpty() && target.Kind != RuntimeTypeTuple && expr.RuntimeType().Kind == RuntimeTypeTuple {
		return nil, fmt.Errorf("multiple-value value cannot be assigned to %s", target.Raw)
	}
	if target.IsAny() {
		return cloneVarForAssign(expr), nil
	}
	if target.Kind == RuntimeTypeTuple {
		actual := expr
		if ctx.Executor != nil {
			actual = ctx.Executor.unwrapValue(expr)
		}
		if actual == nil || actual.VType != TypeArray {
			return nil, fmt.Errorf("type mismatch: cannot assign %s to %s", runtimeTypeForAssignment(actual).Raw, target.Raw)
		}
		arr, ok := actual.Ref.(*VMArray)
		if !ok {
			return nil, fmt.Errorf("type mismatch: expected tuple-compatible array for %s", target.Raw)
		}
		rawItems := arr.Snapshot()
		if len(rawItems) != len(target.Params) {
			return nil, fmt.Errorf("tuple assignment count mismatch: %d = %d", len(rawItems), len(target.Params))
		}
		items := make([]*Var, len(rawItems))
		for i := range rawItems {
			item, err := ctx.prepareAssignedValue(target.Params[i], rawItems[i])
			if err != nil {
				return nil, fmt.Errorf("tuple item %d: %w", i, err)
			}
			items[i] = item
		}
		return &Var{TypeInfo: target, VType: TypeArray, Ref: &VMArray{Data: items}}, nil
	}
	if target.IsInterface() {
		if ctx.Executor == nil {
			return nil, fmt.Errorf("missing executor for interface assignment to %s", target.Raw)
		}
		if expr.RuntimeType().IsInterface() {
			if expr.RawType().Equals(target.Raw) {
				return cloneVarForAssign(expr), nil
			}
			if inter, ok := expr.Ref.(*VMInterface); ok && inter.Target != nil {
				return ctx.Executor.CheckSatisfaction(inter.Target, string(target.Raw))
			}
		}
		return ctx.Executor.CheckSatisfaction(expr, string(target.Raw))
	}
	if ctx.Executor != nil && target.Kind == RuntimeTypeNamed {
		if _, ok := ctx.Executor.resolveInterfaceSpec(target.Raw); ok {
			if expr.RuntimeType().IsInterface() {
				if expr.RawType().Equals(target.Raw) {
					return cloneVarForAssign(expr), nil
				}
				if inter, ok := expr.Ref.(*VMInterface); ok && inter.Target != nil {
					return ctx.Executor.CheckSatisfaction(inter.Target, string(target.Raw))
				}
			}
			return ctx.Executor.CheckSatisfaction(expr, string(target.Raw))
		}
	}
	actual := expr
	if ctx.Executor != nil {
		actual = ctx.Executor.unwrapValue(expr)
	} else if expr != nil && expr.VType == TypeAny {
		if inner, ok := expr.Ref.(*Var); ok {
			actual = inner
		}
	}
	source := runtimeTypeForAssignment(actual)
	if source.IsEmpty() || source.IsAny() {
		return nil, fmt.Errorf("cannot assign dynamically typed value to %s", target.Raw)
	}
	if !source.IsAssignableTo(target) {
		return nil, fmt.Errorf("type mismatch: cannot assign %s to %s", source.Raw, target.Raw)
	}
	if target.IsNumeric() && source.IsNumeric() {
		switch {
		case target.IsInt():
			switch actual.VType {
			case TypeInt:
				value := NewInt(actual.I64)
				value.SetRuntimeType(target)
				return value, nil
			case TypeFloat:
				value := NewInt(int64(actual.F64))
				value.SetRuntimeType(target)
				return value, nil
			}
		default:
			switch actual.VType {
			case TypeInt:
				value := NewFloat(float64(actual.I64))
				value.SetRuntimeType(target)
				return value, nil
			case TypeFloat:
				value := NewFloat(actual.F64)
				value.SetRuntimeType(target)
				return value, nil
			}
		}
		return nil, fmt.Errorf("type mismatch: expected numeric value for %s, got %s", target.Raw, actual.VType)
	}
	if target.IsString() && source.Raw == TypeSpec("Error") {
		text, err := actual.ToError()
		if err != nil {
			return nil, err
		}
		value := NewString(text)
		value.SetRuntimeType(target)
		return value, nil
	}
	value := cloneVarForAssign(actual)
	if value != nil {
		value.SetRuntimeType(target)
	}
	return value, nil
}

func runtimeTypeForAssignment(v *Var) RuntimeType {
	if v == nil {
		return RuntimeType{}
	}
	declared := v.RuntimeType()
	if !declared.IsEmpty() && !declared.IsAny() {
		return declared
	}
	switch v.VType {
	case TypeInt:
		return MustParseRuntimeType("Int64")
	case TypeFloat:
		return MustParseRuntimeType("Float64")
	case TypeString:
		return MustParseRuntimeType("String")
	case TypeBytes:
		return MustParseRuntimeType("TypeBytes")
	case TypeBool:
		return MustParseRuntimeType("Bool")
	case TypeError:
		return MustParseRuntimeType("Error")
	case TypeStruct:
		if st, ok := v.Ref.(*VMStruct); ok && st != nil && st.Spec != nil && !st.Spec.TypeInfo.IsEmpty() {
			return st.Spec.TypeInfo
		}
		return declared
	case TypeInterface:
		if !declared.IsEmpty() {
			return declared
		}
		return MustParseRuntimeType("Any")
	case TypeClosure:
		if cl, ok := v.Ref.(*VMClosure); ok && cl != nil && cl.FunctionSig != nil {
			return runtimeTypeFromFuncSig(cl.FunctionSig)
		}
		if route, ok := v.Ref.(FFIRoute); ok && route.FuncSig != nil {
			return runtimeTypeFromFuncSig(route.FuncSig)
		}
		return declared
	default:
		return declared
	}
}

func runtimeTypeFromFuncSig(sig *RuntimeFuncSig) RuntimeType {
	if sig == nil {
		return RuntimeType{}
	}
	spec := sig.Spec
	if spec.IsEmpty() {
		spec = TypeSpec(sig.SignatureString())
	}
	typ, err := ParseRuntimeType(spec)
	if err != nil {
		return RuntimeType{Raw: spec}
	}
	return typ
}

func nilValueForType(target RuntimeType) (*Var, error) {
	switch {
	case target.IsAny():
		return NewVarWithRuntimeType(target, TypeAny), nil
	case target.IsInterface():
		return NewVarWithRuntimeType(target, TypeInterface), nil
	case target.IsPtr():
		return NewVarWithRuntimeType(target, TypeHandle), nil
	case target.IsHostRef():
		return NewVarWithRuntimeType(target, TypeHostRef), nil
	case target.IsArray():
		return &Var{TypeInfo: target, VType: TypeArray, Ref: &VMArray{Data: nil}}, nil
	case target.IsMap():
		return &Var{TypeInfo: target, VType: TypeMap, Ref: &VMMap{Data: nil}}, nil
	case target.Kind == RuntimeTypeTuple:
		return &Var{TypeInfo: target, VType: TypeArray, Ref: &VMArray{Data: make([]*Var, len(target.Params))}}, nil
	case target.Kind == RuntimeTypeFunction:
		return NewVarWithRuntimeType(target, TypeClosure), nil
	case target.Raw == "TypeBytes":
		return &Var{TypeInfo: target, VType: TypeBytes}, nil
	case target.Raw == "Error":
		return nil, nil
	default:
		return nil, fmt.Errorf("nil is not assignable to %s", target.Raw)
	}
}

func newSlotForExpr(expr *Var) *Slot {
	if expr == nil {
		return NewSlot(MustParseRuntimeType("Any"), nil)
	}
	return NewSlot(expr.RuntimeType(), nil)
}

func (ctx *StackContext) storeSlot(slot **Slot, expr *Var) error {
	if *slot == nil {
		*slot = newSlotForExpr(expr)
	}
	return ctx.Assign(*slot, expr)
}

func (ctx *StackContext) StoreSymbol(sym SymbolRef, expr *Var) error {
	switch sym.Kind {
	case SymbolLocal:
		if ctx.Stack == nil {
			return ctx.Store(sym.Name, expr)
		}
		if ctx.Stack.Frame == nil {
			ctx.Stack.Frame = &SlotFrame{}
		}
		ctx.Stack.Frame.ensureLocalSlot(sym.Slot, sym.Name)
		if sym.Name != "" && sym.Name != "_" {
			if ctx.Stack.Symbols == nil {
				ctx.Stack.Symbols = make(map[string]SymbolRef)
			}
			ctx.Stack.Symbols[sym.Name] = sym
		}
		return ctx.storeSlot(&ctx.Stack.Frame.Locals[sym.Slot], expr)
	case SymbolUpvalue:
		if ctx.Stack != nil && ctx.Stack.Frame != nil {
			ctx.Stack.Frame.ensureUpvalueSlot(sym.Slot, sym.Name)
			if sym.Name != "" && sym.Name != "_" {
				if ctx.Stack.Symbols == nil {
					ctx.Stack.Symbols = make(map[string]SymbolRef)
				}
				ctx.Stack.Symbols[sym.Name] = sym
			}
			return ctx.storeSlot(&ctx.Stack.Frame.Upvalues[sym.Slot], expr)
		}
	case SymbolGlobal:
		if ctx.Shared != nil {
			return ctx.Shared.UpdateGlobal(sym.Name, func(current *Slot, exists bool) (*Slot, error) {
				if !exists || current == nil {
					current = newSlotForExpr(expr)
				}
				if err := ctx.Assign(current, expr); err != nil {
					return nil, err
				}
				return current, nil
			})
		}
	}
	return ctx.Store(sym.Name, expr)
}

func (ctx *StackContext) WithFuncScope(name string, exec func(*Stack, *StackContext) error) error {
	old := ctx.Stack
	root := old
	for root != nil && root.Parent != nil {
		root = root.Parent
	}
	ctx.Stack = root
	if err := ctx.ScopeApply(name); err != nil {
		ctx.Stack = old
		return err
	}
	defer func() { ctx.Stack = old }()
	return exec(old, ctx)
}

func (ctx *StackContext) GenerateStackTrace(current *Task) []StackFrame {
	var frames []StackFrame

	// 1. Add current frame
	if current != nil && current.Source != nil {
		funcName := "main"
		if ctx.Stack != nil && ctx.Stack.Scope != "" {
			funcName = ctx.Stack.Scope
		}
		frames = append(frames, StackFrame{
			Filename: current.Source.File,
			Function: funcName,
			Line:     current.Source.Line,
			Column:   current.Source.Col,
		})
	}

	// 2. Reconstruct previous frames from TaskStack
	for i := len(ctx.TaskStack) - 1; i >= 0; i-- {
		task := ctx.TaskStack[i]
		if task.Op == OpCallBoundary && task.Source != nil {
			callerName := "main"
			for j := i - 1; j >= 0; j-- {
				if ctx.TaskStack[j].Op == OpCallBoundary {
					if d2, ok := ctx.TaskStack[j].Data.(*CallBoundaryData); ok && d2 != nil && d2.Name != "" {
						callerName = d2.Name
						break
					}
				}
			}
			frames = append(frames, StackFrame{
				Filename: task.Source.File,
				Function: callerName,
				Line:     task.Source.Line,
				Column:   task.Source.Col,
			})
		}
		if len(frames) > 20 {
			break
		}
	}

	return frames
}

func isEmptyVar(v *Var) bool {
	if v == nil {
		return true
	}
	switch v.VType {
	case TypeArray:
		if arr, ok := v.Ref.(*VMArray); ok {
			return arr == nil
		}
		return v.Ref == nil
	case TypeMap:
		if m, ok := v.Ref.(*VMMap); ok {
			return m == nil
		}
		return v.Ref == nil
	case TypeHandle:
		return v.Handle == 0
	case TypeHostRef:
		return v.Handle == 0
	case TypeAny:
		return v.Ref == nil
	}
	return false
}

type Program struct{}
