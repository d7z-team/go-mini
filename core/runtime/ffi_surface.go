package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const FFISurfaceHashVersion = "ffi-surface-v1"

type FFIMemberKind string

const (
	FFIMemberFunc  FFIMemberKind = "func"
	FFIMemberConst FFIMemberKind = "const"
	FFIMemberValue FFIMemberKind = "value"
	FFIMemberType  FFIMemberKind = "type"
)

type ExternalRequirement struct {
	Version     string        `json:"version,omitempty"`
	Package     string        `json:"package,omitempty"`
	PackagePath string        `json:"package_path,omitempty"`
	Member      string        `json:"member,omitempty"`
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
	Value string `json:"value"`
}

type FFIValueSpec struct {
	Spec *ValueSpec `json:"spec,omitempty"`
}

type FFITypeRef struct {
	Name string `json:"name"`
}

type FFITypeSchema struct {
	Name      string                `json:"name"`
	Struct    *RuntimeStructSpec    `json:"struct,omitempty"`
	Interface *RuntimeInterfaceSpec `json:"interface,omitempty"`
}

type BoundFFIPackage struct {
	Path    string
	Members map[string]*BoundFFIMember
}

type BoundFFIMember struct {
	Name       string
	Kind       FFIMemberKind
	Type       RuntimeType
	ReadOnly   bool
	RouteName  string
	ConstValue string
	Value      *Var
}

type BoundFFISurface struct {
	Schema        *FFISurfaceSchema
	Packages      map[string]*BoundFFIPackage
	Routes        map[string]FFIRoute
	Consts        map[string]string
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
		out.Const = &FFIConstSpec{Value: member.Const.Value}
	}
	if member.Value != nil {
		out.Value = &FFIValueSpec{Spec: cloneValueSpec(member.Value.Spec)}
	}
	if member.Type != nil {
		out.Type = &FFITypeRef{Name: member.Type.Name}
	}
	return &out
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

func (s *FFISurfaceSchema) AddConst(pkgPath, member, value string) {
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
	name = strings.TrimSpace(name)
	if name == "" || spec == nil {
		return
	}
	if s.Types == nil {
		s.Types = make(map[string]*FFITypeSchema)
	}
	s.Types[name] = &FFITypeSchema{Name: name, Struct: CloneRuntimeStructSpec(spec)}
}

func (s *FFISurfaceSchema) AddInterface(name string, spec *RuntimeInterfaceSpec) {
	name = strings.TrimSpace(name)
	if name == "" || spec == nil {
		return
	}
	if s.Types == nil {
		s.Types = make(map[string]*FFITypeSchema)
	}
	s.Types[name] = &FFITypeSchema{Name: name, Interface: CloneRuntimeInterfaceSpec(spec)}
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
		if existing := s.Types[name]; existing != nil && existing.Hash() != typ.Hash() {
			return &SchemaConflictError{Kind: "surface type", Name: name, Existing: existing.Hash(), New: typ.Hash()}
		}
		s.Types[name] = &FFITypeSchema{Name: typ.Name, Struct: CloneRuntimeStructSpec(typ.Struct), Interface: CloneRuntimeInterfaceSpec(typ.Interface)}
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
		value := ""
		if m.Const != nil {
			value = m.Const.Value
		}
		return VersionedExternalRequirementHash("const", m.Name, value)
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
	if t.Struct != nil {
		return VersionedExternalRequirementHash("struct", StructSchemaHash(t.Struct))
	}
	if t.Interface != nil {
		return VersionedExternalRequirementHash("interface", InterfaceSchemaHash(t.Interface))
	}
	return VersionedExternalRequirementHash("type", t.Name)
}

func NewBoundFFISurface(schema *FFISurfaceSchema) *BoundFFISurface {
	return &BoundFFISurface{
		Schema:        CloneFFISurfaceSchema(schema),
		Packages:      make(map[string]*BoundFFIPackage),
		Routes:        make(map[string]FFIRoute),
		Consts:        make(map[string]string),
		PackageValues: make(map[string]*BoundPackageValue),
		Structs:       make(map[string]*RuntimeStructSpec),
		Interfaces:    make(map[string]*RuntimeInterfaceSpec),
	}
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

func (b *BoundFFISurface) AddConst(pkgPath, member, value string) {
	pkg := b.EnsurePackage(pkgPath)
	if pkg == nil || member == "" {
		return
	}
	name := ExternalFullName(pkgPath, member)
	if b.Consts == nil {
		b.Consts = make(map[string]string)
	}
	b.Consts[name] = value
	pkg.Members[member] = &BoundFFIMember{Name: member, Kind: FFIMemberConst, ReadOnly: true, ConstValue: value}
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

func ConstSchemaHash(value string) string {
	return VersionedExternalRequirementHash("const", value)
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
	if pkg == "" {
		pkg = req.Package
	}
	member := req.MemberName
	if member == "" {
		member = req.Member
	}
	name := ExternalFullName(pkg, member)
	switch req.Kind {
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

func (e *Executor) registerBoundPackageMemberLocked(pkg string, member *BoundFFIMember) {
	if pkg == "" || member == nil || member.Name == "" || strings.Contains(member.Name, ".") {
		return
	}
	if e.ffiPackages == nil {
		e.ffiPackages = make(map[string]*BoundFFIPackage)
	}
	item := e.ffiPackages[pkg]
	if item == nil {
		item = &BoundFFIPackage{Path: pkg, Members: make(map[string]*BoundFFIMember)}
		e.ffiPackages[pkg] = item
	}
	item.Members[member.Name] = member
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
	for name, route := range surface.Routes {
		if route.Name == "" {
			route.Name = name
		}
		if err := CheckPublicFFIRouteSchema(name, route); err != nil {
			return err
		}
		if existing, ok := e.routes[name]; ok {
			if err := CheckRouteCompatible(name, existing, route); err != nil {
				return err
			}
		}
		e.routes[name] = route
	}
	for name, spec := range surface.Structs {
		if spec == nil {
			continue
		}
		if err := CheckPublicFFIStructSchema(name, spec); err != nil {
			return err
		}
		if existing, ok := e.metadata.structsByName[name]; ok {
			merged, err := MergeStructSchema(name, existing, spec)
			if err != nil {
				return err
			}
			spec = merged
		}
		e.metadata.registerStructSchema(name, CloneRuntimeStructSpec(spec))
	}
	for name, spec := range surface.Interfaces {
		if spec == nil {
			continue
		}
		if err := CheckPublicFFIInterfaceSchema(name, spec); err != nil {
			return err
		}
		if existing, ok := e.metadata.interfacesByName[name]; ok {
			if err := CheckInterfaceSchemaCompatible(name, existing, spec); err != nil {
				return err
			}
		}
		e.metadata.registerInterfaceSpec(name, CloneRuntimeInterfaceSpec(spec))
	}
	if e.packageValues == nil {
		e.packageValues = make(map[string]*BoundPackageValue)
	}
	for name, value := range surface.PackageValues {
		if value != nil {
			if err := CheckPublicFFIValueSpec(name, value.Spec); err != nil {
				return err
			}
		}
		e.packageValues[name] = value
	}
	for name, value := range surface.Consts {
		e.consts[name] = value
	}
	if e.ffiPackages == nil {
		e.ffiPackages = make(map[string]*BoundFFIPackage)
	}
	for path, pkg := range surface.Packages {
		if pkg == nil {
			continue
		}
		target := e.ffiPackages[path]
		if target == nil {
			target = &BoundFFIPackage{Path: path, Members: make(map[string]*BoundFFIMember)}
			e.ffiPackages[path] = target
		}
		for name, member := range pkg.Members {
			target.Members[name] = member
		}
	}
	return nil
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
