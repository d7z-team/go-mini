package compiler

import (
	"strings"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/runtime"
)

func (c *Compiler) buildTemplatePlan(imported map[string]*ast.ProgramStmt) (*calltemplate.Plan, error) {
	funcs, _, values, structs, interfaces, constants := c.externalSchemaMaps()
	return calltemplate.BuildPlan(c.cfg.Templates, calltemplate.PlanOptions{
		FuncSchemas:      funcs,
		StructSchemas:    structs,
		InterfaceSchemas: interfaces,
		Constants:        constants,
		PackageExists: func(path string) (bool, error) {
			return c.templatePackageExists(path, values, funcs, structs, interfaces, constants, imported)
		},
		PackageMemberSig: func(path, member string) (*runtime.RuntimeFuncSig, bool, error) {
			return c.templatePackageMemberSig(path, member, funcs, imported)
		},
	})
}

func (c *Compiler) templatePackageExists(
	path string,
	values map[ast.Ident]*runtime.ValueSpec,
	funcs map[ast.Ident]*runtime.RuntimeFuncSig,
	structs map[ast.Ident]*runtime.RuntimeStructSpec,
	interfaces map[ast.Ident]*runtime.RuntimeInterfaceSpec,
	constants map[string]string,
	imported map[string]*ast.ProgramStmt,
) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if externalPackageExists(path, funcs, values, structs, interfaces, constants) {
		return true, nil
	}
	if _, ok := imported[path]; ok {
		return true, nil
	}
	prog, ok := c.loadTemplateModule(path, imported)
	if ok && prog != nil {
		return true, nil
	}
	return false, nil
}

func (c *Compiler) templatePackageMemberSig(path, member string, funcs map[ast.Ident]*runtime.RuntimeFuncSig, imported map[string]*ast.ProgramStmt) (*runtime.RuntimeFuncSig, bool, error) {
	for _, name := range packageMemberNames(path, member) {
		if sig, ok := funcs[ast.Ident(name)]; ok && sig != nil {
			return runtime.CloneRuntimeFuncSig(sig), true, nil
		}
	}
	prog, ok := imported[path]
	if !ok {
		prog, ok = c.loadTemplateModule(path, imported)
	}
	if !ok || prog == nil {
		return nil, false, nil
	}
	fn, ok := prog.Functions[ast.Ident(member)]
	if !ok || fn == nil {
		return nil, false, nil
	}
	sig, err := runtime.ParseRuntimeFuncSig(fn.FunctionType.MiniType())
	if err != nil {
		return nil, false, err
	}
	return sig, true, nil
}

func (c *Compiler) loadTemplateModule(path string, imported map[string]*ast.ProgramStmt) (*ast.ProgramStmt, bool) {
	if c.cfg.ModuleLoader == nil {
		return nil, false
	}
	prog, err := c.cfg.ModuleLoader(path)
	if err != nil || prog == nil {
		return nil, false
	}
	if imported != nil {
		imported[path] = prog
	}
	return prog, true
}

func externalPackageExists(
	path string,
	funcs map[ast.Ident]*runtime.RuntimeFuncSig,
	values map[ast.Ident]*runtime.ValueSpec,
	structs map[ast.Ident]*runtime.RuntimeStructSpec,
	interfaces map[ast.Ident]*runtime.RuntimeInterfaceSpec,
	constants map[string]string,
) bool {
	for _, prefix := range packagePrefixes(path) {
		for name := range funcs {
			if strings.HasPrefix(string(name), prefix) {
				return true
			}
		}
		for name := range values {
			if strings.HasPrefix(string(name), prefix) {
				return true
			}
		}
		for name := range structs {
			if strings.HasPrefix(string(name), prefix) {
				return true
			}
		}
		for name := range interfaces {
			if strings.HasPrefix(string(name), prefix) {
				return true
			}
		}
		for name := range constants {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

func packagePrefixes(path string) []string {
	dotted := strings.ReplaceAll(path, "/", ".")
	if dotted == path {
		return []string{path + "."}
	}
	return []string{path + ".", dotted + "."}
}

func packageMemberNames(path, member string) []string {
	name := path + "." + member
	dotted := strings.ReplaceAll(path, "/", ".") + "." + member
	if dotted == name {
		return []string{name}
	}
	return []string{name, dotted}
}

func pruneImportedPrograms(imported map[string]*ast.ProgramStmt, program *ast.ProgramStmt) map[string]*ast.ProgramStmt {
	if len(imported) == 0 || program == nil || len(program.Imports) == 0 {
		return nil
	}
	needed := make(map[string]struct{}, len(program.Imports))
	for _, imp := range program.Imports {
		path := strings.TrimSpace(imp.Path)
		if path != "" {
			needed[path] = struct{}{}
		}
	}
	out := make(map[string]*ast.ProgramStmt, len(needed))
	for path := range needed {
		if prog := imported[path]; prog != nil {
			out[path] = prog
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
