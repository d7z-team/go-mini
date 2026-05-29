package compiler

import (
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/reflectspec"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/typespec"
)

func (c *Compiler) ffiSchemaMaps() (
	map[ast.Ident]*runtime.RuntimeFuncSig,
	map[ast.Ident]uint32,
	map[ast.Ident]*runtime.ValueSpec,
	map[ast.Ident]*runtime.RuntimeStructSpec,
	map[ast.Ident]*runtime.RuntimeInterfaceSpec,
	map[string]runtime.FFIConstValue,
	map[ast.Ident]runtime.ModuleMemberName,
	map[string][]runtime.ModuleRequirement,
) {
	funcs := cloneFuncSchemaMap(c.cfg.FuncSchemas)
	methodIDs := cloneFuncMethodIDMap(c.cfg.RegisteredFuncMethodIDs)
	values := cloneValueSchemaMap(c.cfg.ValueSchemas)
	structs := cloneStructSchemaMap(c.cfg.StructSchemas)
	interfaces := cloneInterfaceSchemaMap(c.cfg.InterfaceSchemas)
	constants := cloneFFIConstValueMap(c.cfg.Constants)
	typeOwners := make(map[ast.Ident]runtime.ModuleMemberName)
	packageReqs := make(map[string][]runtime.ModuleRequirement)
	if c.cfg.Surface == nil {
		return funcs, methodIDs, values, structs, interfaces, constants, typeOwners, packageReqs
	}
	for pkgPath, pkg := range c.cfg.Surface.Packages {
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
			name := runtime.QualifiedMemberName(path, memberName)
			switch member.Kind {
			case runtime.FFIMemberFunc:
				if member.Route != nil && member.Route.Sig != nil {
					routeName := member.Route.RouteName
					if routeName == "" {
						routeName = name
					}
					funcs[ast.Ident(routeName)] = runtime.CloneRuntimeFuncSig(member.Route.Sig)
					methodIDs[ast.Ident(routeName)] = member.Route.MethodID
					packageReqs[path] = append(packageReqs[path], runtime.ModuleRequirement{
						Version:    runtime.FFISurfaceHashVersion,
						Path:       path,
						Kind:       runtime.ModuleKindFFI,
						MemberName: memberName,
						MemberKind: runtime.FFIMemberFunc,
						Type:       member.Route.Sig.Spec,
						MethodID:   member.Route.MethodID,
						Hash:       runtime.FuncRouteHash(member.Route.MethodID, member.Route.Sig),
					})
				}
			case runtime.FFIMemberConst:
				if member.Const != nil {
					constants[name] = member.Const.Value
					packageReqs[path] = append(packageReqs[path], runtime.ModuleRequirement{
						Version:    runtime.FFISurfaceHashVersion,
						Path:       path,
						Kind:       runtime.ModuleKindFFI,
						MemberName: memberName,
						MemberKind: runtime.FFIMemberConst,
						Type:       member.Const.Value.Type,
						Hash:       runtime.ConstSchemaHash(member.Const.Value),
					})
				}
			case runtime.FFIMemberValue:
				if member.Value != nil && member.Value.Spec != nil {
					values[ast.Ident(name)] = cloneRuntimeValueSpec(member.Value.Spec)
					packageReqs[path] = append(packageReqs[path], runtime.ModuleRequirement{
						Version:    runtime.FFISurfaceHashVersion,
						Path:       path,
						Kind:       runtime.ModuleKindFFI,
						MemberName: memberName,
						MemberKind: runtime.FFIMemberValue,
						Type:       member.Value.Spec.Type.Raw,
						Hash:       runtime.ValueSchemaHash(member.Value.Spec),
					})
				}
			}
		}
	}
	for _, typ := range c.cfg.Surface.Types {
		if typ == nil {
			continue
		}
		name := typ.CanonicalName()
		if name == "" {
			continue
		}
		typeOwners[ast.Ident(name)] = typ.Owner()
		if typ.Struct != nil {
			structs[ast.Ident(name)] = runtime.CloneRuntimeStructSpec(typ.Struct)
			if req, ok := ownedTypeRequirement(typ.Owner(), name, typ.Struct, nil); ok {
				packageReqs[req.Path] = append(packageReqs[req.Path], req)
			}
		}
		if typ.Interface != nil {
			interfaces[ast.Ident(name)] = runtime.CloneRuntimeInterfaceSpec(typ.Interface)
			if typ.Struct == nil {
				if req, ok := ownedTypeRequirement(typ.Owner(), name, nil, typ.Interface); ok {
					packageReqs[req.Path] = append(packageReqs[req.Path], req)
				}
			}
		}
		for methodName, method := range typ.Methods {
			if method == nil || method.Sig == nil {
				continue
			}
			routeName := method.RouteName
			if routeName == "" {
				routeName = runtime.QualifiedMemberName(name, methodName)
			}
			funcs[ast.Ident(routeName)] = runtime.CloneRuntimeFuncSig(method.Sig)
			methodIDs[ast.Ident(routeName)] = method.MethodID
		}
	}
	return funcs, methodIDs, values, structs, interfaces, constants, typeOwners, packageReqs
}

func (c *Compiler) moduleRequirements(program *ast.ProgramStmt) []runtime.ModuleRequirement {
	if program == nil {
		return nil
	}
	funcs, methodIDs, values, structs, interfaces, constants, typeOwners, packageReqs := c.ffiSchemaMaps()
	collector := &moduleUsageCollector{
		funcs:       funcs,
		methodIDs:   methodIDs,
		registered:  c.registeredFFIFuncs(),
		values:      values,
		structs:     structs,
		interfaces:  interfaces,
		constants:   constants,
		typeOwners:  typeOwners,
		packageReqs: packageReqs,
		reqs:        make(map[string]runtime.ModuleRequirement),
	}
	ast.Walk(collector, program)
	for _, imp := range program.Imports {
		path := strings.TrimSpace(imp.Path)
		if path == "" {
			continue
		}
		if hash := c.cfg.ModuleHashes[path]; hash != "" {
			collector.add(runtime.ModuleRequirement{
				Version: runtime.FFISurfaceHashVersion,
				Path:    path,
				Kind:    runtime.ModuleKindSource,
				Hash:    hash,
			})
		}
	}
	out := make([]runtime.ModuleRequirement, 0, len(collector.reqs))
	for _, req := range collector.reqs {
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.MemberName != b.MemberName {
			return a.MemberName < b.MemberName
		}
		if a.MemberKind != b.MemberKind {
			return a.MemberKind < b.MemberKind
		}
		if a.TypeName != b.TypeName {
			return a.TypeName < b.TypeName
		}
		if a.MethodName != b.MethodName {
			return a.MethodName < b.MethodName
		}
		return a.Kind < b.Kind
	})
	return out
}

type moduleUsageCollector struct {
	funcs       map[ast.Ident]*runtime.RuntimeFuncSig
	methodIDs   map[ast.Ident]uint32
	registered  map[ast.Ident]bool
	values      map[ast.Ident]*runtime.ValueSpec
	structs     map[ast.Ident]*runtime.RuntimeStructSpec
	interfaces  map[ast.Ident]*runtime.RuntimeInterfaceSpec
	constants   map[string]runtime.FFIConstValue
	typeOwners  map[ast.Ident]runtime.ModuleMemberName
	packageReqs map[string][]runtime.ModuleRequirement
	reqs        map[string]runtime.ModuleRequirement
}

func (v *moduleUsageCollector) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.MemberExpr:
		v.recordPackageMember(n)
		v.recordMethodMember(n)
	case *ast.CallExprStmt:
		v.recordReflectLiteralRequirement(n)
	}
	return v
}

func (v *moduleUsageCollector) recordPackageMember(member *ast.MemberExpr) {
	if member == nil || !member.ResolvedPackageMember || member.ResolvedPackageName == "" {
		return
	}
	name := string(member.ResolvedPackageName)
	pkg := strings.TrimSpace(member.ResolvedPackagePath)
	memberName := trimQualifiedPackagePrefix(name, pkg)
	if pkg == "" || memberName == "" {
		return
	}
	if sig := v.funcs[ast.Ident(name)]; sig != nil && v.registered[ast.Ident(name)] {
		methodID := v.methodIDs[ast.Ident(name)]
		v.add(runtime.ModuleRequirement{
			Version:    runtime.FFISurfaceHashVersion,
			Path:       pkg,
			Kind:       runtime.ModuleKindFFI,
			MemberName: memberName,
			MemberKind: runtime.FFIMemberFunc,
			Type:       sig.Spec,
			MethodID:   methodID,
			Hash:       runtime.FuncRouteHash(methodID, sig),
		})
		return
	}
	if spec := v.values[ast.Ident(name)]; spec != nil {
		v.add(runtime.ModuleRequirement{
			Version:    runtime.FFISurfaceHashVersion,
			Path:       pkg,
			Kind:       runtime.ModuleKindFFI,
			MemberName: memberName,
			MemberKind: runtime.FFIMemberValue,
			Type:       spec.Type.Raw,
			Hash:       runtime.ValueSchemaHash(spec),
		})
		return
	}
	if value, ok := v.constants[name]; ok {
		v.add(runtime.ModuleRequirement{
			Version:    runtime.FFISurfaceHashVersion,
			Path:       pkg,
			Kind:       runtime.ModuleKindFFI,
			MemberName: memberName,
			MemberKind: runtime.FFIMemberConst,
			Type:       value.Type,
			Hash:       runtime.ConstSchemaHash(value),
		})
		return
	}
	v.addOwnedTypeRequirement(name)
}

func (v *moduleUsageCollector) recordMethodMember(member *ast.MemberExpr) {
	if member == nil || member.ResolvedPackageMember || member.Object == nil || member.Property == "" {
		return
	}
	objType := strings.TrimSpace(string(member.Object.GetBase().Type))
	typeName := ffiReceiverTypeName(objType)
	if typeName == "" {
		return
	}
	routeName := typeName + "." + string(member.Property)
	sig := v.funcs[ast.Ident(routeName)]
	if sig == nil || !v.registered[ast.Ident(routeName)] {
		return
	}
	methodID := v.methodIDs[ast.Ident(routeName)]
	owner := v.typeOwners[ast.Ident(typeName)]
	if owner.ModulePath == "" || owner.MemberName == "" {
		return
	}
	v.add(runtime.ModuleRequirement{
		Version:    runtime.FFISurfaceHashVersion,
		Path:       owner.ModulePath,
		Kind:       runtime.ModuleKindFFI,
		MemberName: owner.MemberName,
		MemberKind: runtime.FFIMemberFunc,
		TypeName:   typeName,
		MethodName: string(member.Property),
		Type:       sig.Spec,
		MethodID:   methodID,
		Hash:       runtime.FuncRouteHash(methodID, sig),
	})
	v.addOwnedTypeRequirement(typeName)
}

func (v *moduleUsageCollector) recordReflectLiteralRequirement(call *ast.CallExprStmt) {
	if call == nil || len(call.Args) == 0 {
		return
	}
	member, ok := call.Func.(*ast.MemberExpr)
	if !ok || !member.ResolvedPackageMember || member.ResolvedPackagePath != reflectspec.PackagePath {
		return
	}
	value, ok := stringLiteralValue(call.Args[0])
	if !ok {
		return
	}
	switch string(member.Property) {
	case "Package":
		for _, req := range v.packageReqs[value] {
			v.add(req)
		}
	case "TypeFrom", "Zero", "MakeMap":
		typespec.WalkNamedTypes(typespec.Type(value), func(named typespec.Type) {
			v.addReflectTypeRequirement(named.String())
		})
	}
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.LiteralExpr)
	if !ok || lit == nil || lit.Type != ast.TypeString {
		return "", false
	}
	return strings.TrimSpace(lit.Value), true
}

func (v *moduleUsageCollector) addReflectTypeRequirement(typeName string) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return
	}
	v.addOwnedTypeRequirement(typeName)
}

func (v *moduleUsageCollector) add(req runtime.ModuleRequirement) {
	key := string(req.Kind) + "\x00" + req.Path + "\x00" + string(req.MemberKind) + "\x00" + req.MemberName + "\x00" + req.TypeName + "\x00" + req.MethodName
	v.reqs[key] = req
}

func (v *moduleUsageCollector) addOwnedTypeRequirement(typeName string) {
	req, ok := ownedTypeRequirement(
		v.typeOwners[ast.Ident(typeName)],
		typeName,
		v.structs[ast.Ident(typeName)],
		v.interfaces[ast.Ident(typeName)],
	)
	if ok {
		v.add(req)
	}
}

func ownedTypeRequirement(owner runtime.ModuleMemberName, typeName string, structSpec *runtime.RuntimeStructSpec, interfaceSpec *runtime.RuntimeInterfaceSpec) (runtime.ModuleRequirement, bool) {
	if owner.ModulePath == "" || owner.MemberName == "" {
		return runtime.ModuleRequirement{}, false
	}
	switch {
	case structSpec != nil:
		return runtime.ModuleRequirement{
			Version:    runtime.FFISurfaceHashVersion,
			Path:       owner.ModulePath,
			Kind:       runtime.ModuleKindFFI,
			MemberName: owner.MemberName,
			MemberKind: runtime.FFIMemberType,
			TypeName:   typeName,
			Type:       structSpec.Spec,
			Hash:       runtime.StructSchemaHash(structSpec),
		}, true
	case interfaceSpec != nil:
		return runtime.ModuleRequirement{
			Version:    runtime.FFISurfaceHashVersion,
			Path:       owner.ModulePath,
			Kind:       runtime.ModuleKindFFI,
			MemberName: owner.MemberName,
			MemberKind: runtime.FFIMemberType,
			TypeName:   typeName,
			Type:       interfaceSpec.Spec,
			Hash:       runtime.InterfaceSchemaHash(interfaceSpec),
		}, true
	default:
		return runtime.ModuleRequirement{}, false
	}
}

func ffiReceiverTypeName(typ string) string {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "HostRef<") && strings.HasSuffix(typ, ">") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "HostRef<"), ">"))
	}
	return strings.TrimPrefix(typ, "*")
}

func trimQualifiedPackagePrefix(name, pkg string) string {
	prefix := pkg + "."
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix)
	}
	return ""
}

func (c *Compiler) registeredFFIFuncs() map[ast.Ident]bool {
	out := make(map[ast.Ident]bool, len(c.cfg.RegisteredFuncs))
	for name, ok := range c.cfg.RegisteredFuncs {
		if ok {
			out[name] = true
		}
	}
	if c.cfg.Surface == nil {
		return out
	}
	for pkgPath, pkg := range c.cfg.Surface.Packages {
		if pkg == nil {
			continue
		}
		path := pkg.Path
		if path == "" {
			path = pkgPath
		}
		for memberName, member := range pkg.Members {
			if member == nil || member.Kind != runtime.FFIMemberFunc || member.Route == nil {
				continue
			}
			routeName := member.Route.RouteName
			if routeName == "" {
				routeName = runtime.QualifiedMemberName(path, memberName)
			}
			out[ast.Ident(routeName)] = true
		}
	}
	for _, typ := range c.cfg.Surface.Types {
		if typ == nil {
			continue
		}
		typeName := typ.CanonicalName()
		if typeName == "" {
			continue
		}
		for methodName, method := range typ.Methods {
			if method == nil {
				continue
			}
			routeName := method.RouteName
			if routeName == "" {
				routeName = runtime.QualifiedMemberName(typeName, methodName)
			}
			out[ast.Ident(routeName)] = true
		}
	}
	return out
}

func cloneFuncSchemaMap(in map[ast.Ident]*runtime.RuntimeFuncSig) map[ast.Ident]*runtime.RuntimeFuncSig {
	out := make(map[ast.Ident]*runtime.RuntimeFuncSig, len(in))
	for k, v := range in {
		out[k] = runtime.CloneRuntimeFuncSig(v)
	}
	return out
}

func cloneFuncMethodIDMap(in map[ast.Ident]uint32) map[ast.Ident]uint32 {
	out := make(map[ast.Ident]uint32, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneValueSchemaMap(in map[ast.Ident]*runtime.ValueSpec) map[ast.Ident]*runtime.ValueSpec {
	out := make(map[ast.Ident]*runtime.ValueSpec, len(in))
	for k, v := range in {
		out[k] = cloneRuntimeValueSpec(v)
	}
	return out
}

func cloneRuntimeValueSpec(spec *runtime.ValueSpec) *runtime.ValueSpec {
	if spec == nil {
		return nil
	}
	return &runtime.ValueSpec{Type: spec.Type, Doc: spec.Doc, ReadOnly: spec.ReadOnly}
}

func cloneStructSchemaMap(in map[ast.Ident]*runtime.RuntimeStructSpec) map[ast.Ident]*runtime.RuntimeStructSpec {
	out := make(map[ast.Ident]*runtime.RuntimeStructSpec, len(in))
	for k, v := range in {
		out[k] = runtime.CloneRuntimeStructSpec(v)
	}
	return out
}

func cloneInterfaceSchemaMap(in map[ast.Ident]*runtime.RuntimeInterfaceSpec) map[ast.Ident]*runtime.RuntimeInterfaceSpec {
	out := make(map[ast.Ident]*runtime.RuntimeInterfaceSpec, len(in))
	for k, v := range in {
		out[k] = runtime.CloneRuntimeInterfaceSpec(v)
	}
	return out
}

func cloneFFIConstValueMap(in map[string]runtime.FFIConstValue) map[string]runtime.FFIConstValue {
	out := make(map[string]runtime.FFIConstValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ffiConstValuesToStrings(in map[string]runtime.FFIConstValue) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v.DisplayString()
	}
	return out
}

func ffiConstValuesToAST(in map[string]runtime.FFIConstValue) map[string]ast.GoMiniType {
	out := make(map[string]ast.GoMiniType, len(in))
	for k, v := range in {
		if v.Type != "" {
			out[k] = ast.GoMiniType(v.Type)
		}
	}
	return out
}
