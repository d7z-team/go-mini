package runtime

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (e *Executor) dispatch(session *StackContext, task Task) error {
	switch task.Op {
	case OpLineStep:
		// Should be handled in the main loop before dispatch
		return nil
	case OpDeclareInitVars:
		data, ok := task.Data.(*VarDeclData)
		if !ok || data == nil {
			return errors.New("OpDeclareInitVars missing VarDeclData")
		}
		return e.declareInitVars(session, data)
	case OpApplyBinary:
		op := task.Data.(string)
		r := session.ValueStack.Pop()
		l := session.ValueStack.Pop()
		res, err := e.evalBinaryExprDirect(op, l, r)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpApplyUnary:
		op := task.Data.(string)
		val := session.ValueStack.Pop()
		if op == "ToBool" {
			b, err := val.ToBool()
			if err != nil {
				return err
			}
			session.ValueStack.Push(NewBool(b))
			return nil
		}
		res, err := e.evalUnaryExprDirect(op, val)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpJumpIf:
		var op string
		var rightTasks []Task
		if data, ok := task.Data.(*JumpData); ok {
			op = data.Operator
			rightTasks = data.Right
		} else {
			return errors.New("OpJumpIf missing JumpData")
		}
		l := session.ValueStack.Peek()
		lb, err := l.ToBool()
		if err != nil {
			return err
		}
		if op == "&&" || op == "And" {
			if !lb {
				// Short circuit, pop the left value and push false
				session.ValueStack.Pop()
				session.ValueStack.Push(NewBool(false))
				return nil
			}
		} else { // || or Or
			if lb {
				// Short circuit, pop the left value and push true
				session.ValueStack.Pop()
				session.ValueStack.Push(NewBool(true))
				return nil
			}
		}
		session.ValueStack.Pop() // Discard Left
		// Push a task to evaluate Right and ensure it's converted to Bool
		session.TaskStack = append(session.TaskStack, Task{Op: OpApplyUnary, Data: "ToBool"}) // a pseudo unary to enforce bool
		session.TaskStack = append(session.TaskStack, rightTasks...)
		return nil
	case OpLoadVar:
		var (
			name string
			sym  SymbolRef
		)
		switch data := task.Data.(type) {
		case string:
			name = data
		case *LoadVarData:
			name = data.Name
			sym = data.Sym
		default:
			return errors.New("OpLoadVar missing LoadVarData")
		}
		var (
			v   *Var
			err error
		)
		if sym.Kind != SymbolUnknown {
			v, err = session.LoadSymbol(sym)
		} else {
			v, err = session.Load(name)
		}
		if err != nil {
			exec := session.Executor
			if exec != nil {
				if path, ok := exec.importAliases[name]; ok {
					if mod, ok := session.Shared.Module(path); ok {
						session.ValueStack.Push(mod)
						return nil
					}
				}
			}
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpLoadLocal:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpLoadLocal missing SymbolRef")
		}
		v, err := session.LoadSymbol(sym)
		if err != nil {
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpLoadUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpLoadUpvalue missing SymbolRef")
		}
		v, err := session.LoadSymbol(sym)
		if err != nil {
			return err
		}
		session.ValueStack.Push(v)
		return nil
	case OpStoreLocal:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpStoreLocal missing SymbolRef")
		}
		return session.StoreSymbol(sym, session.ValueStack.Pop())
	case OpStoreUpvalue:
		sym, ok := task.Data.(SymbolRef)
		if !ok {
			return errors.New("OpStoreUpvalue missing SymbolRef")
		}
		return session.StoreSymbol(sym, session.ValueStack.Pop())
	case OpScopeEnter:
		scopeName, ok := task.Data.(string)
		if !ok || scopeName == "" {
			return errors.New("OpScopeEnter missing scope name")
		}
		return session.ScopeApply(scopeName)
	case OpScopeExit:
		return session.ScopeExit()
	case OpAssign:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		val := session.ValueStack.Pop()
		lhs := session.LHSStack.Pop()
		return e.assignAddress(session, lhs, val)
	case OpDoCall:
		call := task.Data.(*DoCallData)
		return e.setupFuncCall(session, call.Name, call, call.Args, nil)
	case OpMultiAssign:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		data, ok := task.Data.(*MultiAssignData)
		if !ok || data == nil {
			return errors.New("OpMultiAssign missing MultiAssignData")
		}
		if data.LHSCount <= 0 {
			return fmt.Errorf("multi assignment missing LHS count: %d", data.LHSCount)
		}
		lhsCount := data.LHSCount
		descs := make([]LHSValue, lhsCount)
		for i := lhsCount - 1; i >= 0; i-- {
			descs[i] = session.LHSStack.Pop()
		}

		var elements []*Var
		switch data.Mode {
		case MultiAssignDestructure:
			if data.ValueCount != 1 {
				return fmt.Errorf("multi assignment expected one destructured value, got %d", data.ValueCount)
			}
			val := session.ValueStack.Pop()
			if val == nil {
				return errors.New("multi assignment: RHS evaluated to nil")
			}
			val = e.unwrapValue(val)
			if val == nil {
				return errors.New("multi assignment: RHS evaluated to nil")
			}
			switch val.VType {
			case TypeArray:
				rawElements := val.Ref.(*VMArray).Snapshot()
				elements = make([]*Var, len(rawElements))
				for i, v := range rawElements {
					if v != nil {
						elements[i] = cloneVarForAssign(v)
					} else {
						elements[i] = nil
					}
				}
			default:
				return &VMError{Message: fmt.Sprintf("cannot destructure type %v", val.VType), IsPanic: true}
			}
			if len(elements) != lhsCount {
				return &VMError{Message: fmt.Sprintf("multi assignment: destructure count mismatch (need %d, got %d)", lhsCount, len(elements)), IsPanic: true}
			}
		case MultiAssignPerBinding:
			if data.ValueCount != lhsCount {
				return fmt.Errorf("multi assignment count mismatch: %d names = %d values", lhsCount, data.ValueCount)
			}
			elements = make([]*Var, data.ValueCount)
			for i := data.ValueCount - 1; i >= 0; i-- {
				elements[i] = cloneVarForAssign(session.ValueStack.Pop())
			}
		default:
			return fmt.Errorf("unknown multi assignment mode: %s", data.Mode)
		}

		for i := 0; i < lhsCount; i++ {
			if err := e.storeAddress(session, descs[i], elements[i]); err != nil {
				return err
			}
		}
		return nil
	case OpIncDec:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		op := task.Data.(string)
		lhs := session.LHSStack.Pop()
		return e.updateAddress(session, lhs, op)
	case OpReturn:
		count := task.Data.(int)
		if count > 1 {
			// 多返回值，打包成 Tuple
			elements := make([]*Var, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = session.ValueStack.Pop()
			}
			res := &Var{VType: TypeArray, Ref: &VMArray{Data: elements}}
			if err := session.StoreReturn(res); err != nil {
				return err
			}
		} else if count == 1 {
			// 单返回值
			res := session.ValueStack.Pop()
			if res != nil {
				if err := session.StoreReturn(res); err != nil {
					return err
				}
			}
		}
		session.UnwindMode = UnwindReturn
		return nil
	case OpInterrupt:
		interruptType := task.Data.(string)
		switch interruptType {
		case "break":
			session.UnwindMode = UnwindBreak
		case "continue":
			session.UnwindMode = UnwindContinue
		}
		return nil
	case OpEvalLHS:
		if session.LHSStack == nil {
			session.LHSStack = &LHSStack{}
		}
		if task.Data == nil {
			session.LHSStack.Push(nil)
			return nil
		}
		if lhsData, ok := task.Data.(*LHSData); ok {
			switch lhsData.Kind {
			case LHSTypeEnv:
				session.LHSStack.Push(&LHSEnv{Name: lhsData.Name, Sym: lhsData.Sym})
				return nil
			case LHSTypeIndex:
				idx := e.unwrapAddressVar(session.ValueStack.Pop())
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				if idx != nil {
					idx = cloneVarForAssign(idx)
				}
				session.LHSStack.Push(&LHSIndex{Obj: obj, Index: idx})
				return nil
			case LHSTypeMember:
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSMember{Obj: obj, Property: lhsData.Property})
				return nil
			case LHSTypeStar:
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSDeref{Target: obj})
				return nil
			case LHSTypeSlice:
				var high, low *Var
				if lhsData.HasHigh {
					high = e.unwrapAddressVar(session.ValueStack.Pop())
					if high != nil {
						high = cloneVarForAssign(high)
					}
				}
				if lhsData.HasLow {
					low = e.unwrapAddressVar(session.ValueStack.Pop())
					if low != nil {
						low = cloneVarForAssign(low)
					}
				}
				obj := e.unwrapAddressVar(session.ValueStack.Pop())
				session.LHSStack.Push(&LHSSlice{Obj: obj, Low: low, High: high})
				return nil
			case LHSTypeNone:
				session.LHSStack.Push(nil)
				return nil
			}
		}

		return errors.New("OpEvalLHS missing LHSData")
	case OpIndex:
		idx := session.ValueStack.Pop()
		obj := session.ValueStack.Pop()
		data, ok := task.Data.(*IndexData)
		if !ok {
			return errors.New("OpIndex missing IndexData")
		}

		if data.Multi {
			obj = e.unwrapValue(obj)
			if obj == nil || isEmptyVar(obj) {
				return errors.New("index access on nil")
			}
			if idx == nil {
				return errors.New("index access with nil index")
			}
			if obj.VType == TypeMap {
				m := obj.Ref.(*VMMap)
				key, err := e.varToMapKey(idx)
				if err != nil {
					return err
				}
				tuple := make([]*Var, 2)
				if val, ok := m.Load(key); ok {
					tuple[0] = val
					tuple[1] = NewBool(true)
				} else {
					_, valType, _ := obj.RuntimeType().GetMapKeyValueTypes()
					tuple[0] = e.ToVar(session, valType.ZeroVar(), nil)
					tuple[1] = NewBool(false)
				}
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(data.ResultType)
				session.ValueStack.Push(v)
				return nil
			}
			return fmt.Errorf("multi-index only supported for maps, got %v", obj.VType)
		}
		res, err := e.evalIndexExprDirect(session, obj, idx)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpMember:
		prop := task.Data.(string)
		obj := session.ValueStack.Pop()
		res, err := e.evalMemberExprDirect(session, obj, prop)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpPop:
		session.ValueStack.Pop()
		return nil
	case OpComposite:
		var (
			typ     RuntimeType
			entries []CompositeEntryData
		)
		if data, ok := task.Data.(*CompositeData); ok {
			typ = data.Type
			entries = data.Entries
		} else {
			return errors.New("OpComposite missing CompositeData")
		}
		exec := e
		if session.Executor != nil {
			exec = session.Executor
		}
		if typ.IsHostRef() || exec.runtimeTypeContainsHostOpaqueValue(typ, 0) {
			return &VMError{Message: fmt.Sprintf("opaque host type %s cannot be created by VM", typ.Raw), IsPanic: true}
		}
		isArray := typ.IsArray()
		isMap := typ.IsMap()

		if isArray {
			elemType, _ := typ.ReadArrayItemType()
			res := make([]*Var, len(entries))
			// ValueStack has [V1, V2, ..., VN]
			// So we must pop in reverse: V_N first, then V_N-1...
			for i := len(entries) - 1; i >= 0; i-- {
				val := e.normalizeTypedValue(session.ValueStack.Pop(), elemType)
				res[i] = val
			}
			v := &Var{VType: TypeArray, Ref: &VMArray{Data: res}}
			v.SetRuntimeType(typ)
			session.ValueStack.Push(v)
			return nil
		}

		if !isMap && !typ.IsEmpty() {
			v, err := exec.initializeType(session, typ, 0)
			if err != nil {
				return err
			}
			if v.VType != TypeStruct {
				fields := make([]*Slot, len(entries))
				byName := make(map[string]int, len(entries))
				specFields := make([]RuntimeStructField, len(entries))
				for i := len(entries) - 1; i >= 0; i-- {
					val := session.ValueStack.Pop()
					fieldName := entries[i].IdentKey
					if fieldName == "" {
						fieldName = strconv.Itoa(i)
					}
					fieldType := MustParseRuntimeType("Any")
					if val != nil && !val.RuntimeType().IsEmpty() {
						fieldType = val.RuntimeType()
					}
					fields[i] = NewSlot(fieldType, cloneVarForAssign(val))
					byName[fieldName] = i
					specFields[i] = RuntimeStructField{Name: fieldName, TypeInfo: fieldType}
				}
				v = &Var{
					VType:    TypeStruct,
					TypeInfo: typ,
					Ref: &VMStruct{
						Spec:   &RuntimeStructSpec{Spec: typ.Raw, TypeInfo: typ, Fields: specFields},
						Fields: fields,
						ByName: byName,
					},
				}
				session.ValueStack.Push(v)
				return nil
			}
			st := v.Ref.(*VMStruct)
			for i := len(entries) - 1; i >= 0; i-- {
				val := session.ValueStack.Pop()
				fieldName := entries[i].IdentKey
				if fieldName == "" && i < len(st.Fields) && st.Spec != nil && i < len(st.Spec.Fields) {
					fieldName = st.Spec.Fields[i].Name
				}
				field, ok := st.Field(fieldName)
				if !ok {
					return &VMError{Message: fmt.Sprintf("unknown field %s in %s", fieldName, typ.Raw), IsPanic: true}
				}
				if err := session.Assign(field, val); err != nil {
					return err
				}
			}
			session.ValueStack.Push(v)
			return nil
		}

		res := make(map[string]*Var)
		var keyType RuntimeType
		var valType RuntimeType
		keyType, valType, _ = typ.GetMapKeyValueTypes()

		// Values are pushed as [..., K_i, V_i]
		// So we must pop in reverse order: V_i then K_i
		for i := len(entries) - 1; i >= 0; i-- {
			val := session.ValueStack.Pop()
			val = e.normalizeTypedValue(val, valType)

			keyName := ""
			if entries[i].IdentKey != "" {
				keyName = entries[i].IdentKey
			} else if entries[i].HasExprKey {
				keyVal := session.ValueStack.Pop()
				var err error
				keyName, err = exec.varToTypedMapKey(keyVal, keyType)
				if err != nil {
					return err
				}
			}

			res[keyName] = val
		}
		v := &Var{VType: TypeMap, Ref: &VMMap{Data: res}}
		v.SetRuntimeType(typ)
		session.ValueStack.Push(v)
		return nil
	case OpSlice:
		var high, low, obj *Var
		data := task.Data.(*SliceData)
		if data.HasHigh {
			high = session.ValueStack.Pop()
		}
		if data.HasLow {
			low = session.ValueStack.Pop()
		}
		obj = session.ValueStack.Pop()

		res, err := e.evalSliceExprDirect(session, obj, low, high)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpCall:
		var name string
		var receiver *Var
		var mod *VMModule
		var callable *Var
		data := task.Data.(*CallData)
		argCount := data.ArgCount

		// Arguments are on top of stack, then Func eval result (if any)
		// Let's pop arguments first!
		args := make([]*Var, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = session.ValueStack.Pop()
		}
		var argLHS []LHSValue
		if data.CaptureArgLHS {
			argLHS = make([]LHSValue, argCount)
			for i := argCount - 1; i >= 0; i-- {
				argLHS[i] = session.LHSStack.Pop()
			}
		}

		// 处理变长参数展开 f(args...)
		ellipsis := data.Ellipsis
		if ellipsis && len(args) > 0 {
			last := e.unwrapValue(args[len(args)-1])
			if last != nil && last.VType == TypeArray {
				arr := last.Ref.(*VMArray)
				items := arr.Snapshot()
				newArgs := make([]*Var, len(args)-1+len(items))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], items)
				args = newArgs
			}
		}

		switch data.Mode {
		case CallByName:
			name = data.Name
		case CallByMember:
			obj := session.ValueStack.Pop()
			if obj == nil {
				return errors.New("calling method on nil object")
			}

			res, err := e.evalMemberExprDirect(session, obj, data.Name)
			if err != nil {
				return err
			}

			if res != nil && res.VType == TypeClosure {
				callable = res
			} else if res != nil && res.VType == TypeModule {
				mod = res.Ref.(*VMModule)
				name = data.Name
			} else if res != nil {
				callable = res
			} else {
				return fmt.Errorf("property %s is not a callable function on %v", data.Name, obj.VType)
			}
		case CallByValue:
			callable = session.ValueStack.Pop()
		}

		if name != "" && mod == nil && callable == nil {
			loadTarget := data.Sym
			if loadTarget.Kind == SymbolUnknown {
				loadTarget = SymbolRef{Name: name}
			}
			if v, err := session.LoadSymbol(loadTarget); err == nil && v != nil {
				callable = v
			}
		}

		totalArgs := len(args)
		offset := 0
		if receiver != nil {
			totalArgs++
			offset = 1
		}
		finalArgs := make([]*Var, totalArgs)
		var finalArgLHS []LHSValue
		if argLHS != nil {
			finalArgLHS = make([]LHSValue, totalArgs)
		}
		if receiver != nil {
			finalArgs[0] = receiver
			if finalArgLHS != nil {
				finalArgLHS[0] = nil
			}
		}
		copy(finalArgs[offset:], args)
		if finalArgLHS != nil {
			copy(finalArgLHS[offset:], argLHS)
		}

		return e.invokeCall(session, name, receiver, mod, callable, finalArgs, finalArgLHS)
	case OpGo:
		var name string
		var receiver *Var
		var mod *VMModule
		var callable *Var
		data := task.Data.(*CallData)
		argCount := data.ArgCount

		args := make([]*Var, argCount)
		for i := argCount - 1; i >= 0; i-- {
			args[i] = session.ValueStack.Pop()
		}

		if data.Ellipsis && len(args) > 0 {
			last := e.unwrapValue(args[len(args)-1])
			if last != nil && last.VType == TypeArray {
				arr := last.Ref.(*VMArray)
				items := arr.Snapshot()
				newArgs := make([]*Var, len(args)-1+len(items))
				copy(newArgs, args[:len(args)-1])
				copy(newArgs[len(args)-1:], items)
				args = newArgs
			}
		}

		switch data.Mode {
		case CallByName:
			name = data.Name
		case CallByMember:
			obj := session.ValueStack.Pop()
			if obj == nil {
				return errors.New("calling method on nil object")
			}

			res, err := e.evalMemberExprDirect(session, obj, data.Name)
			if err != nil {
				return err
			}

			if res != nil && res.VType == TypeClosure {
				callable = res
			} else if res != nil && res.VType == TypeModule {
				mod = res.Ref.(*VMModule)
				name = data.Name
			} else if res != nil {
				callable = res
			} else {
				return fmt.Errorf("property %s is not a callable function on %v", data.Name, obj.VType)
			}
		case CallByValue:
			callable = session.ValueStack.Pop()
		}

		if name != "" && mod == nil && callable == nil {
			loadTarget := data.Sym
			if loadTarget.Kind == SymbolUnknown {
				loadTarget = SymbolRef{Name: name}
			}
			if v, err := session.LoadSymbol(loadTarget); err == nil && v != nil {
				callable = v
			}
		}

		totalArgs := len(args)
		offset := 0
		if receiver != nil {
			totalArgs++
			offset = 1
		}
		finalArgs := make([]*Var, totalArgs)
		if receiver != nil {
			finalArgs[0] = receiver
		}
		copy(finalArgs[offset:], args)

		err := e.goCall(session, name, receiver, mod, callable, finalArgs)
		return err
	case OpInvokeDirect:
		data := task.Data.(*DirectCallData)
		args := append([]*Var(nil), data.Args...)
		return e.invokeCall(session, data.Name, data.Receiver, data.Module, data.Callable, args, nil)
	case OpResumeFFI:
		data, ok := task.Data.(*ResumeFFIData)
		if !ok || data == nil {
			return errors.New("OpResumeFFI missing ResumeFFIData")
		}
		res, err := e.finishFFI(session, data.Route, data.CopyBackTargets, data.Ret, data.Err)
		if err != nil {
			return err
		}
		session.ValueStack.Push(res)
		return nil
	case OpResumeModule:
		data, ok := task.Data.(*ResumeModuleData)
		if !ok || data == nil {
			return errors.New("OpResumeModule missing ResumeModuleData")
		}
		if data.Err != nil {
			return data.Err
		}
		session.ValueStack.Push(data.Value)
		return nil
	case OpCallBoundary:
		data, ok := task.Data.(*CallBoundaryData)
		if !ok || data == nil {
			return fmt.Errorf("OpCallBoundary data is not *CallBoundaryData: %T (%v)", task.Data, task.Data)
		}
		if scheduleCallBoundaryDefers(session, task, data, nil) {
			return nil
		}
		oldStack := data.OldStack
		hasReturn := data.HasReturn
		valueBase := data.ValueBase
		lhsBase := data.LHSBase

		// Restore executor if saved (cross-module calls)
		if data.OldExec != nil {
			session.Executor = data.OldExec
		}
		if data.OldShared != nil {
			session.Shared = data.OldShared
		}

		var retVal *Var
		if hasReturn {
			retVal, _ = session.LoadReturn()
		}

		session.Stack = oldStack
		if session.ValueStack != nil {
			session.ValueStack.Truncate(valueBase)
		}
		if session.LHSStack != nil {
			session.LHSStack.Truncate(lhsBase)
		}

		if hasReturn {
			session.ValueStack.Push(retVal)
		}

		if session.UnwindMode == UnwindReturn {
			setUnwindMode(session, UnwindNone)
		}
		return nil
	case OpAssert:
		val := session.ValueStack.Pop()
		var (
			targetType RuntimeType
			multi      bool
			resultType RuntimeType
		)
		if data, ok := task.Data.(*AssertData); ok {
			targetType = data.TargetType
			multi = data.Multi
			resultType = data.ResultType
		} else {
			return errors.New("OpAssert missing AssertData")
		}
		res, err := e.CheckSatisfaction(val, targetType.Raw.String())
		if multi {
			if err != nil {
				// 返回 (nil, false)
				tuple := make([]*Var, 2)
				tuple[0] = nil
				tuple[1] = NewBool(false)
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(resultType)
				session.ValueStack.Push(v)
			} else {
				// 返回 (res, true)
				tuple := make([]*Var, 2)
				tuple[0] = res
				tuple[1] = NewBool(true)
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: tuple}}
				v.SetRuntimeType(resultType)
				session.ValueStack.Push(v)
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("interface conversion: %v", err)
		}
		session.ValueStack.Push(res)
		return nil
	case OpRunDefers:
		if owner := callBoundaryDeferOwner(session); owner != nil && len(owner.DeferStack) > 0 {
			owner.RunDefers()
		}
		return nil
	case OpScheduleDefer:
		data := task.Data.(*DeferData)
		session.Stack.AddDefer(func() {
			if data.PopResult {
				session.TaskStack = append(session.TaskStack, Task{Op: OpPop})
			}
			session.TaskStack = append(session.TaskStack, data.Tasks...)
		})
		return nil
	case OpLoopBoundary:
		if err := session.Context.Err(); err != nil {
			return err
		}
		if data, ok := task.Data.(*ForData); ok {
			if len(data.Cond) > 0 {
				session.TaskStack = append(session.TaskStack, Task{Op: OpForCond, Data: data})
				session.TaskStack = append(session.TaskStack, data.Cond...)
			} else {
				e.scheduleForBody(session, data)
			}
			return nil
		}
		if task.Data == nil {
			// Marker boundary (e.g. for Range)
			return nil
		}
		if _, ok := task.Data.(*SwitchData); ok {
			// Switch boundary, just a placeholder for break/continue
			return nil
		}
		return errors.New("OpLoopBoundary missing ForData")
	case OpForStart:
		data, ok := task.Data.(*ForData)
		if !ok || data == nil {
			return errors.New("OpForStart missing ForData")
		}
		if len(data.Cond) > 0 {
			session.TaskStack = append(session.TaskStack, Task{Op: OpForCond, Data: data})
			session.TaskStack = append(session.TaskStack, data.Cond...)
			return nil
		}
		e.scheduleForBody(session, data)
		return nil
	case OpForCond:
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		if b {
			if data, ok := task.Data.(*ForData); ok {
				e.scheduleForBody(session, data)
			} else {
				return errors.New("OpForCond missing ForData")
			}
		}
		return nil
	case OpForScopeEnter:
		return session.ScopeApplyLoopBody("for_body")
	case OpForScopeExit:
		session.SyncLoopScope()
		return session.ScopeExit()
	case OpLoopContinue:
		return nil
	case OpRangeInit:
		obj := session.ValueStack.Pop()
		if obj == nil {
			return nil
		}
		data := task.Data.(*RangeData)
		rData := &RangeData{
			Key:     data.Key,
			Value:   data.Value,
			KeySym:  data.KeySym,
			ValSym:  data.ValSym,
			KeyType: data.KeyType,
			ValType: data.ValType,
			Define:  data.Define,
			Body:    data.Body,
			Obj:     obj,
		}
		switch obj.VType {
		case TypeArray:
			rData.Length = obj.Ref.(*VMArray).Len()
		case TypeMap:
			m := obj.Ref.(*VMMap)
			rData.Keys = m.Keys()
			rData.Length = len(rData.Keys)
		}
		session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary})
		session.TaskStack = append(session.TaskStack, Task{Op: OpRangeIter, Data: rData})
		return nil
	case OpRangeIter:
		rData := task.Data.(*RangeData)
		if err := session.Context.Err(); err != nil {
			return err
		}
		if rData.Index >= rData.Length {
			return nil
		}
		var key, val *Var
		if rData.Obj.VType == TypeArray {
			key = NewInt(int64(rData.Index))
			val, _ = rData.Obj.Ref.(*VMArray).Load(rData.Index)
		} else {
			k := rData.Keys[rData.Index]
			keyType, _, _ := rData.Obj.RuntimeType().GetMapKeyValueTypes()
			key = e.mapKeyToVar(k, keyType)
			val, _ = rData.Obj.Ref.(*VMMap).Load(k)
		}
		rData.Index++

		session.TaskStack = append(session.TaskStack, Task{Op: OpRangeIter, Data: rData})
		session.TaskStack = append(session.TaskStack, Task{Op: OpScopeExit})
		session.TaskStack = append(session.TaskStack, rData.Body...)
		session.TaskStack = append(session.TaskStack, Task{
			Op:   OpRangeScopeEnter,
			Data: &RangeScopeData{Range: rData, Key: key, Val: val},
		})
		return nil
	case OpRangeScopeEnter:
		data, ok := task.Data.(*RangeScopeData)
		if !ok || data == nil {
			return errors.New("OpRangeScopeEnter missing RangeScopeData")
		}
		rData := data.Range
		key := data.Key
		val := data.Val
		if err := session.ScopeApply("for_range_body"); err != nil {
			return err
		}
		if rData.Define {
			if rData.Key != "" && rData.Key != "_" {
				if rData.KeySym.Kind == SymbolLocal {
					_ = session.DeclareSymbol(rData.KeySym, rData.KeyType)
					_ = session.StoreSymbol(rData.KeySym, key)
				} else {
					_ = session.AddVariable(rData.Key, key)
				}
			}
			if rData.Value != "" && rData.Value != "_" && val != nil {
				if rData.ValSym.Kind == SymbolLocal {
					_ = session.DeclareSymbol(rData.ValSym, rData.ValType)
					_ = session.StoreSymbol(rData.ValSym, val)
				} else {
					_ = session.AddVariable(rData.Value, val)
				}
			}
		} else {
			if rData.Key != "" && rData.Key != "_" {
				if rData.KeySym.Kind != SymbolUnknown {
					_ = session.StoreSymbol(rData.KeySym, key)
				} else {
					_ = session.Store(rData.Key, key)
				}
			}
			if rData.Value != "" && rData.Value != "_" && val != nil {
				if rData.ValSym.Kind != SymbolUnknown {
					_ = session.StoreSymbol(rData.ValSym, val)
				} else {
					_ = session.Store(rData.Value, val)
				}
			}
		}
		return nil
	case OpSwitchTag:
		if plan, ok := task.Data.(*SwitchData); ok {
			tag := NewBool(true)
			if plan.HasTag {
				tag = session.ValueStack.Pop()
			}
			session.TaskStack = append(session.TaskStack, Task{
				Op:   OpSwitchNextCase,
				Data: &SwitchState{Plan: plan, Tag: tag},
			})
			return nil
		}
		return errors.New("OpSwitchTag missing SwitchData")
	case OpSwitchStart:
		if plan, ok := task.Data.(*SwitchData); ok {
			session.TaskStack = append(session.TaskStack, Task{Op: OpLoopBoundary, Data: plan})
			session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchTag, Data: plan})
			return nil
		}
		return errors.New("OpSwitchStart missing SwitchData")
	case OpSwitchNextCase:
		if state, ok := task.Data.(*SwitchState); ok {
			if state.Index >= len(state.Plan.Cases) {
				if len(state.Plan.DefaultBody) > 0 {
					session.TaskStack = append(session.TaskStack, e.switchCaseTasks(state.Plan, state.Tag, state.Plan.DefaultBody, "switch_default")...)
				}
				return nil
			}

			caseData := state.Plan.Cases[state.Index]
			if state.Plan.IsType {
				if e.switchTypeCaseMatches(state.Tag, caseData.TypeNames) {
					session.TaskStack = append(session.TaskStack, e.switchCaseTasks(state.Plan, state.Tag, caseData.Body, "switch_matched")...)
					return nil
				}
				state.Index++
				state.ExprIx = 0
				session.TaskStack = append(session.TaskStack, task)
				return nil
			}
			if state.ExprIx < len(caseData.Exprs) {
				next := *state
				next.ExprIx++
				session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchMatchCase, Data: &next})
				session.TaskStack = append(session.TaskStack, caseData.Exprs[state.ExprIx]...)
				return nil
			}

			state.Index++
			state.ExprIx = 0
			session.TaskStack = append(session.TaskStack, task)
			return nil
		}
		return errors.New("OpSwitchNextCase missing SwitchState")
	case OpSwitchMatchCase:
		if state, ok := task.Data.(*SwitchState); ok {
			val := session.ValueStack.Pop()
			res, _ := e.evalComparison("==", state.Tag, val)
			if res != nil && res.Bool {
				caseData := state.Plan.Cases[state.Index]
				session.TaskStack = append(session.TaskStack, caseData.Body...)
				return nil
			}
			session.TaskStack = append(session.TaskStack, Task{Op: OpSwitchNextCase, Data: state})
			return nil
		}
		return errors.New("OpSwitchMatchCase missing SwitchState")
	case OpCatchBoundary:
		return nil
	case OpFinally:
		if data, ok := task.Data.(*FinallyData); ok {
			session.TaskStack = append(session.TaskStack, data.Body...)
		} else {
			return errors.New("OpFinally missing FinallyData")
		}
		return nil
	case OpCatchScopeEnter:
		data, ok := task.Data.(*CatchScopeData)
		if !ok || data == nil || data.Catch == nil {
			return errors.New("OpCatchScopeEnter missing CatchScopeData")
		}
		varName := data.Catch.VarName
		varSym := data.Catch.Sym
		panicVar := data.Panic
		if err := session.ScopeApply("catch"); err != nil {
			return err
		}
		if varName != "" {
			if varSym.Kind == SymbolLocal {
				_ = session.DeclareSymbol(varSym, MustParseRuntimeType("Any"))
				_ = session.StoreSymbol(varSym, panicVar)
			} else {
				_ = session.NewVar(varName, MustParseRuntimeType("Any"))
				_ = session.Store(varName, panicVar)
			}
		}
		return nil
	case OpBranchIf:
		condVal := session.ValueStack.Pop()
		b, err := condVal.ToBool()
		if err != nil {
			return err
		}
		if data, ok := task.Data.(*BranchData); ok {
			if b {
				session.TaskStack = append(session.TaskStack, data.Then...)
			} else if len(data.Else) > 0 {
				session.TaskStack = append(session.TaskStack, data.Else...)
			}
			return nil
		}
		return errors.New("OpBranchIf missing BranchData")
	case OpInitVar:
		name := task.Data.(string)
		val := session.ValueStack.Pop()
		return session.AddVariable(name, val)
	case OpInitGlobal:
		data, ok := task.Data.(*InitGlobalData)
		if !ok || data == nil {
			return errors.New("OpInitGlobal missing InitGlobalData")
		}
		if data.HasInit {
			session.TaskStack = append(session.TaskStack, Task{Op: OpStoreGlobalInit, Data: &InitGlobalData{Name: data.Name, Kind: data.Kind}})
			session.TaskStack = append(session.TaskStack, cloneTasks(data.Plan)...)
			return nil
		}
		kind := data.Kind
		if kind.IsEmpty() {
			kind = MustParseRuntimeType("Any")
		}
		return session.InitGlobal(data.Name, kind, nil)
	case OpStoreGlobalInit:
		data, ok := task.Data.(*InitGlobalData)
		if !ok || data == nil || data.Name == "" {
			return errors.New("OpStoreGlobalInit missing InitGlobalData")
		}
		return session.InitGlobal(data.Name, data.Kind, session.ValueStack.Pop())
	case OpFinishSharedInit:
		data, _ := task.Data.(*FinishSharedInitData)
		finishSessionSharedInitialization(session, nil)
		if data != nil {
			session.Shared.ApplyEnv(data.Env)
		}
		return nil
	case OpResumeUnwind:
		mode := task.Data.(UnwindMode)
		if session.UnwindMode == UnwindNone {
			// Keep panic unwinding alive as long as any panic state remains.
			// Some runtime panic sites historically populated message/trace
			// without a concrete panic value, and treating "nil PanicVar" as
			// "not a panic anymore" can accidentally downgrade the unwind into
			// a return and swallow the original failure.
			if mode == UnwindPanic && session.PanicVar == nil && session.PanicMessage == "" && len(session.PanicTrace) == 0 {
				session.UnwindMode = UnwindReturn
			} else {
				session.UnwindMode = mode
			}
		}
		return nil
	case OpImportInit:
		path := task.Data.(*ImportInitData).Path
		path = strings.Trim(path, " \t\n\r")
		if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
			return fmt.Errorf("invalid import path: %s", path)
		}

		if session.ImportChain[path] {
			return fmt.Errorf("circular dependency detected: %s", path)
		}
		if v, loadState := session.Shared.BeginModuleLoad(path); loadState == ModuleLoadReady {
			session.ValueStack.Push(v)
			return nil
		} else if loadState == ModuleLoadWait {
			if e.scheduler == nil || e.scheduler.Current() == nil {
				return fmt.Errorf("module %s is already loading and cannot be parked without an active scheduler", path)
			}
			execCtx, frame, err := e.scheduler.ParkCurrent()
			if err != nil {
				return err
			}
			session.Shared.AddModuleWaiter(path, moduleWaiter{
				ExecutionContext: execCtx,
				Frame:            frame,
				Resume: Task{
					Op:   OpResumeModule,
					Data: &ResumeModuleData{Path: path},
				},
			})
			return errExecutionContextSuspend
		}
		session.ImportChain[path] = true
		defer delete(session.ImportChain, path)

		if e.ModulePlanLoader != nil {
			prepared, err := e.ModulePlanLoader(path)
			if err == nil {
				err := e.startImportedProgram(session, path, prepared)
				if err != nil && !errors.Is(err, errExecutionContextYield) {
					waiters := session.Shared.finishModuleLoad(path, nil)
					e.scheduleModuleWaiters(waiters, nil, err)
					return err
				}
				return err
			}
			if !errors.Is(err, ErrModuleNotFound) {
				waiters := session.Shared.finishModuleLoad(path, nil)
				e.scheduleModuleWaiters(waiters, nil, err)
				return err
			}
		}

		if pkg, ok := e.lookupFFIPackage(path); ok {
			ffiMod := &VMModule{Name: path, Data: make(map[string]*Var)}
			for _, member := range sortedBoundPackageMembers(pkg) {
				if member == nil {
					continue
				}
				switch member.Kind {
				case FFIMemberFunc:
					if route, ok := e.routes[member.RouteName]; ok {
						ffiMod.Store(member.Name, &Var{
							VType: TypeAny,
							Str:   member.RouteName,
							Ref:   route,
						})
					}
				case FFIMemberConst:
					ffiMod.Store(member.Name, e.evalLiteralToVar(member.ConstValue))
				case FFIMemberValue:
					if member.Value != nil {
						ffiMod.Store(member.Name, cloneVarForAssign(member.Value))
					}
				}
			}
			res := &Var{VType: TypeModule, Ref: ffiMod}
			waiters := session.Shared.finishModuleLoad(path, res)
			session.ValueStack.Push(res)
			e.scheduleModuleWaiters(waiters, res, nil)
			return nil
		}
		err := fmt.Errorf("failed to load module %s", path)
		waiters := session.Shared.finishModuleLoad(path, nil)
		e.scheduleModuleWaiters(waiters, nil, err)
		return err

	case OpImportDone:
		return errors.New("OpImportDone should not be reached in synchronous import mode")
	case OpPush:
		if v, ok := task.Data.(*Var); ok {
			session.ValueStack.Push(v)
		} else {
			session.ValueStack.Push(nil)
		}
		return nil
	case OpMakeClosure:
		data := task.Data.(*ClosureData)
		clCtx := &StackContext{
			Context:   session.Context,
			Executor:  session.Executor,
			Shared:    session.Shared,
			Stack:     session.Stack,
			StepLimit: session.StepLimit,
			Debugger:  session.Debugger,
		}
		closure := &VMClosure{
			FunctionSig:  CloneRuntimeFuncSig(data.FunctionSig),
			BodyTasks:    data.BodyTasks,
			UpvalueSlots: make([]*Slot, len(data.CaptureRefs)),
			UpvalueNames: make([]string, len(data.CaptureRefs)),
			Context:      &LexicalContext{Executor: clCtx.Executor, Shared: clCtx.Shared, Stack: clCtx.Stack},
		}
		for i, capture := range data.CaptureRefs {
			cellVar, err := session.CaptureSymbol(capture)
			if err != nil {
				return fmt.Errorf("failed to capture variable %s: %w", capture.Name, err)
			}
			closure.UpvalueSlots[i] = cellVar
			closure.UpvalueNames[i] = capture.Name
		}
		v := NewVar(SpecClosure, TypeClosure)
		v.Ref = closure
		session.ValueStack.Push(v)
		return nil
	default:
		return fmt.Errorf("unhandled opcode: %v", task.Op)
	}
}
