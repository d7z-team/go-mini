package runtime

type runtimeMetadataRegistry struct {
	namedTypesByName   map[string]RuntimeType
	namedTypesByTypeID map[string]RuntimeType
	structsByName      map[string]*RuntimeStructSpec
	structsByTypeID    map[string]*RuntimeStructSpec
	interfacesByName   map[string]*RuntimeInterfaceSpec
	interfacesByTypeID map[string]*RuntimeInterfaceSpec
}

func newRuntimeMetadataRegistry() *runtimeMetadataRegistry {
	return &runtimeMetadataRegistry{
		namedTypesByName:   make(map[string]RuntimeType),
		namedTypesByTypeID: make(map[string]RuntimeType),
		structsByName:      make(map[string]*RuntimeStructSpec),
		structsByTypeID:    make(map[string]*RuntimeStructSpec),
		interfacesByName:   make(map[string]*RuntimeInterfaceSpec),
		interfacesByTypeID: make(map[string]*RuntimeInterfaceSpec),
	}
}

func cloneRuntimeMetadataRegistry(in *runtimeMetadataRegistry) *runtimeMetadataRegistry {
	out := newRuntimeMetadataRegistry()
	if in == nil {
		return out
	}
	out.namedTypesByName = cloneRuntimeTypeMap(in.namedTypesByName)
	out.namedTypesByTypeID = cloneRuntimeTypeMap(in.namedTypesByTypeID)
	out.structsByName = cloneRuntimeStructSpecMap(in.structsByName)
	out.structsByTypeID = cloneRuntimeStructSpecMap(in.structsByTypeID)
	out.interfacesByName = cloneRuntimeInterfaceSpecMap(in.interfacesByName)
	out.interfacesByTypeID = cloneRuntimeInterfaceSpecMap(in.interfacesByTypeID)
	if out.namedTypesByName == nil {
		out.namedTypesByName = make(map[string]RuntimeType)
	}
	if out.namedTypesByTypeID == nil {
		out.namedTypesByTypeID = make(map[string]RuntimeType)
	}
	if out.structsByName == nil {
		out.structsByName = make(map[string]*RuntimeStructSpec)
	}
	if out.structsByTypeID == nil {
		out.structsByTypeID = make(map[string]*RuntimeStructSpec)
	}
	if out.interfacesByName == nil {
		out.interfacesByName = make(map[string]*RuntimeInterfaceSpec)
	}
	if out.interfacesByTypeID == nil {
		out.interfacesByTypeID = make(map[string]*RuntimeInterfaceSpec)
	}
	return out
}

func (r *runtimeMetadataRegistry) registerNamedType(name string, typeInfo RuntimeType) {
	r.namedTypesByName[name] = typeInfo
	switch typeInfo.Kind {
	case RuntimeTypeNamed, RuntimeTypeStruct, RuntimeTypeInterface:
	default:
		return
	}
	if typeInfo.TypeID != "" {
		r.namedTypesByTypeID[typeInfo.TypeID] = typeInfo
	}
}

func (r *runtimeMetadataRegistry) registerInterfaceSpec(name string, spec *RuntimeInterfaceSpec) {
	if spec == nil {
		if existing, ok := r.interfacesByName[name]; ok && existing != nil && existing.TypeID != "" {
			delete(r.interfacesByTypeID, existing.TypeID)
		}
		delete(r.interfacesByName, name)
		return
	}
	r.interfacesByName[name] = spec
	if spec.TypeID != "" {
		r.interfacesByTypeID[spec.TypeID] = spec
	}
}

func (r *runtimeMetadataRegistry) registerStructSchema(name string, spec *RuntimeStructSpec) {
	if spec == nil {
		if existing, ok := r.structsByName[name]; ok && existing != nil && existing.TypeID != "" {
			delete(r.structsByTypeID, existing.TypeID)
		}
		delete(r.structsByName, name)
		return
	}
	r.structsByName[name] = spec
	if spec.TypeID != "" {
		r.structsByTypeID[spec.TypeID] = spec
	}
}

func (r *runtimeMetadataRegistry) resolveNamedType(typ TypeSpec) (RuntimeType, bool) {
	if typeInfo, ok := r.namedTypesByName[string(typ)]; ok {
		return typeInfo, true
	}
	typeInfo, ok := r.namedTypesByTypeID[CanonicalTypeID(string(typ))]
	return typeInfo, ok
}

func (r *runtimeMetadataRegistry) resolveInterfaceSpec(typ TypeSpec) (*RuntimeInterfaceSpec, bool) {
	if spec, ok := r.interfacesByName[string(typ)]; ok {
		return spec, true
	}
	spec, ok := r.interfacesByTypeID[CanonicalTypeID(string(typ))]
	return spec, ok
}

func (r *runtimeMetadataRegistry) resolveStructSchema(typ TypeSpec) (*RuntimeStructSpec, bool) {
	if schema, ok := r.structsByName[string(typ)]; ok {
		return schema, true
	}
	schema, ok := r.structsByTypeID[CanonicalTypeID(string(typ))]
	return schema, ok
}
