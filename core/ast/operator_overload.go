package ast

import (
	"fmt"

	"gopkg.d7z.net/go-mini/core/typespec"
)

type OperatorResolution struct {
	MethodName Ident      `json:"-"`
	ReturnType GoMiniType `json:"-"`
}

func binaryOperatorMethod(op typespec.BinaryOperator) (Ident, bool) {
	switch op {
	case typespec.OpPlus:
		return "OpAdd", true
	case typespec.OpMinus:
		return "OpSub", true
	case typespec.OpMult:
		return "OpMul", true
	case typespec.OpDiv:
		return "OpDiv", true
	case typespec.OpMod:
		return "OpMod", true
	case typespec.OpBitAnd:
		return "OpBitAnd", true
	case typespec.OpBitOr:
		return "OpBitOr", true
	case typespec.OpBitXor:
		return "OpBitXor", true
	case typespec.OpLsh:
		return "OpLsh", true
	case typespec.OpRsh:
		return "OpRsh", true
	case typespec.OpEq:
		return "OpEq", true
	case typespec.OpNeq:
		return "OpNeq", true
	case typespec.OpLt:
		return "OpLt", true
	case typespec.OpLe:
		return "OpLe", true
	case typespec.OpGt:
		return "OpGt", true
	case typespec.OpGe:
		return "OpGe", true
	default:
		return "", false
	}
}

func unaryOperatorMethod(op Ident) (Ident, bool) {
	switch op {
	case "Sub":
		return "OpNeg", true
	case "Plus":
		return "OpPos", true
	case "Not":
		return "OpNot", true
	case "BitXor":
		return "OpBitNot", true
	default:
		return "", false
	}
}

func (c *ValidContext) ResolveBinaryOperatorMethod(op typespec.BinaryOperator, leftType, rightType GoMiniType) (*OperatorResolution, bool, error) {
	methodName, ok := binaryOperatorMethod(op)
	if !ok {
		return nil, false, nil
	}
	sig, ok, err := c.operatorCallSignature(leftType, methodName)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	if len(sig.Params) != 1 {
		return nil, true, fmt.Errorf("%s method must accept exactly one operand, got %d", methodName, len(sig.Params))
	}
	if !c.IsAssignableTo(rightType, sig.Params[0]) {
		return nil, true, fmt.Errorf("%s method operand type mismatch: expected %s, got %s", methodName, sig.Params[0], rightType)
	}
	if err := validateOperatorReturn(methodName, sig.Returns, op.IsComparison()); err != nil {
		return nil, true, err
	}
	return &OperatorResolution{MethodName: methodName, ReturnType: sig.Returns}, true, nil
}

func (c *ValidContext) ResolveUnaryOperatorMethod(op Ident, operandType GoMiniType) (*OperatorResolution, bool, error) {
	methodName, ok := unaryOperatorMethod(op)
	if !ok {
		return nil, false, nil
	}
	sig, ok, err := c.operatorCallSignature(operandType, methodName)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	if len(sig.Params) != 0 {
		return nil, true, fmt.Errorf("%s method must not accept operands, got %d", methodName, len(sig.Params))
	}
	if err := validateOperatorReturn(methodName, sig.Returns, op == "Not"); err != nil {
		return nil, true, err
	}
	return &OperatorResolution{MethodName: methodName, ReturnType: sig.Returns}, true, nil
}

func (c *ValidContext) operatorCallSignature(receiverType GoMiniType, methodName Ident) (CallFunctionType, bool, error) {
	if interfaceType, ok := c.resolveInterfaceType(receiverType); ok {
		if methods, ok := interfaceType.ReadInterfaceMethods(); ok {
			if method := methods[string(methodName)]; method != nil {
				return method.ToCallFunctionType(), true, nil
			}
		}
	}

	st, ok := c.GetStruct(Ident(receiverType))
	if !ok {
		if base := receiverType.BaseName(); base != "" {
			st, ok = c.GetStruct(Ident(base))
		}
	}
	if !ok {
		return CallFunctionType{}, false, nil
	}
	sig, ok := st.Methods[methodName]
	if !ok {
		return CallFunctionType{}, false, nil
	}
	if len(sig.Params) == 0 {
		return sig, true, nil
	}
	first := sig.Params[0]
	if GoMiniType(first.BaseName()) != GoMiniType(receiverType.BaseName()) {
		return sig, true, nil
	}
	if !c.IsAssignableTo(receiverType, first) {
		return CallFunctionType{}, true, fmt.Errorf("%s method receiver type mismatch: expected %s, got %s", methodName, first, receiverType)
	}
	sig.Params = append([]GoMiniType(nil), sig.Params[1:]...)
	return sig, true, nil
}

func validateOperatorReturn(methodName Ident, ret GoMiniType, mustBool bool) error {
	if ret.IsVoid() || ret.IsTuple() {
		return fmt.Errorf("%s method must return a single non-Void value, got %s", methodName, ret)
	}
	if mustBool && ret != TypeBool {
		return fmt.Errorf("%s method must return Bool, got %s", methodName, ret)
	}
	return nil
}
