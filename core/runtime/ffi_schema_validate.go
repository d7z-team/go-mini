package runtime

import (
	"errors"
	"fmt"
	"strings"
)

func CheckPublicFFIRouteSchema(name string, route FFIRoute) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(route.Name)
	}
	if name == "" {
		return errors.New("ffi route missing name")
	}
	if route.FuncSig == nil {
		return fmt.Errorf("ffi route %s missing schema", name)
	}
	return CheckPublicRuntimeFuncSig("ffi route "+name, route.FuncSig)
}

func CheckPublicRuntimeFuncSig(scope string, sig *RuntimeFuncSig) error {
	if sig == nil {
		return fmt.Errorf("%s missing function schema", scope)
	}
	if len(sig.ParamModes) != 0 && len(sig.ParamModes) != len(sig.ParamTypes) {
		return fmt.Errorf("%s param mode count mismatch: have %d want %d", scope, len(sig.ParamModes), len(sig.ParamTypes))
	}
	for i, mode := range sig.ParamModes {
		if mode != FFIParamIn && mode != FFIParamInOutBytes && mode != FFIParamInOutArray {
			return fmt.Errorf("%s parameter %d has unsupported FFI mode %d", scope, i, mode)
		}
		if sig.Variadic && i == len(sig.ParamModes)-1 && mode != FFIParamIn {
			return fmt.Errorf("%s variadic inout parameters are not supported", scope)
		}
	}
	for i := range sig.ParamTypes {
		if err := checkPublicFFIType(fmt.Sprintf("%s parameter %d", scope, i), sig.ParamTypes[i]); err != nil {
			return err
		}
	}
	return checkPublicFFIType(scope+" return", sig.ReturnType)
}

func CheckPublicFFIStructSchema(name string, spec *RuntimeStructSpec) error {
	if spec == nil {
		return nil
	}
	scope := strings.TrimSpace(name)
	if scope == "" {
		scope = spec.Name
	}
	if scope == "" {
		scope = string(spec.Spec)
	}
	for _, field := range spec.Fields {
		fieldScope := fmt.Sprintf("ffi struct %s field %s", scope, field.Name)
		if err := checkPublicFFIType(fieldScope, field.TypeInfo); err != nil {
			return err
		}
	}
	for _, method := range spec.Methods {
		if err := CheckPublicRuntimeFuncSig(fmt.Sprintf("ffi struct %s method %s", scope, method.Name), method.Spec); err != nil {
			return err
		}
	}
	return nil
}

func CheckPublicFFIInterfaceSchema(name string, spec *RuntimeInterfaceSpec) error {
	if spec == nil {
		return nil
	}
	scope := strings.TrimSpace(name)
	if scope == "" {
		scope = spec.TypeID
	}
	if scope == "" {
		scope = string(spec.Spec)
	}
	for _, method := range spec.Methods {
		if err := CheckPublicRuntimeFuncSig(fmt.Sprintf("ffi interface %s method %s", scope, method.Name), method.Spec); err != nil {
			return err
		}
	}
	return nil
}

func CheckPublicFFIValueSpec(name string, spec *ValueSpec) error {
	if spec == nil {
		return nil
	}
	scope := strings.TrimSpace(name)
	if scope == "" {
		scope = "package value"
	}
	return checkPublicFFIType("ffi package value "+scope, spec.Type)
}

func CheckPublicFFISurfaceSchema(schema *FFISurfaceSchema) error {
	if schema == nil {
		return nil
	}
	for pkgPath, pkg := range schema.Packages {
		if pkg == nil {
			continue
		}
		path := pkg.Path
		if path == "" {
			path = pkgPath
		}
		for memberName, member := range pkg.Members {
			if member == nil {
				continue
			}
			name := ExternalFullName(path, memberName)
			switch member.Kind {
			case FFIMemberFunc:
				route := FFIRoute{Name: name}
				if member.Route != nil {
					route.Name = member.Route.RouteName
					if route.Name == "" {
						route.Name = name
					}
					route.MethodID = member.Route.MethodID
					route.FuncSig = member.Route.Sig
					route.Doc = member.Route.Doc
				}
				if err := CheckPublicFFIRouteSchema(name, route); err != nil {
					return err
				}
			case FFIMemberValue:
				if member.Value != nil {
					if err := CheckPublicFFIValueSpec(name, member.Value.Spec); err != nil {
						return err
					}
				}
			case FFIMemberConst:
				if member.Const == nil {
					return fmt.Errorf("FFI constant %s missing schema", name)
				}
				if err := member.Const.Value.Validate(); err != nil {
					return fmt.Errorf("FFI constant %s invalid: %w", name, err)
				}
			}
		}
	}
	for name, typ := range schema.Types {
		if typ == nil {
			continue
		}
		if err := CheckPublicFFIStructSchema(name, typ.Struct); err != nil {
			return err
		}
		if err := CheckPublicFFIInterfaceSchema(name, typ.Interface); err != nil {
			return err
		}
		for methodName, method := range typ.Methods {
			routeName := ExternalFullName(name, methodName)
			route := FFIRoute{Name: routeName}
			if method != nil {
				route.Name = method.RouteName
				if route.Name == "" {
					route.Name = routeName
				}
				route.MethodID = method.MethodID
				route.FuncSig = method.Sig
				route.Doc = method.Doc
			}
			if err := CheckPublicFFIRouteSchema(routeName, route); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPublicFFIType(scope string, typ RuntimeType) error {
	if typ.IsEmpty() || typ.IsVoid() {
		return nil
	}
	switch typ.Kind {
	case RuntimeTypePointer:
		return fmt.Errorf("%s uses Ptr<T>, which is not allowed in public FFI schema: %s", scope, typ.Raw)
	case RuntimeTypeHostRef:
		if typ.Elem == nil || typ.Elem.IsAny() {
			return fmt.Errorf("%s uses HostRef<Any>, which is not allowed in public FFI schema", scope)
		}
		return checkPublicFFIType(scope+" host ref element", *typ.Elem)
	case RuntimeTypeArray:
		if typ.Elem != nil {
			return checkPublicFFIType(scope+" array element", *typ.Elem)
		}
	case RuntimeTypeMap:
		if typ.Key != nil {
			if err := checkPublicFFIType(scope+" map key", *typ.Key); err != nil {
				return err
			}
		}
		if typ.Value != nil {
			return checkPublicFFIType(scope+" map value", *typ.Value)
		}
	case RuntimeTypeChannel:
		if typ.Elem != nil {
			return checkPublicFFIType(scope+" channel element", *typ.Elem)
		}
	case RuntimeTypeTuple:
		for i := range typ.Params {
			if err := checkPublicFFIType(fmt.Sprintf("%s tuple item %d", scope, i), typ.Params[i]); err != nil {
				return err
			}
		}
	case RuntimeTypeFunction:
		for i := range typ.Params {
			if err := checkPublicFFIType(fmt.Sprintf("%s function parameter %d", scope, i), typ.Params[i]); err != nil {
				return err
			}
		}
		if typ.Return != nil {
			return checkPublicFFIType(scope+" function return", *typ.Return)
		}
	case RuntimeTypeStruct:
		for _, field := range typ.Fields {
			if err := checkPublicFFIType(fmt.Sprintf("%s struct field %s", scope, field.Name), field.TypeInfo); err != nil {
				return err
			}
		}
	case RuntimeTypeInterface:
		for _, method := range typ.Methods {
			if err := CheckPublicRuntimeFuncSig(fmt.Sprintf("%s interface method %s", scope, method.Name), method.Spec); err != nil {
				return err
			}
		}
	}
	return nil
}
