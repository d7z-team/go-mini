package ffigo

import (
	"strings"

	"gopkg.d7z.net/go-mini/core/typespec"
)

const (
	// PackageImportPath is the canonical Go import path for the FFI helper package.
	PackageImportPath = "gopkg.d7z.net/go-mini/core/ffigo"

	// BytesRefQualifiedType is the fully-qualified Go type name for BytesRef.
	BytesRefQualifiedType = PackageImportPath + ".BytesRef"
	// ArrayRefQualifiedType is the fully-qualified Go type name for ArrayRef.
	ArrayRefQualifiedType = PackageImportPath + ".ArrayRef"
	// AsyncQualifiedType is the fully-qualified Go type name for Async.
	AsyncQualifiedType = PackageImportPath + ".Async"
	// VoidQualifiedType is the fully-qualified Go type name for Void.
	VoidQualifiedType = PackageImportPath + ".Void"
	// Tuple2QualifiedType is the fully-qualified Go type name for Tuple2.
	Tuple2QualifiedType = PackageImportPath + ".Tuple2"
)

// SplitGenericType splits a VM-style generic type string such as Array<Int64>.
func SplitGenericType(typeName string) (string, []string, bool) {
	start := strings.Index(typeName, "<")
	end := strings.LastIndex(typeName, ">")
	if start <= 0 || end <= start {
		return "", nil, false
	}
	base := strings.TrimSpace(typeName[:start])
	inner := strings.TrimSpace(typeName[start+1 : end])
	if inner == "" {
		return base, nil, true
	}
	return base, typespec.SplitCommaAware(inner), true
}

// RefElementType returns the element type for HostRef<T> and Ptr<T>.
func RefElementType(typeName string) (string, bool) {
	elem, ok := typespec.Type(strings.TrimSpace(typeName)).RefElement()
	if !ok {
		return "", false
	}
	return elem.String(), true
}

// IsRefTypeString reports whether typeName is a VM reference wrapper string.
func IsRefTypeString(typeName string) bool {
	_, ok := RefElementType(typeName)
	return ok
}

// IsGenericTypeBase reports whether a generic type's base is one of bases.
func IsGenericTypeBase(typeName string, bases ...string) bool {
	base, _, ok := SplitGenericType(typeName)
	if !ok {
		return false
	}
	base = strings.TrimSpace(base)
	for _, want := range bases {
		if base == want {
			return true
		}
	}
	return false
}

// IsBytesRefTypeString reports whether typeName names ffigo.BytesRef.
func IsBytesRefTypeString(typeName string) bool {
	typeName = strings.TrimSpace(typeName)
	if inner, ok := RefElementType(typeName); ok {
		typeName = inner
	}
	return typeName == BytesRefQualifiedType || typeName == "ffigo.BytesRef" || typeName == "BytesRef"
}

// IsArrayRefTypeString reports whether typeName names ffigo.ArrayRef.
func IsArrayRefTypeString(typeName string) bool {
	typeName = strings.TrimSpace(typeName)
	if inner, ok := RefElementType(typeName); ok {
		typeName = inner
	}
	if base, _, ok := SplitGenericType(typeName); ok {
		typeName = base
	}
	return typeName == ArrayRefQualifiedType || typeName == "ffigo.ArrayRef" || typeName == "ArrayRef"
}

// AsyncElemTypeString returns the element type for ffigo.Async<T>.
func AsyncElemTypeString(typeName string) (string, bool) {
	if !IsGenericTypeBase(typeName, AsyncQualifiedType, "ffigo.Async", "Async") {
		return "", false
	}
	_, args, _ := SplitGenericType(typeName)
	if len(args) != 1 {
		return "", false
	}
	return args[0], true
}

// Tuple2ElemTypeStrings returns the two element types for ffigo.Tuple2[A, B].
func Tuple2ElemTypeStrings(typeName string) ([]string, bool) {
	if !IsGenericTypeBase(typeName, Tuple2QualifiedType, "ffigo.Tuple2", "Tuple2") {
		return nil, false
	}
	_, args, _ := SplitGenericType(typeName)
	if len(args) != 2 {
		return nil, false
	}
	return args, true
}

// ReadArrayItemType returns the element type for Array<T>.
func ReadArrayItemType(typeName string) (string, bool) {
	elem, ok := typespec.Type(strings.TrimSpace(typeName)).ReadArrayItemType()
	if !ok {
		return "", false
	}
	return elem.String(), true
}

// ReadMapKeyValueTypes returns the key and value type strings for Map<K, V>.
func ReadMapKeyValueTypes(typeName string) (string, string, bool) {
	key, value, ok := typespec.Type(strings.TrimSpace(typeName)).MapTypes()
	if !ok {
		return "", "", false
	}
	return key.String(), value.String(), true
}

// ReadChanElemType returns the element type for Chan<T>, RecvChan<T>, or SendChan<T>.
func ReadChanElemType(typeName string) (string, bool) {
	elem, ok := typespec.Type(strings.TrimSpace(typeName)).ChanElement()
	if !ok {
		return "", false
	}
	return elem.String(), true
}
