package runtime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
)

type RuntimeTypeKind uint8

const (
	RuntimeTypeInvalid RuntimeTypeKind = iota
	RuntimeTypeVoid
	RuntimeTypeAny
	RuntimeTypePrimitive
	RuntimeTypeNamed
	RuntimeTypePointer
	RuntimeTypeArray
	RuntimeTypeMap
	RuntimeTypeTuple
	RuntimeTypeFunction
	RuntimeTypeStruct
	RuntimeTypeInterface
)

type TypeSpec string

func (s TypeSpec) String() string { return string(s) }
func (s TypeSpec) Ast() ast.GoMiniType {
	return ast.GoMiniType(s)
}
func (s TypeSpec) IsEmpty() bool                      { return s.Ast().IsEmpty() }
func (s TypeSpec) IsVoid() bool                       { return s.Ast().IsVoid() }
func (s TypeSpec) IsAny() bool                        { return s.Ast().IsAny() }
func (s TypeSpec) IsInt() bool                        { return s.Ast().IsInt() }
func (s TypeSpec) IsString() bool                     { return s.Ast().IsString() }
func (s TypeSpec) IsBool() bool                       { return s.Ast().IsBool() }
func (s TypeSpec) IsNumeric() bool                    { return s.Ast().IsNumeric() }
func (s TypeSpec) IsPtr() bool                        { return s.Ast().IsPtr() }
func (s TypeSpec) IsArray() bool                      { return s.Ast().IsArray() }
func (s TypeSpec) IsMap() bool                        { return s.Ast().IsMap() }
func (s TypeSpec) IsInterface() bool                  { return s.Ast().IsInterface() }
func (s TypeSpec) IsTuple() bool                      { return s.Ast().IsTuple() }
func (s TypeSpec) Equals(other TypeSpec) bool         { return s.Ast().Equals(other.Ast()) }
func (s TypeSpec) IsAssignableTo(other TypeSpec) bool { return s.Ast().IsAssignableTo(other.Ast()) }
func (s TypeSpec) ReadArrayItemType() (TypeSpec, bool) {
	elem, ok := s.Ast().ReadArrayItemType()
	return TypeSpec(elem), ok
}
func (s TypeSpec) GetMapKeyValueTypes() (TypeSpec, TypeSpec, bool) {
	key, value, ok := s.Ast().GetMapKeyValueTypes()
	return TypeSpec(key), TypeSpec(value), ok
}
func (s TypeSpec) ZeroVar() interface{} { return s.Ast().ZeroVar() }

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
		parsed, err := ParseRuntimeType(t.Raw.Ast())
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
		*t = RuntimeType(alias)
		if t.Kind == RuntimeTypeInvalid && !t.Raw.IsEmpty() {
			parsed, parseErr := ParseRuntimeType(t.Raw.Ast())
			if parseErr == nil {
				*t = parsed
			}
		}
		return nil
	}

	var raw ast.GoMiniType
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

// RuntimeStructSpec is the parsed FFI struct schema cached at registration time.
type RuntimeStructSpec struct {
	Name   string
	TypeID string
	Spec   TypeSpec

	TypeInfo RuntimeType
	Layout   StructLayout
	Fields   []RuntimeStructField
	ByName   map[string]RuntimeStructField
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

func CanonicalTypeID(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "*")
	if strings.HasPrefix(name, "Ptr<") && strings.HasSuffix(name, ">") {
		return CanonicalTypeID(name[4 : len(name)-1])
	}
	return name
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
	elemInfo, err := ParseRuntimeType(elem.Ast())
	if err != nil {
		return RuntimeType{}, false
	}
	return elemInfo, true
}

func (t RuntimeType) GetMapKeyValueTypes() (RuntimeType, RuntimeType, bool) {
	if t.Key != nil && t.Value != nil {
		return *t.Key, *t.Value, true
	}
	key, value, ok := t.Raw.GetMapKeyValueTypes()
	if !ok {
		return RuntimeType{}, RuntimeType{}, false
	}
	keyInfo, err := ParseRuntimeType(key.Ast())
	if err != nil {
		return RuntimeType{}, RuntimeType{}, false
	}
	valueInfo, err := ParseRuntimeType(value.Ast())
	if err != nil {
		return RuntimeType{}, RuntimeType{}, false
	}
	return keyInfo, valueInfo, true
}

func (t RuntimeType) ZeroVar() interface{} {
	return t.Raw.ZeroVar()
}

func ParseRuntimeType[S ~string](spec S) (RuntimeType, error) {
	specType := ast.GoMiniType(strings.TrimSpace(string(spec)))
	if specType.IsEmpty() || specType.IsVoid() {
		return RuntimeType{Kind: RuntimeTypeVoid, Raw: TypeSpec(specType)}, nil
	}
	if specType == ast.TypeAny || specType == ast.TypeModule || specType == ast.TypeClosure {
		return RuntimeType{Kind: RuntimeTypeAny, Raw: TypeSpec(specType), TypeID: CanonicalTypeID(string(specType))}, nil
	}
	if specType.IsPrimitive() {
		return RuntimeType{Kind: RuntimeTypePrimitive, Raw: TypeSpec(specType), TypeID: CanonicalTypeID(string(specType))}, nil
	}
	if specType.IsPtr() {
		elem, ok := specType.GetPtrElementType()
		if !ok {
			return RuntimeType{}, fmt.Errorf("invalid pointer type: %s", specType)
		}
		elemType, err := ParseRuntimeType(elem)
		if err != nil {
			return RuntimeType{}, err
		}
		return RuntimeType{
			Kind:   RuntimeTypePointer,
			Raw:    TypeSpec(specType),
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
			Raw:    TypeSpec(specType),
			TypeID: elemType.TypeID,
			Elem:   &elemType,
		}, nil
	}
	if specType.IsMap() {
		key, value, ok := specType.GetMapKeyValueTypes()
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
			Raw:    TypeSpec(specType),
			TypeID: valueType.TypeID,
			Key:    &keyType,
			Value:  &valueType,
		}, nil
	}
	if types, ok := specType.ReadTuple(); ok {
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
			Raw:    TypeSpec(specType),
			TypeID: CanonicalTypeID(string(specType)),
			Params: params,
		}, nil
	}
	if fn, ok := specType.ReadFunc(); ok {
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
			Raw:      TypeSpec(specType),
			TypeID:   CanonicalTypeID(string(specType)),
			Params:   params,
			Return:   &retType,
			Variadic: fn.Variadic,
		}, nil
	}
	if specType.IsStruct() {
		return parseRuntimeStructType(TypeSpec(specType))
	}
	if specType.IsInterface() {
		return parseRuntimeInterfaceType(TypeSpec(specType))
	}
	return RuntimeType{
		Kind:   RuntimeTypeNamed,
		Raw:    TypeSpec(specType),
		TypeID: CanonicalTypeID(string(specType)),
	}, nil
}

func ParseRuntimeFuncSig[S ~string](spec S) (*RuntimeFuncSig, error) {
	specType := ast.GoMiniType(spec)
	if specType.IsEmpty() {
		return nil, nil
	}
	fn, ok := specType.ReadFunc()
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
		names = append(names, string(p.Name))
	}
	retType, err := ParseRuntimeType(fn.Return)
	if err != nil {
		return nil, err
	}
	return &RuntimeFuncSig{
		Spec:       TypeSpec(specType),
		ParamNames: names,
		ParamTypes: params,
		ParamModes: defaultFFIParamModes(len(params)),
		ReturnType: retType,
		Variadic:   fn.Variadic,
	}, nil
}

func RuntimeFuncSigFromFunction(fn ast.FunctionType) (*RuntimeFuncSig, error) {
	params := make([]RuntimeType, 0, len(fn.Params))
	names := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		paramType, err := ParseRuntimeType(p.Type)
		if err != nil {
			return nil, err
		}
		params = append(params, paramType)
		names = append(names, string(p.Name))
	}
	retType, err := ParseRuntimeType(fn.Return)
	if err != nil {
		return nil, err
	}
	return &RuntimeFuncSig{
		Spec:       TypeSpec(fn.MiniType()),
		ParamNames: names,
		ParamTypes: params,
		ParamModes: defaultFFIParamModes(len(params)),
		ReturnType: retType,
		Variadic:   fn.Variadic,
	}, nil
}

func (s *RuntimeFuncSig) FunctionType() ast.FunctionType {
	if s == nil {
		return ast.FunctionType{}
	}
	params := make([]ast.FunctionParam, 0, len(s.ParamTypes))
	for i, paramType := range s.ParamTypes {
		name := ""
		if i < len(s.ParamNames) {
			name = s.ParamNames[i]
		}
		params = append(params, ast.FunctionParam{
			Name: ast.Ident(name),
			Type: paramType.Raw.Ast(),
		})
	}
	return ast.FunctionType{
		Params:   params,
		Return:   s.ReturnType.Raw.Ast(),
		Variadic: s.Variadic,
	}
}

func (s *RuntimeFuncSig) SignatureString() string {
	if s == nil {
		return ""
	}
	if !s.Spec.IsEmpty() {
		return string(s.Spec)
	}
	fn := s.FunctionType()
	return string(fn.MiniType())
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

func ParseRuntimeStructSpec[S ~string](name string, spec S) (*RuntimeStructSpec, error) {
	specType := ast.GoMiniType(spec)
	if specType.IsEmpty() {
		return nil, nil
	}
	typeInfo, err := parseRuntimeStructType(TypeSpec(specType))
	if err != nil {
		return nil, fmt.Errorf("invalid struct spec for %s: %w", name, err)
	}

	fields := make([]RuntimeStructField, len(typeInfo.Fields))
	byName := make(map[string]RuntimeStructField, len(typeInfo.Fields))
	copy(fields, typeInfo.Fields)
	for _, field := range fields {
		byName[field.Name] = field
	}
	layout := buildStructLayout(fields)

	return &RuntimeStructSpec{
		Name:     name,
		TypeID:   CanonicalTypeID(name),
		Spec:     TypeSpec(specType),
		TypeInfo: typeInfo,
		Layout:   layout,
		Fields:   fields,
		ByName:   byName,
	}, nil
}

func ParseRuntimeInterfaceSpec[S ~string](spec S) (*RuntimeInterfaceSpec, error) {
	specType := ast.GoMiniType(spec)
	if specType.IsEmpty() {
		return nil, nil
	}
	typeInfo, err := parseRuntimeInterfaceType(TypeSpec(specType))
	if err != nil {
		return nil, err
	}

	methods := make([]RuntimeInterfaceMethod, len(typeInfo.Methods))
	byName := make(map[string]*RuntimeFuncSig, len(typeInfo.Methods))
	methodIndex := make(map[string]int, len(typeInfo.Methods))
	for i, method := range typeInfo.Methods {
		fnSig, err := ParseRuntimeFuncSig(method.Spec.Spec.Ast())
		if err != nil {
			return nil, err
		}
		methods[i] = RuntimeInterfaceMethod{Index: i, Name: method.Name, Spec: fnSig}
		byName[method.Name] = fnSig
		methodIndex[method.Name] = i
	}

	return &RuntimeInterfaceSpec{
		TypeID:      typeInfo.TypeID,
		Spec:        TypeSpec(specType),
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

func MustRuntimeFuncSigFromFunction(fn ast.FunctionType) *RuntimeFuncSig {
	sig, err := RuntimeFuncSigFromFunction(fn)
	if err != nil {
		panic(err)
	}
	return sig
}

func MustParseRuntimeFuncSigWithModes[S ~string](spec S, modes ...FFIParamMode) *RuntimeFuncSig {
	return CloneRuntimeFuncSigWithParamModes(MustParseRuntimeFuncSig(spec), modes...)
}

func MustParseRuntimeStructSpec[S ~string](name string, spec S) *RuntimeStructSpec {
	parsed, err := ParseRuntimeStructSpec(name, spec)
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
	raw := strings.TrimSpace(spec.String())
	start := strings.Index(raw, "{")
	if start == -1 || !strings.HasSuffix(raw, "}") {
		return RuntimeType{}, fmt.Errorf("malformed struct type: %s", spec)
	}
	inner := raw[start+1 : len(raw)-1]
	parts := strings.Split(inner, ";")
	fields := make([]RuntimeStructField, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items := strings.SplitN(part, " ", 2)
		if len(items) != 2 {
			return RuntimeType{}, fmt.Errorf("invalid struct field: %s", part)
		}
		fieldType := TypeSpec(strings.TrimSpace(items[1]))
		typeInfo, err := ParseRuntimeType(fieldType)
		if err != nil {
			return RuntimeType{}, err
		}
		fields = append(fields, RuntimeStructField{
			Name:     strings.TrimSpace(items[0]),
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
	methods, ok := spec.Ast().ReadInterfaceMethods()
	if !ok {
		return RuntimeType{}, fmt.Errorf("invalid interface type: %s", spec)
	}
	names := make([]string, 0, len(methods))
	for name := range methods {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]RuntimeInterfaceMethod, 0, len(names))
	for index, name := range names {
		fn := methods[name]
		if fn == nil {
			continue
		}
		methodSpec := fn.MiniType()
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
