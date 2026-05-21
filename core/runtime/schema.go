package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/typespec"
)

type RuntimeTypeKind uint8

const (
	RuntimeTypeInvalid RuntimeTypeKind = iota
	RuntimeTypeVoid
	RuntimeTypeAny
	RuntimeTypePrimitive
	RuntimeTypeNamed
	RuntimeTypePointer
	RuntimeTypeHostRef
	RuntimeTypeArray
	RuntimeTypeMap
	RuntimeTypeTuple
	RuntimeTypeFunction
	RuntimeTypeStruct
	RuntimeTypeInterface
)

type TypeSpec = typespec.Type

const (
	SpecInt64   TypeSpec = typespec.Int64
	SpecFloat64 TypeSpec = typespec.Float64
	SpecString  TypeSpec = typespec.String
	SpecBool    TypeSpec = typespec.Bool
	SpecBytes   TypeSpec = typespec.Bytes
	SpecAny     TypeSpec = typespec.Any
	SpecError   TypeSpec = typespec.Error
	SpecVoid    TypeSpec = typespec.Void
	SpecModule  TypeSpec = typespec.Module
	SpecClosure TypeSpec = typespec.Closure
)

func PtrType(elem TypeSpec) TypeSpec     { return typespec.Ptr(elem) }
func HostRefType(elem TypeSpec) TypeSpec { return typespec.HostRef(elem) }
func ArrayType(elem TypeSpec) TypeSpec   { return typespec.Array(elem) }
func MapType(key, value TypeSpec) TypeSpec {
	return typespec.Map(key, value)
}

func TupleType(items ...TypeSpec) TypeSpec {
	types := append([]typespec.Type(nil), items...)
	return typespec.Tuple(types...)
}

func FuncType(params []RuntimeFuncParam, ret TypeSpec, variadic bool) TypeSpec {
	items := make([]typespec.Param, 0, len(params))
	for _, param := range params {
		items = append(items, typespec.Param{Name: param.Name, Type: param.Type.Raw})
	}
	return typespec.Func(items, ret, variadic)
}

func InterfaceType(methods []RuntimeInterfaceMethod) TypeSpec {
	items := make([]typespec.Method, 0, len(methods))
	for _, method := range methods {
		if method.Name == "" || method.Spec == nil {
			continue
		}
		params := make([]typespec.Param, 0, len(method.Spec.ParamTypes))
		for i, paramType := range method.Spec.ParamTypes {
			name := ""
			if i < len(method.Spec.ParamNames) {
				name = method.Spec.ParamNames[i]
			}
			params = append(params, typespec.Param{Name: name, Type: paramType.Raw})
		}
		items = append(items, typespec.Method{
			Name: method.Name,
			Sig: typespec.Function{
				Params:   params,
				Return:   method.Spec.ReturnType.Raw,
				Variadic: method.Spec.Variadic,
			},
		})
	}
	return typespec.Interface(items)
}

func MustRuntimeFuncSig(ret TypeSpec, variadic bool, params ...TypeSpec) *RuntimeFuncSig {
	items := make([]RuntimeFuncParam, 0, len(params))
	for _, param := range params {
		items = append(items, RuntimeFuncParam{Type: MustParseRuntimeType(param)})
	}
	spec := FuncType(items, ret, variadic)
	return MustParseRuntimeFuncSig(spec)
}

// RuntimeType is a parsed, structural view of Go-Mini type metadata.
type RuntimeType struct {
	Kind RuntimeTypeKind
	Raw  TypeSpec

	// TypeID is the canonical named type identifier when the type carries one.
	TypeID string

	Elem   *RuntimeType
	Key    *RuntimeType
	Value  *RuntimeType
	Params []RuntimeType
	Return *RuntimeType

	Variadic bool
	Fields   []RuntimeStructField
	Methods  []RuntimeInterfaceMethod
}

func (t RuntimeType) MarshalJSON() ([]byte, error) {
	type runtimeTypeAlias RuntimeType
	if t.Kind == RuntimeTypeInvalid && !t.Raw.IsEmpty() {
		parsed, err := ParseRuntimeType(t.Raw)
		if err != nil {
			return nil, err
		}
		t = parsed
	}
	return json.Marshal(runtimeTypeAlias(t))
}

func (t *RuntimeType) UnmarshalJSON(data []byte) error {
	type runtimeTypeAlias RuntimeType
	var alias runtimeTypeAlias
	if err := json.Unmarshal(data, &alias); err == nil {
		if alias.Kind == RuntimeTypeInvalid && alias.Raw.IsEmpty() {
			*t = RuntimeType(alias)
			return nil
		}
		if alias.Raw.IsEmpty() {
			return errors.New("runtime type missing raw type")
		}
		parsed, parseErr := ParseRuntimeType(alias.Raw)
		if parseErr != nil {
			return parseErr
		}
		*t = parsed
		return nil
	}

	var raw TypeSpec
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseRuntimeType(raw)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

type FFIParamMode uint8

const (
	FFIParamIn FFIParamMode = iota
	FFIParamInOutBytes
	FFIParamInOutArray
)

type RuntimeFuncParam struct {
	Name string
	Type RuntimeType
}

// RuntimeFuncSig is the parsed FFI function schema cached at registration time.
type RuntimeFuncSig struct {
	Spec       TypeSpec
	ParamNames []string
	ParamTypes []RuntimeType
	ParamModes []FFIParamMode
	ReturnType RuntimeType
	Variadic   bool
}

// RuntimeStructField stores a field in declaration order for FFI struct codecs.
type RuntimeStructField struct {
	Name string
	Type TypeSpec

	TypeInfo RuntimeType
}

type RuntimeStructOwnership string

const (
	StructOwnershipVMValue    RuntimeStructOwnership = "VMValue"
	StructOwnershipHostOpaque RuntimeStructOwnership = "HostOpaque"
)

func (o RuntimeStructOwnership) Valid() bool {
	return o == StructOwnershipVMValue || o == StructOwnershipHostOpaque
}

type RuntimeStructMethod struct {
	Name string
	Spec *RuntimeFuncSig
}

// RuntimeStructSpec is the parsed FFI struct schema cached at registration time.
type RuntimeStructSpec struct {
	Name      string
	TypeID    string
	Spec      TypeSpec
	Ownership RuntimeStructOwnership

	TypeInfo RuntimeType
	Layout   StructLayout
	Fields   []RuntimeStructField
	ByName   map[string]RuntimeStructField
	Methods  []RuntimeStructMethod
	ByMethod map[string]*RuntimeFuncSig
}

type StructLayout struct {
	FieldOrder  []string
	FieldIndex  map[string]int
	FieldOffset map[string]int
	Size        int
}

type RuntimeInterfaceMethod struct {
	Index int
	Name  string
	Spec  *RuntimeFuncSig
}

type RuntimeInterfaceSpec struct {
	TypeID string
	Spec   TypeSpec

	TypeInfo    RuntimeType
	Methods     []RuntimeInterfaceMethod
	ByName      map[string]*RuntimeFuncSig
	MethodIndex map[string]int
}

// CloneRuntimeFuncSig returns a detached copy of runtime function signature metadata.
func CloneRuntimeFuncSig(sig *RuntimeFuncSig) *RuntimeFuncSig {
	if sig == nil {
		return nil
	}
	res := *sig
	res.ParamNames = append([]string(nil), sig.ParamNames...)
	res.ParamTypes = append([]RuntimeType(nil), sig.ParamTypes...)
	res.ParamModes = append([]FFIParamMode(nil), sig.ParamModes...)
	return &res
}

// CloneRuntimeStructSpec returns a detached copy of runtime struct metadata.
func CloneRuntimeStructSpec(spec *RuntimeStructSpec) *RuntimeStructSpec {
	if spec == nil {
		return nil
	}
	fields := append([]RuntimeStructField(nil), spec.Fields...)
	byName := make(map[string]RuntimeStructField, len(spec.ByName))
	for k, v := range spec.ByName {
		byName[k] = v
	}
	methods := make([]RuntimeStructMethod, len(spec.Methods))
	byMethod := make(map[string]*RuntimeFuncSig, len(spec.ByMethod))
	for i, method := range spec.Methods {
		methods[i] = RuntimeStructMethod{
			Name: method.Name,
			Spec: CloneRuntimeFuncSig(method.Spec),
		}
	}
	for k, v := range spec.ByMethod {
		byMethod[k] = CloneRuntimeFuncSig(v)
	}
	typeInfo := spec.TypeInfo
	typeInfo.Fields = append([]RuntimeStructField(nil), spec.TypeInfo.Fields...)
	return &RuntimeStructSpec{
		Name:      spec.Name,
		TypeID:    spec.TypeID,
		Spec:      spec.Spec,
		Ownership: spec.Ownership,
		TypeInfo:  typeInfo,
		Layout:    spec.Layout,
		Fields:    fields,
		ByName:    byName,
		Methods:   methods,
		ByMethod:  byMethod,
	}
}

// CloneRuntimeInterfaceSpec returns a detached copy of runtime interface metadata.
func CloneRuntimeInterfaceSpec(spec *RuntimeInterfaceSpec) *RuntimeInterfaceSpec {
	if spec == nil {
		return nil
	}
	methods := make([]RuntimeInterfaceMethod, len(spec.Methods))
	byName := make(map[string]*RuntimeFuncSig, len(spec.ByName))
	methodIndex := make(map[string]int, len(spec.MethodIndex))
	for i, method := range spec.Methods {
		methods[i] = RuntimeInterfaceMethod{
			Index: method.Index,
			Name:  method.Name,
			Spec:  CloneRuntimeFuncSig(method.Spec),
		}
	}
	for k, v := range spec.ByName {
		byName[k] = CloneRuntimeFuncSig(v)
	}
	for k, v := range spec.MethodIndex {
		methodIndex[k] = v
	}
	typeInfo := spec.TypeInfo
	typeInfo.Methods = append([]RuntimeInterfaceMethod(nil), spec.TypeInfo.Methods...)
	return &RuntimeInterfaceSpec{
		TypeID:      spec.TypeID,
		Spec:        spec.Spec,
		TypeInfo:    typeInfo,
		Methods:     methods,
		ByName:      byName,
		MethodIndex: methodIndex,
	}
}

func CanonicalTypeID(name string) string {
	return typespec.CanonicalTypeID(name)
}

func MustParseRuntimeType[S ~string](spec S) RuntimeType {
	parsed, err := ParseRuntimeType(spec)
	if err != nil {
		panic(err)
	}
	return parsed
}

func (t RuntimeType) String() string {
	return string(t.Raw)
}

func (t RuntimeType) IsEmpty() bool {
	return t.Raw.IsEmpty()
}

func (t RuntimeType) IsVoid() bool {
	return t.Kind == RuntimeTypeVoid || t.Raw.IsVoid()
}

func (t RuntimeType) IsAny() bool {
	return t.Kind == RuntimeTypeAny || t.Raw.IsAny()
}

func (t RuntimeType) IsInt() bool {
	return t.Raw.IsInt()
}

func (t RuntimeType) IsString() bool {
	return t.Raw.IsString()
}

func (t RuntimeType) IsBool() bool {
	return t.Raw.IsBool()
}

func (t RuntimeType) IsNumeric() bool {
	return t.Raw.IsNumeric()
}

func (t RuntimeType) IsPtr() bool {
	return t.Kind == RuntimeTypePointer || t.Raw.IsPtr()
}

func (t RuntimeType) IsHostRef() bool {
	return t.Kind == RuntimeTypeHostRef || t.Raw.IsHostRef()
}

func (t RuntimeType) IsArray() bool {
	return t.Kind == RuntimeTypeArray || t.Raw.IsArray()
}

func (t RuntimeType) IsMap() bool {
	return t.Kind == RuntimeTypeMap || t.Raw.IsMap()
}

func (t RuntimeType) IsInterface() bool {
	return t.Kind == RuntimeTypeInterface || t.Raw.IsInterface()
}

func (t RuntimeType) Equals(other RuntimeType) bool {
	return t.Raw.Equals(other.Raw)
}

func (t RuntimeType) IsAssignableTo(other RuntimeType) bool {
	return t.Raw.IsAssignableTo(other.Raw)
}

func (t RuntimeType) ReadArrayItemType() (RuntimeType, bool) {
	if t.Elem != nil {
		return *t.Elem, true
	}
	elem, ok := t.Raw.ReadArrayItemType()
	if !ok {
		return RuntimeType{}, false
	}
	elemInfo, err := ParseRuntimeType(elem)
	if err != nil {
		return RuntimeType{}, false
	}
	return elemInfo, true
}

func (t RuntimeType) GetMapKeyValueTypes() (RuntimeType, RuntimeType, bool) {
	if t.Key != nil && t.Value != nil {
		return *t.Key, *t.Value, true
	}
	key, value, ok := t.Raw.MapTypes()
	if !ok {
		return RuntimeType{}, RuntimeType{}, false
	}
	keyInfo, err := ParseRuntimeType(key)
	if err != nil {
		return RuntimeType{}, RuntimeType{}, false
	}
	valueInfo, err := ParseRuntimeType(value)
	if err != nil {
		return RuntimeType{}, RuntimeType{}, false
	}
	return keyInfo, valueInfo, true
}

func (t RuntimeType) ZeroVar() interface{} {
	return t.Raw.ZeroValue()
}

func ParseRuntimeType[S ~string](spec S) (RuntimeType, error) {
	specType := TypeSpec(strings.TrimSpace(string(spec)))
	if specType.IsEmpty() || specType.IsVoid() {
		return RuntimeType{Kind: RuntimeTypeVoid, Raw: specType}, nil
	}
	if err := specType.ValidateCanonical(); err != nil {
		return RuntimeType{}, err
	}
	if specType == typespec.Any || specType == typespec.Module || specType == typespec.Closure {
		return RuntimeType{Kind: RuntimeTypeAny, Raw: specType, TypeID: CanonicalTypeID(string(specType))}, nil
	}
	if specType.IsPrimitive() {
		return RuntimeType{Kind: RuntimeTypePrimitive, Raw: specType, TypeID: CanonicalTypeID(string(specType))}, nil
	}
	if specType.IsPtr() {
		elem, ok := specType.PtrElement()
		if !ok {
			return RuntimeType{}, fmt.Errorf("invalid pointer type: %s", specType)
		}
		elemType, err := ParseRuntimeType(elem)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:   RuntimeTypePointer,
			Raw:    specType,
			TypeID: elemType.TypeID,
			Elem:   &elemType,
		}, nil
	}
	if specType.IsHostRef() {
		elem, ok := specType.HostRefElement()
		if !ok {
			return RuntimeType{}, fmt.Errorf("invalid host reference type: %s", specType)
		}
		elemType, err := ParseRuntimeType(elem)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:   RuntimeTypeHostRef,
			Raw:    specType,
			TypeID: elemType.TypeID,
			Elem:   &elemType,
		}, nil
	}
	if specType.IsArray() {
		elem, ok := specType.ReadArrayItemType()
		if !ok {
			return RuntimeType{}, fmt.Errorf("invalid array type: %s", specType)
		}
		elemType, err := ParseRuntimeType(elem)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:   RuntimeTypeArray,
			Raw:    specType,
			TypeID: elemType.TypeID,
			Elem:   &elemType,
		}, nil
	}
	if specType.IsMap() {
		key, value, ok := specType.MapTypes()
		if !ok {
			return RuntimeType{}, fmt.Errorf("invalid map type: %s", specType)
		}
		keyType, err := ParseRuntimeType(key)
		if err != nil {
			return RuntimeType{}, err
		}
		valueType, err := ParseRuntimeType(value)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:   RuntimeTypeMap,
			Raw:    specType,
			TypeID: valueType.TypeID,
			Key:    &keyType,
			Value:  &valueType,
		}, nil
	}
	if types, ok := specType.TupleTypes(); ok {
		params := make([]RuntimeType, 0, len(types))
		for _, item := range types {
			itemType, err := ParseRuntimeType(item)
			if err != nil {
				return RuntimeType{}, err
			}
			params = append(params, itemType)
		}
		return RuntimeType{
			Kind:   RuntimeTypeTuple,
			Raw:    specType,
			TypeID: CanonicalTypeID(string(specType)),
			Params: params,
		}, nil
	}
	if fn, ok := specType.Function(); ok {
		params := make([]RuntimeType, 0, len(fn.Params))
		for _, p := range fn.Params {
			paramType, err := ParseRuntimeType(p.Type)
			if err != nil {
				return RuntimeType{}, err
			}
			params = append(params, paramType)
		}
		retType, err := ParseRuntimeType(fn.Return)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:     RuntimeTypeFunction,
			Raw:      specType,
			TypeID:   CanonicalTypeID(string(specType)),
			Params:   params,
			Return:   &retType,
			Variadic: fn.Variadic,
		}, nil
	}
	if specType.IsStruct() {
		return parseRuntimeStructType(specType)
	}
	if specType.IsInterface() {
		return parseRuntimeInterfaceType(specType)
	}
	return RuntimeType{
		Kind:   RuntimeTypeNamed,
		Raw:    specType,
		TypeID: CanonicalTypeID(string(specType)),
	}, nil
}

func ParseRuntimeFuncSig[S ~string](spec S) (*RuntimeFuncSig, error) {
	specType := TypeSpec(strings.TrimSpace(string(spec)))
	if specType.IsEmpty() {
		return nil, nil
	}
	fn, ok := specType.Function()
	if !ok {
		return nil, fmt.Errorf("invalid function spec: %s", specType)
	}
	params := make([]RuntimeType, 0, len(fn.Params))
	names := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		paramType, err := ParseRuntimeType(p.Type)
		if err != nil {
			return nil, err
		}
		params = append(params, paramType)
		names = append(names, p.Name)
	}
	retType, err := ParseRuntimeType(fn.Return)
	if err != nil {
		return nil, err
	}
	return &RuntimeFuncSig{
		Spec:       specType,
		ParamNames: names,
		ParamTypes: params,
		ParamModes: defaultFFIParamModes(len(params)),
		ReturnType: retType,
		Variadic:   fn.Variadic,
	}, nil
}

func (s *RuntimeFuncSig) SignatureString() string {
	if s == nil {
		return ""
	}
	if !s.Spec.IsEmpty() {
		return string(s.Spec)
	}
	params := make([]RuntimeFuncParam, 0, len(s.ParamTypes))
	for i, typ := range s.ParamTypes {
		name := ""
		if i < len(s.ParamNames) {
			name = s.ParamNames[i]
		}
		params = append(params, RuntimeFuncParam{Name: name, Type: typ})
	}
	return FuncType(params, s.ReturnType.Raw, s.Variadic).String()
}

func defaultFFIParamModes(n int) []FFIParamMode {
	if n == 0 {
		return nil
	}
	modes := make([]FFIParamMode, n)
	for i := range modes {
		modes[i] = FFIParamIn
	}
	return modes
}

func cloneFFIParamModes(modes []FFIParamMode) []FFIParamMode {
	if len(modes) == 0 {
		return nil
	}
	cloned := make([]FFIParamMode, len(modes))
	copy(cloned, modes)
	return cloned
}

func CloneRuntimeFuncSigWithParamModes(sig *RuntimeFuncSig, modes ...FFIParamMode) *RuntimeFuncSig {
	if sig == nil {
		return nil
	}
	cloned := *sig
	cloned.ParamTypes = append([]RuntimeType(nil), sig.ParamTypes...)
	if len(modes) == 0 {
		cloned.ParamModes = defaultFFIParamModes(len(sig.ParamTypes))
		return &cloned
	}
	if len(modes) != len(sig.ParamTypes) {
		panic(fmt.Sprintf("ffi param mode count mismatch: have %d want %d", len(modes), len(sig.ParamTypes)))
	}
	cloned.ParamModes = cloneFFIParamModes(modes)
	return &cloned
}

func ParseRuntimeStructSpec[S ~string](name string, ownership RuntimeStructOwnership, spec S) (*RuntimeStructSpec, error) {
	if !ownership.Valid() {
		return nil, fmt.Errorf("invalid struct ownership for %s: %s", name, ownership)
	}
	specType := TypeSpec(strings.TrimSpace(string(spec)))
	if specType.IsEmpty() {
		return nil, nil
	}
	typeInfo, err := parseRuntimeStructType(specType)
	if err != nil {
		return nil, fmt.Errorf("invalid struct spec for %s: %w", name, err)
	}

	fields, methods, err := splitRuntimeStructMembers(typeInfo.Fields)
	if err != nil {
		return nil, fmt.Errorf("invalid struct spec for %s: %w", name, err)
	}
	if ownership == StructOwnershipHostOpaque && len(fields) > 0 {
		return nil, fmt.Errorf("host opaque struct %s must not declare data fields", name)
	}

	byName := make(map[string]RuntimeStructField, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	byMethod := make(map[string]*RuntimeFuncSig, len(methods))
	for _, method := range methods {
		byMethod[method.Name] = method.Spec
	}
	layout := buildStructLayout(fields)
	typeInfo.Fields = fields

	return &RuntimeStructSpec{
		Name:      name,
		TypeID:    CanonicalTypeID(name),
		Spec:      specType,
		Ownership: ownership,
		TypeInfo:  typeInfo,
		Layout:    layout,
		Fields:    fields,
		ByName:    byName,
		Methods:   methods,
		ByMethod:  byMethod,
	}, nil
}

func ParseRuntimeInterfaceSpec[S ~string](spec S) (*RuntimeInterfaceSpec, error) {
	specType := TypeSpec(strings.TrimSpace(string(spec)))
	if specType.IsEmpty() {
		return nil, nil
	}
	typeInfo, err := parseRuntimeInterfaceType(specType)
	if err != nil {
		return nil, err
	}

	methods := make([]RuntimeInterfaceMethod, len(typeInfo.Methods))
	byName := make(map[string]*RuntimeFuncSig, len(typeInfo.Methods))
	methodIndex := make(map[string]int, len(typeInfo.Methods))
	for i, method := range typeInfo.Methods {
		fnSig, err := ParseRuntimeFuncSig(method.Spec.Spec)
		if err != nil {
			return nil, err
		}
		methods[i] = RuntimeInterfaceMethod{Index: i, Name: method.Name, Spec: fnSig}
		byName[method.Name] = fnSig
		methodIndex[method.Name] = i
	}

	return &RuntimeInterfaceSpec{
		TypeID:      typeInfo.TypeID,
		Spec:        specType,
		TypeInfo:    typeInfo,
		Methods:     methods,
		ByName:      byName,
		MethodIndex: methodIndex,
	}, nil
}

func (s *RuntimeInterfaceSpec) MethodStringMap() map[string]string {
	if s == nil {
		return nil
	}
	res := make(map[string]string, len(s.Methods))
	for _, method := range s.Methods {
		if method.Spec != nil {
			res[method.Name] = string(method.Spec.Spec)
		}
	}
	return res
}

func MustParseRuntimeFuncSig[S ~string](spec S) *RuntimeFuncSig {
	sig, err := ParseRuntimeFuncSig(spec)
	if err != nil {
		panic(err)
	}
	return sig
}

func MustParseRuntimeFuncSigWithModes[S ~string](spec S, modes ...FFIParamMode) *RuntimeFuncSig {
	return CloneRuntimeFuncSigWithParamModes(MustParseRuntimeFuncSig(spec), modes...)
}

func MustParseRuntimeStructSpec[S ~string](name string, ownership RuntimeStructOwnership, spec S) *RuntimeStructSpec {
	parsed, err := ParseRuntimeStructSpec(name, ownership, spec)
	if err != nil {
		panic(err)
	}
	return parsed
}

func MustParseRuntimeInterfaceSpec[S ~string](spec S) *RuntimeInterfaceSpec {
	parsed, err := ParseRuntimeInterfaceSpec(spec)
	if err != nil {
		panic(err)
	}
	return parsed
}

func parseRuntimeStructType(spec TypeSpec) (RuntimeType, error) {
	members, ok := spec.StructFields()
	if !ok {
		return RuntimeType{}, fmt.Errorf("malformed struct type: %s", spec)
	}
	fields := make([]RuntimeStructField, 0, len(members))
	for _, member := range members {
		fieldType := member.Type
		typeInfo, err := ParseRuntimeType(fieldType)
		if err != nil {
			return RuntimeType{}, err
		}
		fields = append(fields, RuntimeStructField{
			Name:     member.Name,
			Type:     fieldType,
			TypeInfo: typeInfo,
		})
	}
	return RuntimeType{
		Kind:   RuntimeTypeStruct,
		Raw:    spec,
		TypeID: CanonicalTypeID(spec.String()),
		Fields: fields,
	}, nil
}

func splitRuntimeStructMembers(members []RuntimeStructField) ([]RuntimeStructField, []RuntimeStructMethod, error) {
	fields := make([]RuntimeStructField, 0, len(members))
	methods := make([]RuntimeStructMethod, 0)
	for _, member := range members {
		if member.TypeInfo.Kind != RuntimeTypeFunction {
			fields = append(fields, member)
			continue
		}
		sig, err := ParseRuntimeFuncSig(member.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid method %s: %w", member.Name, err)
		}
		methods = append(methods, RuntimeStructMethod{Name: member.Name, Spec: sig})
	}
	return fields, methods, nil
}

func buildStructLayout(fields []RuntimeStructField) StructLayout {
	order := make([]string, len(fields))
	index := make(map[string]int, len(fields))
	offset := make(map[string]int, len(fields))
	for i, field := range fields {
		order[i] = field.Name
		index[field.Name] = i
		offset[field.Name] = i
	}
	return StructLayout{
		FieldOrder:  order,
		FieldIndex:  index,
		FieldOffset: offset,
		Size:        len(fields),
	}
}

func parseRuntimeInterfaceType(spec TypeSpec) (RuntimeType, error) {
	methods, ok := spec.InterfaceMethods()
	if !ok {
		return RuntimeType{}, fmt.Errorf("invalid interface type: %s", spec)
	}
	byName := make(map[string]typespec.Function, len(methods))
	names := make([]string, 0, len(methods))
	for _, method := range methods {
		byName[method.Name] = method.Sig
		names = append(names, method.Name)
	}
	sort.Strings(names)
	items := make([]RuntimeInterfaceMethod, 0, len(names))
	for index, name := range names {
		fn := byName[name]
		methodSpec := typespec.Func(fn.Params, fn.Return, fn.Variadic)
		fnSig, err := ParseRuntimeFuncSig(methodSpec)
		if err != nil {
			return RuntimeType{}, err
		}
		items = append(items, RuntimeInterfaceMethod{Index: index, Name: name, Spec: fnSig})
	}
	return RuntimeType{
		Kind:    RuntimeTypeInterface,
		Raw:     spec,
		TypeID:  CanonicalTypeID(spec.String()),
		Methods: items,
	}, nil
}
