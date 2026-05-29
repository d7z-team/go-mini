package reflectlib

import (
	"gopkg.d7z.net/go-mini/core/reflectspec"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
	"gopkg.d7z.net/go-mini/core/typespec"
)

func SurfaceReflect() *surface.Bundle {
	schema := runtime.NewFFISurfaceSchema()
	for _, spec := range reflectspec.Structs() {
		if err := schema.AddStruct(spec.Owner.PackagePath, spec.Owner.MemberName, runtime.MustParseRuntimeStructSpec(spec.Name.String(), runtime.StructOwnershipVMValue, typespec.Struct(spec.Members))); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	for name, value := range reflectConsts() {
		if err := schema.AddConst(reflectspec.PackagePath, name, runtime.ConstInt64(value)); err != nil {
			return &surface.Bundle{Err: err}
		}
	}
	for _, route := range reflectspec.Routes() {
		sig := runtime.MustParseRuntimeFuncSig(route.Signature())
		if route.TypeOwner != (reflectspec.Owner{}) {
			if err := schema.AddTypeMethod(route.TypeOwner.PackagePath, route.TypeOwner.MemberName, route.MethodName, route.RouteName, route.MethodID, sig, route.Doc); err != nil {
				return &surface.Bundle{Err: err}
			}
			continue
		}
		if err := schema.AddFunc(route.PackagePath, route.MemberName, route.RouteName, route.MethodID, sig, route.Doc); err != nil {
			return &surface.Bundle{Err: err}
		}
	}

	return surface.New(schema, func(_ runtime.FFIBindContext) (*runtime.BoundFFISurface, error) {
		bound := runtime.NewBoundFFISurfaceFromSchema(schema)
		for _, decl := range reflectspec.Routes() {
			route := runtime.FFIRoute{
				Name:     decl.RouteName,
				Native:   runtime.NativeReflect,
				MethodID: decl.MethodID,
				FuncSig:  runtime.MustParseRuntimeFuncSig(decl.Signature()),
				Doc:      decl.Doc,
			}
			if bound.Routes == nil {
				bound.Routes = make(map[string]runtime.FFIRoute)
			}
			if existing, ok := bound.Routes[route.Name]; ok {
				if err := runtime.CheckRouteCompatible(route.Name, existing, route); err != nil {
					return nil, err
				}
			}
			bound.Routes[route.Name] = route
			if decl.TypeOwner == (reflectspec.Owner{}) {
				pkg := bound.EnsurePackage(decl.PackagePath)
				if pkg != nil {
					pkg.Members[decl.MemberName] = &runtime.BoundFFIMember{Name: decl.MemberName, Kind: runtime.FFIMemberFunc, ReadOnly: true, RouteName: route.Name}
				}
			}
		}
		return bound, nil
	})
}

func reflectConsts() map[string]int64 {
	return map[string]int64{
		"Invalid":   runtime.ReflectKindInvalid,
		"Bool":      runtime.ReflectKindBool,
		"Int":       runtime.ReflectKindInt64,
		"Int64":     runtime.ReflectKindInt64,
		"Float64":   runtime.ReflectKindFloat64,
		"String":    runtime.ReflectKindString,
		"Bytes":     runtime.ReflectKindBytes,
		"Array":     runtime.ReflectKindArray,
		"Map":       runtime.ReflectKindMap,
		"Struct":    runtime.ReflectKindStruct,
		"Ptr":       runtime.ReflectKindPtr,
		"HostRef":   runtime.ReflectKindHostRef,
		"Chan":      runtime.ReflectKindChan,
		"Func":      runtime.ReflectKindFunc,
		"Interface": runtime.ReflectKindInterface,
		"Error":     runtime.ReflectKindError,
		"Module":    runtime.ReflectKindModule,
		"Any":       runtime.ReflectKindAny,
	}
}
