package calltemplate

import (
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type PlanOptions struct {
	FuncSchemas      map[ast.Ident]*runtime.RuntimeFuncSig
	StructSchemas    map[ast.Ident]*runtime.RuntimeStructSpec
	InterfaceSchemas map[ast.Ident]*runtime.RuntimeInterfaceSpec
	Constants        map[string]string
	PackageExists    func(path string) (bool, error)
	PackageMemberSig func(path, member string) (*runtime.RuntimeFuncSig, bool, error)
}

type Plan struct {
	funcSchemas      map[ast.Ident]*runtime.RuntimeFuncSig
	compileOnlyPaths map[string]struct{}
}

func BuildPlan(registry *Registry, opts PlanOptions) (*Plan, error) {
	plan := &Plan{
		funcSchemas:      make(map[ast.Ident]*runtime.RuntimeFuncSig),
		compileOnlyPaths: make(map[string]struct{}),
	}
	if registry == nil {
		return plan, nil
	}

	for name, tpl := range registry.globals {
		if realGlobalSymbolExists(name, opts) {
			return nil, fmt.Errorf("global call template %s conflicts with existing symbol %s", tpl.ID, name)
		}
	}
	packageExistsCache := make(map[string]bool)
	for _, tpl := range registry.packages {
		exists, ok := packageExistsCache[tpl.PackagePath]
		if !ok {
			got, err := packageExists(tpl.PackagePath, opts)
			if err != nil {
				return nil, fmt.Errorf("check package %s for call template %s: %w", tpl.PackagePath, tpl.ID, err)
			}
			exists = got
			packageExistsCache[tpl.PackagePath] = exists
		}
		if !exists {
			plan.compileOnlyPaths[tpl.PackagePath] = struct{}{}
			continue
		}
		actual, ok, err := packageMemberSig(tpl.PackagePath, tpl.Name, opts)
		if err != nil {
			return nil, fmt.Errorf("check package member %s.%s for call template %s: %w", tpl.PackagePath, tpl.Name, tpl.ID, err)
		}
		if !ok {
			return nil, fmt.Errorf("call template %s references missing package member %s.%s", tpl.ID, tpl.PackagePath, tpl.Name)
		}
		if !sameRuntimeFuncSig(actual, tpl.SourceSig) {
			return nil, fmt.Errorf("call template %s source signature %s does not match existing package member %s.%s signature %s", tpl.ID, tpl.SourceSig.SignatureString(), tpl.PackagePath, tpl.Name, actual.SignatureString())
		}
	}
	for _, tpl := range allTemplates(registry) {
		for path := range tpl.pkgRefs {
			if _, compileOnly := plan.compileOnlyPaths[path]; compileOnly {
				continue
			}
			exists, ok := packageExistsCache[path]
			if !ok {
				got, err := packageExists(path, opts)
				if err != nil {
					return nil, fmt.Errorf("check package %s for call template %s: %w", path, tpl.ID, err)
				}
				exists = got
				packageExistsCache[path] = exists
			}
			if !exists {
				return nil, fmt.Errorf("call template %s references missing package %s", tpl.ID, path)
			}
		}
	}
	for name, tpl := range registry.globals {
		plan.funcSchemas[ast.Ident(name)] = runtime.CloneRuntimeFuncSig(tpl.SourceSig)
	}
	for _, tpl := range registry.packages {
		plan.funcSchemas[ast.Ident(tpl.PackagePath+"."+tpl.Name)] = runtime.CloneRuntimeFuncSig(tpl.SourceSig)
	}
	return plan, nil
}

func (p *Plan) FuncSchemas() map[ast.Ident]*runtime.RuntimeFuncSig {
	if p == nil || len(p.funcSchemas) == 0 {
		return nil
	}
	out := make(map[ast.Ident]*runtime.RuntimeFuncSig, len(p.funcSchemas))
	for name, sig := range p.funcSchemas {
		out[name] = runtime.CloneRuntimeFuncSig(sig)
	}
	return out
}

func (p *Plan) compileOnlyPackage(path string) bool {
	if p == nil {
		return false
	}
	_, ok := p.compileOnlyPaths[path]
	return ok
}

func realGlobalSymbolExists(name string, opts PlanOptions) bool {
	if _, ok := opts.FuncSchemas[ast.Ident(name)]; ok {
		return true
	}
	if _, ok := opts.Constants[name]; ok {
		return true
	}
	if _, ok := opts.StructSchemas[ast.Ident(name)]; ok {
		return true
	}
	if _, ok := opts.InterfaceSchemas[ast.Ident(name)]; ok {
		return true
	}
	return false
}

func packageExists(path string, opts PlanOptions) (bool, error) {
	if opts.PackageExists != nil {
		return opts.PackageExists(path)
	}
	return false, nil
}

func packageMemberSig(path, name string, opts PlanOptions) (*runtime.RuntimeFuncSig, bool, error) {
	if opts.PackageMemberSig != nil {
		return opts.PackageMemberSig(path, name)
	}
	return nil, false, nil
}

func allTemplates(registry *Registry) []registeredTemplate {
	if registry == nil {
		return nil
	}
	out := make([]registeredTemplate, 0, len(registry.globals)+len(registry.packages))
	for _, tpl := range registry.globals {
		out = append(out, cloneRegisteredTemplate(tpl))
	}
	for _, tpl := range registry.packages {
		out = append(out, cloneRegisteredTemplate(tpl))
	}
	return out
}
