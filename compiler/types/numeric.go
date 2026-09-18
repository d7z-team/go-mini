package types

// NumericKind is the semantic numeric category used by operator and constant
// checking. It is intentionally independent of the display form of a type.
type NumericKind uint8

const (
	NumericInvalid NumericKind = iota
	NumericSigned
	NumericUnsigned
	NumericFloat
	NumericComplex
)

type NumericInfo struct {
	Type      TypeRef
	Primitive PrimitiveKind
	Kind      NumericKind
	Bits      int
}

func NumericTypeInfo(table *TypeTable, ref TypeRef) (NumericInfo, bool) {
	if !ref.Valid() {
		return NumericInfo{}, false
	}
	if table != nil {
		ref = table.Underlying(ref)
	}
	if ref.Kind != Primitive {
		return NumericInfo{}, false
	}
	kind, bits, ok := primitiveNumericInfo(ref.Primitive)
	if !ok {
		return NumericInfo{}, false
	}
	return NumericInfo{Type: ref, Primitive: ref.Primitive, Kind: kind, Bits: bits}, true
}

func primitiveNumericInfo(kind PrimitiveKind) (NumericKind, int, bool) {
	switch kind {
	case PrimitiveInt, PrimitiveInt8, PrimitiveInt16, PrimitiveInt32, PrimitiveInt64:
		bits := 64
		switch kind {
		case PrimitiveInt8:
			bits = 8
		case PrimitiveInt16:
			bits = 16
		case PrimitiveInt32:
			bits = 32
		}
		return NumericSigned, bits, true
	case PrimitiveUint, PrimitiveUint8, PrimitiveUint16, PrimitiveUint32, PrimitiveUint64, PrimitiveUintptr:
		bits := 64
		switch kind {
		case PrimitiveUint8:
			bits = 8
		case PrimitiveUint16:
			bits = 16
		case PrimitiveUint32:
			bits = 32
		}
		return NumericUnsigned, bits, true
	case PrimitiveFloat32:
		return NumericFloat, 32, true
	case PrimitiveFloat64:
		return NumericFloat, 64, true
	case PrimitiveComplex64:
		return NumericComplex, 64, true
	case PrimitiveComplex128:
		return NumericComplex, 128, true
	default:
		return NumericInvalid, 0, false
	}
}

func IsNumericType(table *TypeTable, ref TypeRef) bool {
	_, ok := NumericTypeInfo(table, ref)
	return ok
}

func IsSignedIntegerType(table *TypeTable, ref TypeRef) bool {
	info, ok := NumericTypeInfo(table, ref)
	return ok && info.Kind == NumericSigned
}

func IsUnsignedIntegerType(table *TypeTable, ref TypeRef) bool {
	info, ok := NumericTypeInfo(table, ref)
	return ok && info.Kind == NumericUnsigned
}

func IsIntegerType(table *TypeTable, ref TypeRef) bool {
	info, ok := NumericTypeInfo(table, ref)
	return ok && (info.Kind == NumericSigned || info.Kind == NumericUnsigned)
}

func IsFloatType(table *TypeTable, ref TypeRef) bool {
	info, ok := NumericTypeInfo(table, ref)
	return ok && info.Kind == NumericFloat
}

func IsComplexType(table *TypeTable, ref TypeRef) bool {
	info, ok := NumericTypeInfo(table, ref)
	return ok && info.Kind == NumericComplex
}

func IsPrimitiveType(table *TypeTable, ref TypeRef) bool {
	if !ref.Valid() {
		return false
	}
	if table != nil {
		ref = table.Underlying(ref)
	}
	if ref.Kind != Primitive {
		return false
	}
	return ref.Primitive == PrimitiveBool || ref.Primitive == PrimitiveString || IsNumericType(table, ref)
}
