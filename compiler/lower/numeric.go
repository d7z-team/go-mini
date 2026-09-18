package lower

import "github.com/d7z-team/mini-go/compiler/types"

// numericTypeInfo classifies canonical primitive text at constant and HIR
// boundaries. Named and composite source types use the TypeTable-aware method.
func numericTypeInfo(text string) (types.NumericInfo, bool) {
	primitive, ok := types.PrimitiveByName(text)
	if !ok {
		return types.NumericInfo{}, false
	}
	return types.NumericTypeInfo(nil, types.Builtin(primitive))
}

func (l *lowerer) numericTypeInfo(text string) (types.NumericInfo, bool) {
	view, ok := l.typeView(text)
	if !ok {
		return types.NumericInfo{}, false
	}
	return view.NumericInfo()
}

func (l *lowerer) isSignedIntegerType(text string) bool {
	info, ok := l.numericTypeInfo(text)
	return ok && info.Kind == types.NumericSigned
}

func (l *lowerer) isUnsignedIntegerType(text string) bool {
	info, ok := l.numericTypeInfo(text)
	return ok && info.Kind == types.NumericUnsigned
}

func (l *lowerer) isIntegerType(text string) bool {
	info, ok := l.numericTypeInfo(text)
	return ok && (info.Kind == types.NumericSigned || info.Kind == types.NumericUnsigned)
}

func (l *lowerer) isFloatType(text string) bool {
	info, ok := l.numericTypeInfo(text)
	return ok && info.Kind == types.NumericFloat
}

func (l *lowerer) isComplexType(text string) bool {
	info, ok := l.numericTypeInfo(text)
	return ok && info.Kind == types.NumericComplex
}

func isNumericType(text string) bool {
	_, ok := numericTypeInfo(text)
	return ok
}

func isSignedIntegerType(text string) bool {
	info, ok := numericTypeInfo(text)
	return ok && info.Kind == types.NumericSigned
}

func isUnsignedIntegerType(text string) bool {
	info, ok := numericTypeInfo(text)
	return ok && info.Kind == types.NumericUnsigned
}

func isIntegerType(text string) bool {
	info, ok := numericTypeInfo(text)
	return ok && (info.Kind == types.NumericSigned || info.Kind == types.NumericUnsigned)
}

func isFloatType(text string) bool {
	info, ok := numericTypeInfo(text)
	return ok && info.Kind == types.NumericFloat
}

func isComplexType(text string) bool {
	info, ok := numericTypeInfo(text)
	return ok && info.Kind == types.NumericComplex
}
