package calltemplate

import (
	"fmt"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// PlanOptions provides the real compiler schemas and package lookup hooks used
// while building and validating a call template expansion plan.
type PlanOptions struct {
	// FuncSchemas are real callable schemas already visible to the compiler.
	FuncSchemas map[ast.Ident]*runtime.RuntimeFuncSig
	// StructSchemas are real struct schemas already visible to the compiler.
	StructSchemas map[ast.Ident]*runtime.RuntimeStructSpec
	// InterfaceSchemas are real interface schemas already visible to the
	// compiler.
	InterfaceSchemas map[ast.Ident]*runtime.RuntimeInterfaceSpec
	// Constants are real constants already visible to the compiler.
	Constants map[string]string
	// PackageExists reports whether a package path is backed by a real module or
	// runtime package.
	PackageExists func(path string) (bool, error)
	// PackageMemberSig returns the real signature of a package member when one
	// exists.
	PackageMemberSig func(path, member string) (*runtime.RuntimeFuncSig, bool, error)
}

// Plan is the compiler-side expansion plan for call templates.
//
// It exposes template signatures to the first semantic check and lazily records
// package-template dependencies when matching templates are expanded.
type Plan struct {
	funcSchemas      map[ast.Ident]*runtime.RuntimeFuncSig
	rawArgs          map[ast.Ident][]int
	templateOnly     map[ast.Ident]struct{}
	compileOnlyPaths map[string]struct{}
	templatePackages map[string]struct{}
	packageCache     map[string]bool
	opts             PlanOptions
}

// BuildPlan creates the template signatures and validation state used by the
// compiler.
//
// Global template names are checked immediately because they share the top-level
// symbol namespace. Package-member templates are validated on actual use so an
// unused bad template cannot make unrelated source fail to compile.
func BuildPlan(registry *Registry, opts PlanOptions) (*Plan, error) {
	plan := &Plan{
		funcSchemas:      make(map[ast.Ident]*runtime.RuntimeFuncSig),
		rawArgs:          make(map[ast.Ident][]int),
		templateOnly:     make(map[ast.Ident]struct{}),
		compileOnlyPaths: make(map[string]struct{}),
		templatePackages: make(map[string]struct{}),
		packageCache:     make(map[string]bool),
		opts:             opts,
	}
	if registry == nil {
		return plan, nil
	}

	for name, tpl := range registry.globals {
		if realGlobalSymbolExists(name, opts) {
			return nil, fmt.Errorf("global call template %s conflicts with existing symbol %s", tpl.ID, name)
		}
	}
	for name, tpl := range registry.globals {
		plan.funcSchemas[ast.Ident(name)] = runtime.CloneRuntimeFuncSig(tpl.SourceSig)
		plan.rawArgs[ast.Ident(name)] = append([]int(nil), tpl.RawArgs...)
	}
	for _, tpl := range registry.packages {
		plan.templatePackages[tpl.PackagePath] = struct{}{}
		key := ast.Ident(tpl.PackagePath + "." + tpl.Name)
		plan.funcSchemas[key] = runtime.CloneRuntimeFuncSig(tpl.SourceSig)
		plan.rawArgs[key] = append([]int(nil), tpl.RawArgs...)
		if tpl.TemplateOnly {
			plan.templateOnly[key] = struct{}{}
		}
	}
	return plan, nil
}

// FuncSchemas returns the template signatures that must be visible during the
// first semantic check before expansion.
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

func (p *Plan) RawArgs() map[ast.Ident][]int {
	if p == nil || len(p.rawArgs) == 0 {
		return nil
	}
	out := make(map[ast.Ident][]int, len(p.rawArgs))
	for name, raw := range p.rawArgs {
		out[name] = append([]int(nil), raw...)
	}
	return out
}

func (p *Plan) TemplateOnlyMembers() map[ast.Ident]struct{} {
	if p == nil || len(p.templateOnly) == 0 {
		return nil
	}
	out := make(map[ast.Ident]struct{}, len(p.templateOnly))
	for name := range p.templateOnly {
		out[name] = struct{}{}
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

func (p *Plan) validateTemplateUse(tpl registeredTemplate) error {
	if p == nil || tpl.PackagePath == "" {
		return nil
	}
	exists, err := p.packageExists(tpl.PackagePath)
	if err != nil {
		return fmt.Errorf("check package %s for call template %s: %w", tpl.PackagePath, tpl.ID, err)
	}
	if !exists {
		p.compileOnlyPaths[tpl.PackagePath] = struct{}{}
		return nil
	}
	actual, ok, err := packageMemberSig(tpl.PackagePath, tpl.Name, p.opts)
	if err != nil {
		return fmt.Errorf("check package member %s.%s for call template %s: %w", tpl.PackagePath, tpl.Name, tpl.ID, err)
	}
	if tpl.TemplateOnly {
		if ok {
			return fmt.Errorf("template-only call template %s conflicts with existing package member %s.%s", tpl.ID, tpl.PackagePath, tpl.Name)
		}
		return nil
	}
	if !ok {
		return fmt.Errorf("call template %s references missing package member %s.%s", tpl.ID, tpl.PackagePath, tpl.Name)
	}
	if !runtime.SameRuntimeFuncSchema(actual, tpl.SourceSig) {
		return fmt.Errorf("call template %s source signature %s does not match existing package member %s.%s signature %s", tpl.ID, tpl.SourceSig.SignatureString(), tpl.PackagePath, tpl.Name, actual.SignatureString())
	}
	return nil
}

func (p *Plan) ensureRenderPackage(templateID, path string) error {
	if p == nil || path == "" || p.compileOnlyPackage(path) {
		return nil
	}
	exists, err := p.packageExists(path)
	if err != nil {
		return fmt.Errorf("check package %s for call template %s: %w", path, templateID, err)
	}
	if exists {
		return nil
	}
	if _, ok := p.templatePackages[path]; ok {
		p.compileOnlyPaths[path] = struct{}{}
		return nil
	}
	return fmt.Errorf("call template %s references missing package %s", templateID, path)
}

func (p *Plan) packageExists(path string) (bool, error) {
	if p == nil {
		return false, nil
	}
	if exists, ok := p.packageCache[path]; ok {
		return exists, nil
	}
	exists, err := packageExists(path, p.opts)
	if err != nil {
		return false, err
	}
	p.packageCache[path] = exists
	return exists, nil
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
