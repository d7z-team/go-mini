package compiler

import (
	"sort"
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (c *Compiler) externalSchemaMaps() (
	map[ast.Ident]*runtime.RuntimeFuncSig,
	map[ast.Ident]*runtime.ValueSpec,
	map[ast.Ident]*runtime.RuntimeStructSpec,
	map[ast.Ident]*runtime.RuntimeInterfaceSpec,
	map[string]string,
) {
	funcs := cloneFuncSchemaMap(c.cfg.FuncSchemas)
	values := cloneValueSchemaMap(c.cfg.ValueSchemas)
	structs := cloneStructSchemaMap(c.cfg.StructSchemas)
	interfaces := cloneInterfaceSchemaMap(c.cfg.InterfaceSchemas)
	constants := cloneStringMap(c.cfg.Constants)
	if c.cfg.Surface == nil {
		return funcs, values, structs, interfaces, constants
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
			name := runtime.ExternalFullName(path, memberName)
			switch member.Kind {
			case runtime.FFIMemberFunc:
				if member.Route != nil && member.Route.Sig != nil {
					routeName := member.Route.RouteName
					if routeName == "" {
						routeName = name
					}
					funcs[ast.Ident(routeName)] = runtime.CloneRuntimeFuncSig(member.Route.Sig)
				}
			case runtime.FFIMemberConst:
				if member.Const != nil {
					constants[name] = member.Const.Value
				}
			case runtime.FFIMemberValue:
				if member.Value != nil && member.Value.Spec != nil {
					values[ast.Ident(name)] = cloneRuntimeValueSpec(member.Value.Spec)
				}
			}
		}
	}
	for name, typ := range c.cfg.Surface.Types {
		if typ == nil {
			continue
		}
		if typ.Struct != nil {
			structs[ast.Ident(name)] = runtime.CloneRuntimeStructSpec(typ.Struct)
		}
		if typ.Interface != nil {
			interfaces[ast.Ident(name)] = runtime.CloneRuntimeInterfaceSpec(typ.Interface)
		}
	}
	return funcs, values, structs, interfaces, constants
}

func (c *Compiler) externalRequirements(program *ast.ProgramStmt) []runtime.ExternalRequirement {
	if program == nil {
		return nil
	}
	funcs, values, structs, interfaces, constants := c.externalSchemaMaps()
	collector := &externalUsageCollector{
		funcs:      funcs,
		registered: c.externalRegisteredFuncs(),
		values:     values,
		structs:    structs,
		interfaces: interfaces,
		constants:  constants,
		reqs:       make(map[string]runtime.ExternalRequirement),
	}
	ast.Walk(collector, program)
	out := make([]runtime.ExternalRequirement, 0, len(collector.reqs))
	for _, req := range collector.reqs {
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PackagePath != b.PackagePath {
			return a.PackagePath < b.PackagePath
		}
		if a.MemberName != b.MemberName {
			return a.MemberName < b.MemberName
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

type externalUsageCollector struct {
	funcs      map[ast.Ident]*runtime.RuntimeFuncSig
	registered map[ast.Ident]bool
	values     map[ast.Ident]*runtime.ValueSpec
	structs    map[ast.Ident]*runtime.RuntimeStructSpec
	interfaces map[ast.Ident]*runtime.RuntimeInterfaceSpec
	constants  map[string]string
	reqs       map[string]runtime.ExternalRequirement
}

func (v *externalUsageCollector) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.MemberExpr:
		v.recordPackageMember(n)
		v.recordMethodMember(n)
	}
	return v
}

func (v *externalUsageCollector) recordPackageMember(member *ast.MemberExpr) {
	if member == nil || !member.ResolvedPackageMember || member.ResolvedPackageName == "" {
		return
	}
	name := string(member.ResolvedPackageName)
	pkg, memberName := runtime.SplitExternalName(name)
	if member.ResolvedPackagePath != "" {
		pkg = member.ResolvedPackagePath
		memberName = trimExternalPackagePrefix(name, pkg)
	}
	if pkg == "" || memberName == "" {
		return
	}
	if sig := v.funcs[ast.Ident(name)]; sig != nil && v.registered[ast.Ident(name)] {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  memberName,
			Kind:        runtime.FFIMemberFunc,
			Type:        sig.Spec,
			Hash:        runtime.FuncSchemaHash(sig),
		})
		return
	}
	if spec := v.values[ast.Ident(name)]; spec != nil {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  memberName,
			Kind:        runtime.FFIMemberValue,
			Type:        spec.Type.Raw,
			Hash:        runtime.ValueSchemaHash(spec),
		})
		return
	}
	if value, ok := v.constants[name]; ok {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  memberName,
			Kind:        runtime.FFIMemberConst,
			Hash:        runtime.ConstSchemaHash(value),
		})
		return
	}
	if spec := v.structs[ast.Ident(name)]; spec != nil {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  memberName,
			Kind:        runtime.FFIMemberType,
			TypeName:    name,
			Type:        spec.Spec,
			Hash:        runtime.StructSchemaHash(spec),
		})
		return
	}
	if spec := v.interfaces[ast.Ident(name)]; spec != nil {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  memberName,
			Kind:        runtime.FFIMemberType,
			TypeName:    name,
			Type:        spec.Spec,
			Hash:        runtime.InterfaceSchemaHash(spec),
		})
	}
}

func (v *externalUsageCollector) recordMethodMember(member *ast.MemberExpr) {
	if member == nil || member.ResolvedPackageMember || member.Object == nil || member.Property == "" {
		return
	}
	objType := strings.TrimSpace(string(member.Object.GetBase().Type))
	typeName := externalReceiverTypeName(objType)
	if typeName == "" {
		return
	}
	routeName := typeName + "." + string(member.Property)
	sig := v.funcs[ast.Ident(routeName)]
	if sig == nil || !v.registered[ast.Ident(routeName)] {
		return
	}
	pkg, memberName := runtime.SplitExternalName(routeName)
	if pkg == "" || memberName == "" {
		return
	}
	v.add(runtime.ExternalRequirement{
		Version:     runtime.FFISurfaceHashVersion,
		PackagePath: pkg,
		MemberName:  memberName,
		Kind:        runtime.FFIMemberFunc,
		TypeName:    typeName,
		MethodName:  string(member.Property),
		Type:        sig.Spec,
		Hash:        runtime.FuncSchemaHash(sig),
	})
	if spec := v.structs[ast.Ident(typeName)]; spec != nil {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  strings.TrimPrefix(typeName, pkg+"."),
			Kind:        runtime.FFIMemberType,
			TypeName:    typeName,
			Type:        spec.Spec,
			Hash:        runtime.StructSchemaHash(spec),
		})
		return
	}
	if spec := v.interfaces[ast.Ident(typeName)]; spec != nil {
		v.add(runtime.ExternalRequirement{
			Version:     runtime.FFISurfaceHashVersion,
			PackagePath: pkg,
			MemberName:  strings.TrimPrefix(typeName, pkg+"."),
			Kind:        runtime.FFIMemberType,
			TypeName:    typeName,
			Type:        spec.Spec,
			Hash:        runtime.InterfaceSchemaHash(spec),
		})
	}
}

func (v *externalUsageCollector) add(req runtime.ExternalRequirement) {
	key := string(req.Kind) + "\x00" + req.PackagePath + "\x00" + req.MemberName + "\x00" + req.TypeName + "\x00" + req.MethodName
	v.reqs[key] = req
}

func externalReceiverTypeName(typ string) string {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "HostRef<") && strings.HasSuffix(typ, ">") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "HostRef<"), ">"))
	}
	return strings.TrimPrefix(typ, "*")
}

func trimExternalPackagePrefix(name, pkg string) string {
	for _, prefix := range []string{pkg + ".", strings.ReplaceAll(pkg, "/", ".") + "."} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	_, member := runtime.SplitExternalName(name)
	return member
}

func (c *Compiler) externalRegisteredFuncs() map[ast.Ident]bool {
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
				routeName = runtime.ExternalFullName(path, memberName)
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

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
