package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type ModuleKind string

const (
	ModuleKindSource ModuleKind = "source"
	ModuleKindFFI    ModuleKind = "ffi"
)

type runtimeModule struct {
	Path       string
	Kind       ModuleKind
	Hash       string
	Prepared   *PreparedProgram
	FFIPackage *BoundFFIPackage
	Members    map[string]*runtimeModuleMember
}

type runtimeModuleMember struct {
	Name         string
	Kind         FFIMemberKind
	Type         RuntimeType
	ReadOnly     bool
	RouteName    string
	Const        FFIConstValue
	Value        *Var
	SourceTarget string
}

type runtimeModuleRegistry struct {
	modules map[string]*runtimeModule
}

func newRuntimeModuleRegistry() *runtimeModuleRegistry {
	return &runtimeModuleRegistry{modules: make(map[string]*runtimeModule)}
}

func cloneRuntimeModuleRegistry(in *runtimeModuleRegistry) *runtimeModuleRegistry {
	out := newRuntimeModuleRegistry()
	if in == nil {
		return out
	}
	for path, module := range in.modules {
		out.modules[path] = cloneRuntimeModule(module)
	}
	return out
}

func cloneRuntimeModule(in *runtimeModule) *runtimeModule {
	if in == nil {
		return nil
	}
	out := &runtimeModule{
		Path:       in.Path,
		Kind:       in.Kind,
		Hash:       in.Hash,
		Prepared:   clonePreparedProgram(in.Prepared),
		FFIPackage: cloneBoundFFIPackage(in.FFIPackage),
		Members:    make(map[string]*runtimeModuleMember, len(in.Members)),
	}
	for name, member := range in.Members {
		out.Members[name] = cloneRuntimeModuleMember(member)
	}
	return out
}

func cloneRuntimeModuleMember(in *runtimeModuleMember) *runtimeModuleMember {
	if in == nil {
		return nil
	}
	out := *in
	out.Value = cloneVarForAssign(in.Value)
	return &out
}

func cloneBoundFFIPackage(in *BoundFFIPackage) *BoundFFIPackage {
	if in == nil {
		return nil
	}
	out := &BoundFFIPackage{
		Path:    in.Path,
		Members: make(map[string]*BoundFFIMember, len(in.Members)),
	}
	for name, member := range in.Members {
		out.Members[name] = cloneBoundFFIMember(member)
	}
	return out
}

func (r *runtimeModuleRegistry) RegisterSource(path string, prepared *PreparedProgram, hash string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("source module missing path")
	}
	if prepared == nil {
		return fmt.Errorf("source module %s missing prepared program", path)
	}
	if existing := r.lookup(path); existing != nil && existing.Kind != ModuleKindSource {
		return fmt.Errorf("module %s conflicts: existing=%s new=%s", path, existing.Kind, ModuleKindSource)
	}
	module := &runtimeModule{
		Path:     path,
		Kind:     ModuleKindSource,
		Hash:     hash,
		Prepared: clonePreparedProgram(prepared),
		Members:  sourceModuleMembers(prepared),
	}
	r.ensure()[path] = module
	return nil
}

func (r *runtimeModuleRegistry) RegisterFFIPackage(pkg *BoundFFIPackage) error {
	if pkg == nil {
		return nil
	}
	path := strings.TrimSpace(pkg.Path)
	if path == "" {
		return errors.New("ffi module missing path")
	}
	if existing := r.lookup(path); existing != nil && existing.Kind != ModuleKindFFI {
		return fmt.Errorf("module %s conflicts: existing=%s new=%s", path, existing.Kind, ModuleKindFFI)
	}
	module := &runtimeModule{
		Path:       path,
		Kind:       ModuleKindFFI,
		FFIPackage: cloneBoundFFIPackage(pkg),
		Members:    ffiModuleMembers(pkg),
	}
	r.ensure()[path] = module
	return nil
}

func (r *runtimeModuleRegistry) lookup(path string) *runtimeModule {
	if r == nil {
		return nil
	}
	return r.modules[strings.TrimSpace(path)]
}

func (r *runtimeModuleRegistry) Lookup(path string) (*runtimeModule, bool) {
	module := r.lookup(path)
	return module, module != nil
}

func (r *runtimeModuleRegistry) Paths() []string {
	if r == nil || len(r.modules) == 0 {
		return nil
	}
	paths := make([]string, 0, len(r.modules))
	for path := range r.modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedRuntimeModuleMembers(module *runtimeModule) []*runtimeModuleMember {
	if module == nil || len(module.Members) == 0 {
		return nil
	}
	names := make([]string, 0, len(module.Members))
	for name := range module.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*runtimeModuleMember, 0, len(names))
	for _, name := range names {
		out = append(out, module.Members[name])
	}
	return out
}

func (r *runtimeModuleRegistry) ensure() map[string]*runtimeModule {
	if r.modules == nil {
		r.modules = make(map[string]*runtimeModule)
	}
	return r.modules
}

func sourceModuleMembers(prepared *PreparedProgram) map[string]*runtimeModuleMember {
	if prepared == nil || len(prepared.Exports) == 0 {
		return nil
	}
	members := make(map[string]*runtimeModuleMember, len(prepared.Exports))
	for name, export := range prepared.Exports {
		member := &runtimeModuleMember{
			Name:         name,
			Type:         export.Type,
			SourceTarget: export.TargetName,
			ReadOnly:     export.Kind != PreparedExportGlobal,
		}
		if member.SourceTarget == "" {
			member.SourceTarget = export.Name
		}
		switch export.Kind {
		case PreparedExportFunc:
			member.Kind = FFIMemberFunc
		case PreparedExportGlobal:
			member.Kind = FFIMemberValue
		case PreparedExportConst:
			member.Kind = FFIMemberConst
			if value, ok := prepared.Constants[member.SourceTarget]; ok {
				member.Const = value
			}
		case PreparedExportType, PreparedExportStruct, PreparedExportInterface:
			member.Kind = FFIMemberType
		default:
			continue
		}
		members[name] = member
	}
	return members
}

func ffiModuleMembers(pkg *BoundFFIPackage) map[string]*runtimeModuleMember {
	if pkg == nil || len(pkg.Members) == 0 {
		return nil
	}
	members := make(map[string]*runtimeModuleMember, len(pkg.Members))
	for name, member := range pkg.Members {
		if member == nil {
			continue
		}
		members[name] = &runtimeModuleMember{
			Name:      member.Name,
			Kind:      member.Kind,
			Type:      member.Type,
			ReadOnly:  member.ReadOnly,
			RouteName: member.RouteName,
			Const:     member.Const,
			Value:     cloneVarForAssign(member.Value),
		}
	}
	return members
}
