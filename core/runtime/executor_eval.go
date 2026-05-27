package runtime

import (
	"errors"
	"fmt"
	"strconv"

	"gopkg.d7z.net/go-mini/core/typespec"
)

// isEmptyVar is already defined in executor.go, but we can reuse it since it doesn't have a receiver.
// Wait, isEmptyVar is private in the package, so we can just use it without redefining it.

func (e *Executor) evalBinaryExprDirect(operator string, l, r *Var) (*Var, error) {
	normalized, ok := typespec.NormalizeBinaryOperator(operator)
	if !ok {
		return nil, &VMError{Message: "unsupported operator: " + operator, IsPanic: true}
	}

	if normalized.IsEquality() && (isEmptyVar(l) || isEmptyVar(r)) {
		return e.evalComparison(string(normalized), l, r)
	}

	l = e.unwrapValue(l)
	r = e.unwrapValue(r)

	if l == nil || r == nil {
		if normalized.IsEquality() {
			return e.evalComparison(string(normalized), l, r)
		}
		return nil, &VMError{Message: "binary op with nil operand", IsPanic: true}
	}

	switch normalized {
	case typespec.OpPlus, typespec.OpMinus, typespec.OpMult, typespec.OpDiv, typespec.OpMod:
		return e.evalArithmetic(string(normalized), l, r)
	case typespec.OpBitAnd, typespec.OpBitOr, typespec.OpBitXor, typespec.OpLsh, typespec.OpRsh:
		return e.evalBitwise(string(normalized), l, r)
	case typespec.OpEq, typespec.OpNeq, typespec.OpLt, typespec.OpGt, typespec.OpLe, typespec.OpGe:
		return e.evalComparison(string(normalized), l, r)
	case typespec.OpAnd, typespec.OpOr:
		return e.evalLogic(string(normalized), l, r)
	}

	return nil, &VMError{Message: "unsupported operator: " + operator, IsPanic: true}
}

func (e *Executor) evalArithmetic(op string, l, r *Var) (*Var, error) {
	if op == string(typespec.OpPlus) {
		if l.VType == TypeString && r.VType == TypeString {
			return NewString(l.Str + r.Str), nil
		}
		if l.VType == TypeBytes && r.VType == TypeBytes {
			resB := make([]byte, len(l.B)+len(r.B))
			copy(resB, l.B)
			copy(resB[len(l.B):], r.B)
			return NewBytes(resB), nil
		}
	}
	if l.VType != TypeInt && l.VType != TypeFloat {
		return nil, &VMError{Message: fmt.Sprintf("arithmetic operation %s on non-numeric type %v", op, l.VType), IsPanic: true}
	}
	if r.VType != TypeInt && r.VType != TypeFloat {
		return nil, &VMError{Message: fmt.Sprintf("arithmetic operation %s on non-numeric type %v", op, r.VType), IsPanic: true}
	}
	if op == string(typespec.OpMod) && (l.VType != TypeInt || r.VType != TypeInt) {
		return nil, &VMError{Message: fmt.Sprintf("Mod operator expects Int64 operands, got %v and %v", l.VType, r.VType), IsPanic: true}
	}

	lf, _ := l.ToFloat()
	rf, _ := r.ToFloat()
	useFloat := l.VType == TypeFloat || r.VType == TypeFloat

	switch op {
	case string(typespec.OpPlus):
		if useFloat {
			return NewFloat(lf + rf), nil
		}
		return NewInt(l.I64 + r.I64), nil
	case string(typespec.OpMinus):
		if useFloat {
			return NewFloat(lf - rf), nil
		}
		return NewInt(l.I64 - r.I64), nil
	case string(typespec.OpMult):
		if useFloat {
			return NewFloat(lf * rf), nil
		}
		return NewInt(l.I64 * r.I64), nil
	case string(typespec.OpDiv):
		if rf == 0 {
			return nil, &VMError{Message: "division by zero", IsPanic: true}
		}
		if useFloat {
			return NewFloat(lf / rf), nil
		}
		if l.I64 == -9223372036854775808 && r.I64 == -1 {
			return NewInt(-9223372036854775808), nil
		}
		return NewInt(l.I64 / r.I64), nil
	case string(typespec.OpMod):
		if r.I64 == 0 {
			return nil, &VMError{Message: "division by zero", IsPanic: true}
		}
		if l.I64 == -9223372036854775808 && r.I64 == -1 {
			return NewInt(0), nil
		}
		return NewInt(l.I64 % r.I64), nil
	}
	return nil, &VMError{Message: "unsupported arithmetic operator: " + op, IsPanic: true}
}

func (e *Executor) evalBitwise(op string, l, r *Var) (*Var, error) {
	li, err := l.ToInt()
	if err != nil {
		return nil, err
	}
	ri, err := r.ToInt()
	if err != nil {
		return nil, err
	}

	if ri < 0 {
		return nil, &VMError{Message: fmt.Sprintf("negative shift count %d", ri), IsPanic: true}
	}

	switch op {
	case string(typespec.OpBitAnd):
		return NewInt(li & ri), nil
	case string(typespec.OpBitOr):
		return NewInt(li | ri), nil
	case string(typespec.OpBitXor):
		return NewInt(li ^ ri), nil
	case string(typespec.OpLsh):
		return NewInt(li << uint(ri)), nil
	case string(typespec.OpRsh):
		return NewInt(li >> uint(ri)), nil
	}
	return nil, &VMError{Message: "unsupported bitwise operator: " + op, IsPanic: true}
}

func (e *Executor) evalComparison(op string, l, r *Var) (*Var, error) {
	if normalized, ok := typespec.NormalizeBinaryOperator(op); ok {
		op = string(normalized)
	}
	lEmpty := isEmptyVar(l)
	rEmpty := isEmptyVar(r)
	eqOp := op == string(typespec.OpEq)
	neqOp := op == string(typespec.OpNeq)

	if eqOp {
		if lEmpty && rEmpty {
			return NewBool(true), nil
		}
		if lEmpty || rEmpty {
			if !runtimeValueNilComparable(nonEmptyVar(l, r)) {
				return nil, &VMError{Message: fmt.Sprintf("cannot compare %s and nil", runtimeTypeForAssignment(nonEmptyVar(l, r)).Raw), IsPanic: true}
			}
			return NewBool(false), nil
		}
	}
	if neqOp {
		if lEmpty && rEmpty {
			return NewBool(false), nil
		}
		if lEmpty || rEmpty {
			if !runtimeValueNilComparable(nonEmptyVar(l, r)) {
				return nil, &VMError{Message: fmt.Sprintf("cannot compare %s and nil", runtimeTypeForAssignment(nonEmptyVar(l, r)).Raw), IsPanic: true}
			}
			return NewBool(true), nil
		}
	}

	if l != nil && r != nil {
		if l.VType == TypeString && r.VType == TypeString {
			switch op {
			case string(typespec.OpEq):
				return NewBool(l.Str == r.Str), nil
			case string(typespec.OpNeq):
				return NewBool(l.Str != r.Str), nil
			case string(typespec.OpLt):
				return NewBool(l.Str < r.Str), nil
			case string(typespec.OpGt):
				return NewBool(l.Str > r.Str), nil
			case string(typespec.OpLe):
				return NewBool(l.Str <= r.Str), nil
			case string(typespec.OpGe):
				return NewBool(l.Str >= r.Str), nil
			}
		}
		if l.VType == TypeError && r.VType == TypeError {
			eq := sameGoError(goErrorFromVar(l), goErrorFromVar(r))
			switch op {
			case string(typespec.OpEq):
				return NewBool(eq), nil
			case string(typespec.OpNeq):
				return NewBool(!eq), nil
			}
		}
		if l.VType == TypeBool && r.VType == TypeBool {
			switch op {
			case string(typespec.OpEq):
				return NewBool(l.Bool == r.Bool), nil
			case string(typespec.OpNeq):
				return NewBool(l.Bool != r.Bool), nil
			}
		}

		lf, lErr := l.ToFloat()
		rf, rErr := r.ToFloat()
		if lErr == nil && rErr == nil {
			switch op {
			case string(typespec.OpEq):
				return NewBool(lf == rf), nil
			case string(typespec.OpNeq):
				return NewBool(lf != rf), nil
			case string(typespec.OpLt):
				return NewBool(lf < rf), nil
			case string(typespec.OpGt):
				return NewBool(lf > rf), nil
			case string(typespec.OpLe):
				return NewBool(lf <= rf), nil
			case string(typespec.OpGe):
				return NewBool(lf >= rf), nil
			}
		}
	}

	if eqOp {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeChannel, TypeModule, TypeClosure:
				return NewBool(l.Ref == r.Ref), nil
			case TypeHostRef:
				return NewBool(l.Handle == r.Handle), nil
			case TypePointer:
				return NewBool(l.Ref == r.Ref), nil
			case TypeInterface:
				return NewBool(interfaceIdentity(l) == interfaceIdentity(r)), nil
			}
		}
		return nil, &VMError{Message: fmt.Sprintf("unsupported equality comparison between %s and %s", runtimeTypeForAssignment(l).Raw, runtimeTypeForAssignment(r).Raw), IsPanic: true}
	}
	if neqOp {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeChannel, TypeModule, TypeClosure:
				return NewBool(l.Ref != r.Ref), nil
			case TypeHostRef:
				return NewBool(l.Handle != r.Handle), nil
			case TypePointer:
				return NewBool(l.Ref != r.Ref), nil
			case TypeInterface:
				return NewBool(interfaceIdentity(l) != interfaceIdentity(r)), nil
			}
		}
		return nil, &VMError{Message: fmt.Sprintf("unsupported equality comparison between %s and %s", runtimeTypeForAssignment(l).Raw, runtimeTypeForAssignment(r).Raw), IsPanic: true}
	}

	return nil, &VMError{Message: fmt.Sprintf("unsupported comparison %s between %v and %v", op, l, r), IsPanic: true}
}

func nonEmptyVar(a, b *Var) *Var {
	if !isEmptyVar(a) {
		return a
	}
	return b
}

func runtimeValueNilComparable(v *Var) bool {
	if v == nil {
		return true
	}
	switch v.VType {
	case TypeAny, TypePointer, TypeHostRef, TypeChannel, TypeArray, TypeMap, TypeModule, TypeClosure, TypeInterface, TypeBytes, TypeError:
		return true
	}
	return false
}

func interfaceIdentity(v *Var) interface{} {
	if v == nil || v.VType != TypeInterface {
		return nil
	}
	inter, _ := v.Ref.(*VMInterface)
	if inter == nil {
		return nil
	}
	if inter.Target != nil {
		return inter.Target
	}
	return inter
}

func (e *Executor) evalLogic(op string, l, r *Var) (*Var, error) {
	lb, err := l.ToBool()
	if err != nil {
		return nil, err
	}
	rb, err := r.ToBool()
	if err != nil {
		return nil, err
	}

	switch op {
	case string(typespec.OpAnd):
		return NewBool(lb && rb), nil
	case string(typespec.OpOr):
		return NewBool(lb || rb), nil
	}
	return nil, &VMError{Message: "unsupported logic operator: " + op, IsPanic: true}
}

func (e *Executor) evalUnaryExprDirect(operator string, val *Var) (*Var, error) {
	val = e.unwrapValue(val)
	if val == nil {
		return nil, &VMError{Message: "unary op with nil operand", IsPanic: true}
	}
	switch operator {
	case "!", "Not":
		return NewBool(!val.Bool), nil
	case "-", "Sub", "Minus":
		if val.VType == TypeInt {
			return NewInt(-val.I64), nil
		}
		if val.VType == TypeFloat {
			return NewFloat(-val.F64), nil
		}
	case "^", "BitXor", "Xor":
		if val.VType == TypeInt {
			return NewInt(^val.I64), nil
		}
	case "Dereference":
		return e.dereferenceValue(val)
	}
	return nil, &VMError{Message: "unsupported unary op " + operator, IsPanic: true}
}

func (e *Executor) evalLiteralToVar(val string) *Var {
	return e.evalLiteralToVarWithType(val, RuntimeType{})
}

func (e *Executor) evalLiteralToVarWithType(val string, typ RuntimeType) *Var {
	switch {
	case typ.IsString():
		return NewString(val)
	case typ.IsInt():
		if v, err := strconv.ParseInt(val, 0, 64); err == nil {
			return NewInt(v)
		}
		return NewInt(0)
	case typ.Raw == SpecFloat64:
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			return NewFloat(v)
		}
		return NewFloat(0)
	case typ.IsBool():
		return NewBool(val == "true")
	}
	if v, err := strconv.ParseInt(val, 0, 64); err == nil {
		return NewInt(v)
	}
	if v, err := strconv.ParseFloat(val, 64); err == nil {
		return NewFloat(v)
	}
	if val == "true" {
		return NewBool(true)
	}
	if val == "false" {
		return NewBool(false)
	}
	return NewString(val)
}

func (e *Executor) evalIndexExprDirect(ctx *StackContext, obj, idx *Var) (*Var, error) {
	obj = e.unwrapValue(obj)
	idx = e.unwrapValue(idx)

	if obj == nil || isEmptyVar(obj) {
		return nil, &VMError{Message: "index access on nil", IsPanic: true}
	}

	if idx == nil {
		return nil, &VMError{Message: "index access with nil index", IsPanic: true}
	}

	switch obj.VType {
	case TypeBytes:
		if idx.VType != TypeInt {
			return nil, &VMError{Message: fmt.Sprintf("bytes index must be Int64, got %v", idx.VType), IsPanic: true}
		}
		i := int(idx.I64)
		if i < 0 || i >= len(obj.B) {
			return nil, &VMError{Message: fmt.Sprintf("index out of range [%d] with length %d", i, len(obj.B)), IsPanic: true}
		}
		return NewInt(int64(obj.B[i])), nil
	case TypeString:
		if idx.VType != TypeInt {
			return nil, &VMError{Message: fmt.Sprintf("string index must be Int64, got %v", idx.VType), IsPanic: true}
		}
		i := int(idx.I64)
		if i < 0 || i >= len(obj.Str) {
			return nil, &VMError{Message: fmt.Sprintf("index out of range [%d] with length %d", i, len(obj.Str)), IsPanic: true}
		}
		return NewInt(int64(obj.Str[i])), nil
	case TypeArray:
		if idx.VType != TypeInt {
			return nil, &VMError{Message: fmt.Sprintf("array index must be Int64, got %v", idx.VType), IsPanic: true}
		}
		arr := obj.Ref.(*VMArray)
		i := int(idx.I64)
		val, ok := arr.Load(i)
		if !ok {
			return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
		}
		return val, nil
	case TypeMap:
		m := obj.Ref.(*VMMap)
		// Dynamic Key Type Validation
		keyType, valType, _ := obj.RuntimeType().GetMapKeyValueTypes()
		if !keyType.IsAny() {
			if keyType.IsInt() && idx.VType != TypeInt {
				return nil, &VMError{Message: fmt.Sprintf("invalid map key type: expected Int64, got %v", idx.VType), IsPanic: true}
			}
			if keyType.IsString() && idx.VType != TypeString {
				return nil, &VMError{Message: fmt.Sprintf("invalid map key type: expected String, got %v", idx.VType), IsPanic: true}
			}
			if keyType.IsBool() && idx.VType != TypeBool {
				return nil, &VMError{Message: fmt.Sprintf("invalid map key type: expected Bool, got %v", idx.VType), IsPanic: true}
			}
			if keyType.Raw == SpecFloat64 && idx.VType != TypeFloat {
				return nil, &VMError{Message: fmt.Sprintf("invalid map key type: expected Float64, got %v", idx.VType), IsPanic: true}
			}
		}

		key, err := e.varToTypedMapKey(idx, keyType)
		if err != nil {
			return nil, err
		}
		if val, ok := m.Load(key); ok {
			return val, nil
		}
		return e.ToVar(ctx, valType.ZeroVar(), nil), nil
	}
	return nil, &VMError{Message: fmt.Sprintf("type %v does not support indexing", obj.VType), IsPanic: true}
}

func (e *Executor) evalMemberExprDirect(_ *StackContext, obj *Var, property string) (*Var, error) {
	return e.evalMemberExprDirectWithType(obj, property, RuntimeType{})
}

func (e *Executor) evalMemberExprDirectWithType(obj *Var, property string, staticType RuntimeType) (*Var, error) {
	obj = e.unwrapValue(obj)
	if obj == nil || isEmptyVar(obj) {
		return nil, &VMError{Message: "member access on nil object: " + property, IsPanic: true}
	}

	if obj.VType == TypeInterface {
		inter := obj.Ref.(*VMInterface)
		if inter.Spec == nil {
			return nil, &VMError{Message: fmt.Sprintf("interface contract missing for %s", obj.RawType()), IsPanic: true}
		}
		idx, ok := inter.Spec.MethodIndex[property]
		if !ok {
			return nil, &VMError{Message: fmt.Sprintf("method %s not in interface contract %s", property, obj.RawType()), IsPanic: true}
		}
		if idx < len(inter.VTable) && inter.VTable[idx] != nil {
			return inter.VTable[idx], nil
		}
		sig := inter.Spec.ByName[property]
		if sig == nil {
			return nil, &VMError{Message: fmt.Sprintf("method %s missing vtable entry for %s", property, obj.RawType()), IsPanic: true}
		}
		v := &Var{
			VType: TypeClosure,
			Ref: &VMMethodValue{
				Receiver:      inter.Target,
				Method:        property,
				FuncSig:       CloneRuntimeFuncSig(sig),
				DynamicInvoke: true,
			},
		}
		v.SetRawType(sig.Spec.String())
		return v, nil
	}

	switch obj.VType {
	case TypeError:
		if property == "Error" {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: "Error"}}, nil
		}
	case TypeMap:
		m := obj.Ref.(*VMMap)
		if val, ok := m.Load(property); ok {
			return val, nil
		}
		return nil, nil
	case TypeStruct:
		st := obj.Ref.(*VMStruct)
		if field, ok := st.Field(property); ok {
			return field.Value, nil
		}
		tName := string(obj.RawType())
		if tName != "" && tName != "Any" {
			if methodName, ok := e.resolveVMMethodRoute(tName, property, staticType); ok {
				return e.methodClosure(obj, methodName), nil
			}
		}
		return nil, nil
	case TypePointer:
		tName := string(obj.RawType())
		if tName == "" || tName == "Any" {
			if slot, ok := e.slotPointerSlot(obj); ok && slot != nil && !slot.Decl.IsEmpty() {
				tName = PtrType(slot.Decl.Raw).String()
			}
		}
		if tName != "" && tName != "Any" {
			if methodName, ok := e.resolveVMMethodRoute(tName, property, staticType); ok {
				return e.methodClosure(obj, methodName), nil
			}
		}
		// Check if it's a pointer to something that has fields (like a struct in Ref)
		if valVar, ok := e.slotPointerTarget(obj); ok {
			return e.evalMemberExprDirectWithType(valVar, property, staticType)
		}
	case TypeHostRef:
		if obj.Handle == 0 {
			return nil, &VMError{Message: fmt.Sprintf("nil host reference %s has no member %s", obj.RawType(), property), IsPanic: true}
		}
		tName := string(obj.RawType())
		if tName == "" {
			tName = obj.VType.String()
		}
		if methodName, ok := e.resolveHostMethodRoute(tName, property, staticType); ok {
			return e.methodClosure(obj, methodName), nil
		}
	case TypeModule:
		mod := obj.Ref.(*VMModule)
		if val, ok := mod.Load(property); ok {
			return val, nil
		}
		return nil, &VMError{Message: fmt.Sprintf("module member %s not found in %s", property, mod.Name), IsPanic: true}
	case TypeAny:
		if obj.Ref != nil {
			if m, ok := obj.Ref.(*VMMap); ok {
				if val, ok := m.Load(property); ok {
					return val, nil
				}
			}
		}
		return nil, nil
	}

	tName := string(obj.RawType())
	if tName == "" {
		tName = obj.VType.String()
	}
	if obj.VType != TypeHostRef {
		if methodName, ok := e.resolveVMMethodRoute(tName, property, staticType); ok {
			return e.methodClosure(obj, methodName), nil
		}
	}

	if obj.RuntimeType().IsAny() {
		return nil, nil
	}

	return nil, &VMError{Message: fmt.Sprintf("type %v does not support member access: %s", obj.VType, property), IsPanic: true}
}

func (e *Executor) evalSliceExprDirect(_ *StackContext, obj, lowVar, highVar *Var) (*Var, error) {
	obj = e.unwrapValue(obj)
	lowVar = e.unwrapValue(lowVar)
	highVar = e.unwrapValue(highVar)

	if obj == nil {
		return nil, &VMError{Message: "slice on nil object", IsPanic: true}
	}

	low, high := 0, -1
	if lowVar != nil {
		if lowVar.VType != TypeInt {
			return nil, &VMError{Message: fmt.Sprintf("slice low index must be Int64, got %v", lowVar.VType), IsPanic: true}
		}
		low = int(lowVar.I64)
	}
	if highVar != nil {
		if highVar.VType != TypeInt {
			return nil, &VMError{Message: fmt.Sprintf("slice high index must be Int64, got %v", highVar.VType), IsPanic: true}
		}
		high = int(highVar.I64)
	}

	switch obj.VType {
	case TypeBytes:
		l := len(obj.B)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, l), IsPanic: true}
		}
		return NewBytes(obj.B[low:high]), nil
	case TypeString:
		l := len(obj.Str)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, l), IsPanic: true}
		}
		return NewString(obj.Str[low:high]), nil
	case TypeArray:
		arr := obj.Ref.(*VMArray)
		l := arr.Len()
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, l), IsPanic: true}
		}
		v := &Var{VType: TypeArray, Ref: &VMArray{Data: arr.Slice(low, high)}}
		v.SetRuntimeType(obj.RuntimeType())
		return v, nil
	}
	return nil, &VMError{Message: fmt.Sprintf("type %v does not support slice operations", obj.VType), IsPanic: true}
}

func (e *Executor) invokeCall(session *StackContext, name string, receiver *Var, mod *VMModule, callable *Var, args []*Var, argLHS []LHSValue) error {
	// 0. 特殊类型方法 (Built-in methods on Error)
	if receiver != nil && receiver.VType == TypeError && name == "Error" {
		if errObj := goErrorFromVar(receiver); errObj != nil {
			session.ValueStack.Push(NewString(errObj.Error()))
			return nil
		}
	}
	// 1. Intrinsics
	if mod == nil && receiver == nil && callable == nil {
		switch name {
		case "make":
			if len(args) == 0 {
				return &VMError{Message: "make requires at least 1 argument", IsPanic: true}
			}
			typVar := args[0]
			typVar = e.unwrapValue(typVar)
			if typVar == nil || (typVar.VType != TypeString && !typVar.RuntimeType().IsString()) {
				return &VMError{Message: "make first argument must be a type string", IsPanic: true}
			}
			tStr := typVar.Str
			targetType, err := ParseRuntimeType(tStr)
			if err != nil {
				return &VMError{Message: fmt.Sprintf("invalid make type %q: %v", tStr, err), IsPanic: true}
			}
			if targetType.IsMap() {
				v := &Var{VType: TypeMap, Ref: &VMMap{Data: make(map[string]*Var)}}
				v.SetRawType(tStr)
				session.ValueStack.Push(v)
				return nil
			} else if targetType.IsChan() {
				capacity := 0
				if len(args) > 1 && args[1] != nil {
					cInt, err := e.unwrapValue(args[1]).ToInt()
					if err != nil {
						return &VMError{Message: fmt.Sprintf("make channel capacity must be Int64: %v", err), IsPanic: true}
					}
					if cInt < 0 {
						return &VMError{Message: fmt.Sprintf("negative channel capacity in make: %d", cInt), IsPanic: true}
					}
					if cInt > 10000000 {
						return &VMError{Message: fmt.Sprintf("requested channel capacity too large: %d", cInt), IsPanic: true}
					}
					capacity = int(cInt)
				}
				elemType, ok := targetType.ReadChanElemType()
				if !ok {
					return &VMError{Message: fmt.Sprintf("invalid channel type %s", targetType.Raw), IsPanic: true}
				}
				v := &Var{VType: TypeChannel, Ref: NewVMChannel(elemType, capacity)}
				v.SetRawType(tStr)
				session.ValueStack.Push(v)
				return nil
			} else if targetType.IsArray() || targetType.Raw == SpecBytes {
				length := 0
				capacity := 0
				if len(args) > 1 && args[1] != nil {
					lInt, err := e.unwrapValue(args[1]).ToInt()
					if err != nil {
						return &VMError{Message: fmt.Sprintf("make length must be Int64: %v", err), IsPanic: true}
					}
					if lInt < 0 {
						return &VMError{Message: fmt.Sprintf("negative length in make: %d", lInt), IsPanic: true}
					}
					if lInt > 10000000 {
						return &VMError{Message: fmt.Sprintf("requested length too large: %d", lInt), IsPanic: true}
					}
					length = int(lInt)
					capacity = length
				}
				if len(args) > 2 && args[2] != nil {
					cInt, err := e.unwrapValue(args[2]).ToInt()
					if err != nil {
						return &VMError{Message: fmt.Sprintf("make capacity must be Int64: %v", err), IsPanic: true}
					}
					if int(cInt) < length {
						return &VMError{Message: fmt.Sprintf("capacity %d less than length %d", cInt, length), IsPanic: true}
					}
					if cInt > 10000000 {
						return &VMError{Message: fmt.Sprintf("requested capacity too large: %d", cInt), IsPanic: true}
					}
					capacity = int(cInt)
				}
				if targetType.Raw == SpecBytes {
					v := &Var{VType: TypeBytes, B: make([]byte, length, capacity)}
					v.SetRawType(tStr)
					session.ValueStack.Push(v)
				} else {
					arr := make([]*Var, length, capacity)
					innerType, _ := targetType.ReadArrayItemType()
					for i := 0; i < length; i++ {
						item, err := e.initializeType(session, innerType, 0)
						if err != nil {
							return err
						}
						arr[i] = item
					}
					v := &Var{VType: TypeArray, Ref: &VMArray{Data: arr}}
					v.SetRawType(tStr)
					session.ValueStack.Push(v)
				}
				return nil
			}
			// Fallback
			res, err := e.initializeType(session, MustParseRuntimeType(tStr), 0)
			if err != nil {
				return err
			}
			session.ValueStack.Push(res)
			return nil
		case "len":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "len requires exactly 1 argument", IsPanic: true}
			}
			obj := args[0]
			obj = e.unwrapValue(obj)
			if obj == nil {
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			switch obj.VType {
			case TypeString:
				session.ValueStack.Push(NewInt(int64(len(obj.Str))))
				return nil
			case TypeBytes:
				session.ValueStack.Push(NewInt(int64(len(obj.B))))
				return nil
			case TypeArray:
				session.ValueStack.Push(NewInt(int64(obj.Ref.(*VMArray).Len())))
				return nil
			case TypeMap:
				session.ValueStack.Push(NewInt(int64(obj.Ref.(*VMMap).Len())))
				return nil
			case TypeChannel:
				if ch, ok := obj.Ref.(*VMChannel); ok && ch != nil {
					session.ValueStack.Push(NewInt(int64(ch.Len())))
					return nil
				}
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			return &VMError{Message: fmt.Sprintf("invalid argument for len: %v", obj.VType), IsPanic: true}
		case "cap":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "cap requires exactly 1 argument", IsPanic: true}
			}
			obj := args[0]
			obj = e.unwrapValue(obj)
			if obj == nil {
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			switch obj.VType {
			case TypeBytes:
				session.ValueStack.Push(NewInt(int64(cap(obj.B))))
				return nil
			case TypeArray:
				session.ValueStack.Push(NewInt(int64(obj.Ref.(*VMArray).Cap())))
				return nil
			case TypeChannel:
				if ch, ok := obj.Ref.(*VMChannel); ok && ch != nil {
					session.ValueStack.Push(NewInt(int64(ch.Cap())))
					return nil
				}
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			return &VMError{Message: fmt.Sprintf("invalid argument for cap: %v", obj.VType), IsPanic: true}
		case "close":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "close requires exactly 1 channel argument", IsPanic: true}
			}
			obj := e.unwrapValue(args[0])
			ch, ok := asVMChannel(obj)
			if !ok {
				return &VMError{Message: fmt.Sprintf("close requires channel, got %v", runtimeTypeForAssignment(obj).Raw), IsPanic: true}
			}
			if obj.RuntimeType().IsRecvChan() {
				return &VMError{Message: fmt.Sprintf("close of receive-only channel %s", obj.RuntimeType().Raw), IsPanic: true}
			}
			if err := ch.Close(); err != nil {
				return &VMError{Message: err.Error(), IsPanic: true}
			}
			session.ValueStack.Push(nil)
			return nil
		case "append":
			if len(args) < 2 || args[0] == nil {
				return &VMError{Message: "append requires at least 2 arguments", IsPanic: true}
			}
			base := e.unwrapValue(args[0])
			if base == nil {
				return &VMError{Message: "append requires array or bytes as first argument", IsPanic: true}
			}
			switch base.VType {
			case TypeArray:
				arr := base.Ref.(*VMArray)
				items := arr.Snapshot()
				newArr := make([]*Var, len(items), len(items)+len(args)-1)
				for i, item := range items {
					newArr[i] = cloneVarForAssign(item)
				}
				elemType, ok := base.RuntimeType().ReadArrayItemType()
				if !ok {
					elemType = MustParseRuntimeType(SpecAny)
				}
				for i, arg := range args[1:] {
					prepared, err := e.prepareValueForType(session, arg, elemType)
					if err != nil {
						return fmt.Errorf("append argument %d: %w", i+2, err)
					}
					newArr = append(newArr, prepared)
				}
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: newArr}}
				v.SetRuntimeType(base.RuntimeType())
				session.ValueStack.Push(v)
				return nil
			case TypeBytes:
				buf := make([]byte, len(base.B))
				copy(buf, base.B)
				for i, arg := range args[1:] {
					if arg == nil {
						return fmt.Errorf("append bytes argument %d is nil", i+2)
					}
					val, err := e.unwrapValue(arg).ToInt()
					if err != nil {
						return fmt.Errorf("append bytes argument %d: %w", i+2, err)
					}
					if val < 0 || val > 255 {
						return &VMError{Message: fmt.Sprintf("append bytes argument %d out of byte range: %d", i+2, val), IsPanic: true}
					}
					buf = append(buf, byte(val))
				}
				v := &Var{VType: TypeBytes, B: buf}
				v.SetRuntimeType(base.RuntimeType())
				session.ValueStack.Push(v)
				return nil
			}
			return &VMError{Message: "append requires array or bytes as first argument", IsPanic: true}
		case "delete":
			if len(args) != 2 || args[0] == nil || args[1] == nil {
				return &VMError{Message: "delete requires map and key", IsPanic: true}
			}
			obj := args[0]
			obj = e.unwrapValue(obj)
			keyArg := e.unwrapValue(args[1])
			if obj.VType == TypeMap {
				m := obj.Ref.(*VMMap)
				keyType, _, _ := obj.RuntimeType().GetMapKeyValueTypes()
				key, err := e.varToTypedMapKey(keyArg, keyType)
				if err != nil {
					return err
				}
				m.Delete(key)
				session.ValueStack.Push(nil) // Void return
				return nil
			}
			return &VMError{Message: "delete requires map", IsPanic: true}
		case "panic":
			var val *Var
			if len(args) > 0 {
				val = args[0]
			} else {
				val = NewString("panic")
			}
			panicVal := e.errorVarForPanic(session, val)
			msg, _ := panicVal.ToError()
			return &VMError{Value: panicVal, Message: msg, IsPanic: true}
		case "recover":
			res := session.PanicVar
			session.PanicVar = nil
			session.PanicMessage = ""
			session.PanicTrace = nil
			session.UnwindMode = UnwindNone
			if res == nil {
				res = &Var{VType: TypeAny}
			}
			session.ValueStack.Push(res)
			return nil
		case "String":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				switch arg.VType {
				case TypeString:
					session.ValueStack.Push(NewString(arg.Str))
					return nil
				case TypeBytes:
					session.ValueStack.Push(NewString(string(arg.B)))
					return nil
				case TypeInt:
					session.ValueStack.Push(NewString(strconv.FormatInt(arg.I64, 10)))
					return nil
				case TypeFloat:
					session.ValueStack.Push(NewString(strconv.FormatFloat(arg.F64, 'f', -1, 64)))
					return nil
				case TypeBool:
					session.ValueStack.Push(NewString(strconv.FormatBool(arg.Bool)))
					return nil
				case TypeError:
					text, _ := arg.ToError()
					session.ValueStack.Push(NewString(text))
					return nil
				}
				return &VMError{Message: fmt.Sprintf("cannot convert %v to String", arg.VType), IsPanic: true}
			}
			session.ValueStack.Push(NewString(""))
			return nil
		case "TypeBytes":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				switch arg.VType {
				case TypeBytes:
					session.ValueStack.Push(arg)
					return nil
				case TypeString:
					session.ValueStack.Push(NewBytes([]byte(arg.Str)))
					return nil
				}
				return &VMError{Message: fmt.Sprintf("cannot convert %v to TypeBytes", arg.VType), IsPanic: true}
			}
			session.ValueStack.Push(NewBytes(nil))
			return nil
		case "Int64":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				switch arg.VType {
				case TypeInt:
					session.ValueStack.Push(arg)
					return nil
				case TypeFloat:
					session.ValueStack.Push(NewInt(int64(arg.F64)))
					return nil
				case TypeString:
					val, err := strconv.ParseInt(arg.Str, 10, 64)
					if err != nil {
						return &VMError{Message: fmt.Sprintf("cannot convert String to Int64: %v", err), IsPanic: true}
					}
					session.ValueStack.Push(NewInt(val))
					return nil
				case TypeBool:
					if arg.Bool {
						session.ValueStack.Push(NewInt(1))
						return nil
					}
					session.ValueStack.Push(NewInt(0))
					return nil
				}
				return &VMError{Message: fmt.Sprintf("cannot convert %v to Int64", arg.VType), IsPanic: true}
			}
			session.ValueStack.Push(NewInt(0))
			return nil
		case "Float64":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				switch arg.VType {
				case TypeFloat:
					session.ValueStack.Push(arg)
					return nil
				case TypeInt:
					session.ValueStack.Push(NewFloat(float64(arg.I64)))
					return nil
				case TypeString:
					val, err := strconv.ParseFloat(arg.Str, 64)
					if err != nil {
						return &VMError{Message: fmt.Sprintf("cannot convert String to Float64: %v", err), IsPanic: true}
					}
					session.ValueStack.Push(NewFloat(val))
					return nil
				case TypeBool:
					if arg.Bool {
						session.ValueStack.Push(NewFloat(1.0))
						return nil
					}
					session.ValueStack.Push(NewFloat(0.0))
					return nil
				}
				return &VMError{Message: fmt.Sprintf("cannot convert %v to Float64", arg.VType), IsPanic: true}
			}
			session.ValueStack.Push(NewFloat(0.0))
			return nil
		case "Bool":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				b, err := arg.ToBool()
				if err != nil {
					return &VMError{Message: fmt.Sprintf("cannot convert %v to Bool", arg.VType), IsPanic: true}
				}
				session.ValueStack.Push(NewBool(b))
				return nil
			}
			session.ValueStack.Push(NewBool(false))
			return nil
		case "new":
			if len(args) < 1 || args[0] == nil || (args[0].VType != TypeString && !args[0].RuntimeType().IsString()) {
				return &VMError{Message: "new requires a type string as argument", IsPanic: true}
			}
			tStr := args[0].Str
			targetType, err := ParseRuntimeType(tStr)
			if err != nil {
				return &VMError{Message: fmt.Sprintf("invalid new type %q: %v", tStr, err), IsPanic: true}
			}
			innerType := targetType
			if targetType.IsPtr() && targetType.Elem != nil {
				innerType = *targetType.Elem
			}
			val, err := e.initializeType(session, innerType, 0)
			if err != nil {
				return err
			}

			res := e.newSlotPointer(innerType, NewSlot(innerType, val))
			res.SetRawType(PtrType(innerType.Raw).String())
			session.ValueStack.Push(res)
			return nil
		}
	}

	// 2. Closure / Method Value / FFI Route
	if callable != nil {
		c := e.unwrapValue(callable)
		if c == nil {
			c = callable
		}
		if c != nil {
			if route, ok := c.Ref.(FFIRoute); ok {
				return e.evalFFIAndPush(session, route, args, argLHS)
			}
		}

		if c.VType == TypeClosure {
			switch ref := c.Ref.(type) {
			case *VMClosure:
				return e.setupFuncCall(session, "closure", nil, args, ref)
			case *VMMethodValue:
				if !ref.DynamicInvoke && ref.Receiver != nil && ref.Receiver.VType == TypeError && ref.Method == "Error" {
					if errObj := goErrorFromVar(ref.Receiver); errObj != nil {
						session.ValueStack.Push(NewString(errObj.Error()))
						return nil
					}
				}
				// Resolve as FFI method
				if route, ok := e.routes[ref.Method]; ok {
					callArgs := args
					callArgLHS := argLHS
					if !ref.DynamicInvoke {
						callArgs = append([]*Var{ref.Receiver}, args...)
						if argLHS != nil {
							callArgLHS = make([]LHSValue, len(callArgs))
							copy(callArgLHS[1:], argLHS)
						}
					}
					return e.evalFFIAndPush(session, route, callArgs, callArgLHS)
				}
				if !ref.DynamicInvoke {
					callArgs := append([]*Var{ref.Receiver}, args...)
					if fn, ok := e.lookupFunction(ref.Method); ok {
						return e.setupFuncCall(session, ref.Method, &DoCallData{
							Name:        ref.Method,
							FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
							BodyTasks:   cloneTasks(fn.BodyTasks),
							Args:        callArgs,
						}, callArgs, nil)
					}
					if ref.FuncSig != nil && len(ref.BodyTasks) > 0 {
						return e.setupFuncCall(session, ref.Method, &DoCallData{
							Name:        ref.Method,
							FunctionSig: CloneRuntimeFuncSig(ref.FuncSig),
							BodyTasks:   cloneTasks(ref.BodyTasks),
							Args:        callArgs,
						}, callArgs, nil)
					}
				}
				if ref.DynamicInvoke && ref.FuncSig != nil && ref.Receiver != nil && ref.Receiver.VType == TypeHostRef && ref.Receiver.Bridge != nil {
					route := FFIRoute{
						Bridge:   ref.Receiver.Bridge,
						MethodID: 0,
						Name:     ref.Method,
						FuncSig:  ref.FuncSig,
					}
					return e.evalFFIAndPush(session, route, args, argLHS)
				}
			}
		}
	}

	// 3. FFI Call (Global or Module)
	routeKey := name
	if mod != nil {
		routeKey = fmt.Sprintf("%s.%s", mod.Name, name)
	}
	if route, ok := e.routes[routeKey]; ok {
		return e.evalFFIAndPush(session, route, args, argLHS)
	}

	// 3a. Dynamic Method Call for Maps (Interfaces)
	if receiver != nil && receiver.VType == TypeMap && name != "" {
		m := receiver.Ref.(*VMMap)
		if val, ok := m.Load(name); ok {
			// Found it! It could be a closure stored in the map.
			// IMPORTANT: If we found the method via a receiver, we should strip the receiver
			// from args if it was automatically prepended by OpCall.
			actualArgs := args
			if len(args) > 0 && args[0] == receiver {
				actualArgs = args[1:]
			}
			return e.invokeCall(session, "", nil, nil, val, actualArgs, nil)
		}
	}

	// 3b. Dynamic Method Call for Modules (Interfaces)
	if receiver != nil && receiver.VType == TypeModule && name != "" {
		mod := receiver.Ref.(*VMModule)
		routeKey := fmt.Sprintf("%s.%s", mod.Name, name)
		if route, ok := e.routes[routeKey]; ok {
			actualArgs := args
			if len(args) > 0 && args[0] == receiver {
				actualArgs = args[1:]
			}
			return e.evalFFIAndPush(session, route, actualArgs, nil)
		}
	}

	// 4. Module Function Call
	if mod != nil {
		if fVar, ok := mod.Load(name); ok && fVar.VType == TypeClosure {
			if closure, ok := fVar.Ref.(*VMClosure); ok {
				return e.setupFuncCall(session, name, nil, args, closure)
			}
		}
	}

	// 5. Internal Function Call
	if fn, ok := e.lookupFunction(name); ok {
		return e.setupFuncCall(session, name, &DoCallData{
			Name:        name,
			FunctionSig: CloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
			Args:        args,
		}, args, nil)
	}

	if callable != nil {
		return &VMError{Message: fmt.Sprintf("type %v is not callable", callable.VType), IsPanic: true}
	}

	return &VMError{Message: fmt.Sprintf("function or method %s not found", name), IsPanic: true}
}

func (e *Executor) goCall(parent *StackContext, name string, receiver *Var, mod *VMModule, callable *Var, args []*Var) error {
	if e.scheduler == nil {
		return &VMError{Message: "VM execution context scheduler is not initialized", IsPanic: true}
	}
	if e.scheduler.Current() == nil {
		return &VMError{Message: "go requires an active VM scheduler", IsPanic: true}
	}
	if parent == nil {
		return &VMError{Message: "missing parent session for go", IsPanic: true}
	}
	child := e.NewSession(parent.Context, "go")
	child.StepLimit = parent.StepLimit
	child.Shared = parent.Shared
	child.Debugger = parent.Debugger
	child.ImportChain = make(map[string]bool, len(parent.ImportChain))
	for k, v := range parent.ImportChain {
		child.ImportChain[k] = v
	}
	child.TaskStack = append(child.TaskStack, Task{
		Op: OpInvokeDirect,
		Data: &DirectCallData{
			Name:     name,
			Receiver: receiver,
			Module:   mod,
			Callable: callable,
			Args:     append([]*Var(nil), args...),
		},
	})
	if _, err := e.scheduler.Go(child, e); err != nil {
		e.CleanupSession(child)
		return &VMError{Message: err.Error(), IsPanic: true}
	}
	return nil
}

func (e *Executor) setupFuncCall(session *StackContext, name string, fn *DoCallData, args []*Var, closure *VMClosure) error {
	if session.ValueStack == nil {
		session.ValueStack = &ValueStack{}
	}
	if session.LHSStack == nil {
		session.LHSStack = &LHSStack{}
	}
	old := session.Stack
	oldExec := session.Executor
	oldShared := session.Shared
	var sig *RuntimeFuncSig
	var bodyTasks []Task
	if closure != nil {
		sig = CloneRuntimeFuncSig(closure.FunctionSig)
		bodyTasks = closure.BodyTasks
	}
	if fn != nil {
		if sig == nil {
			sig = CloneRuntimeFuncSig(fn.FunctionSig)
		}
		if len(bodyTasks) == 0 {
			bodyTasks = fn.BodyTasks
		}
	}
	if sig == nil {
		sig = MustRuntimeFuncSig(SpecVoid, false)
	}

	// Default lexical scope is global
	root := old
	for root != nil && root.Parent != nil {
		root = root.Parent
	}

	// If it's a closure, its lexical root is its captured context
	if closure != nil && closure.Context != nil {
		root = closure.Context.Stack
		session.Executor = closure.Context.Executor
		session.Shared = closure.Context.Shared
	}

	newDepth := 1
	if root != nil {
		newDepth = root.Depth + 1
	}
	if newDepth > DefaultMaxStackDepth {
		return errors.New("stack overflow")
	}
	newStack := &Stack{
		Parent:     root,
		MemoryPtr:  make(map[string]*Slot),
		Symbols:    make(map[string]SymbolRef),
		Frame:      &SlotFrame{},
		Scope:      name,
		Depth:      newDepth,
		DeferOwner: nil,
	}
	newStack.DeferOwner = newStack

	session.Stack = newStack

	// Inject captured variables
	if closure != nil {
		newStack.Frame.Upvalues = append(newStack.Frame.Upvalues, closure.UpvalueSlots...)
		newStack.Frame.UpvalueNames = append(newStack.Frame.UpvalueNames, closure.UpvalueNames...)
		for i, name := range closure.UpvalueNames {
			if name == "" || name == "_" {
				continue
			}
			newStack.Frame.ensureUpvalueSlot(i, name)
			newStack.Symbols[name] = SymbolRef{Name: name, Kind: SymbolUpvalue, Slot: i}
		}
	}

	// Inject params
	for i, paramType := range sig.ParamTypes {
		paramName := ""
		if i < len(sig.ParamNames) {
			paramName = sig.ParamNames[i]
		}
		paramSym := SymbolRef{Name: paramName, Kind: SymbolLocal, Slot: i}
		if err := session.DeclareSymbol(paramSym, paramType); err != nil {
			return fmt.Errorf("function %s parameter %d: %w", name, i+1, err)
		}
		if sig.Variadic && i == len(sig.ParamTypes)-1 {
			var variadicArgs []*Var
			if i < len(args) {
				variadicArgs = args[i:]
			}
			arr := &Var{VType: TypeArray, Ref: &VMArray{Data: variadicArgs}}
			arr.SetRuntimeType(paramType)
			if err := session.StoreSymbol(paramSym, arr); err != nil {
				return fmt.Errorf("function %s variadic argument: %w", name, err)
			}
		} else if i < len(args) && args[i] != nil {
			if err := session.StoreSymbol(paramSym, args[i]); err != nil {
				return fmt.Errorf("function %s argument %d: %w", name, i+1, err)
			}
		}
	}
	if !sig.ReturnType.IsVoid() {
		if err := session.InitReturn(sig.ReturnType); err != nil {
			return fmt.Errorf("function %s return slot: %w", name, err)
		}
	}

	// Push CallBoundary
	session.TaskStack = append(session.TaskStack, Task{
		Op: OpCallBoundary,
		Data: &CallBoundaryData{
			Name:      name,
			OldStack:  old,
			OldExec:   oldExec,
			OldShared: oldShared,
			HasReturn: !sig.ReturnType.IsVoid(),
			ValueBase: session.ValueStack.Len(),
			LHSBase:   session.LHSStack.Len(),
		},
	})
	// Push body
	if len(bodyTasks) > 0 {
		session.TaskStack = append(session.TaskStack, bodyTasks...)
	}

	return nil
}

func (e *Executor) evalFFIAndPush(session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) error {
	res, err := e.evalRoute(session, route, args, argLHS)
	if err != nil {
		return err
	}

	session.ValueStack.Push(res)
	return nil
}

func (e *Executor) initializeType(ctx *StackContext, t RuntimeType, depth int) (*Var, error) {
	if depth > 10 {
		return NewVarWithRuntimeType(t, TypeAny), nil
	}
	if err := e.ensureRuntimeTypeCreatable(t); err != nil {
		return nil, err
	}

	// 1. Resolve to the underlying shape for initialization, but keep t as the logical type
	shape := t
	if resolved, ok, err := e.resolveNamedTypeChain(t.Raw); err == nil && ok {
		shape = resolved
	}
	if shape.Raw != t.Raw {
		if err := e.ensureRuntimeTypeCreatable(shape); err != nil {
			return nil, err
		}
	}

	if shape.IsHostRef() {
		v := &Var{VType: TypeHostRef, Handle: 0}
		v.SetRuntimeType(t)
		return v, nil
	}

	if shape.IsPtr() {
		v := &Var{VType: TypePointer}
		v.SetRuntimeType(t)
		return v, nil
	}

	if shape.IsChan() {
		v := &Var{VType: TypeChannel}
		v.SetRuntimeType(t)
		return v, nil
	}

	if shape.IsInterface() {
		v := &Var{VType: TypeInterface, Ref: nil}
		v.SetRuntimeType(t)
		return v, nil
	}

	if shape.IsArray() {
		v := &Var{VType: TypeArray, Ref: &VMArray{Data: nil}}
		v.SetRuntimeType(t)
		return v, nil
	}

	if shape.IsMap() || shape.IsAny() {
		return NewVarWithRuntimeType(t, TypeAny), nil
	}

	// 基础类型初始化
	zero := shape.ZeroVar()
	res := e.ToVar(ctx, zero, nil)
	if res != nil {
		res.SetRuntimeType(t) // 还原为用户请求的命名类型
		return res, nil
	}

	if sDef, ok := e.runtimeStructSchemaForType(shape); ok {
		fields := make([]*Slot, len(sDef.Fields))
		byName := make(map[string]int, len(sDef.Fields))
		for i, field := range sDef.Fields {
			fieldVal, err := e.initializeType(ctx, field.TypeInfo, depth+1)
			if err != nil {
				return nil, err
			}
			fields[i] = NewSlot(field.TypeInfo, fieldVal)
			byName[field.Name] = i
		}
		v := &Var{VType: TypeStruct, Ref: &VMStruct{Spec: sDef, Fields: fields, ByName: byName}}
		v.SetRuntimeType(t)
		return v, nil
	}

	return NewVarWithRuntimeType(t, TypeAny), nil
}

func (e *Executor) runtimeStructSchemaForType(t RuntimeType) (*RuntimeStructSpec, bool) {
	if sDef, ok := e.resolveStructSchema(t.Raw); ok {
		return sDef, true
	}
	if len(t.Fields) == 0 {
		return nil, false
	}
	fields := make([]RuntimeStructField, len(t.Fields))
	byName := make(map[string]RuntimeStructField, len(t.Fields))
	for i, field := range t.Fields {
		fields[i] = field
		byName[field.Name] = field
	}
	return &RuntimeStructSpec{
		Spec:     t.Raw,
		TypeInfo: t,
		Fields:   fields,
		ByName:   byName,
	}, true
}

func (e *Executor) ensureRuntimeTypeCreatable(t RuntimeType) error {
	if e.runtimeTypeContainsHostOpaqueValue(t, 0) {
		return &VMError{Message: fmt.Sprintf("opaque host type %s cannot be created by VM", t.Raw), IsPanic: true}
	}
	return nil
}

func (e *Executor) runtimeTypeContainsHostOpaqueValue(t RuntimeType, depth int) bool {
	if depth > 64 || t.IsEmpty() || t.IsAny() || t.IsHostRef() {
		return false
	}
	if t.IsPtr() {
		if t.Elem != nil && e.runtimeTypeIsHostOpaqueNamed(*t.Elem) {
			return true
		}
		if t.Elem != nil {
			return e.runtimeTypeContainsHostOpaqueValue(*t.Elem, depth+1)
		}
		return false
	}
	if t.IsChan() {
		elem, ok := t.ReadChanElemType()
		return ok && e.runtimeTypeContainsHostOpaqueValue(elem, depth+1)
	}
	if t.IsArray() {
		elem, ok := t.ReadArrayItemType()
		return ok && e.runtimeTypeContainsHostOpaqueValue(elem, depth+1)
	}
	if t.IsMap() {
		k, v, ok := t.GetMapKeyValueTypes()
		return ok && (e.runtimeTypeContainsHostOpaqueValue(k, depth+1) || e.runtimeTypeContainsHostOpaqueValue(v, depth+1))
	}
	if t.Kind == RuntimeTypeTuple {
		for _, item := range t.Params {
			if e.runtimeTypeContainsHostOpaqueValue(item, depth+1) {
				return true
			}
		}
		return false
	}
	if t.Kind == RuntimeTypeFunction {
		if t.Return != nil && e.runtimeTypeContainsHostOpaqueValue(*t.Return, depth+1) {
			return true
		}
		for _, param := range t.Params {
			if e.runtimeTypeContainsHostOpaqueValue(param, depth+1) {
				return true
			}
		}
		return false
	}
	if t.Kind == RuntimeTypeStruct {
		for _, field := range t.Fields {
			if e.runtimeTypeContainsHostOpaqueValue(field.TypeInfo, depth+1) {
				return true
			}
		}
		return false
	}
	return e.runtimeTypeIsHostOpaqueNamed(t)
}

func (e *Executor) runtimeTypeIsHostOpaqueNamed(t RuntimeType) bool {
	if t.IsHostRef() || t.IsPtr() {
		return false
	}
	schema, ok := e.lookupStructSchema(t)
	return ok && schema.Ownership == StructOwnershipHostOpaque
}
