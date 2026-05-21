package calltemplate

import (
	"fmt"
	"strings"

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
	for _, tpl := range registry.packages {
		if tpl.PackageMode == CompileOnlyPackage {
			plan.compileOnlyPaths[tpl.PackagePath] = struct{}{}
		}
	}
	for _, tpl := range registry.packages {
		if tpl.PackageMode != RuntimePackage {
			continue
		}
		exists, err := packageExists(tpl.PackagePath, opts)
		if err != nil {
			return nil, fmt.Errorf("check package %s for call template %s: %w", tpl.PackagePath, tpl.ID, err)
		}
		if !exists {
			return nil, fmt.Errorf("call template %s references missing package %s", tpl.ID, tpl.PackagePath)
		}
		actual, ok, err := packageMemberSig(tpl.PackagePath, tpl.Member, opts)
		if err != nil {
			return nil, fmt.Errorf("check package member %s.%s for call template %s: %w", tpl.PackagePath, tpl.Member, tpl.ID, err)
		}
		if !ok {
			return nil, fmt.Errorf("call template %s references missing package member %s.%s", tpl.ID, tpl.PackagePath, tpl.Member)
		}
		if !sameRuntimeFuncSig(actual, tpl.SourceSig) {
			return nil, fmt.Errorf("call template %s source signature %s does not match existing package member %s.%s signature %s", tpl.ID, tpl.SourceSig.SignatureString(), tpl.PackagePath, tpl.Member, actual.SignatureString())
		}
	}
	for _, tpl := range allTemplates(registry) {
		for _, imp := range tpl.Imports {
			path := strings.TrimSpace(imp.Path)
			if _, compileOnly := plan.compileOnlyPaths[path]; compileOnly {
				continue
			}
			exists, err := packageExists(path, opts)
			if err != nil {
				return nil, fmt.Errorf("check import %s for call template %s: %w", path, tpl.ID, err)
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
		plan.funcSchemas[ast.Ident(tpl.PackagePath+"."+tpl.Member)] = runtime.CloneRuntimeFuncSig(tpl.SourceSig)
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

func (p *Plan) CompileOnlyPackage(path string) bool {
	if p == nil {
		return false
	}
	_, ok := p.compileOnlyPaths[path]
	return ok
}

func (p *Plan) CompileOnlyPackages() map[string]struct{} {
	if p == nil || len(p.compileOnlyPaths) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(p.compileOnlyPaths))
	for path := range p.compileOnlyPaths {
		out[path] = struct{}{}
	}
	return out
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

func packageMemberSig(path, member string, opts PlanOptions) (*runtime.RuntimeFuncSig, bool, error) {
	if opts.PackageMemberSig != nil {
		return opts.PackageMemberSig(path, member)
	}
	return nil, false, nil
}

func allTemplates(registry *Registry) []FunctionTemplate {
	if registry == nil {
		return nil
	}
	out := make([]FunctionTemplate, 0, len(registry.globals)+len(registry.packages))
	for _, tpl := range registry.globals {
		out = append(out, tpl)
	}
	for _, tpl := range registry.packages {
		out = append(out, tpl)
	}
	return out
}
