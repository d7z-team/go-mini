package ast

import (
	"fmt"
	"strings"

	"gopkg.d7z.net/go-mini/core/typespec"
)

// GoMiniType 类型的表达形式
type GoMiniType string

const (
	TypeInt64   GoMiniType = "Int64"
	TypeFloat64 GoMiniType = "Float64"
	TypeString  GoMiniType = "String"
	TypeBool    GoMiniType = "Bool"
	TypeByte    GoMiniType = "Byte"
	TypeRune    GoMiniType = "Rune"
	TypeAny     GoMiniType = "Any"
	TypeError   GoMiniType = "Error"
	TypeVoid    GoMiniType = "Void"
	TypeModule  GoMiniType = "TypeModule" // 动态模块对象 (JS/Python 风格)
	TypeClosure GoMiniType = "TypeClosure"
)

func (o GoMiniType) IsEmpty() bool { return o == "" }

// BaseName 提取类型的基准名称 (例如 Ptr<MyStruct> -> MyStruct)
func (o GoMiniType) BaseName() string {
	return typespec.Type(o).BaseName().String()
}

func (o GoMiniType) IsVoid() bool {
	return typespec.Type(o).IsVoid()
}

func (o GoMiniType) IsPrimitive() bool {
	return typespec.Type(o).IsPrimitive()
}

func (o GoMiniType) ReadCallFunc() (*CallFunctionType, bool) {
	fn, ok := o.ReadFunc()
	if !ok {
		return nil, false
	}
	res := fn.ToCallFunctionType()
	return &res, true
}

func (o GoMiniType) IsAny() bool     { return typespec.Type(o).IsAny() }
func (o GoMiniType) IsModule() bool  { return typespec.Type(o).IsModule() }
func (o GoMiniType) IsClosure() bool { return typespec.Type(o).IsClosure() }
func (o GoMiniType) IsString() bool  { return typespec.Type(o).IsString() }
func (o GoMiniType) IsInt() bool     { return typespec.Type(o).IsInt() }
func (o GoMiniType) IsByte() bool    { return typespec.Type(o).IsByte() }
func (o GoMiniType) IsRune() bool    { return typespec.Type(o).IsRune() }
func (o GoMiniType) IsBool() bool    { return typespec.Type(o).IsBool() }
func (o GoMiniType) IsNumeric() bool {
	return typespec.Type(o).IsNumeric()
}

func (o GoMiniType) IsPtr() bool {
	return typespec.Type(o).IsPtr()
}

func (o GoMiniType) IsHostRef() bool {
	return typespec.Type(o).IsHostRef()
}

func (o GoMiniType) IsChan() bool {
	return typespec.Type(o).IsChan()
}

func (o GoMiniType) IsRecvChan() bool {
	return typespec.Type(o).IsRecvChan()
}

func (o GoMiniType) IsSendChan() bool {
	return typespec.Type(o).IsSendChan()
}

func (o GoMiniType) IsArray() bool {
	return typespec.Type(o).IsArray()
}

func (o GoMiniType) IsInterface() bool {
	return typespec.Type(o).IsInterface()
}

func (o GoMiniType) IsStruct() bool {
	return typespec.Type(o).IsStruct()
}

func (o GoMiniType) ReadStructFields() (map[string]GoMiniType, bool) {
	fields, ok := typespec.Type(o).StructFields()
	if !ok {
		return nil, false
	}
	res := make(map[string]GoMiniType, len(fields))
	for _, field := range fields {
		res[field.Name] = GoMiniType(field.Type)
	}
	return res, true
}

func (o GoMiniType) ReadStructFieldList() ([]StructMemberType, bool) {
	fields, ok := typespec.Type(o).StructFields()
	if !ok {
		return nil, false
	}
	res := make([]StructMemberType, 0, len(fields))
	for _, field := range fields {
		res = append(res, StructMemberType{Name: field.Name, Type: GoMiniType(field.Type)})
	}
	return res, true
}

func (o GoMiniType) ReadInterfaceMethods() (map[string]*FunctionType, bool) {
	methods, ok := typespec.Type(o).InterfaceMethods()
	if !ok {
		return nil, false
	}
	res := make(map[string]*FunctionType, len(methods))
	for _, method := range methods {
		fn := functionFromTypeSpec(method.Sig)
		res[method.Name] = &fn
	}
	return res, true
}

func (o GoMiniType) IsMap() bool {
	return typespec.Type(o).IsMap()
}

func (o GoMiniType) ReadArrayItemType() (GoMiniType, bool) {
	elem, ok := typespec.Type(o).ReadArrayItemType()
	return GoMiniType(elem), ok
}

func CreateArrayType(elementType GoMiniType) GoMiniType {
	return GoMiniType(typespec.Array(typespec.Type(elementType)))
}

func (o GoMiniType) GetPtrElementType() (GoMiniType, bool) {
	elem, ok := typespec.Type(o).PtrElement()
	return GoMiniType(elem), ok
}

func (o GoMiniType) ToPtr() GoMiniType {
	return GoMiniType(typespec.Ptr(typespec.Type(o)))
}

func (o GoMiniType) GetHostRefElementType() (GoMiniType, bool) {
	elem, ok := typespec.Type(o).HostRefElement()
	return GoMiniType(elem), ok
}

func (o GoMiniType) ToHostRef() GoMiniType {
	return GoMiniType(typespec.HostRef(typespec.Type(o)))
}

func (o GoMiniType) ReadChanElemType() (GoMiniType, bool) {
	elem, ok := typespec.Type(o).ChanElement()
	return GoMiniType(elem), ok
}

func CreateChanType(elementType GoMiniType) GoMiniType {
	return GoMiniType(typespec.Chan(typespec.Type(elementType)))
}

func CreateRecvChanType(elementType GoMiniType) GoMiniType {
	return GoMiniType(typespec.RecvChan(typespec.Type(elementType)))
}

func CreateSendChanType(elementType GoMiniType) GoMiniType {
	return GoMiniType(typespec.SendChan(typespec.Type(elementType)))
}

func (o GoMiniType) GetMapKeyValueTypes() (keyType, valueType GoMiniType, ok bool) {
	key, value, ok := typespec.Type(o).MapTypes()
	return GoMiniType(key), GoMiniType(value), ok
}

func CreateMapType(keyType, valueType GoMiniType) GoMiniType {
	return GoMiniType(typespec.Map(typespec.Type(keyType), typespec.Type(valueType)))
}

func (o GoMiniType) ReadFunc() (*FunctionType, bool) {
	fn, ok := typespec.Type(o).Function()
	if !ok {
		return nil, false
	}
	res := functionFromTypeSpec(fn)
	return &res, true
}

type FunctionType struct {
	Params   []FunctionParam `json:"params,omitempty"`
	Return   GoMiniType      `json:"return"`
	Variadic bool            `json:"variadic,omitempty"`
}

func (ft *FunctionType) MiniType() GoMiniType {
	return CreateFunctionType(ft.Params, ft.Return, ft.Variadic)
}

type FunctionParam struct {
	Name Ident
	Type GoMiniType
}

type CallFunctionType struct {
	Params   []GoMiniType
	Returns  GoMiniType
	Doc      string
	Variadic bool
}

func (c CallFunctionType) String() string {
	params := make([]FunctionParam, 0, len(c.Params))
	for _, p := range c.Params {
		params = append(params, FunctionParam{Type: p})
	}
	return string(CreateFunctionType(params, c.Returns, c.Variadic))
}

func (c CallFunctionType) MiniType() GoMiniType { return GoMiniType(c.String()) }

func (o GoMiniType) ReadTuple() ([]GoMiniType, bool) {
	items, ok := typespec.Type(o).TupleTypes()
	if !ok {
		return nil, false
	}
	res := make([]GoMiniType, 0, len(items))
	for _, item := range items {
		res = append(res, GoMiniType(item))
	}
	return res, true
}

func (ft *FunctionType) ToCallFunctionType() CallFunctionType {
	var callParams []GoMiniType
	for _, param := range ft.Params {
		callParams = append(callParams, param.Type)
	}
	return CallFunctionType{Params: callParams, Returns: ft.Return, Variadic: ft.Variadic}
}

func (o GoMiniType) Equals(other GoMiniType) bool {
	return typespec.Type(o).Equals(typespec.Type(other))
}

func (o GoMiniType) IsTuple() bool {
	return typespec.Type(o).IsTuple()
}

func (o GoMiniType) StructName() (Ident, bool) {
	s := string(o)
	if strings.Contains(s, "(") || strings.Contains(s, "<") {
		return "", false
	}
	// 排除基础类型
	switch s {
	case "Any", "String", "Int64", "Float64", "Bool", "Byte", "Rune", "Void":
		return "", false
	}
	return Ident(s), true
}

func CreateTupleType(types ...GoMiniType) GoMiniType {
	items := make([]typespec.Type, 0, len(types))
	for _, typ := range types {
		items = append(items, typespec.Type(typ))
	}
	return GoMiniType(typespec.Tuple(items...))
}

func (o GoMiniType) IsCanonical() bool {
	return typespec.Type(o).IsCanonical()
}

func (o GoMiniType) ValidateCanonical() error {
	return typespec.Type(o).ValidateCanonical()
}

func (o GoMiniType) Resolve(v *ValidContext) GoMiniType {
	if o.IsEmpty() {
		return o
	}

	// 1. 处理已有的规范化逻辑
	if o.IsAny() || o == TypeVoid || o == TypeError || o.IsModule() || o.IsClosure() || o.IsNumeric() || o.IsString() || o.IsBool() {
		return o
	}
	if o.IsArray() {
		elem, _ := o.ReadArrayItemType()
		return CreateArrayType(elem.Resolve(v))
	}
	if o.IsMap() {
		k, val, _ := o.GetMapKeyValueTypes()
		return CreateMapType(k.Resolve(v), val.Resolve(v))
	}
	if o.IsPtr() {
		elem, _ := o.GetPtrElementType()
		resolved := elem.Resolve(v)
		if resolved.IsHostRef() {
			return resolved
		}
		if v != nil && v.IsHostOpaqueNamedType(resolved) {
			return resolved.ToHostRef()
		}
		return resolved.ToPtr()
	}
	if o.IsHostRef() {
		elem, _ := o.GetHostRefElementType()
		resolved := elem.Resolve(v)
		if resolved.IsHostRef() {
			return resolved
		}
		return resolved.ToHostRef()
	}
	if o.IsChan() {
		elem, _ := o.ReadChanElemType()
		resolved := elem.Resolve(v)
		switch {
		case o.IsRecvChan():
			return CreateRecvChanType(resolved)
		case o.IsSendChan():
			return CreateSendChanType(resolved)
		default:
			return CreateChanType(resolved)
		}
	}
	if o.IsTuple() {
		types, _ := o.ReadTuple()
		resolved := make([]GoMiniType, len(types))
		for i, t := range types {
			resolved[i] = t.Resolve(v)
		}
		return CreateTupleType(resolved...)
	}
	if fn, ok := o.ReadFunc(); ok {
		params := make([]FunctionParam, len(fn.Params))
		for i, p := range fn.Params {
			params[i] = FunctionParam{Name: p.Name, Type: p.Type.Resolve(v)}
		}
		return CreateFunctionType(params, fn.Return.Resolve(v), fn.Variadic)
	}
	if o.IsInterface() {
		methods, ok := o.ReadInterfaceMethods()
		if !ok {
			return o
		}
		resolved := make(map[string]*FunctionType, len(methods))
		for name, sig := range methods {
			if sig == nil {
				continue
			}
			params := make([]FunctionParam, len(sig.Params))
			for i, p := range sig.Params {
				params[i] = FunctionParam{Name: p.Name, Type: p.Type.Resolve(v)}
			}
			resolved[name] = &FunctionType{
				Params:   params,
				Return:   sig.Return.Resolve(v),
				Variadic: sig.Variadic,
			}
		}
		return CreateInterfaceType(resolved)
	}

	s := string(o)
	if strings.Contains(s, ".") {
		resolved := o
		if v != nil && v.root != nil {
			if prefix, member, ok := splitQualifiedMember(s); ok {
				if realPkg, ok := v.root.Imports[prefix]; ok {
					resolved = GoMiniType(fmt.Sprintf("%s.%s", realPkg, member))
				}
			}
			if actual, ok := v.GetType(Ident(resolved)); ok && !actual.IsStruct() {
				if actual == resolved {
					return actual
				}
				if actual.IsHostRef() {
					if elem, ok := actual.GetHostRefElementType(); ok && elem == resolved {
						return actual
					}
				}
				return actual.Resolve(v)
			}
		}
		// 如果前缀不在导入表中，可能是 FFI 或动态注入的包名，保留原始形式
		return resolved
	}
	if v != nil && v.root != nil {
		if v.root.program != nil && v.root.program.Structs[Ident(o)] != nil {
			return GoMiniType(v.QualifiedTypeName(Ident(o)))
		}
		if v.root.program != nil && v.root.program.Interfaces[Ident(o)] != nil {
			return GoMiniType(v.QualifiedTypeName(Ident(o)))
		}
		if actual, ok := v.root.types[Ident(o)]; ok {
			if actual == o {
				return actual
			}
			if actual.IsHostRef() {
				if elem, ok := actual.GetHostRefElementType(); ok && elem == o {
					return actual
				}
			}
			return actual.Resolve(v)
		}
	}
	return o
}

func (o GoMiniType) Valid(v *ValidContext) bool {
	if !o.IsCanonical() {
		return false
	}
	if o.IsAny() || o == TypeVoid || o == TypeError || o.IsModule() || o.IsClosure() || o.IsNumeric() || o.IsString() || o.IsBool() {
		return true
	}
	if o.IsArray() {
		elem, ok := o.ReadArrayItemType()
		return ok && elem.Resolve(v).Valid(v)
	}
	if o.IsPtr() {
		elem, ok := o.GetPtrElementType()
		return ok && elem.Resolve(v).Valid(v)
	}
	if o.IsHostRef() {
		elem, ok := o.GetHostRefElementType()
		if !ok {
			return false
		}
		resolved := elem.Resolve(v)
		if resolved.IsHostRef() {
			hostElem, ok := resolved.GetHostRefElementType()
			if !ok {
				return false
			}
			if v == nil {
				return hostElem.IsCanonical()
			}
			if _, ok := v.GetType(Ident(hostElem)); ok {
				return true
			}
			if _, ok := v.GetStruct(Ident(hostElem)); ok {
				return true
			}
			if _, ok := v.GetInterface(Ident(hostElem)); ok {
				return true
			}
			return false
		}
		return resolved.Valid(v)
	}
	if o.IsChan() {
		elem, ok := o.ReadChanElemType()
		return ok && elem.Resolve(v).Valid(v)
	}
	if o.IsTuple() {
		types, ok := o.ReadTuple()
		if !ok {
			return false
		}
		for _, t := range types {
			if !t.Resolve(v).Valid(v) {
				return false
			}
		}
		return true
	}
	if o.IsInterface() {
		return true
	}
	if fn, ok := o.ReadFunc(); ok {
		if !fn.Return.Resolve(v).Valid(v) {
			return false
		}
		for _, p := range fn.Params {
			if !p.Type.Resolve(v).Valid(v) {
				return false
			}
		}
		return true
	}
	if _, ok := v.GetType(Ident(o)); ok {
		return true
	}
	if _, ok := v.GetInterface(Ident(o)); ok {
		return true
	}
	_, ok := v.GetStruct(Ident(o))
	return ok
}

func (ft *FunctionType) String() string {
	return string(CreateFunctionType(ft.Params, ft.Return, ft.Variadic))
}

func (o GoMiniType) IsAssignableTo(target GoMiniType) bool {
	return typespec.Type(o).IsAssignableTo(typespec.Type(target))
}

// IsValid 检查类型字符串是否符合规范（基础类型、规范容器格式或带点包路径）
func (o GoMiniType) IsValid() bool {
	return o.IsCanonical()
}

// IsStrictValid 严格检查类型是否为预定义的原语或规范容器格式
func (o GoMiniType) IsStrictValid() bool {
	return o.IsCanonical()
}

// ZeroVar 返回该类型的默认零值（以 Var 形式）
// 注意：该方法返回的是一个简单的值对象，复合类型返回 nil Ref 的 Var
func (o GoMiniType) ZeroVar() interface{} {
	return typespec.Type(o).ZeroValue()
}

func CreateFunctionType(params []FunctionParam, ret GoMiniType, variadic bool) GoMiniType {
	items := make([]typespec.Param, 0, len(params))
	for _, param := range params {
		items = append(items, typespec.Param{Name: string(param.Name), Type: typespec.Type(param.Type)})
	}
	return GoMiniType(typespec.Func(items, typespec.Type(ret), variadic))
}

func CreateInterfaceType(methods map[string]*FunctionType) GoMiniType {
	items := make(map[string]typespec.Function, len(methods))
	for name, method := range methods {
		if method == nil {
			continue
		}
		items[name] = functionToTypeSpec(*method)
	}
	return GoMiniType(typespec.Interface(typespec.SortedMethods(items)))
}

func CreateStructType(fields []StructMemberType) GoMiniType {
	members := make([]typespec.Member, 0, len(fields))
	for _, field := range fields {
		members = append(members, typespec.Member{Name: field.Name, Type: typespec.Type(field.Type)})
	}
	return GoMiniType(typespec.Struct(members))
}

func CreateQualifiedType(owner, name string) GoMiniType {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" {
		return GoMiniType(name)
	}
	if name == "" {
		return GoMiniType(owner)
	}
	return GoMiniType(fmt.Sprintf("%s.%s", owner, name))
}

type StructMemberType struct {
	Name string
	Type GoMiniType
}

func functionFromTypeSpec(fn typespec.Function) FunctionType {
	params := make([]FunctionParam, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, FunctionParam{Name: Ident(param.Name), Type: GoMiniType(param.Type)})
	}
	return FunctionType{Params: params, Return: GoMiniType(fn.Return), Variadic: fn.Variadic}
}

func functionToTypeSpec(fn FunctionType) typespec.Function {
	params := make([]typespec.Param, 0, len(fn.Params))
	for _, param := range fn.Params {
		params = append(params, typespec.Param{Name: string(param.Name), Type: typespec.Type(param.Type)})
	}
	return typespec.Function{Params: params, Return: typespec.Type(fn.Return), Variadic: fn.Variadic}
}
