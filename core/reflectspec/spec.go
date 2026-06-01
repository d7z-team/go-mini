package reflectspec

import "gopkg.d7z.net/go-mini/core/typespec"

const (
	PackagePath = "reflect"

	TypeType        typespec.Type = "reflect.Type"
	StructFieldType typespec.Type = "reflect.StructField"
	MethodType      typespec.Type = "reflect.Method"
	RouteType       typespec.Type = "reflect.Route"
	PackageInfoType typespec.Type = "reflect.PackageInfo"
	MemberType      typespec.Type = "reflect.Member"
)

const (
	RouteTypeOf        = "reflect.TypeOf"
	RouteTypeFrom      = "reflect.TypeFrom"
	RouteKindOf        = "reflect.KindOf"
	RouteKindOfType    = "reflect.KindOfType"
	RouteFields        = "reflect.Fields"
	RouteFieldsOfType  = "reflect.FieldsOfType"
	RouteField         = "reflect.Field"
	RouteSetField      = "reflect.SetField"
	RouteZero          = "reflect.Zero"
	RouteMethods       = "reflect.Methods"
	RouteMethodsOfType = "reflect.MethodsOfType"
	RouteIsNil         = "reflect.IsNil"
	RouteIsStruct      = "reflect.IsStruct"
	RouteIsPtr         = "reflect.IsPtr"
	RouteIsHostRef     = "reflect.IsHostRef"
	RouteIsChan        = "reflect.IsChan"
	RouteIsFunc        = "reflect.IsFunc"
	RouteIsFFIFunc     = "reflect.IsFFIFunc"
	RouteIsVMFunc      = "reflect.IsVMFunc"
	RouteIsNativeFunc  = "reflect.IsNativeFunc"
	RoutePackage       = "reflect.Package"
	RoutePackages      = "reflect.Packages"
	RouteMembers       = "reflect.Members"
	RouteMemberByName  = "reflect.MemberByName"
	RouteLen           = "reflect.Len"
	RouteIndex         = "reflect.Index"
	RouteMapKeys       = "reflect.MapKeys"
	RouteMapIndex      = "reflect.MapIndex"
	RouteMakeMap       = "reflect.MakeMap"
	RouteSetMapIndex   = "reflect.SetMapIndex"
	RouteUnwrap        = "reflect.Unwrap"
	RouteAssign        = "reflect.Assign"
	RouteAppend        = "reflect.Append"

	RouteTypeString       = "reflect.Type.String"
	RouteTypeKind         = "reflect.Type.Kind"
	RouteTypeName         = "reflect.Type.Name"
	RouteTypePkgPath      = "reflect.Type.PkgPath"
	RouteTypeElem         = "reflect.Type.Elem"
	RouteTypeKey          = "reflect.Type.Key"
	RouteTypeAssignableTo = "reflect.Type.AssignableTo"
	RouteTypeComparable   = "reflect.Type.Comparable"
	RouteTypeNumField     = "reflect.Type.NumField"
	RouteTypeField        = "reflect.Type.Field"
	RouteTypeFieldByName  = "reflect.Type.FieldByName"
	RouteTypeNumMethod    = "reflect.Type.NumMethod"
	RouteTypeMethod       = "reflect.Type.Method"
	RouteTypeMethodByName = "reflect.Type.MethodByName"
	RouteTypeNumIn        = "reflect.Type.NumIn"
	RouteTypeIn           = "reflect.Type.In"
	RouteTypeNumOut       = "reflect.Type.NumOut"
	RouteTypeOut          = "reflect.Type.Out"
	RouteTypeIsVariadic   = "reflect.Type.IsVariadic"
)

type Route struct {
	PackagePath string
	MemberName  string
	TypeOwner   Owner
	MethodName  string
	RouteName   string
	MethodID    uint32
	Return      typespec.Type
	Params      []typespec.Type
	RawArgs     []int
	Doc         string
}

type Owner struct {
	PackagePath string
	MemberName  string
}

type Struct struct {
	Owner   Owner
	Name    typespec.Type
	Members []typespec.Member
}

func TypeOwner(member string) Owner {
	return Owner{PackagePath: PackagePath, MemberName: member}
}

func (o Owner) TypeName() typespec.Type {
	if o.PackagePath == "" || o.MemberName == "" {
		return ""
	}
	return typespec.Type(o.PackagePath + "." + o.MemberName)
}

func (r Route) Signature() typespec.Type {
	params := make([]typespec.Param, len(r.Params))
	for i, param := range r.Params {
		params[i] = typespec.Param{Type: param}
	}
	return typespec.Func(params, r.Return, false)
}

func (r Route) HasRawArg(idx int) bool {
	for _, raw := range r.RawArgs {
		if raw == idx {
			return true
		}
	}
	return false
}

func Structs() []Struct {
	typeMembers := []typespec.Member{{Name: "Raw", Type: typespec.String}}
	for _, route := range TypeMethods() {
		typeMembers = append(typeMembers, typespec.Member{Name: route.MethodName, Type: route.Signature()})
	}
	return []Struct{
		{
			Owner:   TypeOwner("Type"),
			Name:    TypeType,
			Members: typeMembers,
		},
		{
			Owner: TypeOwner("StructField"),
			Name:  StructFieldType,
			Members: []typespec.Member{
				{Name: "Name", Type: typespec.String},
				{Name: "Type", Type: TypeType},
				{Name: "Kind", Type: typespec.Int64},
				{Name: "Index", Type: typespec.Int64},
				{Name: "Tag", Type: typespec.String},
				{Name: "Exported", Type: typespec.Bool},
				{Name: "Anonymous", Type: typespec.Bool},
			},
		},
		{
			Owner: TypeOwner("Method"),
			Name:  MethodType,
			Members: []typespec.Member{
				{Name: "Name", Type: typespec.String},
				{Name: "Type", Type: TypeType},
				{Name: "Index", Type: typespec.Int64},
				{Name: "Route", Type: RouteType},
				{Name: "IsFFI", Type: typespec.Bool},
				{Name: "IsNative", Type: typespec.Bool},
			},
		},
		{
			Owner: TypeOwner("Route"),
			Name:  RouteType,
			Members: []typespec.Member{
				{Name: "Name", Type: typespec.String},
				{Name: "MethodID", Type: typespec.Int64},
				{Name: "Type", Type: TypeType},
				{Name: "IsFFI", Type: typespec.Bool},
				{Name: "IsNative", Type: typespec.Bool},
			},
		},
		{
			Owner: TypeOwner("PackageInfo"),
			Name:  PackageInfoType,
			Members: []typespec.Member{
				{Name: "Path", Type: typespec.String},
			},
		},
		{
			Owner: TypeOwner("Member"),
			Name:  MemberType,
			Members: []typespec.Member{
				{Name: "Name", Type: typespec.String},
				{Name: "Kind", Type: typespec.String},
				{Name: "Type", Type: TypeType},
				{Name: "ReadOnly", Type: typespec.Bool},
				{Name: "Route", Type: RouteType},
			},
		},
	}
}

func PackageFunctions() []Route {
	return []Route{
		{PackagePath: PackagePath, MemberName: "TypeOf", RouteName: RouteTypeOf, MethodID: 1, Return: TypeType, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return schema type metadata for a VM value"},
		{PackagePath: PackagePath, MemberName: "TypeFrom", RouteName: RouteTypeFrom, MethodID: 2, Return: typespec.Tuple(TypeType, typespec.Bool), Params: []typespec.Type{typespec.String}, Doc: "Resolve type metadata by canonical type name"},
		{PackagePath: PackagePath, MemberName: "KindOf", RouteName: RouteKindOf, MethodID: 3, Return: typespec.Int64, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return the reflect kind for a VM value"},
		{PackagePath: PackagePath, MemberName: "KindOfType", RouteName: RouteKindOfType, MethodID: 4, Return: typespec.Int64, Params: []typespec.Type{TypeType}, Doc: "Return the reflect kind for a type"},
		{PackagePath: PackagePath, MemberName: "Fields", RouteName: RouteFields, MethodID: 5, Return: typespec.Array(StructFieldType), Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return struct field metadata for a value"},
		{PackagePath: PackagePath, MemberName: "FieldsOfType", RouteName: RouteFieldsOfType, MethodID: 6, Return: typespec.Array(StructFieldType), Params: []typespec.Type{TypeType}, Doc: "Return struct field metadata for a type"},
		{PackagePath: PackagePath, MemberName: "Field", RouteName: RouteField, MethodID: 7, Return: typespec.Tuple(typespec.Any, typespec.Bool), Params: []typespec.Type{typespec.Any, typespec.String}, RawArgs: []int{0}, Doc: "Read a pure-value struct field by name"},
		{PackagePath: PackagePath, MemberName: "SetField", RouteName: RouteSetField, MethodID: 8, Return: typespec.Error, Params: []typespec.Type{typespec.Any, typespec.String, typespec.Any}, RawArgs: []int{0, 2}, Doc: "Assign a struct field on a pointer or Any-wrapped struct value"},
		{PackagePath: PackagePath, MemberName: "Zero", RouteName: RouteZero, MethodID: 9, Return: typespec.Any, Params: []typespec.Type{typespec.String}, Doc: "Create a pure Any zero value for a type"},
		{PackagePath: PackagePath, MemberName: "Methods", RouteName: RouteMethods, MethodID: 10, Return: typespec.Array(MethodType), Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return method metadata for a value"},
		{PackagePath: PackagePath, MemberName: "MethodsOfType", RouteName: RouteMethodsOfType, MethodID: 11, Return: typespec.Array(MethodType), Params: []typespec.Type{TypeType}, Doc: "Return method metadata for a type"},
		{PackagePath: PackagePath, MemberName: "IsNil", RouteName: RouteIsNil, MethodID: 12, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is nil"},
		{PackagePath: PackagePath, MemberName: "IsStruct", RouteName: RouteIsStruct, MethodID: 13, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a struct"},
		{PackagePath: PackagePath, MemberName: "IsPtr", RouteName: RouteIsPtr, MethodID: 14, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a VM pointer"},
		{PackagePath: PackagePath, MemberName: "IsHostRef", RouteName: RouteIsHostRef, MethodID: 15, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a host reference"},
		{PackagePath: PackagePath, MemberName: "IsChan", RouteName: RouteIsChan, MethodID: 16, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a channel"},
		{PackagePath: PackagePath, MemberName: "IsFunc", RouteName: RouteIsFunc, MethodID: 17, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is callable"},
		{PackagePath: PackagePath, MemberName: "IsFFIFunc", RouteName: RouteIsFFIFunc, MethodID: 18, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is an FFI function"},
		{PackagePath: PackagePath, MemberName: "IsVMFunc", RouteName: RouteIsVMFunc, MethodID: 19, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a VM function"},
		{PackagePath: PackagePath, MemberName: "IsNativeFunc", RouteName: RouteIsNativeFunc, MethodID: 20, Return: typespec.Bool, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Report whether a VM value is a native core function"},
		{PackagePath: PackagePath, MemberName: "Package", RouteName: RoutePackage, MethodID: 21, Return: typespec.Tuple(PackageInfoType, typespec.Bool), Params: []typespec.Type{typespec.String}, Doc: "Resolve an FFI package by import path"},
		{PackagePath: PackagePath, MemberName: "Packages", RouteName: RoutePackages, MethodID: 22, Return: typespec.Array(PackageInfoType), Doc: "List registered FFI packages"},
		{PackagePath: PackagePath, MemberName: "Members", RouteName: RouteMembers, MethodID: 23, Return: typespec.Array(MemberType), Params: []typespec.Type{PackageInfoType}, Doc: "List registered FFI package members"},
		{PackagePath: PackagePath, MemberName: "MemberByName", RouteName: RouteMemberByName, MethodID: 24, Return: typespec.Tuple(MemberType, typespec.Bool), Params: []typespec.Type{PackageInfoType, typespec.String}, Doc: "Resolve an FFI package member by name"},
		{PackagePath: PackagePath, MemberName: "Len", RouteName: RouteLen, MethodID: 25, Return: typespec.Int64, Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return length for a VM string, bytes, array, map, or channel value"},
		{PackagePath: PackagePath, MemberName: "Index", RouteName: RouteIndex, MethodID: 26, Return: typespec.Tuple(typespec.Any, typespec.Bool), Params: []typespec.Type{typespec.Any, typespec.Int64}, RawArgs: []int{0}, Doc: "Read a pure-value array, string, or bytes item by index"},
		{PackagePath: PackagePath, MemberName: "MapKeys", RouteName: RouteMapKeys, MethodID: 27, Return: typespec.Tuple(typespec.Array(typespec.Any), typespec.Bool), Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return pure-value map keys as a snapshot"},
		{PackagePath: PackagePath, MemberName: "MapIndex", RouteName: RouteMapIndex, MethodID: 28, Return: typespec.Tuple(typespec.Any, typespec.Bool), Params: []typespec.Type{typespec.Any, typespec.Any}, RawArgs: []int{0, 1}, Doc: "Read a pure-value map item by key"},
		{PackagePath: PackagePath, MemberName: "MakeMap", RouteName: RouteMakeMap, MethodID: 29, Return: typespec.Tuple(typespec.Any, typespec.Bool), Params: []typespec.Type{typespec.String}, Doc: "Create an empty pure Any map for a canonical map type"},
		{PackagePath: PackagePath, MemberName: "SetMapIndex", RouteName: RouteSetMapIndex, MethodID: 30, Return: typespec.Error, Params: []typespec.Type{typespec.Any, typespec.Any, typespec.Any}, RawArgs: []int{0, 1, 2}, Doc: "Assign a pure-value map entry using VM map assignment rules"},
		{PackagePath: PackagePath, MemberName: "Unwrap", RouteName: RouteUnwrap, MethodID: 31, Return: typespec.Tuple(typespec.Any, typespec.Bool), Params: []typespec.Type{typespec.Any}, RawArgs: []int{0}, Doc: "Return the pure value inside Any or interface wrappers"},
		{PackagePath: PackagePath, MemberName: "Assign", RouteName: RouteAssign, MethodID: 32, Return: typespec.Error, Params: []typespec.Type{typespec.Any, typespec.Any}, RawArgs: []int{0, 1}, Doc: "Assign a VM value into a writable target using normal VM assignment rules"},
		{PackagePath: PackagePath, MemberName: "Append", RouteName: RouteAppend, MethodID: 33, Return: typespec.Error, Params: []typespec.Type{typespec.Any, typespec.Any}, RawArgs: []int{0, 1}, Doc: "Append a value into an array target using normal VM append and assignment rules"},
	}
}

func TypeMethods() []Route {
	typeOwner := TypeOwner("Type")
	return []Route{
		{TypeOwner: typeOwner, MethodName: "String", RouteName: RouteTypeString, MethodID: 101, Return: typespec.String, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Kind", RouteName: RouteTypeKind, MethodID: 102, Return: typespec.Int64, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Name", RouteName: RouteTypeName, MethodID: 103, Return: typespec.String, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "PkgPath", RouteName: RouteTypePkgPath, MethodID: 104, Return: typespec.String, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Elem", RouteName: RouteTypeElem, MethodID: 105, Return: TypeType, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Key", RouteName: RouteTypeKey, MethodID: 106, Return: TypeType, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "AssignableTo", RouteName: RouteTypeAssignableTo, MethodID: 107, Return: typespec.Bool, Params: []typespec.Type{TypeType, TypeType}},
		{TypeOwner: typeOwner, MethodName: "Comparable", RouteName: RouteTypeComparable, MethodID: 108, Return: typespec.Bool, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "NumField", RouteName: RouteTypeNumField, MethodID: 109, Return: typespec.Int64, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Field", RouteName: RouteTypeField, MethodID: 110, Return: StructFieldType, Params: []typespec.Type{TypeType, typespec.Int64}},
		{TypeOwner: typeOwner, MethodName: "FieldByName", RouteName: RouteTypeFieldByName, MethodID: 111, Return: typespec.Tuple(StructFieldType, typespec.Bool), Params: []typespec.Type{TypeType, typespec.String}},
		{TypeOwner: typeOwner, MethodName: "NumMethod", RouteName: RouteTypeNumMethod, MethodID: 112, Return: typespec.Int64, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Method", RouteName: RouteTypeMethod, MethodID: 113, Return: MethodType, Params: []typespec.Type{TypeType, typespec.Int64}},
		{TypeOwner: typeOwner, MethodName: "MethodByName", RouteName: RouteTypeMethodByName, MethodID: 114, Return: typespec.Tuple(MethodType, typespec.Bool), Params: []typespec.Type{TypeType, typespec.String}},
		{TypeOwner: typeOwner, MethodName: "NumIn", RouteName: RouteTypeNumIn, MethodID: 115, Return: typespec.Int64, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "In", RouteName: RouteTypeIn, MethodID: 116, Return: TypeType, Params: []typespec.Type{TypeType, typespec.Int64}},
		{TypeOwner: typeOwner, MethodName: "NumOut", RouteName: RouteTypeNumOut, MethodID: 117, Return: typespec.Int64, Params: []typespec.Type{TypeType}},
		{TypeOwner: typeOwner, MethodName: "Out", RouteName: RouteTypeOut, MethodID: 118, Return: TypeType, Params: []typespec.Type{TypeType, typespec.Int64}},
		{TypeOwner: typeOwner, MethodName: "IsVariadic", RouteName: RouteTypeIsVariadic, MethodID: 119, Return: typespec.Bool, Params: []typespec.Type{TypeType}},
	}
}

func Routes() []Route {
	routes := PackageFunctions()
	routes = append(routes, TypeMethods()...)
	return routes
}

func PackageFunction(member string) (Route, bool) {
	for _, route := range PackageFunctions() {
		if route.MemberName == member {
			return route, true
		}
	}
	return Route{}, false
}
