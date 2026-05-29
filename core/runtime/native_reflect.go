package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/reflectspec"
	"gopkg.d7z.net/go-mini/core/typespec"
)

const (
	ReflectKindInvalid int64 = iota
	ReflectKindBool
	ReflectKindInt64
	ReflectKindFloat64
	ReflectKindString
	ReflectKindBytes
	ReflectKindArray
	ReflectKindMap
	ReflectKindStruct
	ReflectKindPtr
	ReflectKindHostRef
	ReflectKindChan
	ReflectKindFunc
	ReflectKindInterface
	ReflectKindError
	ReflectKindModule
	ReflectKindAny
)

const (
	reflectTypeSpec        TypeSpec = reflectspec.TypeType
	reflectStructFieldSpec TypeSpec = reflectspec.StructFieldType
	reflectMethodSpec      TypeSpec = reflectspec.MethodType
	reflectRouteSpec       TypeSpec = reflectspec.RouteType
	reflectPackageInfoSpec TypeSpec = reflectspec.PackageInfoType
	reflectMemberSpec      TypeSpec = reflectspec.MemberType
)

type reflectMethodInfo struct {
	Name  string
	Sig   *RuntimeFuncSig
	Route FFIRoute
}

// NativeReflect exposes VM/runtime schema metadata. It never calls Go's native reflect package.
func NativeReflect(e *Executor, session *StackContext, route FFIRoute, args []*Var, _ []LHSValue) (*Var, error) {
	if e == nil {
		return nil, errors.New("reflect requires an executor")
	}
	switch route.Name {
	case reflectspec.RouteTypeOf:
		return e.reflectTypeVar(e.reflectTypeSpecOfVar(reflectArg(args, 0))), nil
	case reflectspec.RouteTypeFrom:
		raw := TypeSpec(reflectStringArg(args, 0))
		_, ok := e.reflectRuntimeType(raw, true)
		if !ok {
			raw = ""
		}
		return e.reflectTuple([]TypeSpec{reflectTypeSpec, SpecBool}, e.reflectTypeVar(raw), NewBool(ok)), nil
	case reflectspec.RouteKindOf:
		return NewInt(e.reflectKindOfVar(reflectArg(args, 0))), nil
	case reflectspec.RouteKindOfType, reflectspec.RouteTypeKind:
		return NewInt(e.reflectKindOfType(e.reflectTypeRaw(reflectArg(args, 0)))), nil
	case reflectspec.RouteFields:
		return e.reflectFieldsArray(e.reflectTypeSpecOfVar(reflectArg(args, 0))), nil
	case reflectspec.RouteFieldsOfType:
		return e.reflectFieldsArray(e.reflectTypeRaw(reflectArg(args, 0))), nil
	case reflectspec.RouteField:
		value, declared, ok := e.reflectFieldValue(reflectArg(args, 0), reflectStringArg(args, 1))
		if !ok {
			return e.reflectTuple([]TypeSpec{SpecAny, SpecBool}, e.wrapAnyVar(nil), NewBool(false)), nil
		}
		anyValue, ok := e.reflectAnyValue(value, declared)
		return e.reflectTuple([]TypeSpec{SpecAny, SpecBool}, anyValue, NewBool(ok)), nil
	case reflectspec.RouteSetField:
		return e.reflectSetField(session, reflectArg(args, 0), reflectStringArg(args, 1), reflectArg(args, 2)), nil
	case reflectspec.RouteZero:
		return e.reflectZeroValue(session, reflectStringArg(args, 0))
	case reflectspec.RouteMethods:
		return e.reflectMethodsArray(e.reflectTypeSpecOfVar(reflectArg(args, 0))), nil
	case reflectspec.RouteMethodsOfType:
		return e.reflectMethodsArray(e.reflectTypeRaw(reflectArg(args, 0))), nil
	case reflectspec.RouteIsNil:
		return NewBool(isNilValue(reflectArg(args, 0))), nil
	case reflectspec.RouteIsStruct:
		v := e.unwrapValue(reflectArg(args, 0))
		return NewBool(v != nil && v.VType == TypeStruct), nil
	case reflectspec.RouteIsPtr:
		v := e.unwrapValue(reflectArg(args, 0))
		return NewBool(v != nil && v.VType == TypePointer), nil
	case reflectspec.RouteIsHostRef:
		v := e.unwrapValue(reflectArg(args, 0))
		return NewBool(v != nil && v.VType == TypeHostRef), nil
	case reflectspec.RouteIsChan:
		v := e.unwrapValue(reflectArg(args, 0))
		return NewBool(v != nil && v.VType == TypeChannel), nil
	case reflectspec.RouteIsFunc:
		return NewBool(e.reflectIsFunc(reflectArg(args, 0))), nil
	case reflectspec.RouteIsFFIFunc:
		route, ok := e.reflectRouteFromValue(reflectArg(args, 0))
		return NewBool(ok && route.Bridge != nil && route.Native == nil), nil
	case reflectspec.RouteIsVMFunc:
		return NewBool(e.reflectIsVMFunc(reflectArg(args, 0))), nil
	case reflectspec.RouteIsNativeFunc:
		route, ok := e.reflectRouteFromValue(reflectArg(args, 0))
		return NewBool(ok && route.Native != nil), nil
	case reflectspec.RoutePackage:
		path := reflectStringArg(args, 0)
		_, ok := e.reflectLookupPackage(path)
		if !ok {
			path = ""
		}
		return e.reflectTuple([]TypeSpec{reflectPackageInfoSpec, SpecBool}, e.reflectPackageVar(path), NewBool(ok)), nil
	case reflectspec.RoutePackages:
		return e.reflectPackagesArray(), nil
	case reflectspec.RouteMembers:
		return e.reflectMembersArray(e.reflectPackagePath(reflectArg(args, 0))), nil
	case reflectspec.RouteMemberByName:
		member, ok := e.reflectMemberByName(e.reflectPackagePath(reflectArg(args, 0)), reflectStringArg(args, 1))
		return e.reflectTuple([]TypeSpec{reflectMemberSpec, SpecBool}, member, NewBool(ok)), nil
	case reflectspec.RouteLen:
		return NewInt(e.reflectLen(reflectArg(args, 0))), nil
	case reflectspec.RouteIndex:
		value, declared, ok := e.reflectIndexValue(reflectArg(args, 0), reflectIntArg(args))
		return e.reflectAnyTuple(value, declared, ok), nil
	case reflectspec.RouteMapKeys:
		keys, ok := e.reflectMapKeys(reflectArg(args, 0))
		return e.reflectTuple([]TypeSpec{ArrayType(SpecAny), SpecBool}, keys, NewBool(ok)), nil
	case reflectspec.RouteMapIndex:
		value, declared, ok := e.reflectMapIndexValue(reflectArg(args, 0), reflectArg(args, 1))
		return e.reflectAnyTuple(value, declared, ok), nil
	case reflectspec.RouteMakeMap:
		value, ok := e.reflectMakeMap(reflectStringArg(args, 0))
		return e.reflectAnyTuple(value, RuntimeType{}, ok), nil
	case reflectspec.RouteSetMapIndex:
		return e.reflectSetMapIndex(session, reflectArg(args, 0), reflectArg(args, 1), reflectArg(args, 2)), nil
	case reflectspec.RouteUnwrap:
		value, ok := e.reflectUnwrapValue(reflectArg(args, 0))
		return e.reflectAnyTuple(value, RuntimeType{}, ok), nil
	case reflectspec.RouteTypeString:
		return NewString(e.reflectTypeRaw(reflectArg(args, 0)).String()), nil
	case reflectspec.RouteTypeName:
		name, _ := reflectSplitTypeName(e.reflectTypeRaw(reflectArg(args, 0)))
		return NewString(name), nil
	case reflectspec.RouteTypePkgPath:
		_, pkgPath := reflectSplitTypeName(e.reflectTypeRaw(reflectArg(args, 0)))
		return NewString(pkgPath), nil
	case reflectspec.RouteTypeElem:
		if elem, ok := e.reflectTypeElem(e.reflectTypeRaw(reflectArg(args, 0))); ok {
			return e.reflectTypeVar(elem), nil
		}
		return e.reflectTypeVar(""), nil
	case reflectspec.RouteTypeKey:
		if key, ok := e.reflectTypeKey(e.reflectTypeRaw(reflectArg(args, 0))); ok {
			return e.reflectTypeVar(key), nil
		}
		return e.reflectTypeVar(""), nil
	case reflectspec.RouteTypeAssignableTo:
		left, leftOK := e.reflectResolvedRuntimeType(e.reflectTypeRaw(reflectArg(args, 0)), false)
		right, rightOK := e.reflectResolvedRuntimeType(e.reflectTypeRaw(reflectArg(args, 1)), false)
		return NewBool(leftOK && rightOK && left.IsAssignableTo(right)), nil
	case reflectspec.RouteTypeComparable:
		typ, ok := e.reflectResolvedRuntimeType(e.reflectTypeRaw(reflectArg(args, 0)), false)
		return NewBool(ok && isEqualityComparableRuntimeType(typ)), nil
	case reflectspec.RouteTypeNumField:
		return NewInt(int64(len(e.reflectFields(e.reflectTypeRaw(reflectArg(args, 0)))))), nil
	case reflectspec.RouteTypeField:
		fields := e.reflectFields(e.reflectTypeRaw(reflectArg(args, 0)))
		idx := int(reflectIntArg(args))
		if idx < 0 || idx >= len(fields) {
			return e.reflectStructFieldVar(RuntimeStructField{}, -1), nil
		}
		return e.reflectStructFieldVar(fields[idx], idx), nil
	case reflectspec.RouteTypeFieldByName:
		field, idx, ok := e.reflectFieldByName(e.reflectTypeRaw(reflectArg(args, 0)), reflectStringArg(args, 1))
		return e.reflectTuple([]TypeSpec{reflectStructFieldSpec, SpecBool}, e.reflectStructFieldVar(field, idx), NewBool(ok)), nil
	case reflectspec.RouteTypeNumMethod:
		return NewInt(int64(len(e.reflectMethods(e.reflectTypeRaw(reflectArg(args, 0)))))), nil
	case reflectspec.RouteTypeMethod:
		methods := e.reflectMethods(e.reflectTypeRaw(reflectArg(args, 0)))
		idx := int(reflectIntArg(args))
		if idx < 0 || idx >= len(methods) {
			return e.reflectMethodVar(reflectMethodInfo{}, -1), nil
		}
		return e.reflectMethodVar(methods[idx], idx), nil
	case reflectspec.RouteTypeMethodByName:
		method, idx, ok := e.reflectMethodByName(e.reflectTypeRaw(reflectArg(args, 0)), reflectStringArg(args, 1))
		return e.reflectTuple([]TypeSpec{reflectMethodSpec, SpecBool}, e.reflectMethodVar(method, idx), NewBool(ok)), nil
	case reflectspec.RouteTypeNumIn:
		sig := e.reflectFuncSig(e.reflectTypeRaw(reflectArg(args, 0)))
		if sig == nil {
			return NewInt(0), nil
		}
		return NewInt(int64(len(sig.ParamTypes))), nil
	case reflectspec.RouteTypeIn:
		sig := e.reflectFuncSig(e.reflectTypeRaw(reflectArg(args, 0)))
		idx := int(reflectIntArg(args))
		if sig == nil || idx < 0 || idx >= len(sig.ParamTypes) {
			return e.reflectTypeVar(""), nil
		}
		return e.reflectTypeVar(sig.ParamTypes[idx].Raw), nil
	case reflectspec.RouteTypeNumOut:
		sig := e.reflectFuncSig(e.reflectTypeRaw(reflectArg(args, 0)))
		if sig == nil || sig.ReturnType.IsVoid() {
			return NewInt(0), nil
		}
		if sig.ReturnType.Kind == RuntimeTypeTuple {
			return NewInt(int64(len(sig.ReturnType.Params))), nil
		}
		return NewInt(1), nil
	case reflectspec.RouteTypeOut:
		sig := e.reflectFuncSig(e.reflectTypeRaw(reflectArg(args, 0)))
		idx := int(reflectIntArg(args))
		if sig == nil || idx < 0 || sig.ReturnType.IsVoid() {
			return e.reflectTypeVar(""), nil
		}
		if sig.ReturnType.Kind == RuntimeTypeTuple {
			if idx >= len(sig.ReturnType.Params) {
				return e.reflectTypeVar(""), nil
			}
			return e.reflectTypeVar(sig.ReturnType.Params[idx].Raw), nil
		}
		if idx == 0 {
			return e.reflectTypeVar(sig.ReturnType.Raw), nil
		}
		return e.reflectTypeVar(""), nil
	case reflectspec.RouteTypeIsVariadic:
		sig := e.reflectFuncSig(e.reflectTypeRaw(reflectArg(args, 0)))
		return NewBool(sig != nil && sig.Variadic), nil
	default:
		return nil, fmt.Errorf("unknown native reflect route %s", route.Name)
	}
}

func reflectArg(args []*Var, idx int) *Var {
	if idx < 0 || idx >= len(args) {
		return nil
	}
	return args[idx]
}

func reflectStringArg(args []*Var, idx int) string {
	v := reflectArg(args, idx)
	if v == nil {
		return ""
	}
	if v.VType == TypeAny {
		if inner, ok := v.Ref.(*Var); ok {
			v = inner
		}
	}
	if v != nil && v.VType == TypeString {
		return v.Str
	}
	return ""
}

func reflectIntArg(args []*Var) int64 {
	v := reflectArg(args, 1)
	if v == nil {
		return 0
	}
	if v.VType == TypeAny {
		if inner, ok := v.Ref.(*Var); ok {
			v = inner
		}
	}
	if v != nil && v.VType == TypeInt {
		return v.I64
	}
	return 0
}

func (e *Executor) reflectTypeVar(raw TypeSpec) *Var {
	return e.reflectStructValue(reflectTypeSpec, map[string]*Var{
		"Raw": NewString(raw.String()),
	})
}

func (e *Executor) reflectStructValue(typeName TypeSpec, values map[string]*Var) *Var {
	spec, ok := e.resolveStructSchema(typeName)
	if !ok || spec == nil {
		return NewVarWithRuntimeType(MustParseRuntimeType(SpecAny), TypeAny)
	}
	fields := make([]*Slot, len(spec.Fields))
	byName := make(map[string]int, len(spec.Fields))
	for i, field := range spec.Fields {
		val := values[field.Name]
		if val == nil {
			val = e.reflectZeroField(field.Type)
		}
		fields[i] = NewSlot(field.TypeInfo, val)
		byName[field.Name] = i
	}
	typ := spec.TypeInfo
	if spec.Name != "" {
		typ.Raw = TypeSpec(spec.Name)
		typ.TypeID = spec.TypeID
	}
	res := &Var{VType: TypeStruct, Ref: &VMStruct{Spec: spec, Fields: fields, ByName: byName}}
	res.SetRuntimeType(typ)
	return res
}

func (e *Executor) reflectZeroField(raw TypeSpec) *Var {
	switch raw {
	case reflectTypeSpec:
		return e.reflectTypeVar("")
	case reflectRouteSpec:
		return e.reflectRouteVar(FFIRoute{})
	case reflectPackageInfoSpec:
		return e.reflectPackageVar("")
	case reflectMethodSpec:
		return e.reflectMethodVar(reflectMethodInfo{}, -1)
	case reflectStructFieldSpec:
		return e.reflectStructFieldVar(RuntimeStructField{}, -1)
	}
	typ, err := ParseRuntimeType(raw)
	if err != nil {
		return NewVarWithRuntimeType(MustParseRuntimeType(SpecAny), TypeAny)
	}
	return zeroVarForRuntimeType(typ)
}

func (e *Executor) reflectTypeRaw(v *Var) TypeSpec {
	v = e.unwrapValue(v)
	if v == nil || v.VType != TypeStruct {
		return ""
	}
	st, _ := v.Ref.(*VMStruct)
	if st == nil {
		return ""
	}
	slot, ok := st.Field("Raw")
	if !ok || slot == nil {
		return ""
	}
	raw := e.unwrapValue(slot.Value)
	if raw == nil || raw.VType != TypeString {
		return ""
	}
	return TypeSpec(raw.Str)
}

func (e *Executor) reflectTypeElem(raw TypeSpec) (TypeSpec, bool) {
	typ, ok := e.reflectResolvedRuntimeType(raw, false)
	if !ok {
		return "", false
	}
	if typ.Elem != nil {
		return typ.Elem.Raw, true
	}
	if typ.Value != nil {
		return typ.Value.Raw, true
	}
	return typ.Raw.Element()
}

func (e *Executor) reflectTypeKey(raw TypeSpec) (TypeSpec, bool) {
	typ, ok := e.reflectResolvedRuntimeType(raw, false)
	if !ok {
		return "", false
	}
	if typ.Key != nil {
		return typ.Key.Raw, true
	}
	key, _, ok := typ.Raw.MapTypes()
	return key, ok
}

func (e *Executor) reflectPackagePath(v *Var) string {
	v = e.unwrapValue(v)
	if v == nil || v.VType != TypeStruct {
		return ""
	}
	st, _ := v.Ref.(*VMStruct)
	if st == nil {
		return ""
	}
	slot, ok := st.Field("Path")
	if !ok || slot == nil {
		return ""
	}
	path := e.unwrapValue(slot.Value)
	if path == nil || path.VType != TypeString {
		return ""
	}
	return path.Str
}

func (e *Executor) reflectTypeSpecOfVar(v *Var) TypeSpec {
	if v == nil {
		return ""
	}
	if v.VType == TypeAny {
		switch ref := v.Ref.(type) {
		case *Var:
			return e.reflectTypeSpecOfVar(ref)
		case FFIRoute:
			if ref.FuncSig != nil {
				return ref.FuncSig.Spec
			}
			return SpecClosure
		case *RuntimeStructSpec:
			return TypeSpec(ref.Name)
		case *RuntimeInterfaceSpec:
			return ref.Spec
		}
	}
	v = e.unwrapValue(v)
	if v == nil {
		return ""
	}
	switch v.VType {
	case TypeStruct:
		if st, ok := v.Ref.(*VMStruct); ok && st != nil && st.Spec != nil && st.Spec.Name != "" {
			return TypeSpec(st.Spec.Name)
		}
	case TypeClosure:
		switch ref := v.Ref.(type) {
		case *VMClosure:
			if ref != nil && ref.FunctionSig != nil {
				return ref.FunctionSig.Spec
			}
		case *VMMethodValue:
			if ref != nil {
				if ref.FuncSig != nil {
					return ref.FuncSig.Spec
				}
				if route, ok := e.routes[ref.Method]; ok && route.FuncSig != nil {
					return route.FuncSig.Spec
				}
			}
		}
	}
	if !v.RuntimeType().Raw.IsEmpty() {
		return v.RuntimeType().Raw
	}
	switch v.VType {
	case TypeInt:
		return SpecInt64
	case TypeFloat:
		return SpecFloat64
	case TypeString:
		return SpecString
	case TypeBytes:
		return SpecBytes
	case TypeBool:
		return SpecBool
	case TypeError:
		return SpecError
	case TypeModule:
		return SpecModule
	case TypeClosure:
		return SpecClosure
	}
	return SpecAny
}

func (e *Executor) reflectRuntimeType(raw TypeSpec, requireKnown bool) (RuntimeType, bool) {
	raw = TypeSpec(strings.TrimSpace(raw.String()))
	if raw.IsEmpty() {
		return RuntimeType{}, false
	}
	if spec, ok := e.resolveStructSchema(raw); ok && spec != nil {
		if requireKnown && !e.reflectNamedLeavesKnown(raw) {
			return RuntimeType{}, false
		}
		typ := spec.TypeInfo
		if spec.Name != "" {
			typ.Raw = TypeSpec(spec.Name)
			typ.TypeID = spec.TypeID
		}
		return typ, true
	}
	if spec, ok := e.resolveInterfaceSpec(raw); ok && spec != nil {
		if requireKnown && !e.reflectNamedLeavesKnown(raw) {
			return RuntimeType{}, false
		}
		typ := spec.TypeInfo
		if !raw.IsInterface() {
			typ.Raw = raw
			typ.TypeID = CanonicalTypeID(raw.String())
		}
		return typ, true
	}
	if typ, ok := e.resolveNamedType(raw); ok {
		if requireKnown && !e.reflectNamedLeavesKnown(typ.Raw) {
			return RuntimeType{}, false
		}
		return typ, true
	}
	typ, err := ParseRuntimeType(raw)
	if err != nil {
		return RuntimeType{}, false
	}
	if requireKnown && !e.reflectNamedLeavesKnown(raw) {
		return RuntimeType{}, false
	}
	return typ, true
}

func (e *Executor) reflectResolvedRuntimeType(raw TypeSpec, requireKnown bool) (RuntimeType, bool) {
	raw = TypeSpec(strings.TrimSpace(raw.String()))
	if raw.IsEmpty() {
		return RuntimeType{}, false
	}
	typ, ok := e.reflectRuntimeType(raw, requireKnown)
	if !ok {
		return RuntimeType{}, false
	}
	resolved, chainOK, err := e.resolveNamedTypeChain(raw)
	if err != nil || !chainOK || resolved.Raw.IsEmpty() || resolved.Raw.Equals(raw) {
		return typ, true
	}
	if final, finalOK := e.reflectRuntimeType(resolved.Raw, requireKnown); finalOK {
		return final, true
	}
	if requireKnown {
		return RuntimeType{}, false
	}
	return resolved, true
}

func (e *Executor) reflectResolvedValueType(v *Var) RuntimeType {
	if v == nil {
		return RuntimeType{}
	}
	typ := runtimeTypeForAssignment(v)
	if typ.Raw.IsEmpty() {
		return typ
	}
	if resolved, ok := e.reflectResolvedRuntimeType(typ.Raw, false); ok {
		return resolved
	}
	return typ
}

func (e *Executor) reflectNamedLeavesKnown(raw TypeSpec) bool {
	known := true
	typespec.WalkNamedTypes(raw, func(named typespec.Type) {
		if !known {
			return
		}
		if _, ok := e.resolveNamedType(named); ok {
			return
		}
		if _, ok := e.resolveStructSchema(named); ok {
			return
		}
		if _, ok := e.resolveInterfaceSpec(named); ok {
			return
		}
		known = false
	})
	return known
}

func (e *Executor) reflectKindOfVar(v *Var) int64 {
	if v == nil {
		return ReflectKindInvalid
	}
	if v.VType == TypeAny {
		if _, ok := v.Ref.(FFIRoute); ok {
			return ReflectKindFunc
		}
	}
	v = e.unwrapValue(v)
	if v == nil {
		return ReflectKindInvalid
	}
	switch v.VType {
	case TypeBool:
		return ReflectKindBool
	case TypeInt:
		return ReflectKindInt64
	case TypeFloat:
		return ReflectKindFloat64
	case TypeString:
		return ReflectKindString
	case TypeBytes:
		return ReflectKindBytes
	case TypeArray:
		return ReflectKindArray
	case TypeMap:
		return ReflectKindMap
	case TypeStruct:
		return ReflectKindStruct
	case TypePointer:
		return ReflectKindPtr
	case TypeHostRef:
		return ReflectKindHostRef
	case TypeChannel:
		return ReflectKindChan
	case TypeClosure:
		return ReflectKindFunc
	case TypeInterface:
		return ReflectKindInterface
	case TypeError:
		return ReflectKindError
	case TypeModule:
		return ReflectKindModule
	case TypeAny:
		return ReflectKindAny
	default:
		return ReflectKindInvalid
	}
}

func (e *Executor) reflectKindOfType(raw TypeSpec) int64 {
	if raw.IsEmpty() {
		return ReflectKindInvalid
	}
	if spec, ok := e.resolveStructSchema(raw); ok && spec != nil {
		return ReflectKindStruct
	}
	if spec, ok := e.resolveInterfaceSpec(raw); ok && spec != nil {
		return ReflectKindInterface
	}
	typ, ok := e.reflectResolvedRuntimeType(raw, false)
	if !ok {
		return ReflectKindInvalid
	}
	return reflectKindForRuntimeType(typ)
}

func reflectKindForRuntimeType(typ RuntimeType) int64 {
	if typ.IsEmpty() || typ.IsVoid() {
		return ReflectKindInvalid
	}
	switch {
	case typ.IsAny():
		return ReflectKindAny
	case typ.IsBool():
		return ReflectKindBool
	case typ.IsInt():
		return ReflectKindInt64
	case typ.Raw == SpecFloat64:
		return ReflectKindFloat64
	case typ.IsString():
		return ReflectKindString
	case typ.Raw == SpecBytes:
		return ReflectKindBytes
	case typ.IsArray():
		return ReflectKindArray
	case typ.IsMap():
		return ReflectKindMap
	case typ.IsPtr():
		return ReflectKindPtr
	case typ.IsHostRef():
		return ReflectKindHostRef
	case typ.IsChan():
		return ReflectKindChan
	case typ.Kind == RuntimeTypeFunction || typ.Raw == SpecClosure:
		return ReflectKindFunc
	case typ.Kind == RuntimeTypeStruct:
		return ReflectKindStruct
	case typ.IsInterface():
		return ReflectKindInterface
	case typ.Raw == SpecError:
		return ReflectKindError
	case typ.Raw == SpecModule:
		return ReflectKindModule
	default:
		return ReflectKindInvalid
	}
}

func (e *Executor) reflectFields(raw TypeSpec) []RuntimeStructField {
	spec, ok := e.reflectStructSchema(raw)
	if !ok || spec == nil {
		return nil
	}
	return append([]RuntimeStructField(nil), spec.Fields...)
}

func (e *Executor) reflectStructSchema(raw TypeSpec) (*RuntimeStructSpec, bool) {
	if elem, ok := raw.PtrElement(); ok {
		raw = elem
	}
	if spec, ok := e.resolveStructSchema(raw); ok && spec != nil {
		return spec, true
	}
	typ, err := ParseRuntimeType(raw)
	if err != nil || typ.Kind != RuntimeTypeStruct {
		return nil, false
	}
	fields := append([]RuntimeStructField(nil), typ.Fields...)
	byName := make(map[string]RuntimeStructField, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	return &RuntimeStructSpec{Spec: raw, TypeInfo: typ, Fields: fields, ByName: byName}, true
}

func (e *Executor) reflectFieldsArray(raw TypeSpec) *Var {
	fields := e.reflectFields(raw)
	items := make([]*Var, len(fields))
	for i, field := range fields {
		items[i] = e.reflectStructFieldVar(field, i)
	}
	return e.reflectArray(reflectStructFieldSpec, items)
}

func (e *Executor) reflectFieldByName(raw TypeSpec, name string) (RuntimeStructField, int, bool) {
	fields := e.reflectFields(raw)
	for i, field := range fields {
		if field.Name == name {
			return field, i, true
		}
	}
	return RuntimeStructField{}, -1, false
}

func (e *Executor) reflectStructFieldVar(field RuntimeStructField, index int) *Var {
	exported := false
	if field.Name != "" {
		ch := field.Name[0]
		exported = ch >= 'A' && ch <= 'Z'
	}
	return e.reflectStructValue(reflectStructFieldSpec, map[string]*Var{
		"Name":      NewString(field.Name),
		"Type":      e.reflectTypeVar(field.Type),
		"Kind":      NewInt(e.reflectKindOfType(field.Type)),
		"Index":     NewInt(int64(index)),
		"Tag":       NewString(field.Tag),
		"Exported":  NewBool(exported),
		"Anonymous": NewBool(false),
	})
}

func (e *Executor) reflectFieldValue(v *Var, name string) (*Var, RuntimeType, bool) {
	target := e.unwrapValue(v)
	if target == nil {
		return nil, RuntimeType{}, false
	}
	if target.VType == TypePointer {
		var ok bool
		target, ok = e.slotPointerTarget(target)
		if !ok {
			return nil, RuntimeType{}, false
		}
		target = e.unwrapValue(target)
	}
	if target == nil || target.VType != TypeStruct {
		return nil, RuntimeType{}, false
	}
	st, _ := target.Ref.(*VMStruct)
	if st == nil {
		return nil, RuntimeType{}, false
	}
	slot, ok := st.Field(name)
	if !ok || slot == nil {
		return nil, RuntimeType{}, false
	}
	return slot.Value, slot.Decl, true
}

func (e *Executor) reflectLen(v *Var) int64 {
	v = e.unwrapValue(v)
	if v == nil {
		return 0
	}
	switch v.VType {
	case TypeString:
		return int64(len(v.Str))
	case TypeBytes:
		return int64(len(v.B))
	case TypeArray:
		return int64(arrayRef(v).Len())
	case TypeMap:
		return int64(mapRef(v).Len())
	case TypeChannel:
		ch, _ := asVMChannel(v)
		return int64(ch.Len())
	default:
		return 0
	}
}

func (e *Executor) reflectIndexValue(v *Var, idx int64) (*Var, RuntimeType, bool) {
	v = e.unwrapValue(v)
	maxInt := int64(int(^uint(0) >> 1))
	if v == nil || idx < 0 || idx > maxInt {
		return nil, RuntimeType{}, false
	}
	i := int(idx)
	intType := MustParseRuntimeType(SpecInt64)
	switch v.VType {
	case TypeString:
		if i >= len(v.Str) {
			return nil, RuntimeType{}, false
		}
		return NewInt(int64(v.Str[i])), intType, true
	case TypeBytes:
		if i >= len(v.B) {
			return nil, RuntimeType{}, false
		}
		return NewInt(int64(v.B[i])), intType, true
	case TypeArray:
		val, ok := arrayRef(v).Load(i)
		elem, _ := e.reflectResolvedValueType(v).ReadArrayItemType()
		return val, elem, ok
	default:
		return nil, RuntimeType{}, false
	}
}

func (e *Executor) reflectMapKeys(v *Var) (*Var, bool) {
	v = e.unwrapValue(v)
	empty := e.reflectArray(SpecAny, nil)
	if v == nil || v.VType != TypeMap {
		return empty, false
	}
	m := mapRef(v)
	if m == nil {
		return empty, true
	}
	keyType, valueType, ok := e.reflectResolvedValueType(v).GetMapKeyValueTypes()
	if !ok || !e.reflectTypeCanEnterAny(keyType, 0) || !e.reflectTypeCanEnterAny(valueType, 0) {
		return empty, false
	}
	entries := m.Entries()
	items := make([]*Var, 0, len(entries))
	for _, entry := range entries {
		key := entry.Key
		if key == nil {
			key = NewString(entry.Encoded)
		}
		anyKey, ok := e.reflectAnyValue(key, keyType)
		if !ok {
			return empty, false
		}
		items = append(items, anyKey)
	}
	return e.reflectArray(SpecAny, items), true
}

func (e *Executor) reflectMapIndexValue(v, key *Var) (*Var, RuntimeType, bool) {
	v = e.unwrapValue(v)
	if v == nil || v.VType != TypeMap {
		return nil, RuntimeType{}, false
	}
	m := mapRef(v)
	if m == nil {
		return nil, RuntimeType{}, false
	}
	keyType, valueType, ok := e.reflectResolvedValueType(v).GetMapKeyValueTypes()
	if !ok {
		return nil, RuntimeType{}, false
	}
	if !e.reflectTypeCanEnterAny(keyType, 0) || !e.reflectTypeCanEnterAny(valueType, 0) {
		return nil, RuntimeType{}, false
	}
	if _, keyOK := e.reflectAnyValue(key, keyType); !keyOK {
		return nil, RuntimeType{}, false
	}
	encoded, _, err := e.comparableMapKey(key, keyType)
	if err != nil {
		return nil, RuntimeType{}, false
	}
	val, ok := m.Load(encoded)
	if !ok {
		return nil, RuntimeType{}, false
	}
	return val, valueType, true
}

func (e *Executor) reflectMakeMap(name string) (*Var, bool) {
	raw := TypeSpec(strings.TrimSpace(name))
	typ, ok := e.reflectResolvedRuntimeType(raw, true)
	if !ok || !typ.IsMap() || !e.reflectTypeCanEnterAny(typ, 0) {
		return nil, false
	}
	res := &Var{VType: TypeMap, Ref: &VMMap{Data: make(map[string]*Var)}}
	res.SetRuntimeType(typ)
	return res, true
}

func (e *Executor) reflectSetMapIndex(session *StackContext, v, key, value *Var) *Var {
	target := e.unwrapValue(v)
	if target == nil || target.VType != TypeMap {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex target must be Map, got %s", reflectKindName(e.reflectKindOfVar(v))))
	}
	m := mapRef(target)
	if m == nil {
		return newErrorVar(errors.New("reflect.SetMapIndex target map is nil"))
	}
	keyType, valueType, ok := e.reflectResolvedValueType(target).GetMapKeyValueTypes()
	if !ok {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex invalid map type %s", target.RuntimeType().Raw))
	}
	if !e.reflectTypeCanEnterAny(keyType, 0) {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex key type %s cannot enter Any", keyType.Raw))
	}
	if !e.reflectTypeCanEnterAny(valueType, 0) {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex value type %s cannot enter Any", valueType.Raw))
	}
	if _, ok := e.reflectAnyValue(key, keyType); !ok {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex key type %s cannot enter Any", keyType.Raw))
	}
	encoded, keyVar, err := e.comparableMapKey(key, keyType)
	if err != nil {
		return newErrorVar(err)
	}
	if session == nil {
		session = &StackContext{Executor: e}
	}
	if session.Executor == nil {
		session.Executor = e
	}
	prepared, err := e.prepareValueForType(session, value, valueType)
	if err != nil {
		return newErrorVar(err)
	}
	if _, ok := e.reflectAnyValue(prepared, valueType); !ok {
		return newErrorVar(fmt.Errorf("reflect.SetMapIndex value type %s cannot enter Any", valueType.Raw))
	}
	m.StoreWithKey(encoded, keyVar, prepared)
	return nil
}

func (e *Executor) reflectSetField(session *StackContext, v *Var, name string, value *Var) *Var {
	if v == nil {
		return newErrorVar(errors.New("reflect.SetField target is nil"))
	}
	var target *Var
	switch v.VType {
	case TypePointer:
		slot, ok := e.slotPointerSlot(v)
		if !ok || slot == nil {
			return newErrorVar(errors.New("reflect.SetField target pointer is nil"))
		}
		target = e.unwrapValue(slot.Value)
	case TypeAny:
		inner, ok := v.Ref.(*Var)
		if !ok || inner == nil {
			return newErrorVar(errors.New("reflect.SetField target is nil"))
		}
		target = e.unwrapValue(inner)
	default:
		return newErrorVar(fmt.Errorf("reflect.SetField target must be pointer to struct or Any-wrapped struct, got %s", reflectKindName(e.reflectKindOfVar(v))))
	}
	if target == nil || target.VType != TypeStruct {
		return newErrorVar(fmt.Errorf("reflect.SetField target is %s, not Struct", reflectKindName(e.reflectKindOfVar(target))))
	}
	st, _ := target.Ref.(*VMStruct)
	if st == nil {
		return newErrorVar(errors.New("reflect.SetField target struct is nil"))
	}
	if st.Spec != nil && reflectMetadataStruct(st.Spec.Name) {
		return newErrorVar(fmt.Errorf("reflect.SetField metadata type %s is read-only", st.Spec.Name))
	}
	slot, ok := st.Field(name)
	if !ok || slot == nil {
		return newErrorVar(fmt.Errorf("reflect.SetField field %s not found", name))
	}
	targetType := slot.Decl
	if targetType.IsEmpty() && slot.Value != nil {
		targetType = slot.Value.RuntimeType()
	}
	if targetType.IsAny() {
		if err := e.validateFFIAnyValue(value); err != nil {
			return newErrorVar(err)
		}
	}
	if session == nil {
		session = &StackContext{Executor: e}
	}
	if session.Executor == nil {
		session.Executor = e
	}
	if err := session.Assign(slot, value); err != nil {
		return newErrorVar(err)
	}
	return nil
}

func (e *Executor) reflectAnyTuple(value *Var, declared RuntimeType, ok bool) *Var {
	if !ok {
		return e.reflectTuple([]TypeSpec{SpecAny, SpecBool}, e.wrapAnyVar(nil), NewBool(false))
	}
	anyValue, valueOK := e.reflectAnyValue(value, declared)
	return e.reflectTuple([]TypeSpec{SpecAny, SpecBool}, anyValue, NewBool(valueOK))
}

func (e *Executor) reflectAnyValue(value *Var, declared RuntimeType) (*Var, bool) {
	if !declared.IsEmpty() && !e.reflectTypeCanEnterAny(declared, 0) {
		return e.wrapAnyVar(nil), false
	}
	if err := e.validateFFIAnyValue(value); err != nil {
		return e.wrapAnyVar(nil), false
	}
	if value == nil {
		return e.wrapAnyVar(nil), true
	}
	if actual := e.reflectResolvedValueType(e.unwrapValue(value)); !actual.IsEmpty() && !e.reflectTypeCanEnterAny(actual, 0) {
		return e.wrapAnyVar(nil), false
	}
	return e.wrapAnyVar(value.DeepCopy()), true
}

func (e *Executor) reflectUnwrapValue(v *Var) (*Var, bool) {
	if v == nil {
		return nil, true
	}
	if v.VType == TypeAny {
		if v.Ref == nil {
			return nil, true
		}
		inner, ok := v.Ref.(*Var)
		if !ok {
			return nil, false
		}
		return inner, true
	}
	target := e.unwrapValue(v)
	if target == nil {
		return nil, true
	}
	if target.VType != TypeInterface {
		return target, true
	}
	iface, ok := target.Ref.(*VMInterface)
	if !ok || iface == nil || iface.Target == nil {
		return nil, true
	}
	return iface.Target, true
}

func (e *Executor) reflectTypeCanEnterAny(typ RuntimeType, depth int) bool {
	if depth > 64 || typ.IsEmpty() || typ.IsVoid() {
		return false
	}
	if !typ.Raw.IsEmpty() {
		if resolved, ok := e.reflectResolvedRuntimeType(typ.Raw, false); ok && (!resolved.Raw.Equals(typ.Raw) || resolved.Kind != typ.Kind) {
			typ = resolved
		}
	}
	if typ.IsAny() || typ.IsBool() || typ.IsInt() || typ.IsString() || typ.IsNumeric() || typ.Raw == SpecBytes || typ.Raw == SpecError {
		return true
	}
	if typ.IsPtr() || typ.IsHostRef() || typ.IsChan() || typ.IsInterface() || typ.Kind == RuntimeTypeFunction || typ.Raw.IsFunction() || typ.Raw == SpecModule || typ.Raw == SpecClosure {
		return false
	}
	if typ.IsArray() {
		elem, ok := typ.ReadArrayItemType()
		return ok && e.reflectTypeCanEnterAny(elem, depth+1)
	}
	if typ.IsMap() {
		key, value, ok := typ.GetMapKeyValueTypes()
		return ok && e.reflectTypeCanEnterAny(key, depth+1) && e.reflectTypeCanEnterAny(value, depth+1)
	}
	if typ.Kind == RuntimeTypeTuple {
		for _, item := range typ.Params {
			if !e.reflectTypeCanEnterAny(item, depth+1) {
				return false
			}
		}
		return true
	}
	if spec, ok := e.runtimeStructSchemaForType(typ); ok && spec != nil {
		if spec.Ownership == StructOwnershipHostOpaque {
			return false
		}
		for _, field := range spec.Fields {
			if !e.reflectTypeCanEnterAny(field.TypeInfo, depth+1) {
				return false
			}
		}
		return true
	}
	if typ.Kind == RuntimeTypeNamed {
		return false
	}
	return true
}

func reflectMetadataStruct(name string) bool {
	switch TypeSpec(name) {
	case reflectTypeSpec, reflectStructFieldSpec, reflectMethodSpec, reflectRouteSpec, reflectPackageInfoSpec, reflectMemberSpec:
		return true
	default:
		return false
	}
}

func (e *Executor) reflectZeroValue(session *StackContext, name string) (*Var, error) {
	raw := TypeSpec(strings.TrimSpace(name))
	typ, ok := e.reflectResolvedRuntimeType(raw, true)
	if !ok {
		return nil, fmt.Errorf("reflect type %s not found", name)
	}
	if !e.reflectTypeCanEnterAny(typ, 0) {
		return nil, fmt.Errorf("reflect.Zero type %s cannot enter Any", name)
	}
	if session == nil {
		session = &StackContext{Executor: e}
	}
	if session.Executor == nil {
		session.Executor = e
	}
	value, err := e.initializeType(session, typ, 0)
	if err != nil {
		return nil, err
	}
	if err := e.validateFFIAnyValue(value); err != nil {
		return nil, fmt.Errorf("reflect.Zero type %s cannot enter Any: %w", name, err)
	}
	return e.wrapAnyVar(value), nil
}

func (e *Executor) reflectMethods(raw TypeSpec) []reflectMethodInfo {
	if elem, ok := raw.PtrElement(); ok {
		raw = elem
	}
	if elem, ok := raw.HostRefElement(); ok {
		raw = elem
	}
	seen := make(map[string]struct{})
	var methods []reflectMethodInfo
	add := func(name string, sig *RuntimeFuncSig, route FFIRoute) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		methods = append(methods, reflectMethodInfo{Name: name, Sig: CloneRuntimeFuncSig(sig), Route: route})
	}
	if spec, ok := e.resolveStructSchema(raw); ok && spec != nil {
		for _, method := range spec.Methods {
			route, _ := e.reflectRouteForMethod(raw, method.Name)
			add(method.Name, method.Spec, route)
		}
	}
	if spec, ok := e.resolveInterfaceSpec(raw); ok && spec != nil {
		for _, method := range spec.Methods {
			add(method.Name, method.Spec, FFIRoute{})
		}
	}
	for methodName, fnName := range e.methodFunctions[normalizeMethodReceiverType(raw.String())] {
		if fn, ok := e.lookupFunction(fnName); ok && fn != nil {
			add(methodName, fn.FunctionSig, FFIRoute{})
		}
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Name < methods[j].Name
	})
	return methods
}

func (e *Executor) reflectRouteForMethod(raw TypeSpec, method string) (FFIRoute, bool) {
	if name, ok := e.resolveHostMethodRoute(raw.String(), method); ok {
		route, routeOK := e.routes[name]
		return route, routeOK
	}
	name := QualifiedMemberName(raw.String(), method)
	route, ok := e.routes[name]
	return route, ok
}

func (e *Executor) reflectMethodsArray(raw TypeSpec) *Var {
	methods := e.reflectMethods(raw)
	items := make([]*Var, len(methods))
	for i, method := range methods {
		items[i] = e.reflectMethodVar(method, i)
	}
	return e.reflectArray(reflectMethodSpec, items)
}

func (e *Executor) reflectMethodByName(raw TypeSpec, name string) (reflectMethodInfo, int, bool) {
	methods := e.reflectMethods(raw)
	for i, method := range methods {
		if method.Name == name {
			return method, i, true
		}
	}
	return reflectMethodInfo{}, -1, false
}

func (e *Executor) reflectMethodVar(method reflectMethodInfo, index int) *Var {
	raw := TypeSpec("")
	if method.Sig != nil {
		raw = method.Sig.Spec
	}
	return e.reflectStructValue(reflectMethodSpec, map[string]*Var{
		"Name":     NewString(method.Name),
		"Type":     e.reflectTypeVar(raw),
		"Index":    NewInt(int64(index)),
		"Route":    e.reflectRouteVar(method.Route),
		"IsFFI":    NewBool(method.Route.Bridge != nil && method.Route.Native == nil),
		"IsNative": NewBool(method.Route.Native != nil),
	})
}

func (e *Executor) reflectRouteVar(route FFIRoute) *Var {
	raw := TypeSpec("")
	if route.FuncSig != nil {
		raw = route.FuncSig.Spec
	}
	return e.reflectStructValue(reflectRouteSpec, map[string]*Var{
		"Name":     NewString(route.Name),
		"MethodID": NewInt(int64(route.MethodID)),
		"Type":     e.reflectTypeVar(raw),
		"IsFFI":    NewBool(route.Bridge != nil && route.Native == nil),
		"IsNative": NewBool(route.Native != nil),
	})
}

func (e *Executor) reflectFuncSig(raw TypeSpec) *RuntimeFuncSig {
	if raw.IsEmpty() {
		return nil
	}
	if typ, ok := e.reflectResolvedRuntimeType(raw, false); ok && typ.Kind == RuntimeTypeFunction {
		ret := RuntimeType{}
		if typ.Return != nil {
			ret = *typ.Return
		}
		return &RuntimeFuncSig{
			Spec:       typ.Raw,
			ParamTypes: append([]RuntimeType(nil), typ.Params...),
			ParamModes: defaultFFIParamModes(len(typ.Params)),
			ReturnType: ret,
			Variadic:   typ.Variadic,
		}
	}
	if sig, err := ParseRuntimeFuncSig(raw); err == nil {
		return sig
	}
	return nil
}

func (e *Executor) reflectIsFunc(v *Var) bool {
	if _, ok := e.reflectRouteFromValue(v); ok {
		return true
	}
	v = e.unwrapValue(v)
	if v == nil {
		return false
	}
	return v.VType == TypeClosure || v.RuntimeType().Kind == RuntimeTypeFunction || v.RawType() == SpecClosure
}

func (e *Executor) reflectRouteFromValue(v *Var) (FFIRoute, bool) {
	if v == nil {
		return FFIRoute{}, false
	}
	if v.VType == TypeAny {
		switch ref := v.Ref.(type) {
		case FFIRoute:
			return ref, true
		case *Var:
			return e.reflectRouteFromValue(ref)
		}
	}
	v = e.unwrapValue(v)
	if v == nil || v.VType != TypeClosure {
		return FFIRoute{}, false
	}
	if method, ok := v.Ref.(*VMMethodValue); ok && method != nil {
		route, routeOK := e.routes[method.Method]
		return route, routeOK
	}
	if route, ok := v.Ref.(FFIRoute); ok {
		return route, true
	}
	return FFIRoute{}, false
}

func (e *Executor) reflectIsVMFunc(v *Var) bool {
	v = e.unwrapValue(v)
	if v == nil || v.VType != TypeClosure {
		return false
	}
	switch ref := v.Ref.(type) {
	case *VMClosure:
		return ref != nil
	case *VMMethodValue:
		if ref == nil {
			return false
		}
		_, ok := e.lookupFunction(ref.Method)
		return ok
	default:
		return false
	}
}

func (e *Executor) reflectPackageVar(path string) *Var {
	return e.reflectStructValue(reflectPackageInfoSpec, map[string]*Var{
		"Path": NewString(path),
	})
}

func (e *Executor) reflectLookupPackage(path string) (*runtimeModule, bool) {
	if e == nil || path == "" {
		return nil, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	module, ok := e.modules.Lookup(path)
	return module, ok
}

func (e *Executor) reflectPackagesArray() *Var {
	e.mu.RLock()
	paths := e.modules.Paths()
	e.mu.RUnlock()
	items := make([]*Var, len(paths))
	for i, path := range paths {
		items[i] = e.reflectPackageVar(path)
	}
	return e.reflectArray(reflectPackageInfoSpec, items)
}

func (e *Executor) reflectMembersArray(path string) *Var {
	e.mu.RLock()
	module, _ := e.modules.Lookup(path)
	members := sortedRuntimeModuleMembers(module)
	items := make([]*Var, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		items = append(items, e.reflectMemberVar(member))
	}
	e.mu.RUnlock()
	return e.reflectArray(reflectMemberSpec, items)
}

func (e *Executor) reflectMemberByName(path, name string) (*Var, bool) {
	e.mu.RLock()
	module, _ := e.modules.Lookup(path)
	if module == nil {
		e.mu.RUnlock()
		return e.reflectMemberVar(nil), false
	}
	member := module.Members[name]
	res := e.reflectMemberVar(member)
	ok := member != nil
	e.mu.RUnlock()
	return res, ok
}

func (e *Executor) reflectMemberVar(member *runtimeModuleMember) *Var {
	name := ""
	kind := ""
	readOnly := false
	raw := TypeSpec("")
	route := FFIRoute{}
	if member != nil {
		name = member.Name
		kind = string(member.Kind)
		readOnly = member.ReadOnly
		switch member.Kind {
		case FFIMemberFunc:
			route = e.routes[member.RouteName]
			if route.FuncSig != nil {
				raw = route.FuncSig.Spec
			} else if !member.Type.IsEmpty() {
				raw = member.Type.Raw
			}
		case FFIMemberConst:
			raw = member.Const.Type
		case FFIMemberValue:
			raw = member.Type.Raw
		case FFIMemberType:
			raw = member.Type.Raw
		}
	}
	return e.reflectStructValue(reflectMemberSpec, map[string]*Var{
		"Name":     NewString(name),
		"Kind":     NewString(kind),
		"Type":     e.reflectTypeVar(raw),
		"ReadOnly": NewBool(readOnly),
		"Route":    e.reflectRouteVar(route),
	})
}

func (e *Executor) reflectArray(elem TypeSpec, items []*Var) *Var {
	typ := MustParseRuntimeType(ArrayType(elem))
	return &Var{TypeInfo: typ, VType: TypeArray, Ref: &VMArray{Data: items}}
}

func (e *Executor) reflectTuple(types []TypeSpec, items ...*Var) *Var {
	typ := MustParseRuntimeType(TupleType(types...))
	return &Var{TypeInfo: typ, VType: TypeArray, Ref: &VMArray{Data: items}}
}

func reflectSplitTypeName(raw TypeSpec) (name, pkgPath string) {
	text := strings.TrimSpace(raw.String())
	if text == "" || strings.ContainsAny(text, "<({") || text == SpecAny.String() || text == SpecModule.String() || text == SpecClosure.String() {
		return "", ""
	}
	idx := strings.LastIndex(text, ".")
	if idx < 0 {
		return text, ""
	}
	return text[idx+1:], text[:idx]
}

func reflectKindName(kind int64) string {
	switch kind {
	case ReflectKindBool:
		return "Bool"
	case ReflectKindInt64:
		return "Int64"
	case ReflectKindFloat64:
		return "Float64"
	case ReflectKindString:
		return "String"
	case ReflectKindBytes:
		return "TypeBytes"
	case ReflectKindArray:
		return "Array"
	case ReflectKindMap:
		return "Map"
	case ReflectKindStruct:
		return "Struct"
	case ReflectKindPtr:
		return "Ptr"
	case ReflectKindHostRef:
		return "HostRef"
	case ReflectKindChan:
		return "Chan"
	case ReflectKindFunc:
		return "Func"
	case ReflectKindInterface:
		return "Interface"
	case ReflectKindError:
		return "Error"
	case ReflectKindModule:
		return "Module"
	case ReflectKindAny:
		return "Any"
	default:
		return "Invalid"
	}
}
