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
	FFIMemberFunc   FFIMemberKind = "func"
	FFIMemberConst  FFIMemberKind = "const"
	FFIMemberValue  FFIMemberKind = "value"
	FFIMemberType   FFIMemberKind = "type"
	FFIMemberModule FFIMemberKind = "module"
)

type ExternalRequirement struct {
	Version     string        `json:"version,omitempty"`
	PackagePath string        `json:"package_path,omitempty"`
	MemberName  string        `json:"member_name,omitempty"`
	Kind        FFIMemberKind `json:"kind"`
	Type        TypeSpec      `json:"type,omitempty"`
	TypeName    string        `json:"type_name,omitempty"`
	MethodName  string        `json:"method_name,omitempty"`
	MethodID    uint32        `json:"method_id,omitempty"`
	Hash        string        `json:"hash,omitempty"`
}

type FFISurfaceSchema struct {
	Version  string                       `json:"version"`
	Packages map[string]*FFIPackageSchema `json:"packages,omitempty"`
	Types    map[string]*FFITypeSchema    `json:"types,omitempty"`
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
	Name string `json:"name"`
}

type FFITypeSchema struct {
	Name      string                   `json:"name"`
	Struct    *RuntimeStructSpec       `json:"struct,omitempty"`
	Interface *RuntimeInterfaceSpec    `json:"interface,omitempty"`
	Methods   map[string]*FFIRouteSpec `json:"methods,omitempty"`
}

type FFIRouteDecl struct {
	PackagePath string
	MemberName  string
	TypeName    string
	MethodName  string
	RouteName   string
	MethodID    uint32
	Sig         *RuntimeFuncSig
	Doc         string
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
			Name:      typ.Name,
			Struct:    CloneRuntimeStructSpec(typ.Struct),
			Interface: CloneRuntimeInterfaceSpec(typ.Interface),
			Methods:   cloneFFIRouteSpecMap(typ.Methods),
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
		out.Type = &FFITypeRef{Name: member.Type.Name}
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

func (s *FFISurfaceSchema) ensureType(name string) *FFITypeSchema {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if s.Types == nil {
		s.Types = make(map[string]*FFITypeSchema)
	}
	typ := s.Types[name]
	if typ == nil {
		typ = &FFITypeSchema{Name: name}
		s.Types[name] = typ
	}
	if typ.Name == "" {
		typ.Name = name
	}
	return typ
}

func (s *FFISurfaceSchema) AddFunc(pkgPath, member, routeName string, methodID uint32, sig *RuntimeFuncSig, doc string) {
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	if routeName == "" {
		routeName = ExternalFullName(pkgPath, member)
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberFunc,
		ReadOnly: true,
		Route:    &FFIRouteSpec{RouteName: routeName, MethodID: methodID, Sig: CloneRuntimeFuncSig(sig), Doc: doc},
		Doc:      doc,
	}
}

func (s *FFISurfaceSchema) AddConst(pkgPath, member string, value FFIConstValue) {
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberConst,
		ReadOnly: true,
		Const:    &FFIConstSpec{Value: value},
	}
}

func (s *FFISurfaceSchema) AddValue(pkgPath, member string, spec *ValueSpec) {
	pkg := s.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	pkg.Members[member] = &FFIMemberSchema{
		Name:     member,
		Kind:     FFIMemberValue,
		ReadOnly: spec == nil || spec.ReadOnly,
		Value:    &FFIValueSpec{Spec: cloneValueSpec(spec)},
	}
}

func (s *FFISurfaceSchema) AddStruct(name string, spec *RuntimeStructSpec) {
	typ := s.ensureType(name)
	if typ == nil || spec == nil {
		return
	}
	typ.Struct = CloneRuntimeStructSpec(spec)
}

func (s *FFISurfaceSchema) AddInterface(name string, spec *RuntimeInterfaceSpec) {
	typ := s.ensureType(name)
	if typ == nil || spec == nil {
		return
	}
	typ.Interface = CloneRuntimeInterfaceSpec(spec)
}

func (s *FFISurfaceSchema) AddTypeMethod(typeName, methodName, routeName string, methodID uint32, sig *RuntimeFuncSig, doc string) {
	typ := s.ensureType(typeName)
	methodName = strings.TrimSpace(methodName)
	if typ == nil || methodName == "" {
		return
	}
	if routeName == "" {
		routeName = ExternalFullName(typ.Name, methodName)
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
}

func (s *FFISurfaceSchema) AddRouteDecls(routes []FFIRouteDecl) {
	for _, route := range routes {
		if route.TypeName != "" {
			s.AddTypeMethod(route.TypeName, route.MethodName, route.RouteName, route.MethodID, route.Sig, route.Doc)
			continue
		}
		s.AddFunc(route.PackagePath, route.MemberName, route.RouteName, route.MethodID, route.Sig, route.Doc)
	}
}

func (s *FFISurfaceSchema) Merge(next *FFISurfaceSchema) error {
	if next == nil {
		return nil
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
				return &SchemaConflictError{Kind: "surface member", Name: ExternalFullName(path, name), Existing: existing.Hash(), New: member.Hash()}
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
		target := s.ensureType(name)
		if target == nil {
			continue
		}
		if typ.Struct != nil {
			if target.Struct != nil {
				if _, err := MergeStructSchema(name, target.Struct, typ.Struct); err != nil {
					return &SchemaConflictError{Kind: "surface type", Name: name, Existing: target.Hash(), New: typ.Hash()}
				}
			} else {
				target.Struct = CloneRuntimeStructSpec(typ.Struct)
			}
		}
		if typ.Interface != nil {
			if target.Interface != nil {
				if err := CheckInterfaceSchemaCompatible(name, target.Interface, typ.Interface); err != nil {
					return &SchemaConflictError{Kind: "surface type", Name: name, Existing: target.Hash(), New: typ.Hash()}
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
						Name:     ExternalFullName(name, methodName),
						Existing: routeSpecHash(existing),
						New:      routeSpecHash(method),
					}
				}
				target.Methods[methodName] = cloneFFIRouteSpec(method)
			}
		}
	}
	return nil
}

func (m *FFIMemberSchema) Hash() string {
	if m == nil {
		return VersionedExternalRequirementHash("member", "")
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
		return VersionedExternalRequirementHash("func", m.Name, routeName, methodID, sigHash, m.Doc)
	case FFIMemberConst:
		value := FFIConstValue{}
		if m.Const != nil {
			value = m.Const.Value
		}
		return VersionedExternalRequirementHash("const", m.Name, value.Hash())
	case FFIMemberValue:
		specHash := ""
		if m.Value != nil {
			specHash = ValueSchemaHash(m.Value.Spec)
		}
		return VersionedExternalRequirementHash("value", m.Name, specHash, strconv.FormatBool(m.ReadOnly))
	case FFIMemberType:
		typeName := ""
		if m.Type != nil {
			typeName = m.Type.Name
		}
		return VersionedExternalRequirementHash("type", m.Name, typeName)
	default:
		return VersionedExternalRequirementHash(string(m.Kind), m.Name)
	}
}

func (t *FFITypeSchema) Hash() string {
	if t == nil {
		return VersionedExternalRequirementHash("type", "")
	}
	parts := []string{"type", t.Name}
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
	return VersionedExternalRequirementHash(parts...)
}

func routeSpecHash(route *FFIRouteSpec) string {
	if route == nil {
		return VersionedExternalRequirementHash("route", "")
	}
	return VersionedExternalRequirementHash(
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
	for name, typ := range schema.Types {
		if typ == nil {
			continue
		}
		if typ.Struct != nil {
			bound.AddStruct(name, typ.Struct)
		}
		if typ.Interface != nil {
			bound.AddInterface(name, typ.Interface)
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
				return fmt.Errorf("ffi route %s missing schema", ExternalFullName(pkgPath, memberName))
			}
			routeName := member.Route.RouteName
			if routeName == "" {
				routeName = ExternalFullName(pkgPath, memberName)
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
	for typeName, typ := range schema.Types {
		if typ == nil {
			continue
		}
		for methodName, method := range typ.Methods {
			if method == nil {
				return fmt.Errorf("ffi type method %s missing schema", ExternalFullName(typeName, methodName))
			}
			routeName := method.RouteName
			if routeName == "" {
				routeName = ExternalFullName(typeName, methodName)
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
		route.Name = ExternalFullName(pkgPath, member)
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
	name := ExternalFullName(pkgPath, member)
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
	name := ExternalFullName(pkgPath, member)
	if b.PackageValues == nil {
		b.PackageValues = make(map[string]*BoundPackageValue)
	}
	if spec == nil {
		spec = &ValueSpec{ReadOnly: true}
	}
	b.PackageValues[name] = &BoundPackageValue{Name: name, Spec: spec, Value: value}
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberValue, Type: spec.Type, ReadOnly: spec.ReadOnly, Value: value}
}

func (b *BoundFFISurface) AddStruct(name string, spec *RuntimeStructSpec) {
	if name == "" || spec == nil {
		return
	}
	if b.Structs == nil {
		b.Structs = make(map[string]*RuntimeStructSpec)
	}
	b.Structs[name] = CloneRuntimeStructSpec(spec)
}

func (b *BoundFFISurface) AddInterface(name string, spec *RuntimeInterfaceSpec) {
	if name == "" || spec == nil {
		return
	}
	if b.Interfaces == nil {
		b.Interfaces = make(map[string]*RuntimeInterfaceSpec)
	}
	b.Interfaces[name] = CloneRuntimeInterfaceSpec(spec)
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

func ExternalFullName(pkg, member string) string {
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

func SplitExternalName(name string) (pkg, member string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	lastDot := strings.LastIndex(name, ".")
	if lastDot <= 0 || lastDot == len(name)-1 {
		return "", ""
	}
	prefix := name[:lastDot]
	leaf := name[lastDot+1:]
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 {
		lastSegment := prefix[slash+1:]
		if dot := strings.Index(lastSegment, "."); dot >= 0 {
			pkg = prefix[:slash+1] + lastSegment[:dot]
			member = lastSegment[dot+1:] + "." + leaf
			return pkg, member
		}
		return prefix, leaf
	}
	if dot := strings.Index(prefix, "."); dot >= 0 {
		return prefix[:dot], prefix[dot+1:] + "." + leaf
	}
	return prefix, leaf
}

func ExternalRequirementHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func VersionedExternalRequirementHash(parts ...string) string {
	return FFISurfaceHashVersion + ":" + ExternalRequirementHash(parts...)
}

func FuncSchemaHash(sig *RuntimeFuncSig) string {
	if sig == nil {
		return VersionedExternalRequirementHash("func", "")
	}
	modes := make([]string, len(sig.ParamModes))
	for i, mode := range sig.ParamModes {
		modes[i] = string(mode)
	}
	return VersionedExternalRequirementHash("func", string(sig.Spec), strings.Join(modes, ","))
}

func FuncRouteHash(methodID uint32, sig *RuntimeFuncSig) string {
	if sig == nil {
		return VersionedExternalRequirementHash("func", strconv.FormatUint(uint64(methodID), 10), "")
	}
	modes := make([]string, len(sig.ParamModes))
	for i, mode := range sig.ParamModes {
		modes[i] = string(mode)
	}
	return VersionedExternalRequirementHash("func", strconv.FormatUint(uint64(methodID), 10), string(sig.Spec), strings.Join(modes, ","))
}

func ValueSchemaHash(spec *ValueSpec) string {
	if spec == nil {
		return VersionedExternalRequirementHash("value", "")
	}
	return VersionedExternalRequirementHash("value", spec.Type.Raw.String(), strconv.FormatBool(spec.ReadOnly))
}

func ConstSchemaHash(value FFIConstValue) string {
	return value.Hash()
}

func StructSchemaHash(spec *RuntimeStructSpec) string {
	if spec == nil {
		return VersionedExternalRequirementHash("type", "")
	}
	return VersionedExternalRequirementHash("struct", spec.Name, string(spec.Ownership), string(spec.Spec))
}

func InterfaceSchemaHash(spec *RuntimeInterfaceSpec) string {
	if spec == nil {
		return VersionedExternalRequirementHash("type", "")
	}
	return VersionedExternalRequirementHash("interface", string(spec.Spec))
}

func (e *Executor) ValidateExternalRequirements() error {
	if e == nil || len(e.externalRequirements) == 0 {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, req := range e.externalRequirements {
		if err := e.validateExternalRequirementLocked(req); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) validateExternalRequirementLocked(req ExternalRequirement) error {
	pkg := req.PackagePath
	member := req.MemberName
	name := ExternalFullName(pkg, member)
	switch req.Kind {
	case FFIMemberModule:
		if e.embeddedModules[pkg] == nil {
			return fmt.Errorf("missing embedded VM module %s", pkg)
		}
		got := e.moduleHashes[pkg]
		if got == "" {
			return fmt.Errorf("missing embedded VM module hash %s", pkg)
		}
		if req.Hash != "" && got != req.Hash {
			return fmt.Errorf("embedded VM module %s schema mismatch", pkg)
		}
	case FFIMemberFunc:
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
	case FFIMemberConst:
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
	case FFIMemberValue:
		value, ok := e.packageValues[name]
		if !ok || value == nil || value.Spec == nil {
			return fmt.Errorf("missing external FFI package value %s", name)
		}
		if got := ValueSchemaHash(value.Spec); req.Hash != "" && got != req.Hash {
			return fmt.Errorf("external FFI package value %s schema mismatch", name)
		}
	case FFIMemberType:
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
		if req.TypeName != "" {
			if spec, ok := e.metadata.structsByName[req.TypeName]; ok {
				if got := StructSchemaHash(spec); req.Hash != "" && got != req.Hash {
					return fmt.Errorf("external FFI type %s schema mismatch", req.TypeName)
				}
				return nil
			}
			if spec, ok := e.metadata.interfacesByName[req.TypeName]; ok {
				if got := InterfaceSchemaHash(spec); req.Hash != "" && got != req.Hash {
					return fmt.Errorf("external FFI type %s schema mismatch", req.TypeName)
				}
				return nil
			}
		}
		return fmt.Errorf("missing external FFI type %s", name)
	default:
		return fmt.Errorf("unsupported external requirement kind %s for %s", req.Kind, name)
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
	}

	e.routes = stagedRoutes
	e.metadata = stagedMetadata
	e.packageValues = stagedPackageValues
	e.consts = stagedConsts
	e.ffiPackages = stagedPackages
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

func sortedBoundPackageMembers(pkg *BoundFFIPackage) []*BoundFFIMember {
	if pkg == nil || len(pkg.Members) == 0 {
		return nil
	}
	names := make([]string, 0, len(pkg.Members))
	for name := range pkg.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*BoundFFIMember, 0, len(names))
	for _, name := range names {
		out = append(out, pkg.Members[name])
	}
	return out
}
