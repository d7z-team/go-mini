package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.d7z.net/go-mini/core/ffigo"
)

const FFISurfaceHashVersion = "ffi-surface-v1"

type FFIMemberKind string

const (
	FFIMemberFunc  FFIMemberKind = "func"
	FFIMemberConst FFIMemberKind = "const"
	FFIMemberValue FFIMemberKind = "value"
	FFIMemberType  FFIMemberKind = "type"
)

type ModuleRequirement struct {
	Version    string        `json:"version,omitempty"`
	Path       string        `json:"path,omitempty"`
	Kind       ModuleKind    `json:"kind"`
	MemberName string        `json:"member_name,omitempty"`
	MemberKind FFIMemberKind `json:"member_kind,omitempty"`
	Type       TypeSpec      `json:"type,omitempty"`
	TypeName   string        `json:"type_name,omitempty"`
	MethodName string        `json:"method_name,omitempty"`
	MethodID   uint32        `json:"method_id,omitempty"`
	Hash       string        `json:"hash,omitempty"`
}

type ModuleMemberName struct {
	ModulePath string `json:"module_path,omitempty"`
	MemberName string `json:"member_name,omitempty"`
}

func NewModuleMemberName(modulePath, memberName string) ModuleMemberName {
	return ModuleMemberName{
		ModulePath: strings.TrimSpace(modulePath),
		MemberName: strings.TrimSpace(memberName),
	}
}

func (m ModuleMemberName) CanonicalName() string {
	return QualifiedMemberName(m.ModulePath, m.MemberName)
}

type FFISurfaceSchema struct {
	Version  string                       `json:"version"`
	Packages map[string]*FFIPackageSchema `json:"packages,omitempty"`
	Types    map[string]*FFITypeSchema    `json:"types,omitempty"`
	err      error
}

type FFIPackageSchema struct {
	Path    string                      `json:"path"`
	Members map[string]*FFIMemberSchema `json:"members,omitempty"`
}

type FFIMemberSchema struct {
	Name     string        `json:"name"`
	Kind     FFIMemberKind `json:"kind"`
	ReadOnly bool          `json:"read_only,omitempty"`
	Route    *FFIRouteSpec `json:"route,omitempty"`
	Const    *FFIConstSpec `json:"const,omitempty"`
	Value    *FFIValueSpec `json:"value,omitempty"`
	Type     *FFITypeRef   `json:"type,omitempty"`
	Doc      string        `json:"doc,omitempty"`
}

type FFIRouteSpec struct {
	RouteName string          `json:"route_name"`
	MethodID  uint32          `json:"method_id"`
	Sig       *RuntimeFuncSig `json:"sig,omitempty"`
	Doc       string          `json:"doc,omitempty"`
}

type FFIConstSpec struct {
	Value FFIConstValue `json:"value"`
}

type FFIValueSpec struct {
	Spec *ValueSpec `json:"spec,omitempty"`
}

type FFITypeRef struct {
	PackagePath string `json:"package_path"`
	MemberName  string `json:"member_name"`
	Name        string `json:"name"`
}

type FFITypeSchema struct {
	PackagePath string                   `json:"package_path"`
	MemberName  string                   `json:"member_name"`
	Name        string                   `json:"name"`
	Struct      *RuntimeStructSpec       `json:"struct,omitempty"`
	Interface   *RuntimeInterfaceSpec    `json:"interface,omitempty"`
	Methods     map[string]*FFIRouteSpec `json:"methods,omitempty"`
}

type FFIRouteDecl struct {
	PackagePath     string
	MemberName      string
	TypePackagePath string
	TypeMemberName  string
	MethodName      string
	RouteName       string
	MethodID        uint32
	Sig             *RuntimeFuncSig
	Doc             string
}

type BoundFFIPackage struct {
	Path    string
	Members map[string]*BoundFFIMember
}

type BoundFFIMember struct {
	Name      string
	Kind      FFIMemberKind
	Type      RuntimeType
	ReadOnly  bool
	RouteName string
	Const     FFIConstValue
	Value     *Var
}

type BoundFFISurface struct {
	Schema        *FFISurfaceSchema
	Packages      map[string]*BoundFFIPackage
	Routes        map[string]FFIRoute
	Consts        map[string]FFIConstValue
	PackageValues map[string]*BoundPackageValue
	Structs       map[string]*RuntimeStructSpec
	Interfaces    map[string]*RuntimeInterfaceSpec
}

func NewFFISurfaceSchema() *FFISurfaceSchema {
	return &FFISurfaceSchema{
		Version:  FFISurfaceHashVersion,
		Packages: make(map[string]*FFIPackageSchema),
		Types:    make(map[string]*FFITypeSchema),
	}
}

func CloneFFISurfaceSchema(schema *FFISurfaceSchema) *FFISurfaceSchema {
	if schema == nil {
		return nil
	}
	out := NewFFISurfaceSchema()
	if schema.Version != "" {
		out.Version = schema.Version
	}
	out.err = schema.err
	for path, pkg := range schema.Packages {
		if pkg == nil {
			continue
		}
		next := &FFIPackageSchema{Path: pkg.Path, Members: make(map[string]*FFIMemberSchema, len(pkg.Members))}
		for name, member := range pkg.Members {
			next.Members[name] = cloneFFIMemberSchema(member)
		}
		out.Packages[path] = next
	}
	for name, typ := range schema.Types {
		if typ == nil {
			continue
		}
		out.Types[name] = &FFITypeSchema{
			PackagePath: typ.PackagePath,
			MemberName:  typ.MemberName,
			Name:        typ.CanonicalName(),
			Struct:      CloneRuntimeStructSpec(typ.Struct),
			Interface:   CloneRuntimeInterfaceSpec(typ.Interface),
			Methods:     cloneFFIRouteSpecMap(typ.Methods),
		}
	}
	return out
}

func cloneFFIMemberSchema(member *FFIMemberSchema) *FFIMemberSchema {
	if member == nil {
		return nil
	}
	out := *member
	if member.Route != nil {
		out.Route = &FFIRouteSpec{
			RouteName: member.Route.RouteName,
			MethodID:  member.Route.MethodID,
			Sig:       CloneRuntimeFuncSig(member.Route.Sig),
			Doc:       member.Route.Doc,
		}
	}
	if member.Const != nil {
		value := member.Const.Value
		out.Const = &FFIConstSpec{Value: value}
	}
	if member.Value != nil {
		out.Value = &FFIValueSpec{Spec: cloneValueSpec(member.Value.Spec)}
	}
	if member.Type != nil {
		out.Type = &FFITypeRef{
			PackagePath: member.Type.PackagePath,
			MemberName:  member.Type.MemberName,
			Name:        member.Type.Name,
		}
	}
	return &out
}

func cloneFFIRouteSpec(spec *FFIRouteSpec) *FFIRouteSpec {
	if spec == nil {
		return nil
	}
	return &FFIRouteSpec{
		RouteName: spec.RouteName,
		MethodID:  spec.MethodID,
		Sig:       CloneRuntimeFuncSig(spec.Sig),
		Doc:       spec.Doc,
	}
}

func cloneFFIRouteSpecMap(in map[string]*FFIRouteSpec) map[string]*FFIRouteSpec {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*FFIRouteSpec, len(in))
	for name, spec := range in {
		out[name] = cloneFFIRouteSpec(spec)
	}
	return out
}

func cloneValueSpec(spec *ValueSpec) *ValueSpec {
	if spec == nil {
		return nil
	}
	return &ValueSpec{Type: spec.Type, Doc: spec.Doc, ReadOnly: spec.ReadOnly}
}

func (s *FFISurfaceSchema) EnsurePackage(path string) *FFIPackageSchema {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if s.Packages == nil {
		s.Packages = make(map[string]*FFIPackageSchema)
	}
	pkg := s.Packages[path]
	if pkg == nil {
		pkg = &FFIPackageSchema{Path: path, Members: make(map[string]*FFIMemberSchema)}
		s.Packages[path] = pkg
	}
	return pkg
}

func (s *FFISurfaceSchema) recordError(err error) error {
	if err != nil && s != nil && s.err == nil {
		s.err = err
	}
	return err
}

func (s *FFISurfaceSchema) ensureType(pkgPath, member string) *FFITypeSchema {
	owner := NewModuleMemberName(pkgPath, member)
	if owner.ModulePath == "" || owner.MemberName == "" {
		return nil
	}
	name := owner.CanonicalName()
	if s.Types == nil {
		s.Types = make(map[string]*FFITypeSchema)
	}
	typ := s.Types[name]
	if typ == nil {
		typ = &FFITypeSchema{
			PackagePath: owner.ModulePath,
			MemberName:  owner.MemberName,
			Name:        name,
		}
		s.Types[name] = typ
	}
	typ.PackagePath = owner.ModulePath
	typ.MemberName = owner.MemberName
	if typ.Name == "" {
		typ.Name = name
	}
	s.addTypeMember(owner.ModulePath, owner.MemberName, name)
	return typ
}

func (s *FFISurfaceSchema) addTypeMember(pkgPath, member, typeName string) {
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberType,
		ReadOnly: true,
		Type: &FFITypeRef{
			PackagePath: pkgPath,
			MemberName:  member,
			Name:        typeName,
		},
	}
}

func (s *FFISurfaceSchema) AddFunc(pkgPath, member, routeName string, methodID uint32, sig *RuntimeFuncSig, doc string) error {
	if s == nil {
		return nil
	}
	pkgPath = strings.TrimSpace(pkgPath)
	member = strings.TrimSpace(member)
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return s.recordError(fmt.Errorf("ffi function route missing package or member: package=%q member=%q", pkgPath, member))
	}
	if routeName == "" {
		routeName = QualifiedMemberName(pkgPath, member)
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberFunc,
		ReadOnly: true,
		Route:    &FFIRouteSpec{RouteName: routeName, MethodID: methodID, Sig: CloneRuntimeFuncSig(sig), Doc: doc},
		Doc:      doc,
	}
	return nil
}

func (s *FFISurfaceSchema) AddConst(pkgPath, member string, value FFIConstValue) error {
	if s == nil {
		return nil
	}
	pkgPath = strings.TrimSpace(pkgPath)
	member = strings.TrimSpace(member)
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return s.recordError(fmt.Errorf("ffi constant missing package or member: package=%q member=%q", pkgPath, member))
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberConst,
		ReadOnly: true,
		Const:    &FFIConstSpec{Value: value},
	}
	return nil
}

func (s *FFISurfaceSchema) AddValue(pkgPath, member string, spec *ValueSpec) error {
	if s == nil {
		return nil
	}
	pkgPath = strings.TrimSpace(pkgPath)
	member = strings.TrimSpace(member)
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return s.recordError(fmt.Errorf("ffi package value missing package or member: package=%q member=%q", pkgPath, member))
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberValue,
		ReadOnly: spec == nil || spec.ReadOnly,
		Value:    &FFIValueSpec{Spec: cloneValueSpec(spec)},
	}
	return nil
}

func (s *FFISurfaceSchema) AddStruct(pkgPath, member string, spec *RuntimeStructSpec) error {
	if s == nil {
		return nil
	}
	typ := s.ensureType(pkgPath, member)
	if typ == nil {
		return s.recordError(fmt.Errorf("ffi struct type missing package or member: package=%q member=%q", pkgPath, member))
	}
	if spec == nil {
		return s.recordError(fmt.Errorf("ffi struct type %s missing schema", typ.CanonicalName()))
	}
	typ.Struct = CloneRuntimeStructSpec(spec)
	s.addTypeMember(typ.PackagePath, typ.MemberName, typ.CanonicalName())
	return nil
}

func (s *FFISurfaceSchema) AddInterface(pkgPath, member string, spec *RuntimeInterfaceSpec) error {
	if s == nil {
		return nil
	}
	typ := s.ensureType(pkgPath, member)
	if typ == nil {
		return s.recordError(fmt.Errorf("ffi interface type missing package or member: package=%q member=%q", pkgPath, member))
	}
	if spec == nil {
		return s.recordError(fmt.Errorf("ffi interface type %s missing schema", typ.CanonicalName()))
	}
	typ.Interface = CloneRuntimeInterfaceSpec(spec)
	s.addTypeMember(typ.PackagePath, typ.MemberName, typ.CanonicalName())
	return nil
}

func (s *FFISurfaceSchema) AddTypeMethod(pkgPath, member, methodName, routeName string, methodID uint32, sig *RuntimeFuncSig, doc string) error {
	if s == nil {
		return nil
	}
	typ := s.ensureType(pkgPath, member)
	methodName = strings.TrimSpace(methodName)
	if typ == nil {
		return s.recordError(fmt.Errorf("ffi type method missing package or member: package=%q member=%q method=%q", pkgPath, member, methodName))
	}
	if methodName == "" {
		return s.recordError(fmt.Errorf("ffi type method %s missing method name", typ.CanonicalName()))
	}
	if routeName == "" {
		routeName = QualifiedMemberName(typ.CanonicalName(), methodName)
	}
	if typ.Methods == nil {
		typ.Methods = make(map[string]*FFIRouteSpec)
	}
	typ.Methods[methodName] = &FFIRouteSpec{
		RouteName: routeName,
		MethodID:  methodID,
		Sig:       CloneRuntimeFuncSig(sig),
		Doc:       doc,
	}
	return nil
}

func (s *FFISurfaceSchema) AddRouteDecls(routes []FFIRouteDecl) error {
	if s == nil {
		return nil
	}
	for _, route := range routes {
		if route.TypePackagePath != "" || route.TypeMemberName != "" {
			if route.TypePackagePath == "" || route.TypeMemberName == "" {
				return s.recordError(fmt.Errorf("ffi type method route %s has incomplete owner: package=%q member=%q", route.RouteName, route.TypePackagePath, route.TypeMemberName))
			}
			if err := s.AddTypeMethod(route.TypePackagePath, route.TypeMemberName, route.MethodName, route.RouteName, route.MethodID, route.Sig, route.Doc); err != nil {
				return err
			}
			continue
		}
		if err := s.AddFunc(route.PackagePath, route.MemberName, route.RouteName, route.MethodID, route.Sig, route.Doc); err != nil {
			return err
		}
	}
	return nil
}

func (t *FFITypeSchema) Owner() ModuleMemberName {
	if t == nil {
		return ModuleMemberName{}
	}
	return NewModuleMemberName(t.PackagePath, t.MemberName)
}

func (t *FFITypeSchema) CanonicalName() string {
	if t == nil {
		return ""
	}
	if name := t.Owner().CanonicalName(); name != "" {
		return name
	}
	return strings.TrimSpace(t.Name)
}

func (s *FFISurfaceSchema) Merge(next *FFISurfaceSchema) error {
	if next == nil {
		return nil
	}
	if s.err != nil {
		return s.err
	}
	if next.err != nil {
		return next.err
	}
	if s.Version == "" {
		s.Version = FFISurfaceHashVersion
	}
	for path, pkg := range next.Packages {
		if pkg == nil {
			continue
		}
		target := s.EnsurePackage(path)
		if target == nil {
			continue
		}
		for name, member := range pkg.Members {
			if existing := target.Members[name]; existing != nil && existing.Hash() != member.Hash() {
				return &SchemaConflictError{Kind: "surface member", Name: QualifiedMemberName(path, name), Existing: existing.Hash(), New: member.Hash()}
			}
			target.Members[name] = cloneFFIMemberSchema(member)
		}
	}
	if s.Types == nil {
		s.Types = make(map[string]*FFITypeSchema)
	}
	for name, typ := range next.Types {
		if typ == nil {
			continue
		}
		typeName := typ.CanonicalName()
		if typeName == "" {
			typeName = strings.TrimSpace(name)
		}
		target := s.ensureType(typ.PackagePath, typ.MemberName)
		if target == nil {
			continue
		}
		if typ.Struct != nil {
			if target.Struct != nil {
				if _, err := MergeStructSchema(typeName, target.Struct, typ.Struct); err != nil {
					return &SchemaConflictError{Kind: "surface type", Name: typeName, Existing: target.Hash(), New: typ.Hash()}
				}
			} else {
				target.Struct = CloneRuntimeStructSpec(typ.Struct)
			}
		}
		if typ.Interface != nil {
			if target.Interface != nil {
				if err := CheckInterfaceSchemaCompatible(typeName, target.Interface, typ.Interface); err != nil {
					return &SchemaConflictError{Kind: "surface type", Name: typeName, Existing: target.Hash(), New: typ.Hash()}
				}
			} else {
				target.Interface = CloneRuntimeInterfaceSpec(typ.Interface)
			}
		}
		if len(typ.Methods) > 0 {
			if target.Methods == nil {
				target.Methods = make(map[string]*FFIRouteSpec, len(typ.Methods))
			}
			for methodName, method := range typ.Methods {
				if existing := target.Methods[methodName]; existing != nil && routeSpecHash(existing) != routeSpecHash(method) {
					return &SchemaConflictError{
						Kind:     "surface type method",
						Name:     QualifiedMemberName(typeName, methodName),
						Existing: routeSpecHash(existing),
						New:      routeSpecHash(method),
					}
				}
				target.Methods[methodName] = cloneFFIRouteSpec(method)
			}
		}
		s.addTypeMember(target.PackagePath, target.MemberName, target.CanonicalName())
	}
	return nil
}

func (m *FFIMemberSchema) Hash() string {
	if m == nil {
		return VersionedModuleRequirementHash("member", "")
	}
	switch m.Kind {
	case FFIMemberFunc:
		routeName := ""
		methodID := ""
		sigHash := ""
		if m.Route != nil {
			routeName = m.Route.RouteName
			methodID = strconv.FormatUint(uint64(m.Route.MethodID), 10)
			sigHash = FuncSchemaHash(m.Route.Sig)
		}
		return VersionedModuleRequirementHash("func", m.Name, routeName, methodID, sigHash, m.Doc)
	case FFIMemberConst:
		value := FFIConstValue{}
		if m.Const != nil {
			value = m.Const.Value
		}
		return VersionedModuleRequirementHash("const", m.Name, value.Hash())
	case FFIMemberValue:
		specHash := ""
		if m.Value != nil {
			specHash = ValueSchemaHash(m.Value.Spec)
		}
		return VersionedModuleRequirementHash("value", m.Name, specHash, strconv.FormatBool(m.ReadOnly))
	case FFIMemberType:
		typePkg := ""
		typeMember := ""
		typeName := ""
		if m.Type != nil {
			typePkg = m.Type.PackagePath
			typeMember = m.Type.MemberName
			typeName = m.Type.Name
		}
		return VersionedModuleRequirementHash("type", m.Name, typePkg, typeMember, typeName)
	default:
		return VersionedModuleRequirementHash(string(m.Kind), m.Name)
	}
}

func (t *FFITypeSchema) Hash() string {
	if t == nil {
		return VersionedModuleRequirementHash("type", "")
	}
	parts := []string{"type", t.PackagePath, t.MemberName, t.CanonicalName()}
	if t.Struct != nil {
		parts = append(parts, "struct", StructSchemaHash(t.Struct))
	}
	if t.Interface != nil {
		parts = append(parts, "interface", InterfaceSchemaHash(t.Interface))
	}
	if len(t.Methods) > 0 {
		names := make([]string, 0, len(t.Methods))
		for name := range t.Methods {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			parts = append(parts, "method", name, routeSpecHash(t.Methods[name]))
		}
	}
	return VersionedModuleRequirementHash(parts...)
}

func routeSpecHash(route *FFIRouteSpec) string {
	if route == nil {
		return VersionedModuleRequirementHash("route", "")
	}
	return VersionedModuleRequirementHash(
		"route",
		route.RouteName,
		strconv.FormatUint(uint64(route.MethodID), 10),
		FuncSchemaHash(route.Sig),
		route.Doc,
	)
}

func NewBoundFFISurface(schema *FFISurfaceSchema) *BoundFFISurface {
	return &BoundFFISurface{
		Schema:        CloneFFISurfaceSchema(schema),
		Packages:      make(map[string]*BoundFFIPackage),
		Routes:        make(map[string]FFIRoute),
		Consts:        make(map[string]FFIConstValue),
		PackageValues: make(map[string]*BoundPackageValue),
		Structs:       make(map[string]*RuntimeStructSpec),
		Interfaces:    make(map[string]*RuntimeInterfaceSpec),
	}
}

func NewBoundFFISurfaceFromSchema(schema *FFISurfaceSchema) *BoundFFISurface {
	bound := NewBoundFFISurface(schema)
	if schema == nil {
		return bound
	}
	for path, pkg := range schema.Packages {
		if pkg == nil {
			continue
		}
		pkgPath := pkg.Path
		if pkgPath == "" {
			pkgPath = path
		}
		for name, member := range pkg.Members {
			if member == nil {
				continue
			}
			switch member.Kind {
			case FFIMemberConst:
				if member.Const != nil {
					bound.AddConst(pkgPath, name, member.Const.Value)
				}
			}
		}
	}
	for _, typ := range schema.Types {
		if typ == nil {
			continue
		}
		typeName := typ.CanonicalName()
		bound.AddTypeMember(typ.PackagePath, typ.MemberName, MustParseRuntimeType(TypeSpec(typeName)))
		if typ.Struct != nil {
			bound.AddStruct(typ.PackagePath, typ.MemberName, typ.Struct)
		}
		if typ.Interface != nil {
			bound.AddInterface(typ.PackagePath, typ.MemberName, typ.Interface)
		}
	}
	return bound
}

func (b *BoundFFISurface) BindSchemaRoutes(schema *FFISurfaceSchema, bridge ffigo.FFIBridge) error {
	if b == nil || schema == nil {
		return nil
	}
	bindRoute := func(routeName string, route FFIRoute) error {
		if b.Routes == nil {
			b.Routes = make(map[string]FFIRoute)
		}
		if existing, ok := b.Routes[routeName]; ok {
			if err := CheckRouteCompatible(routeName, existing, route); err != nil {
				return err
			}
		}
		b.Routes[routeName] = route
		return nil
	}
	for path, pkg := range schema.Packages {
		if pkg == nil {
			continue
		}
		pkgPath := pkg.Path
		if pkgPath == "" {
			pkgPath = path
		}
		for memberName, member := range pkg.Members {
			if member == nil || member.Kind != FFIMemberFunc {
				continue
			}
			if member.Route == nil {
				return fmt.Errorf("ffi route %s missing schema", QualifiedMemberName(pkgPath, memberName))
			}
			routeName := member.Route.RouteName
			if routeName == "" {
				routeName = QualifiedMemberName(pkgPath, memberName)
			}
			route := FFIRoute{
				Name:     routeName,
				Bridge:   bridge,
				MethodID: member.Route.MethodID,
				FuncSig:  CloneRuntimeFuncSig(member.Route.Sig),
				Doc:      member.Route.Doc,
			}
			if err := bindRoute(routeName, route); err != nil {
				return err
			}
			pkg := b.EnsurePackage(pkgPath)
			if pkg != nil {
				pkg.Members[memberName] = &BoundFFIMember{Name: memberName, Kind: FFIMemberFunc, ReadOnly: true, RouteName: routeName}
			}
		}
	}
	for _, typ := range schema.Types {
		if typ == nil {
			continue
		}
		typeName := typ.CanonicalName()
		b.AddTypeMember(typ.PackagePath, typ.MemberName, MustParseRuntimeType(TypeSpec(typeName)))
		for methodName, method := range typ.Methods {
			if method == nil {
				return fmt.Errorf("ffi type method %s missing schema", QualifiedMemberName(typeName, methodName))
			}
			routeName := method.RouteName
			if routeName == "" {
				routeName = QualifiedMemberName(typeName, methodName)
			}
			if err := bindRoute(routeName, FFIRoute{
				Name:     routeName,
				Bridge:   bridge,
				MethodID: method.MethodID,
				FuncSig:  CloneRuntimeFuncSig(method.Sig),
				Doc:      method.Doc,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *BoundFFISurface) EnsurePackage(path string) *BoundFFIPackage {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if b.Packages == nil {
		b.Packages = make(map[string]*BoundFFIPackage)
	}
	pkg := b.Packages[path]
	if pkg == nil {
		pkg = &BoundFFIPackage{Path: path, Members: make(map[string]*BoundFFIMember)}
		b.Packages[path] = pkg
	}
	return pkg
}

func (b *BoundFFISurface) AddRoute(pkgPath, member string, route FFIRoute) {
	pkg := b.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	if route.Name == "" {
		route.Name = QualifiedMemberName(pkgPath, member)
	}
	if b.Routes == nil {
		b.Routes = make(map[string]FFIRoute)
	}
	b.Routes[route.Name] = route
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberFunc, ReadOnly: true, RouteName: route.Name}
}

func (b *BoundFFISurface) AddConst(pkgPath, member string, value FFIConstValue) {
	pkg := b.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	name := QualifiedMemberName(pkgPath, member)
	if b.Consts == nil {
		b.Consts = make(map[string]FFIConstValue)
	}
	b.Consts[name] = value
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberConst, ReadOnly: true, Const: value}
}

func (b *BoundFFISurface) AddPackageValue(pkgPath, member string, spec *ValueSpec, value *Var) {
	pkg := b.EnsurePackage(pkgPath)
	if pkg == nil || member == "" || value == nil {
		return
	}
	name := QualifiedMemberName(pkgPath, member)
	if b.PackageValues == nil {
		b.PackageValues = make(map[string]*BoundPackageValue)
	}
	if spec == nil {
		spec = &ValueSpec{ReadOnly: true}
	}
	b.PackageValues[name] = &BoundPackageValue{Name: name, Spec: spec, Value: value}
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberValue, Type: spec.Type, ReadOnly: spec.ReadOnly, Value: value}
}

func (b *BoundFFISurface) AddTypeMember(pkgPath, member string, typ RuntimeType) {
	pkg := b.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	if typ.IsEmpty() {
		typ = MustParseRuntimeType(TypeSpec(QualifiedMemberName(pkgPath, member)))
	}
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberType, Type: typ, ReadOnly: true}
}

func (b *BoundFFISurface) AddStruct(pkgPath, member string, spec *RuntimeStructSpec) {
	if pkgPath == "" || member == "" || spec == nil {
		return
	}
	name := QualifiedMemberName(pkgPath, member)
	if b.Structs == nil {
		b.Structs = make(map[string]*RuntimeStructSpec)
	}
	b.Structs[name] = CloneRuntimeStructSpec(spec)
	b.AddTypeMember(pkgPath, member, MustParseRuntimeType(TypeSpec(name)))
}

func (b *BoundFFISurface) AddInterface(pkgPath, member string, spec *RuntimeInterfaceSpec) {
	if pkgPath == "" || member == "" || spec == nil {
		return
	}
	name := QualifiedMemberName(pkgPath, member)
	if b.Interfaces == nil {
		b.Interfaces = make(map[string]*RuntimeInterfaceSpec)
	}
	b.Interfaces[name] = CloneRuntimeInterfaceSpec(spec)
	b.AddTypeMember(pkgPath, member, MustParseRuntimeType(TypeSpec(name)))
}

func (b *BoundFFISurface) Merge(next *BoundFFISurface) error {
	if next == nil {
		return nil
	}
	if b.Schema == nil {
		b.Schema = NewFFISurfaceSchema()
	}
	if err := b.Schema.Merge(next.Schema); err != nil {
		return err
	}
	for path, pkg := range next.Packages {
		target := b.EnsurePackage(path)
		if target == nil || pkg == nil {
			continue
		}
		for name, member := range pkg.Members {
			target.Members[name] = member
		}
	}
	for name, route := range next.Routes {
		b.Routes[name] = route
	}
	for name, value := range next.Consts {
		b.Consts[name] = value
	}
	for name, value := range next.PackageValues {
		b.PackageValues[name] = value
	}
	for name, spec := range next.Structs {
		b.Structs[name] = CloneRuntimeStructSpec(spec)
	}
	for name, spec := range next.Interfaces {
		b.Interfaces[name] = CloneRuntimeInterfaceSpec(spec)
	}
	return nil
}

func QualifiedMemberName(pkg, member string) string {
	pkg = strings.TrimSpace(pkg)
	member = strings.TrimSpace(member)
	if pkg == "" {
		return member
	}
	if member == "" {
		return pkg
	}
	return pkg + "." + member
}

func ModuleRequirementHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func VersionedModuleRequirementHash(parts ...string) string {
	return FFISurfaceHashVersion + ":" + ModuleRequirementHash(parts...)
}

func FuncSchemaHash(sig *RuntimeFuncSig) string {
	if sig == nil {
		return VersionedModuleRequirementHash("func", "")
	}
	modes := make([]string, len(sig.ParamModes))
	for i, mode := range sig.ParamModes {
		modes[i] = string(mode)
	}
	return VersionedModuleRequirementHash("func", string(sig.Spec), strings.Join(modes, ","))
}

func FuncRouteHash(methodID uint32, sig *RuntimeFuncSig) string {
	if sig == nil {
		return VersionedModuleRequirementHash("func", strconv.FormatUint(uint64(methodID), 10), "")
	}
	modes := make([]string, len(sig.ParamModes))
	for i, mode := range sig.ParamModes {
		modes[i] = string(mode)
	}
	return VersionedModuleRequirementHash("func", strconv.FormatUint(uint64(methodID), 10), string(sig.Spec), strings.Join(modes, ","))
}

func ValueSchemaHash(spec *ValueSpec) string {
	if spec == nil {
		return VersionedModuleRequirementHash("value", "")
	}
	return VersionedModuleRequirementHash("value", spec.Type.Raw.String(), strconv.FormatBool(spec.ReadOnly))
}

func ConstSchemaHash(value FFIConstValue) string {
	return value.Hash()
}

func StructSchemaHash(spec *RuntimeStructSpec) string {
	if spec == nil {
		return VersionedModuleRequirementHash("type", "")
	}
	tags := make([]string, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		if field.Tag != "" {
			tags = append(tags, field.Name+"="+field.Tag)
		}
	}
	return VersionedModuleRequirementHash("struct", spec.Name, string(spec.Ownership), string(spec.Spec), strings.Join(tags, "\x00"))
}

func InterfaceSchemaHash(spec *RuntimeInterfaceSpec) string {
	if spec == nil {
		return VersionedModuleRequirementHash("type", "")
	}
	return VersionedModuleRequirementHash("interface", string(spec.Spec))
}

func (e *Executor) ValidateModuleRequirements() error {
	if e == nil || len(e.moduleRequirements) == 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, req := range e.moduleRequirements {
		if err := e.validateModuleRequirementLocked(req); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) validateModuleRequirementLocked(req ModuleRequirement) error {
	pkg := req.Path
	member := req.MemberName
	name := QualifiedMemberName(pkg, member)
	switch req.Kind {
	case ModuleKindSource:
		module := e.modules.lookup(pkg)
		if module == nil || module.Kind != ModuleKindSource || module.Prepared == nil {
			return fmt.Errorf("missing source module %s", pkg)
		}
		got := module.Hash
		if got == "" {
			return fmt.Errorf("missing source module hash %s", pkg)
		}
		if req.Hash != "" && got != req.Hash {
			return fmt.Errorf("source module %s schema mismatch", pkg)
		}
	case ModuleKindFFI:
		switch req.MemberKind {
		case FFIMemberFunc:
			return e.validateFFIFuncRequirement(pkg, member, name, req)
		case FFIMemberConst:
			return e.validateFFIConstRequirement(pkg, member, name, req)
		case FFIMemberValue:
			return e.validateFFIValueRequirement(pkg, member, name, req)
		case FFIMemberType:
			return e.validateFFITypeRequirement(pkg, member, name, req)
		default:
			return fmt.Errorf("unsupported ffi requirement kind %s for %s", req.MemberKind, name)
		}
	default:
		return fmt.Errorf("unsupported module requirement kind %s for %s", req.Kind, name)
	}
	return nil
}

func (e *Executor) validateFFIFuncRequirement(pkg, member, name string, req ModuleRequirement) error {
	if req.TypeName == "" {
		if err := e.validateModuleMemberRequirement(pkg, member, req.MemberKind); err != nil {
			return err
		}
	} else {
		if err := e.validateModuleMemberRequirement(pkg, member, FFIMemberType); err != nil {
			return err
		}
		name = QualifiedMemberName(req.TypeName, req.MethodName)
	}
	route, ok := e.routes[name]
	if !ok {
		return fmt.Errorf("missing external FFI function %s", name)
	}
	if req.MethodID != route.MethodID {
		return fmt.Errorf("external FFI function %s method id mismatch", name)
	}
	if got := FuncRouteHash(route.MethodID, route.FuncSig); req.Hash != "" && got != req.Hash {
		return fmt.Errorf("external FFI function %s schema mismatch", name)
	}
	return nil
}

func (e *Executor) validateFFIConstRequirement(pkg, member, name string, req ModuleRequirement) error {
	if err := e.validateModuleMemberRequirement(pkg, member, req.MemberKind); err != nil {
		return err
	}
	value, ok := e.consts[name]
	if !ok {
		return fmt.Errorf("missing external FFI constant %s", name)
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("invalid external FFI constant %s: %w", name, err)
	}
	if got := ConstSchemaHash(value); req.Hash != "" && got != req.Hash {
		return fmt.Errorf("external FFI constant %s schema mismatch", name)
	}
	return nil
}

func (e *Executor) validateFFIValueRequirement(pkg, member, name string, req ModuleRequirement) error {
	if err := e.validateModuleMemberRequirement(pkg, member, req.MemberKind); err != nil {
		return err
	}
	value, ok := e.packageValues[name]
	if !ok || value == nil || value.Spec == nil {
		return fmt.Errorf("missing external FFI package value %s", name)
	}
	if got := ValueSchemaHash(value.Spec); req.Hash != "" && got != req.Hash {
		return fmt.Errorf("external FFI package value %s schema mismatch", name)
	}
	return nil
}

func (e *Executor) validateFFITypeRequirement(pkg, member, name string, req ModuleRequirement) error {
	if err := e.validateModuleMemberRequirement(pkg, member, req.MemberKind); err != nil {
		return err
	}
	if req.TypeName != "" {
		name = req.TypeName
	}
	if spec, ok := e.metadata.structsByName[name]; ok {
		if got := StructSchemaHash(spec); req.Hash != "" && got != req.Hash {
			return fmt.Errorf("external FFI type %s schema mismatch", name)
		}
		return nil
	}
	if spec, ok := e.metadata.interfacesByName[name]; ok {
		if got := InterfaceSchemaHash(spec); req.Hash != "" && got != req.Hash {
			return fmt.Errorf("external FFI type %s schema mismatch", name)
		}
		return nil
	}
	return fmt.Errorf("missing external FFI type %s", name)
}

func (e *Executor) validateModuleMemberRequirement(pkg, member string, kind FFIMemberKind) error {
	module := e.modules.lookup(pkg)
	if module == nil || module.Kind != ModuleKindFFI {
		return fmt.Errorf("missing ffi module %s", pkg)
	}
	modMember := module.Members[member]
	if modMember == nil {
		return fmt.Errorf("missing ffi module member %s.%s", pkg, member)
	}
	if modMember.Kind != kind {
		return fmt.Errorf("ffi module member %s.%s kind mismatch", pkg, member)
	}
	return nil
}

func (e *Executor) lookupFFIPackage(path string) (*BoundFFIPackage, bool) {
	if e == nil || path == "" {
		return nil, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	pkg, ok := e.ffiPackages[path]
	return pkg, ok && pkg != nil
}

func (e *Executor) lookupRuntimeModule(path string) (*runtimeModule, bool) {
	if e == nil || path == "" {
		return nil, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	module, ok := e.modules.Lookup(path)
	return module, ok
}

func (e *Executor) ApplyBoundFFISurface(surface *BoundFFISurface) error {
	if e == nil || surface == nil {
		return nil
	}
	if err := CheckPublicFFISurfaceSchema(surface.Schema); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	stagedRoutes := cloneFFIRouteMap(e.routes)
	stagedMetadata := cloneRuntimeMetadataRegistry(e.metadata)
	stagedPackageValues := cloneBoundPackageValueMap(e.packageValues)
	stagedConsts := cloneFFIConstValueMap(e.consts)
	if stagedConsts == nil {
		stagedConsts = make(map[string]FFIConstValue)
	}
	stagedPackages := cloneBoundFFIPackageMap(e.ffiPackages)
	stagedModules := cloneRuntimeModuleRegistry(e.modules)

	for name, route := range surface.Routes {
		if route.Name == "" {
			route.Name = name
		}
		if err := CheckPublicFFIRouteSchema(name, route); err != nil {
			return err
		}
		if existing, ok := stagedRoutes[name]; ok {
			if err := CheckRouteCompatible(name, existing, route); err != nil {
				return err
			}
		}
		stagedRoutes[name] = route
	}
	for name, spec := range surface.Structs {
		if spec == nil {
			continue
		}
		if err := CheckPublicFFIStructSchema(name, spec); err != nil {
			return err
		}
		if existing, ok := stagedMetadata.structsByName[name]; ok {
			merged, err := MergeStructSchema(name, existing, spec)
			if err != nil {
				return err
			}
			spec = merged
		}
		stagedMetadata.registerStructSchema(name, CloneRuntimeStructSpec(spec))
	}
	for name, spec := range surface.Interfaces {
		if spec == nil {
			continue
		}
		if err := CheckPublicFFIInterfaceSchema(name, spec); err != nil {
			return err
		}
		if existing, ok := stagedMetadata.interfacesByName[name]; ok {
			if err := CheckInterfaceSchemaCompatible(name, existing, spec); err != nil {
				return err
			}
		}
		stagedMetadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
	}
	for name, value := range surface.PackageValues {
		if value != nil {
			if err := CheckPublicFFIValueSpec(name, value.Spec); err != nil {
				return err
			}
			if existing, ok := stagedPackageValues[name]; ok && existing != nil && existing.Spec != nil && value.Spec != nil && existing.Spec.Type.Raw != value.Spec.Type.Raw {
				return &SchemaConflictError{
					Kind:     "package value",
					Name:     name,
					Existing: existing.Spec.Type.Raw.String(),
					New:      value.Spec.Type.Raw.String(),
				}
			}
		}
		stagedPackageValues[name] = cloneBoundPackageValue(value)
	}
	for name, value := range surface.Consts {
		if err := value.Validate(); err != nil {
			return fmt.Errorf("invalid external FFI constant %s: %w", name, err)
		}
		if existing, ok := stagedConsts[name]; ok && existing.Hash() != value.Hash() {
			return &SchemaConflictError{
				Kind:     "constant",
				Name:     name,
				Existing: existing.Hash(),
				New:      value.Hash(),
			}
		}
		stagedConsts[name] = value
	}
	for path, pkg := range surface.Packages {
		if pkg == nil {
			continue
		}
		target := stagedPackages[path]
		if target == nil {
			target = &BoundFFIPackage{Path: path, Members: make(map[string]*BoundFFIMember)}
			stagedPackages[path] = target
		}
		for name, member := range pkg.Members {
			target.Members[name] = cloneBoundFFIMember(member)
		}
		if err := stagedModules.RegisterFFIPackage(target); err != nil {
			return err
		}
	}

	e.routes = stagedRoutes
	e.metadata = stagedMetadata
	e.packageValues = stagedPackageValues
	e.consts = stagedConsts
	e.ffiPackages = stagedPackages
	e.modules = stagedModules
	return nil
}

func cloneFFIRouteMap(in map[string]FFIRoute) map[string]FFIRoute {
	out := make(map[string]FFIRoute, len(in))
	for name, route := range in {
		route.FuncSig = CloneRuntimeFuncSig(route.FuncSig)
		out[name] = route
	}
	return out
}

func cloneBoundPackageValue(in *BoundPackageValue) *BoundPackageValue {
	if in == nil {
		return nil
	}
	out := *in
	out.Spec = cloneValueSpec(in.Spec)
	return &out
}

func cloneBoundPackageValueMap(in map[string]*BoundPackageValue) map[string]*BoundPackageValue {
	out := make(map[string]*BoundPackageValue, len(in))
	for name, value := range in {
		out[name] = cloneBoundPackageValue(value)
	}
	return out
}

func cloneBoundFFIMember(in *BoundFFIMember) *BoundFFIMember {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneBoundFFIPackageMap(in map[string]*BoundFFIPackage) map[string]*BoundFFIPackage {
	out := make(map[string]*BoundFFIPackage, len(in))
	for path, pkg := range in {
		if pkg == nil {
			continue
		}
		cloned := &BoundFFIPackage{
			Path:    pkg.Path,
			Members: make(map[string]*BoundFFIMember, len(pkg.Members)),
		}
		for name, member := range pkg.Members {
			cloned.Members[name] = cloneBoundFFIMember(member)
		}
		out[path] = cloned
	}
	return out
}
