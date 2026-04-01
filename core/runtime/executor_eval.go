package runtime

import (
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
	// 解包 Cell
	if l != nil && l.VType == TypeCell {
		l = l.Ref.(*Cell).Value
	}
	if r != nil && r.VType == TypeCell {
		r = r.Ref.(*Cell).Value
	}

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
	if val == nil {
		return nil, &VMError{Message: "unary op with nil operand", IsPanic: true}
	}
	if val.VType == TypeCell {
		val = val.Ref.(*Cell).Value
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
		if val.VType == TypeHandle && val.Ref != nil {
			if res, ok := val.Ref.(*Var); ok {
				return res, nil
			}
		}
		return nil, &VMError{Message: fmt.Sprintf("cannot dereference type %v", val.VType), IsPanic: true}
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
		if i < 0 || i >= len(arr.Data) {
			return nil, &VMError{Message: fmt.Sprintf("index out of range: %d", i), IsPanic: true}
		}
		return arr.Data[i], nil
	case TypeMap:
		m := obj.Ref.(*VMMap)
		// Dynamic Key Type Validation
		keyType, valType, _ := obj.Type.GetMapKeyValueTypes()
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
		if val, ok := m.Data[key]; ok {
			return val, nil
		}
		return e.ToVar(ctx, valType.ZeroVar(), nil), nil
	}
	return nil, &VMError{Message: fmt.Sprintf("type %v does not support indexing", obj.VType), IsPanic: true}
}

func (e *Executor) evalMemberExprDirect(_ *StackContext, obj *Var, property string) (*Var, error) {
	if obj == nil || isEmptyVar(obj) {
		return nil, &VMError{Message: "member access on nil object: " + property, IsPanic: true}
	}
	if obj.VType == TypeCell {
		obj = obj.Ref.(*Cell).Value
	}

	// 穿透 TypeAny
	if obj.VType == TypeAny && obj.Ref != nil {
		if inner, ok := obj.Ref.(*Var); ok {
			return e.evalMemberExprDirect(nil, inner, property)
		}
	}

	if obj.VType == TypeInterface {
		inter := obj.Ref.(*VMInterface)
		if inter.Spec == nil {
			return nil, &VMError{Message: fmt.Sprintf("interface contract missing for %s", obj.Type), IsPanic: true}
		}
		sig, ok := inter.Spec.ByName[property]
		if !ok || sig == nil {
			return nil, &VMError{Message: fmt.Sprintf("method %s not in interface contract %s", property, obj.Type), IsPanic: true}
		}
		// 如果 Target 是一个 FFI Handle，我们需要一个通用的路由方式
		// 我们可以利用 VMMethodValue，但在 Invoke 时需要知道这是个 FFI 调用
		return &Var{
			VType: TypeClosure,
			Ref: &VMMethodValue{
				Receiver: inter.Target,
				Method:   property, // 对于 FFI 接口，直接存方法名
			},
			Type: sig.Spec,
		}, nil
	}

	switch obj.VType {
	case TypeError:
		if property == "Error" {
			return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: "Error"}}, nil
		}
	case TypeMap:
		m := obj.Ref.(*VMMap)
		if val, ok := m.Data[property]; ok {
			return val, nil
		}
		// Try to look up as a method if it has a type name
		tName := string(obj.Type)
		if tName != "" && tName != "Any" && !strings.HasPrefix(tName, "Map<") {
			methodName := fmt.Sprintf("__method_%s_%s", tName, property)
			if _, ok := e.routes[methodName]; ok {
				return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
			}
			// Also check internal functions
			if _, ok := e.program.Functions[ast.Ident(methodName)]; ok {
				return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
			}
		}
		return nil, nil
	case TypeHandle:
		// Check if it's a pointer to something that has fields (like a struct in Ref)
		if obj.Ref != nil {
			if valVar, ok := obj.Ref.(*Var); ok {
				return e.evalMemberExprDirect(nil, valVar, property)
			}
		}
		// Handle method extraction (implicit binding)
		tName := string(obj.Type)
		if tName == "" {
			tName = obj.VType.String()
		}
		tName = strings.TrimPrefix(tName, "Ptr<")
		tName = strings.TrimPrefix(tName, "*")
		tName = strings.TrimSuffix(tName, ">")
		methodName := fmt.Sprintf("__method_%s_%s", tName, property)

		if _, ok := e.routes[methodName]; ok {
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
		if val, ok := mod.Data[property]; ok {
			return val, nil
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
				if val, ok := m.Data[property]; ok {
					return val, nil
				}
			}
		}
		return nil, nil
	}

	tName := string(obj.Type)
	if tName == "" {
		tName = obj.VType.String()
	}
	tName = strings.TrimPrefix(tName, "Ptr<")
	tName = strings.TrimPrefix(tName, "*")
	tName = strings.TrimSuffix(tName, ">")
	methodName := fmt.Sprintf("__method_%s_%s", tName, property)

	if _, ok := e.routes[methodName]; ok {
		return &Var{VType: TypeClosure, Ref: &VMMethodValue{Receiver: obj, Method: methodName}}, nil
	}

	if string(obj.Type) == "Any" {
		return nil, nil
	}

	return nil, &VMError{Message: fmt.Sprintf("type %v does not support member access: %s", obj.VType, property), IsPanic: true}
}

func (e *Executor) evalSliceExprDirect(_ *StackContext, obj, lowVar, highVar *Var) (*Var, error) {
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
		l := len(arr.Data)
		if high == -1 {
			high = l
		}
		if low < 0 || high < low || high > l {
			return nil, &VMError{Message: fmt.Sprintf("slice bounds out of range [%d:%d] with capacity %d", low, high, l), IsPanic: true}
		}
		return &Var{VType: TypeArray, Ref: &VMArray{Data: arr.Data[low:high]}, Type: obj.Type}, nil
	}
	return nil, &VMError{Message: fmt.Sprintf("type %v does not support slice operations", obj.VType), IsPanic: true}
}

func (e *Executor) invokeCall(session *StackContext, name string, receiver *Var, mod *VMModule, callable *Var, args []*Var) error {
	// 0. 特殊类型方法 (Built-in methods on Error)
	if receiver != nil && receiver.VType == TypeError && name == "Error" {
		if errObj, ok := receiver.Ref.(*VMError); ok {
			session.ValueStack.Push(NewString(errObj.Message))
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
			if typVar != nil && typVar.VType == TypeCell {
				typVar = typVar.Ref.(*Cell).Value
			}
			if typVar == nil || (typVar.VType != TypeString && typVar.Type != "String") {
				return &VMError{Message: "make first argument must be a type string", IsPanic: true}
			}
			tStr := typVar.Str
			t := ast.GoMiniType(tStr)

			if strings.HasPrefix(tStr, "Map<") {
				session.ValueStack.Push(&Var{VType: TypeMap, Ref: &VMMap{Data: make(map[string]*Var)}, Type: t})
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
					session.ValueStack.Push(&Var{VType: TypeBytes, B: make([]byte, length, capacity), Type: t})
				} else {
					arr := make([]*Var, length, capacity)
					innerType, _ := t.ReadArrayItemType()
					for i := 0; i < length; i++ {
						arr[i] = e.initializeType(session, innerType, 0)
					}
					session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: arr}, Type: t})
				}
				return nil
			}
			// Fallback
			res := e.initializeType(session, t, 0)
			session.ValueStack.Push(res)
			return nil
		case "len":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "len requires exactly 1 argument", IsPanic: true}
			}
			obj := args[0]
			if obj.VType == TypeCell {
				obj = obj.Ref.(*Cell).Value
			}
			if obj == nil {
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			if obj.VType == TypeAny && obj.Ref != nil {
				if arr, ok := obj.Ref.(*VMArray); ok {
					session.ValueStack.Push(NewInt(int64(len(arr.Data))))
					return nil
				}
				if m, ok := obj.Ref.(*VMMap); ok {
					session.ValueStack.Push(NewInt(int64(len(m.Data))))
					return nil
				}
			}
			switch obj.VType {
			case TypeString:
				session.ValueStack.Push(NewInt(int64(len(obj.Str))))
				return nil
			case TypeBytes:
				session.ValueStack.Push(NewInt(int64(len(obj.B))))
				return nil
			case TypeArray:
				session.ValueStack.Push(NewInt(int64(len(obj.Ref.(*VMArray).Data))))
				return nil
			case TypeMap:
				session.ValueStack.Push(NewInt(int64(len(obj.Ref.(*VMMap).Data))))
				return nil
			}
			return &VMError{Message: fmt.Sprintf("invalid argument for len: %v", obj.VType), IsPanic: true}
		case "cap":
			if len(args) != 1 || args[0] == nil {
				return &VMError{Message: "cap requires exactly 1 argument", IsPanic: true}
			}
			obj := args[0]
			if obj.VType == TypeCell {
				obj = obj.Ref.(*Cell).Value
			}
			if obj == nil {
				session.ValueStack.Push(NewInt(0))
				return nil
			}
			switch obj.VType {
			case TypeBytes:
				session.ValueStack.Push(NewInt(int64(cap(obj.B))))
				return nil
			case TypeArray:
				session.ValueStack.Push(NewInt(int64(cap(obj.Ref.(*VMArray).Data))))
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
				newArr := make([]*Var, len(arr.Data), len(arr.Data)+len(args)-1)
				copy(newArr, arr.Data)
				newArr = append(newArr, args[1:]...)
				session.ValueStack.Push(&Var{VType: TypeArray, Ref: &VMArray{Data: newArr}, Type: args[0].Type})
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
				session.ValueStack.Push(&Var{VType: TypeBytes, B: buf, Type: args[0].Type})
				return nil
			}
			return &VMError{Message: "append requires array or bytes as first argument", IsPanic: true}
		case "delete":
			if len(args) != 2 || args[0] == nil || args[1] == nil {
				return &VMError{Message: "delete requires map and key", IsPanic: true}
			}
			obj := args[0]
			if obj.VType == TypeAny && obj.Ref != nil {
				if m, ok := obj.Ref.(*VMMap); ok {
					key := args[1].Str
					if args[1].VType == TypeInt {
						key = strconv.FormatInt(args[1].I64, 10)
					}
					delete(m.Data, key)
					session.ValueStack.Push(nil)
					return nil
				}
			}
			if obj.VType == TypeMap {
				m := obj.Ref.(*VMMap)
				key := args[1].Str
				if args[1].VType == TypeInt {
					key = strconv.FormatInt(args[1].I64, 10)
				}
				delete(m.Data, key)
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
				b, _ := args[0].ToBool()
				session.ValueStack.Push(NewBool(b))
				return nil
			}
			session.ValueStack.Push(NewBool(false))
			return nil
		case "new":
			if len(args) < 1 || args[0] == nil || (args[0].VType != TypeString && args[0].Type != "String") {
				return &VMError{Message: "new requires a type string as argument", IsPanic: true}
			}
			tStr := args[0].Str
			innerType := tStr
			if strings.HasPrefix(tStr, "Ptr<") && strings.HasSuffix(tStr, ">") {
				innerType = tStr[4 : len(tStr)-1]
			}
			val := e.initializeType(session, ast.GoMiniType(innerType), 0)

			// For internal "heap" simulation, we can use a non-zero handle ID.
			// Since we only need it to be non-nil for the test, and ideally it should
			// point to something that can be dereferenced or used later.
			internalID := uint32(1000000 + time.Now().UnixNano()%1000000)

			res := &Var{
				VType:  TypeHandle,
				Handle: internalID,
				Type:   ast.GoMiniType("Ptr<" + innerType + ">"),
				Ref:    val, // Store the actual value in Ref for potential future dereference
			}
			session.ValueStack.Push(res)
			return nil
		}
	}

	// 2. Closure / Method Value / FFI Route
	if callable != nil {
		c := callable
		if c.VType == TypeAny && c.Ref != nil {
			if v, ok := c.Ref.(*Var); ok && v.VType == TypeClosure {
				c = v
			} else if route, ok := c.Ref.(FFIRoute); ok {
				return e.evalFFIAndPush(session, route, args)
			}
		}

		if c.VType == TypeClosure {
			switch ref := c.Ref.(type) {
			case *VMClosure:
				return e.setupFuncCall(session, "closure", nil, args, ref)
			case *VMMethodValue:
				// Resolve as FFI method
				if route, ok := e.routes[ref.Method]; ok {
					return e.evalFFIAndPush(session, route, append([]*Var{ref.Receiver}, args...))
				}
				// 动态 FFI 路由：如果 Receiver 是一个带 Bridge 的 Handle，直接发起调用
				if ref.Receiver != nil && ref.Receiver.VType == TypeHandle && ref.Receiver.Bridge != nil {
					// 构造临时路由。注意：这里我们暂时假设返回值为 Any。
					// 完善的做法是：如果 callable 带有签名信息，可以从中提取。
					route := FFIRoute{
						Bridge:   ref.Receiver.Bridge,
						MethodID: 0, // 对于接口调用，通常我们传方法名字符串
						Name:     ref.Method,
						Returns:  "Any", // 默认
						Return:   "Any",
					}
					// 如果 c 有类型信息且是接口方法签名
					if c.Type != "" {
						if ft, ok := c.Type.ReadFunc(); ok {
							route.Returns = string(ft.Return)
							route.Return = ft.Return
						}
					}
					return e.evalFFIAndPush(session, route, append([]*Var{ref.Receiver}, args...))
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
		return e.evalFFIAndPush(session, route, args)
	}

	// 3a. Dynamic FFI Call for Handles (Interfaces)
	if receiver != nil && receiver.VType == TypeHandle && receiver.Bridge != nil && name != "" {
		route := FFIRoute{
			Bridge:   receiver.Bridge,
			MethodID: 0,
			Name:     name,
			Returns:  "Any",
			Return:   "Any",
		}
		// If we had the signature, we could set route.Returns here
		return e.evalFFIAndPush(session, route, args)
	}

	// 3b. Dynamic Method Call for Maps (Interfaces)
	if receiver != nil && receiver.VType == TypeMap && name != "" {
		m := receiver.Ref.(*VMMap)
		if val, ok := m.Data[name]; ok {
			// Found it! It could be a closure stored in the map.
			// IMPORTANT: If we found the method via a receiver, we should strip the receiver
			// from args if it was automatically prepended by OpCall.
			actualArgs := args
			if len(args) > 0 && args[0] == receiver {
				actualArgs = args[1:]
			}
			return e.invokeCall(session, "", nil, nil, val, actualArgs)
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
			return e.evalFFIAndPush(session, route, actualArgs)
		}
	}

	// 4. Module Function Call
	if mod != nil {
		if fVar, ok := mod.Data[name]; ok && fVar.VType == TypeClosure {
			if closure, ok := fVar.Ref.(*VMClosure); ok {
				return e.setupFuncCall(session, name, nil, args, closure)
			}
		}
	}

	// 5. Internal Function Call
	if f, ok := e.program.Functions[ast.Ident(name)]; ok {
		bodyTasks := e.tasksForStmt(f.Body, nil)
		return e.setupFuncCall(session, name, &DoCallData{
			Name:         name,
			FunctionType: f.FunctionType,
			BodyTasks:    bodyTasks,
			Args:         args,
		}, args, nil)
	}

	if callable != nil {
		return &VMError{Message: fmt.Sprintf("type %v is not callable", callable.VType), IsPanic: true}
	}

	return &VMError{Message: fmt.Sprintf("function or method %s not found", name), IsPanic: true}
}

func (e *Executor) setupFuncCall(session *StackContext, name string, fn *DoCallData, args []*Var, closure *VMClosure) error {
	old := session.Stack
	oldExec := session.Executor
	ft := ast.FunctionType{}
	var bodyTasks []Task
	if closure != nil {
		ft = closure.FunctionType
		bodyTasks = closure.BodyTasks
	}
	if fn != nil {
		if len(ft.Params) == 0 && ft.Return == "" {
			ft = fn.FunctionType
		}
		if len(bodyTasks) == 0 {
			bodyTasks = fn.BodyTasks
		}
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
		Scope:     name,
		Depth:     newDepth,
	}

	session.Stack = newStack

	// Inject captured variables
	if closure != nil {
		for k, v := range closure.Upvalues {
			_ = session.AddVariable(k, v)
		}
	}

	// Inject params
	for i, p := range ft.Params {
		_ = session.NewVar(string(p.Name), p.Type)
		if ft.Variadic && i == len(ft.Params)-1 {
			var variadicArgs []*Var
			if i < len(args) {
				variadicArgs = args[i:]
			}
			_ = session.Store(string(p.Name), &Var{VType: TypeArray, Ref: &VMArray{Data: variadicArgs}, Type: p.Type})
		} else if i < len(args) && args[i] != nil {
			_ = session.Store(string(p.Name), args[i])
		}
	}
	if !ft.Return.IsVoid() {
		_ = session.NewVar("__return__", ft.Return)
	}

	// Push CallBoundary
	session.TaskStack = append(session.TaskStack, Task{
		Op: OpCallBoundary,
		Data: map[string]interface{}{
			"oldStack":  old,
			"oldExec":   oldExec,
			"hasReturn": !ft.Return.IsVoid(),
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

func (e *Executor) evalFFIAndPush(session *StackContext, route FFIRoute, args []*Var) error {
	// Let's use the old evalFFI logic
	res, err := e.evalFFI(session, route, args)
	if err != nil {
		return err
	}

	session.ValueStack.Push(res)
	return nil
}

func (e *Executor) initializeType(ctx *StackContext, t ast.GoMiniType, depth int) *Var {
	if depth > 10 {
		return &Var{VType: TypeAny, Type: t}
	}

	// 1. Resolve to the underlying shape for initialization, but keep t as the logical type
	shape := t
	for {
		if actual, ok := e.types[string(shape)]; ok {
			shape = actual
			continue
		}
		break
	}

	if shape.IsPtr() {
		return &Var{VType: TypeHandle, Handle: 0, Type: t}
	}

	if shape.IsInterface() {
		return &Var{VType: TypeInterface, Type: t, Ref: nil}
	}

	if shape.IsArray() || shape.IsMap() || shape.IsAny() {
		return &Var{VType: TypeAny, Type: t}
	}

	// 基础类型初始化
	zero := shape.ZeroVar()
	res := e.ToVar(ctx, zero, nil)
	if res != nil {
		res.Type = t // 还原为用户请求的命名类型
		return res
	}

	// 结构体初始化
	mData := make(map[string]*Var)
	if sDef, ok := e.structs[string(shape)]; ok {
		for _, fName := range sDef.FieldNames {
			fType := sDef.Fields[fName]
			mData[string(fName)] = e.initializeType(ctx, fType, depth+1)
		}
	}
	return &Var{VType: TypeMap, Ref: &VMMap{Data: mData}, Type: t}
}
