package typespec

import "fmt"

type BinaryOperator string

const (
	OpPlus   BinaryOperator = "Plus"
	OpMinus  BinaryOperator = "Minus"
	OpMult   BinaryOperator = "Mult"
	OpDiv    BinaryOperator = "Div"
	OpMod    BinaryOperator = "Mod"
	OpEq     BinaryOperator = "Eq"
	OpNeq    BinaryOperator = "Neq"
	OpLt     BinaryOperator = "Lt"
	OpGt     BinaryOperator = "Gt"
	OpLe     BinaryOperator = "Le"
	OpGe     BinaryOperator = "Ge"
	OpAnd    BinaryOperator = "And"
	OpOr     BinaryOperator = "Or"
	OpBitAnd BinaryOperator = "BitAnd"
	OpBitOr  BinaryOperator = "BitOr"
	OpBitXor BinaryOperator = "BitXor"
	OpLsh    BinaryOperator = "Lsh"
	OpRsh    BinaryOperator = "Rsh"
)

func NormalizeBinaryOperator(op string) (BinaryOperator, bool) {
	switch op {
	case "+", "Plus", "Add":
		return OpPlus, true
	case "-", "Minus", "Sub":
		return OpMinus, true
	case "*", "Mult":
		return OpMult, true
	case "/", "Div":
		return OpDiv, true
	case "%", "Mod":
		return OpMod, true
	case "==", "Eq":
		return OpEq, true
	case "!=", "Neq":
		return OpNeq, true
	case "<", "Lt":
		return OpLt, true
	case ">", "Gt":
		return OpGt, true
	case "<=", "Le":
		return OpLe, true
	case ">=", "Ge":
		return OpGe, true
	case "&&", "And":
		return OpAnd, true
	case "||", "Or":
		return OpOr, true
	case "&", "BitAnd":
		return OpBitAnd, true
	case "|", "BitOr":
		return OpBitOr, true
	case "^", "BitXor":
		return OpBitXor, true
	case "<<", "Lsh":
		return OpLsh, true
	case ">>", "Rsh":
		return OpRsh, true
	}
	return "", false
}

func (op BinaryOperator) IsEquality() bool {
	return op == OpEq || op == OpNeq
}

func (op BinaryOperator) IsComparison() bool {
	return op.IsEquality() || op == OpLt || op == OpGt || op == OpLe || op == OpGe
}

func IsNilType(t Type) bool {
	return t == "nil"
}

func IsDynamicAny(t Type) bool {
	return t == Any
}

func BinaryResultType(op BinaryOperator, left, right Type) (Type, error) {
	switch op {
	case OpAnd, OpOr:
		if IsDynamicAny(left) || IsDynamicAny(right) {
			return Bool, nil
		}
		if left == Bool && right == Bool {
			return Bool, nil
		}
		return "", fmt.Errorf("%s operator expects Bool, got %s and %s", op, left, right)
	case OpPlus:
		if IsDynamicAny(left) || IsDynamicAny(right) {
			if IsNilType(left) || IsNilType(right) {
				return "", fmt.Errorf("%s operator does not support nil", op)
			}
			return Any, nil
		}
		if left.IsNumeric() && right.IsNumeric() {
			if left == Float64 || right == Float64 {
				return Float64, nil
			}
			return Int64, nil
		}
		if left == String && right == String {
			return String, nil
		}
		if left == Bytes && right == Bytes {
			return Bytes, nil
		}
		return "", fmt.Errorf("%s operator does not support %s and %s", op, left, right)
	case OpMinus, OpMult, OpDiv:
		if IsDynamicAny(left) || IsDynamicAny(right) {
			if IsNilType(left) || IsNilType(right) {
				return "", fmt.Errorf("%s operator does not support nil", op)
			}
			return Any, nil
		}
		if left.IsNumeric() && right.IsNumeric() {
			if left == Float64 || right == Float64 {
				return Float64, nil
			}
			return Int64, nil
		}
		return "", fmt.Errorf("%s operator expects numeric operands, got %s and %s", op, left, right)
	case OpMod:
		if IsDynamicAny(left) || IsDynamicAny(right) {
			if IsNilType(left) || IsNilType(right) {
				return "", fmt.Errorf("%s operator does not support nil", op)
			}
			return Any, nil
		}
		if left == Int64 && right == Int64 {
			return Int64, nil
		}
		return "", fmt.Errorf("%s operator expects Int64 operands, got %s and %s", op, left, right)
	case OpBitAnd, OpBitOr, OpBitXor, OpLsh, OpRsh:
		if IsDynamicAny(left) || IsDynamicAny(right) {
			if IsNilType(left) || IsNilType(right) {
				return "", fmt.Errorf("%s operator does not support nil", op)
			}
			return Any, nil
		}
		if left == Int64 && right == Int64 {
			return Int64, nil
		}
		return "", fmt.Errorf("%s operator expects Int64 operands, got %s and %s", op, left, right)
	case OpEq, OpNeq:
		if EqualityComparable(left, right) {
			return Bool, nil
		}
		return "", fmt.Errorf("%s operator cannot compare %s and %s", op, left, right)
	case OpLt, OpGt, OpLe, OpGe:
		if OrderedComparable(left, right) {
			return Bool, nil
		}
		return "", fmt.Errorf("%s operator cannot order %s and %s", op, left, right)
	}
	return "", fmt.Errorf("unsupported operator %s", op)
}

func EqualityComparable(left, right Type) bool {
	if IsDynamicAny(left) || IsDynamicAny(right) {
		return true
	}
	if IsNilType(left) || IsNilType(right) {
		if IsNilType(left) && IsNilType(right) {
			return true
		}
		if IsNilType(left) {
			return IsNilComparable(right)
		}
		return IsNilComparable(left)
	}
	if left.IsNumeric() && right.IsNumeric() {
		return true
	}
	if left == right {
		return left == Bool || left == String || left == Error ||
			left.IsPtr() || left.IsHostRef() || left.IsChan() ||
			left.IsArray() || left.IsMap() || left.IsModule() ||
			left.IsClosure() || left.IsFunction() || left.IsInterface()
	}
	return false
}

func OrderedComparable(left, right Type) bool {
	if IsDynamicAny(left) || IsDynamicAny(right) {
		return true
	}
	if left.IsNumeric() && right.IsNumeric() {
		return true
	}
	return left == String && right == String
}

func IsNilComparable(t Type) bool {
	if IsDynamicAny(t) || t.IsPtr() || t.IsHostRef() || t.IsChan() || t.IsArray() ||
		t.IsMap() || t.IsInterface() || t.IsFunction() || t.IsModule() || t.IsClosure() {
		return true
	}
	return t == Bytes || t == Error
}
