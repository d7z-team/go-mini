package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
)

// isEmptyVar is already defined in executor.go, but we can reuse it since it doesn't have a receiver.
// Wait, isEmptyVar is private in the package, so we can just use it without redefining it.

func (e *Executor) evalBinaryExprDirect(operator string, l, r *Var) (*Var, error) {
	l = e.unwrapValue(l)
	r = e.unwrapValue(r)

	// 允许比较运算的操作数为 nil
	if operator == "==" || operator == "Eq" || operator == "!=" || operator == "Neq" {
		isLEmpty := isEmptyVar(l)
		isREmpty := isEmptyVar(r)

		if isLEmpty && isREmpty {
			return NewBool(operator == "==" || operator == "Eq"), nil
		}
		if isLEmpty || isREmpty {
			return NewBool(operator == "!=" || operator == "Neq"), nil
		}
	}

	if l == nil || r == nil {
		return nil, &VMError{Message: "binary op with nil operand", IsPanic: true}
	}

	switch operator {
	case "+", "Plus", "Add", "-", "Minus", "Sub", "*", "Mult", "/", "Div", "%", "Mod":
		return e.evalArithmetic(operator, l, r)
	case "&", "BitAnd", "|", "BitOr", "^", "BitXor", "<<", "Lsh", ">>", "Rsh":
		return e.evalBitwise(operator, l, r)
	case "==", "Eq", "!=", "Neq", "<", "Lt", ">", "Gt", "<=", "Le", ">=", "Ge":
		return e.evalComparison(operator, l, r)
	case "&&", "And", "||", "Or":
		return e.evalLogic(operator, l, r)
	}

	return nil, &VMError{Message: "unsupported operator: " + operator, IsPanic: true}
}

func (e *Executor) evalArithmetic(op string, l, r *Var) (*Var, error) {
	if l.VType != TypeInt && l.VType != TypeFloat {
		if op == "+" || op == "Plus" || op == "Add" {
			// 字符串拼接尝试：仅限字符串和字节
			if l.VType == TypeString || l.VType == TypeBytes || r.VType == TypeString || r.VType == TypeBytes {
				// 如果两个都是字节，返回字节
				if l.VType == TypeBytes && r.VType == TypeBytes {
					resB := make([]byte, len(l.B)+len(r.B))
					copy(resB, l.B)
					copy(resB[len(l.B):], r.B)
					return NewBytes(resB), nil
				}

				// 否则按字符串拼接 (TypeError 将不再进入此分支)
				lStr, _ := l.ToError()
				rStr, _ := r.ToError()
				return NewString(lStr + rStr), nil
			}
		}
		return nil, &VMError{Message: fmt.Sprintf("arithmetic operation %s on non-numeric type %v", op, l.VType), IsPanic: true}
	}
	if r.VType != TypeInt && r.VType != TypeFloat {
		return nil, &VMError{Message: fmt.Sprintf("arithmetic operation %s on non-numeric type %v", op, r.VType), IsPanic: true}
	}

	lf, _ := l.ToFloat()
	rf, _ := r.ToFloat()
	useFloat := l.VType == TypeFloat || r.VType == TypeFloat

	switch op {
	case "+", "Plus", "Add":
		if useFloat {
			return NewFloat(lf + rf), nil
		}
		return NewInt(l.I64 + r.I64), nil
	case "-", "Minus", "Sub":
		if useFloat {
			return NewFloat(lf - rf), nil
		}
		return NewInt(l.I64 - r.I64), nil
	case "*", "Mult":
		if useFloat {
			return NewFloat(lf * rf), nil
		}
		return NewInt(l.I64 * r.I64), nil
	case "/", "Div":
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
	case "%", "Mod":
		lVal, _ := l.ToInt()
		rVal, _ := r.ToInt()
		if rVal == 0 {
			return nil, &VMError{Message: "division by zero", IsPanic: true}
		}
		if lVal == -9223372036854775808 && rVal == -1 {
			return NewInt(0), nil
		}
		return NewInt(lVal % rVal), nil
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
	case "&", "BitAnd":
		return NewInt(li & ri), nil
	case "|", "BitOr":
		return NewInt(li | ri), nil
	case "^", "BitXor":
		return NewInt(li ^ ri), nil
	case "<<", "Lsh":
		return NewInt(li << uint(ri)), nil
	case ">>", "Rsh":
		return NewInt(li >> uint(ri)), nil
	}
	return nil, &VMError{Message: "unsupported bitwise operator: " + op, IsPanic: true}
}

func (e *Executor) evalComparison(op string, l, r *Var) (*Var, error) {
	lEmpty := isEmptyVar(l)
	rEmpty := isEmptyVar(r)

	if op == "==" || op == "Eq" {
		if lEmpty && rEmpty {
			return NewBool(true), nil
		}
		if lEmpty || rEmpty {
			return NewBool(false), nil
		}
	}
	if op == "!=" || op == "Neq" {
		if lEmpty && rEmpty {
			return NewBool(false), nil
		}
		if lEmpty || rEmpty {
			return NewBool(true), nil
		}
	}

	if l != nil && r != nil {
		if l.VType == TypeString && r.VType == TypeString {
			switch op {
			case "==", "Eq":
				return NewBool(l.Str == r.Str), nil
			case "!=", "Neq":
				return NewBool(l.Str != r.Str), nil
			}
		}
		if l.VType == TypeError && r.VType == TypeError {
			lStr, _ := l.ToError()
			rStr, _ := r.ToError()
			switch op {
			case "==", "Eq":
				return NewBool(lStr == rStr), nil
			case "!=", "Neq":
				return NewBool(lStr != rStr), nil
			}
		}
		if l.VType == TypeBool && r.VType == TypeBool {
			switch op {
			case "==", "Eq":
				return NewBool(l.Bool == r.Bool), nil
			case "!=", "Neq":
				return NewBool(l.Bool != r.Bool), nil
			}
		}

		lf, lErr := l.ToFloat()
		rf, rErr := r.ToFloat()
		if lErr == nil && rErr == nil {
			switch op {
			case "==", "Eq":
				return NewBool(lf == rf), nil
			case "!=", "Neq":
				return NewBool(lf != rf), nil
			case "<", "Lt":
				return NewBool(lf < rf), nil
			case ">", "Gt":
				return NewBool(lf > rf), nil
			case "<=", "Le":
				return NewBool(lf <= rf), nil
			case ">=", "Ge":
				return NewBool(lf >= rf), nil
			}
		}
	}

	if op == "==" || op == "Eq" {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeModule, TypeClosure:
				return NewBool(l.Ref == r.Ref), nil
			case TypeHandle:
				return NewBool(l.Handle == r.Handle), nil
			}
		}
		return NewBool(l == r), nil
	}
	if op == "!=" || op == "Neq" {
		if l != nil && r != nil && l.VType == r.VType {
			switch l.VType {
			case TypeArray, TypeMap, TypeModule, TypeClosure:
				return NewBool(l.Ref != r.Ref), nil
			case TypeHandle:
				return NewBool(l.Handle != r.Handle), nil
			}
		}
		return NewBool(l != r), nil
	}

	return nil, &VMError{Message: fmt.Sprintf("unsupported comparison %s between %v and %v", op, l, r), IsPanic: true}
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
	case "&&", "And":
		return NewBool(lb && rb), nil
	case "||", "Or":
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

func (e *Executor) evalLiteralDirect(n *ast.LiteralExpr) (*Var, error) {
	switch n.Type {
	case "Int64":
		v, _ := strconv.ParseInt(n.Value, 10, 64)
		return NewInt(v), nil
	case "Float64":
		v, _ := strconv.ParseFloat(n.Value, 64)
		return NewFloat(v), nil
	case "String":
		return NewString(n.Value), nil
	case "Bool":
		return NewBool(n.Value == "true"), nil
	}
	return nil, &VMError{Message: fmt.Sprintf("unknown literal %s", n.Type), IsPanic: true}
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
		i := int(idx.I64)
		if i < 0 || i >= len(obj.B) {
			return nil, &VMError{Message: fmt.Sprintf("index out of range [%d] with length %d", i, len(obj.B)), IsPanic: true}
		}
		return NewInt(int64(obj.B[i])), nil
	case TypeString:
		i := int(idx.I64)
		if i < 0 || i >= len(obj.Str) {
			return nil, &VMError{Message: fmt.Sprintf("index out of range [%d] with length %d", i, len(obj.Str)), IsPanic: true}
		}
		return NewInt(int64(obj.Str[i])), nil
	case TypeArray:
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
		}

		key, err := e.varToMapKey(idx)
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
				Receiver: inter.Target,
				Method:   property,
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
		// Try to look up as a method if it has a type name
		tName := string(obj.RawType())
		if tName != "" && tName != "Any" && !strings.HasPrefix(tName, "Map<") {
			if methodName, ok := e.resolveMethodRoute(tName, property); ok {
				return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
			}
		}
		return nil, nil
	case TypeHandle:
		// Check if it's a pointer to something that has fields (like a struct in Ref)
		if valVar, ok := e.vmPointerTarget(obj); ok {
			return e.evalMemberExprDirect(nil, valVar, property)
		}
		// Handle method extraction (implicit binding)
		tName := string(obj.RawType())
		if tName == "" {
			tName = obj.VType.String()
		}
		if methodName, ok := e.resolveMethodRoute(tName, property); ok {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
		}
	case TypeModule:
		mod := obj.Ref.(*VMModule)
		if mod.Context != nil {
			val, err := mod.Context.Load(property)
			if err == nil {
				return val, nil
			}
		}
		if val, ok := mod.Load(property); ok {
			return val, nil
		}
		if mod.Name == "task" && isTaskModuleIntrinsic(property) {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: property}}, nil
		}
		// Fallback to FFI routes
		routeKey := fmt.Sprintf("%s.%s", mod.Name, property)
		if route, ok := e.routes[routeKey]; ok {
			return &Var{VType: TypeAny, Ref: route}, nil
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
	if methodName, ok := e.resolveMethodRoute(tName, property); ok {
		return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
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
	if lowVar != nil && lowVar.VType == TypeInt {
		low = int(lowVar.I64)
	}
	if highVar != nil && highVar.VType == TypeInt {
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
		if errObj, ok := receiver.Ref.(*VMError); ok {
			session.ValueStack.Push(NewString(errObj.Message))
			return nil
		}
	}
	if receiver != nil && receiver.VType == TypeModule && name != "" {
		if modRef, ok := receiver.Ref.(*VMModule); ok && modRef.Name == "task" {
			actualArgs := args
			if len(args) > 0 && args[0] == receiver {
				actualArgs = args[1:]
			}
			if handled, err := e.invokeTaskModuleIntrinsic(session, name, actualArgs); handled {
				return err
			}
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
			if strings.HasPrefix(tStr, "Map<") {
				v := &Var{VType: TypeMap, Ref: &VMMap{Data: make(map[string]*Var)}}
				v.SetRawType(tStr)
				session.ValueStack.Push(v)
				return nil
			} else if strings.HasPrefix(tStr, "Array<") || tStr == "TypeBytes" {
				length := 0
				capacity := 0
				if len(args) > 1 && args[1] != nil {
					lInt, _ := args[1].ToInt()
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
					cInt, _ := args[2].ToInt()
					if int(cInt) < length {
						return &VMError{Message: fmt.Sprintf("capacity %d less than length %d", cInt, length), IsPanic: true}
					}
					if cInt > 10000000 {
						return &VMError{Message: fmt.Sprintf("requested capacity too large: %d", cInt), IsPanic: true}
					}
					capacity = int(cInt)
				}
				if tStr == "TypeBytes" {
					v := &Var{VType: TypeBytes, B: make([]byte, length, capacity)}
					v.SetRawType(tStr)
					session.ValueStack.Push(v)
				} else {
					arr := make([]*Var, length, capacity)
					innerType, _ := MustParseRuntimeType(tStr).ReadArrayItemType()
					for i := 0; i < length; i++ {
						arr[i] = e.initializeType(session, innerType, 0)
					}
					v := &Var{VType: TypeArray, Ref: &VMArray{Data: arr}}
					v.SetRawType(tStr)
					session.ValueStack.Push(v)
				}
				return nil
			}
			// Fallback
			res := e.initializeType(session, MustParseRuntimeType(tStr), 0)
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
			}
			return &VMError{Message: fmt.Sprintf("invalid argument for cap: %v", obj.VType), IsPanic: true}
		case "append":
			if len(args) < 2 || args[0] == nil {
				return &VMError{Message: "append requires at least 2 arguments", IsPanic: true}
			}
			switch args[0].VType {
			case TypeArray:
				arr := args[0].Ref.(*VMArray)
				items := arr.Snapshot()
				newArr := make([]*Var, len(items), len(items)+len(args)-1)
				copy(newArr, items)
				newArr = append(newArr, args[1:]...)
				v := &Var{VType: TypeArray, Ref: &VMArray{Data: newArr}}
				v.SetRuntimeType(args[0].RuntimeType())
				session.ValueStack.Push(v)
				return nil
			case TypeBytes:
				buf := make([]byte, len(args[0].B))
				copy(buf, args[0].B)
				for _, arg := range args[1:] {
					if arg != nil {
						val, _ := arg.ToInt()
						buf = append(buf, byte(val))
					}
				}
				v := &Var{VType: TypeBytes, B: buf}
				v.SetRuntimeType(args[0].RuntimeType())
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
			if obj.VType == TypeMap {
				m := obj.Ref.(*VMMap)
				key := args[1].Str
				if args[1].VType == TypeInt {
					key = strconv.FormatInt(args[1].I64, 10)
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
			msg, _ := val.ToError()
			return &VMError{Value: val, Message: msg, IsPanic: true}
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
				}
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
					val, _ := strconv.ParseInt(arg.Str, 10, 64)
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
					val, _ := strconv.ParseFloat(arg.Str, 64)
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
			}
			session.ValueStack.Push(NewFloat(0.0))
			return nil
		case "Bool":
			if len(args) > 0 && args[0] != nil {
				arg := args[0]
				arg = e.unwrapValue(arg)
				b, _ := arg.ToBool()
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
			innerType := tStr
			if strings.HasPrefix(tStr, "Ptr<") && strings.HasSuffix(tStr, ">") {
				innerType = tStr[4 : len(tStr)-1]
			}
			val := e.initializeType(session, MustParseRuntimeType(innerType), 0)

			// For internal "heap" simulation, we can use a non-zero handle ID.
			// Since we only need it to be non-nil for the test, and ideally it should
			// point to something that can be dereferenced or used later.
			internalID := uint32(1000000 + time.Now().UnixNano()%1000000)

			res := &Var{
				VType:  TypeHandle,
				Handle: internalID,
				Ref:    val, // Store the actual value in Ref for potential future dereference
			}
			res.SetRawType("Ptr<" + innerType + ">")
			session.ValueStack.Push(res)
			return nil
		case "spawn":
			if len(args) == 0 {
				return &VMError{Message: "spawn requires a callable argument", IsPanic: true}
			}
			task, err := e.spawnCall(session, "", nil, nil, args[0], args[1:])
			if err != nil {
				return err
			}
			handle := &Var{
				VType:  TypeHandle,
				Handle: task.ID,
			}
			handle.SetRawType("Ptr<task.Task>")
			session.ValueStack.Push(handle)
			return nil
		case "await":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "await requires exactly 1 task handle", IsPanic: true}
			}
			res, err := e.awaitTaskHandle(session, args[0])
			if err != nil {
				return err
			}
			session.ValueStack.Push(res)
			return nil
		}
	}

	if mod != nil && mod.Name == "task" {
		if handled, err := e.invokeTaskModuleIntrinsic(session, name, args); handled {
			return err
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
				if handled, err := e.invokeTaskModuleRoute(session, route.Name, args); handled {
					return err
				}
				return e.evalFFIAndPush(session, route, args, argLHS)
			}
		}

		if c.VType == TypeClosure {
			switch ref := c.Ref.(type) {
			case *VMClosure:
				return e.setupFuncCall(session, "closure", nil, args, ref)
			case *VMMethodValue:
				if ref.Receiver != nil && ref.Receiver.VType == TypeModule {
					if modRef, ok := ref.Receiver.Ref.(*VMModule); ok && modRef.Name == "task" {
						actualArgs := args
						if len(args) > 0 && args[0] == ref.Receiver {
							actualArgs = args[1:]
						}
						if handled, err := e.invokeTaskModuleIntrinsic(session, ref.Method, actualArgs); handled {
							return err
						}
					}
				}
				// Resolve as FFI method
				if route, ok := e.routes[ref.Method]; ok {
					return e.evalFFIAndPush(session, route, append([]*Var{ref.Receiver}, args...), nil)
				}
				// 动态 FFI 路由：如果 Receiver 是一个带 Bridge 的 Handle，直接发起调用
				if ref.Receiver != nil && ref.Receiver.VType == TypeHandle && ref.Receiver.Bridge != nil {
					// 动态接口调用保持 Any 边界，与宿主侧 InterfaceData/Invoke 契约一致。
					route := FFIRoute{
						Bridge:   ref.Receiver.Bridge,
						MethodID: 0, // 对于接口调用，通常我们传方法名字符串
						Name:     ref.Method,
					}
					return e.evalFFIAndPush(session, route, append([]*Var{ref.Receiver}, args...), nil)
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
		if handled, err := e.invokeTaskModuleRoute(session, route.Name, args); handled {
			return err
		}
		return e.evalFFIAndPush(session, route, args, argLHS)
	}

	// 3a. Dynamic FFI Call for Handles (Interfaces)
	if receiver != nil && receiver.VType == TypeHandle && receiver.Bridge != nil && name != "" {
		route := FFIRoute{
			Bridge:   receiver.Bridge,
			MethodID: 0,
			Name:     name,
		}
		return e.evalFFIAndPush(session, route, args, nil)
	}

	// 3b. Dynamic Method Call for Maps (Interfaces)
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

	// 3c. Dynamic Method Call for Modules (Interfaces)
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
			FunctionSig: cloneRuntimeFuncSig(fn.FunctionSig),
			BodyTasks:   cloneTasks(fn.BodyTasks),
			Args:        args,
		}, args, nil)
	}

	if callable != nil {
		return &VMError{Message: fmt.Sprintf("type %v is not callable", callable.VType), IsPanic: true}
	}

	return &VMError{Message: fmt.Sprintf("function or method %s not found", name), IsPanic: true}
}

func (e *Executor) invokeTaskModuleIntrinsic(session *StackContext, name string, args []*Var) (bool, error) {
	switch name {
	case "NewTaskGroup":
		session.ValueStack.Push(e.newTaskGroupHandle())
		return true, nil
	case "AddTask":
		if err := e.taskGroupAddTaskCall(args); err != nil {
			return true, err
		}
		session.ValueStack.Push(nil)
		return true, nil
	case "WaitTasks":
		if err := e.taskGroupWaitCall(session, args); err != nil {
			return true, err
		}
		session.ValueStack.Push(nil)
		return true, nil
	case "GroupErr":
		errVar, err := e.taskGroupErrCall(args)
		if err != nil {
			return true, err
		}
		session.ValueStack.Push(errVar)
		return true, nil
	case "CancelGroup":
		if err := e.taskGroupCancelCall(args); err != nil {
			return true, err
		}
		session.ValueStack.Push(nil)
		return true, nil
	case "Status":
		status, err := e.taskStatusCall(args)
		if err != nil {
			return true, err
		}
		session.ValueStack.Push(NewString(status))
		return true, nil
	case "Err":
		errVar, err := e.taskErrCall(args)
		if err != nil {
			return true, err
		}
		session.ValueStack.Push(errVar)
		return true, nil
	case "Cancel":
		if err := e.taskCancelCall(args); err != nil {
			return true, err
		}
		session.ValueStack.Push(nil)
		return true, nil
	default:
		return false, nil
	}
}

func (e *Executor) invokeTaskModuleRoute(session *StackContext, routeName string, args []*Var) (bool, error) {
	switch routeName {
	case "task.NewTaskGroup":
		return e.invokeTaskModuleIntrinsic(session, "NewTaskGroup", args)
	case "task.AddTask":
		return e.invokeTaskModuleIntrinsic(session, "AddTask", args)
	case "task.WaitTasks":
		return e.invokeTaskModuleIntrinsic(session, "WaitTasks", args)
	case "task.GroupErr":
		return e.invokeTaskModuleIntrinsic(session, "GroupErr", args)
	case "task.CancelGroup":
		return e.invokeTaskModuleIntrinsic(session, "CancelGroup", args)
	case "task.Status":
		return e.invokeTaskModuleIntrinsic(session, "Status", args)
	case "task.Err":
		return e.invokeTaskModuleIntrinsic(session, "Err", args)
	case "task.Cancel":
		return e.invokeTaskModuleIntrinsic(session, "Cancel", args)
	default:
		return false, nil
	}
}

func isTaskModuleIntrinsic(name string) bool {
	switch name {
	case "NewTaskGroup", "AddTask", "WaitTasks", "GroupErr", "CancelGroup", "Status", "Err", "Cancel":
		return true
	default:
		return false
	}
}

func (e *Executor) awaitTaskHandle(session *StackContext, handle *Var) (*Var, error) {
	taskID, err := e.resolveTaskHandle(handle)
	if err != nil {
		return nil, err
	}
	res, err := e.scheduler.Await(session.Context, taskID)
	if err != nil {
		return nil, &VMError{Message: err.Error(), IsPanic: true}
	}
	return res, nil
}

func (e *Executor) taskStatusCall(args []*Var) (string, error) {
	if len(args) != 1 || args[0] == nil {
		return "", &VMError{Message: "task.Status requires exactly 1 task handle", IsPanic: true}
	}
	taskID, err := e.resolveTaskHandle(args[0])
	if err != nil {
		return "", err
	}
	status, err := e.scheduler.Status(taskID)
	if err != nil {
		return "", &VMError{Message: err.Error(), IsPanic: true}
	}
	return status.String(), nil
}

func (e *Executor) taskErrCall(args []*Var) (*Var, error) {
	if len(args) != 1 || args[0] == nil {
		return nil, &VMError{Message: "task.Err requires exactly 1 task handle", IsPanic: true}
	}
	taskID, err := e.resolveTaskHandle(args[0])
	if err != nil {
		return nil, err
	}
	status, taskErr, err := e.scheduler.Error(taskID)
	if err != nil {
		return nil, &VMError{Message: err.Error(), IsPanic: true}
	}
	if status == TaskPending || status == TaskRunning || taskErr == nil {
		return nil, nil
	}
	return &Var{
		VType: TypeError,
		Ref: &VMError{
			Message: taskErr.Error(),
			Cause:   taskErr,
		},
	}, nil
}

func (e *Executor) taskCancelCall(args []*Var) error {
	if len(args) != 1 || args[0] == nil {
		return &VMError{Message: "task.Cancel requires exactly 1 task handle", IsPanic: true}
	}
	taskID, err := e.resolveTaskHandle(args[0])
	if err != nil {
		return err
	}
	if err := e.scheduler.Cancel(taskID); err != nil {
		return &VMError{Message: err.Error(), IsPanic: true}
	}
	return nil
}

func (e *Executor) newTaskGroupHandle() *Var {
	groupVar := &Var{
		VType: TypeAny,
		Ref:   NewVMTaskGroup(),
	}
	groupVar.SetRawType("task.TaskGroup")
	handle := &Var{
		VType: TypeHandle,
		Ref:   groupVar,
	}
	handle.SetRawType("Ptr<task.TaskGroup>")
	return handle
}

func (e *Executor) taskGroupAddTaskCall(args []*Var) error {
	if len(args) != 2 || args[0] == nil || args[1] == nil {
		return &VMError{Message: "task.AddTask requires group and task handle", IsPanic: true}
	}
	group, err := e.resolveTaskGroupHandle(args[0])
	if err != nil {
		return err
	}
	taskID, err := e.resolveTaskHandle(args[1])
	if err != nil {
		return err
	}
	group.Add(taskID)
	return nil
}

func (e *Executor) taskGroupWaitCall(session *StackContext, args []*Var) error {
	if len(args) != 1 || args[0] == nil {
		return &VMError{Message: "task.WaitTasks requires exactly 1 task group", IsPanic: true}
	}
	group, err := e.resolveTaskGroupHandle(args[0])
	if err != nil {
		return err
	}
	for _, taskID := range group.Snapshot() {
		if _, waitErr := e.scheduler.Await(session.Context, taskID); waitErr != nil {
			group.RememberErr(waitErr)
		}
	}
	return nil
}

func (e *Executor) taskGroupErrCall(args []*Var) (*Var, error) {
	if len(args) != 1 || args[0] == nil {
		return nil, &VMError{Message: "task.GroupErr requires exactly 1 task group", IsPanic: true}
	}
	group, err := e.resolveTaskGroupHandle(args[0])
	if err != nil {
		return nil, err
	}
	if groupErr := group.Err(); groupErr != nil {
		return &Var{
			VType: TypeError,
			Ref: &VMError{
				Message: groupErr.Error(),
				Cause:   groupErr,
			},
		}, nil
	}
	return nil, nil
}

func (e *Executor) taskGroupCancelCall(args []*Var) error {
	if len(args) != 1 || args[0] == nil {
		return &VMError{Message: "task.CancelGroup requires exactly 1 task group", IsPanic: true}
	}
	group, err := e.resolveTaskGroupHandle(args[0])
	if err != nil {
		return err
	}
	for _, taskID := range group.Snapshot() {
		if cancelErr := e.scheduler.Cancel(taskID); cancelErr != nil {
			group.RememberErr(cancelErr)
		}
	}
	return nil
}

func (e *Executor) resolveTaskGroupHandle(handle *Var) (*VMTaskGroup, error) {
	if handle == nil {
		return nil, &VMError{Message: "missing task group handle", IsPanic: true}
	}
	groupVar, ok := e.vmPointerTarget(handle)
	if !ok || groupVar == nil {
		return nil, &VMError{Message: "invalid task group handle", IsPanic: true}
	}
	group, ok := groupVar.Ref.(*VMTaskGroup)
	if !ok || group == nil {
		return nil, &VMError{Message: "invalid task group payload", IsPanic: true}
	}
	return group, nil
}

func (e *Executor) resolveTaskHandle(handle *Var) (uint32, error) {
	if e.scheduler == nil {
		return 0, &VMError{Message: "task scheduler is not initialized", IsPanic: true}
	}
	taskID, err := handle.ToHandle()
	if err != nil {
		return 0, &VMError{Message: err.Error(), IsPanic: true}
	}
	return taskID, nil
}

func (e *Executor) spawnCall(parent *StackContext, name string, receiver *Var, mod *VMModule, callable *Var, args []*Var) (*VMTask, error) {
	if e.scheduler == nil {
		return nil, &VMError{Message: "task scheduler is not initialized", IsPanic: true}
	}
	if parent == nil {
		return nil, &VMError{Message: "missing parent session for spawn", IsPanic: true}
	}

	if callable != nil {
		snapshotted, err := e.snapshotTaskInput(callable)
		if err != nil {
			return nil, err
		}
		callable = snapshotted
	}

	spawnArgs := make([]*Var, len(args))
	for i, arg := range args {
		if arg != nil {
			snapshotted, err := e.snapshotTaskInput(arg)
			if err != nil {
				return nil, err
			}
			spawnArgs[i] = snapshotted
		}
	}
	if receiver != nil {
		snapshotted, err := e.snapshotTaskInput(receiver)
		if err != nil {
			return nil, err
		}
		receiver = snapshotted
	}

	task, err := e.scheduler.Spawn(parent.Context, func(ctx context.Context) (*Var, error) {
		child := e.NewSession(ctx, "task")
		child.StepLimit = parent.StepLimit
		child.Shared = parent.Shared
		child.ImportChain = make(map[string]bool, len(parent.ImportChain))
		for k, v := range parent.ImportChain {
			child.ImportChain[k] = v
		}
		defer e.CleanupSession(child)

		if err := e.invokeCall(child, name, receiver, mod, callable, spawnArgs, nil); err != nil {
			return nil, err
		}
		if len(child.TaskStack) == 0 {
			if child.ValueStack != nil {
				return child.ValueStack.Peek(), nil
			}
			return nil, nil
		}
		if err := e.Run(child); err != nil {
			return nil, err
		}
		if child.ValueStack != nil {
			return child.ValueStack.Peek(), nil
		}
		return nil, nil
	})
	if err != nil {
		return nil, &VMError{Message: err.Error(), IsPanic: true}
	}
	return task, nil
}

type taskSnapshotState struct {
	cloned   map[*Var]*Var
	visiting map[*Var]bool
}

func newTaskSnapshotState() *taskSnapshotState {
	return &taskSnapshotState{
		cloned:   make(map[*Var]*Var),
		visiting: make(map[*Var]bool),
	}
}

func (e *Executor) snapshotTaskInput(v *Var) (*Var, error) {
	return e.snapshotTaskVar(v, newTaskSnapshotState())
}

func (e *Executor) snapshotTaskVar(v *Var, state *taskSnapshotState) (*Var, error) {
	if v == nil {
		return nil, nil
	}
	if cloned, ok := state.cloned[v]; ok {
		return cloned, nil
	}
	if state.visiting[v] {
		return nil, &VMError{Message: "spawn cannot snapshot recursive values", IsPanic: true}
	}
	state.visiting[v] = true
	defer delete(state.visiting, v)

	out := &Var{
		TypeInfo: v.TypeInfo,
		VType:    v.VType,
		I64:      v.I64,
		F64:      v.F64,
		Str:      v.Str,
		Bool:     v.Bool,
		Handle:   v.Handle,
		Bridge:   v.Bridge,
	}
	state.cloned[v] = out
	if v.B != nil {
		out.B = append([]byte(nil), v.B...)
	}

	if v.VType == TypeHandle {
		if _, ok := e.vmPointerTarget(v); ok {
			return nil, &VMError{Message: "spawn cannot snapshot vm pointers", IsPanic: true}
		}
		out.Ref = v.Ref
		if v.Handle != 0 && v.Ref == nil {
			out.Ref = NewVMHandle(v.Handle, v.Bridge)
		}
		return out, nil
	}

	switch ref := v.Ref.(type) {
	case nil:
		return out, nil
	case *VMArray:
		items := ref.Snapshot()
		cloned := make([]*Var, len(items))
		for i, item := range items {
			next, err := e.snapshotTaskVar(item, state)
			if err != nil {
				return nil, err
			}
			cloned[i] = next
		}
		out.Ref = &VMArray{Data: cloned}
	case *VMMap:
		snapshot := ref.Snapshot()
		cloned := make(map[string]*Var, len(snapshot))
		for k, item := range snapshot {
			next, err := e.snapshotTaskVar(item, state)
			if err != nil {
				return nil, err
			}
			cloned[k] = next
		}
		out.Ref = &VMMap{Data: cloned}
	case *Cell:
		next, err := e.snapshotTaskVar(ref.Value, state)
		if err != nil {
			return nil, err
		}
		out.Ref = &Cell{Value: next}
	case *VMClosure:
		snapshotted, err := e.snapshotTaskClosure(ref, state)
		if err != nil {
			return nil, err
		}
		out.Ref = snapshotted
	case *VMError:
		var panicVal *Var
		var err error
		if ref.Value != nil {
			panicVal, err = e.snapshotTaskVar(ref.Value, state)
			if err != nil {
				return nil, err
			}
		}
		out.Ref = &VMError{
			Message: ref.Message,
			Value:   panicVal,
			IsPanic: ref.IsPanic,
			Cause:   ref.Cause,
			Handle:  ref.Handle,
			Bridge:  ref.Bridge,
		}
	case *VMModule:
		return nil, &VMError{Message: "spawn cannot snapshot modules", IsPanic: true}
	case *VMInterface:
		return nil, &VMError{Message: "spawn cannot snapshot interfaces with runtime-backed state", IsPanic: true}
	case *VMHandle:
		out.Ref = &VMHandle{ID: ref.ID, Bridge: ref.Bridge}
	default:
		out.Ref = ref
	}
	return out, nil
}

func (e *Executor) snapshotTaskClosure(closure *VMClosure, state *taskSnapshotState) (*VMClosure, error) {
	if closure == nil {
		return nil, nil
	}
	cloned := &VMClosure{
		FunctionSig:  cloneRuntimeFuncSig(closure.FunctionSig),
		BodyTasks:    cloneTasks(closure.BodyTasks),
		UpvalueSlots: make([]*Var, len(closure.UpvalueSlots)),
		UpvalueNames: append([]string(nil), closure.UpvalueNames...),
		Context:      nil,
	}
	for i, slot := range closure.UpvalueSlots {
		next, err := e.snapshotTaskVar(slot, state)
		if err != nil {
			return nil, err
		}
		cloned.UpvalueSlots[i] = next
	}
	return cloned, nil
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
		sig = cloneRuntimeFuncSig(closure.FunctionSig)
		bodyTasks = closure.BodyTasks
	}
	if fn != nil {
		if sig == nil {
			sig = cloneRuntimeFuncSig(fn.FunctionSig)
		}
		if len(bodyTasks) == 0 {
			bodyTasks = fn.BodyTasks
		}
	}
	if sig == nil {
		sig = MustParseRuntimeFuncSig("function() Void")
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
		panic(errors.New("stack overflow"))
	}
	newStack := &Stack{
		Parent:    root,
		MemoryPtr: make(map[string]*Var),
		Frame:     &SlotFrame{},
		Scope:     name,
		Depth:     newDepth,
	}

	session.Stack = newStack

	// Inject captured variables
	if closure != nil {
		newStack.Frame.Upvalues = append(newStack.Frame.Upvalues, closure.UpvalueSlots...)
		newStack.Frame.UpvalueNames = append(newStack.Frame.UpvalueNames, closure.UpvalueNames...)
	}

	// Inject params
	for i, paramType := range sig.ParamTypes {
		paramName := ""
		if i < len(sig.ParamNames) {
			paramName = sig.ParamNames[i]
		}
		paramSym := SymbolRef{Name: paramName, Kind: SymbolLocal, Slot: i}
		_ = session.DeclareSymbol(paramSym, paramType)
		if sig.Variadic && i == len(sig.ParamTypes)-1 {
			var variadicArgs []*Var
			if i < len(args) {
				variadicArgs = args[i:]
			}
			arr := &Var{VType: TypeArray, Ref: &VMArray{Data: variadicArgs}}
			arr.SetRuntimeType(paramType)
			_ = session.StoreSymbol(paramSym, arr)
		} else if i < len(args) && args[i] != nil {
			_ = session.StoreSymbol(paramSym, args[i])
		}
	}
	if !sig.ReturnType.IsVoid() {
		_ = session.InitReturn(sig.ReturnType)
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
	// Push Defers execution
	session.TaskStack = append(session.TaskStack, Task{Op: OpRunDefers})

	// Push body
	if len(bodyTasks) > 0 {
		session.TaskStack = append(session.TaskStack, bodyTasks...)
	}

	return nil
}

func (e *Executor) evalFFIAndPush(session *StackContext, route FFIRoute, args []*Var, argLHS []LHSValue) error {
	// Let's use the old evalFFI logic
	res, err := e.evalFFI(session, route, args, argLHS)
	if err != nil {
		return err
	}

	session.ValueStack.Push(res)
	return nil
}

func (e *Executor) initializeType(ctx *StackContext, t RuntimeType, depth int) *Var {
	if depth > 10 {
		return NewVarWithRuntimeType(t, TypeAny)
	}

	// 1. Resolve to the underlying shape for initialization, but keep t as the logical type
	shape := t
	if resolved, ok, err := e.resolveNamedTypeChain(t.Raw); err == nil && ok {
		shape = resolved
	}

	if shape.IsPtr() {
		v := &Var{VType: TypeHandle, Handle: 0}
		v.SetRuntimeType(t)
		return v
	}

	if shape.IsInterface() {
		v := &Var{VType: TypeInterface, Ref: nil}
		v.SetRuntimeType(t)
		return v
	}

	if shape.IsArray() || shape.IsMap() || shape.IsAny() {
		return NewVarWithRuntimeType(t, TypeAny)
	}

	// 基础类型初始化
	zero := shape.ZeroVar()
	res := e.ToVar(ctx, zero, nil)
	if res != nil {
		res.SetRuntimeType(t) // 还原为用户请求的命名类型
		return res
	}

	// 结构体初始化
	mData := make(map[string]*Var)
	if sDef, ok := e.resolveStructSchema(shape.Raw); ok {
		for _, field := range sDef.Fields {
			mData[field.Name] = e.initializeType(ctx, field.TypeInfo, depth+1)
		}
	}
	v := &Var{VType: TypeMap, Ref: &VMMap{Data: mData}}
	v.SetRuntimeType(t)
	return v
}
