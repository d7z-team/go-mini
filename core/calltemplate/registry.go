package calltemplate

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
	"text/template"
	"text/template/parse"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/runtime"
)

const InternalNamePrefix = "__gomini_tpl_"

type TemplateKind string

const (
	TemplateGlobalFunc  TemplateKind = "global_func"
	TemplatePackageFunc TemplateKind = "package_func"
)

type TemplateBodyKind string

const (
	TemplateExpr TemplateBodyKind = "expr"
	TemplateStmt TemplateBodyKind = "stmt"
)

type TemplatePackageMode string

const (
	RuntimePackage     TemplatePackageMode = "runtime_package"
	CompileOnlyPackage TemplatePackageMode = "compile_only_package"
)

type TemplateImport struct {
	Path      string
	AliasHint string
}

type FunctionTemplate struct {
	ID          string
	Kind        TemplateKind
	Name        string
	PackagePath string
	Member      string
	SourceSig   *runtime.RuntimeFuncSig
	BodyKind    TemplateBodyKind
	Body        string
	PackageMode TemplatePackageMode
	Imports     []TemplateImport
}

type Registry struct {
	globals  map[string]FunctionTemplate
	packages map[string]FunctionTemplate
}

func NewRegistry() *Registry {
	return &Registry{
		globals:  make(map[string]FunctionTemplate),
		packages: make(map[string]FunctionTemplate),
	}
}

func (r *Registry) Clone() *Registry {
	if r == nil {
		return nil
	}
	out := NewRegistry()
	for k, v := range r.globals {
		out.globals[k] = cloneTemplate(v)
	}
	for k, v := range r.packages {
		out.packages[k] = cloneTemplate(v)
	}
	return out
}

func (r *Registry) Register(t FunctionTemplate) error {
	if r == nil {
		return errors.New("nil template registry")
	}
	t = normalizeTemplate(t)
	if err := validateTemplate(t); err != nil {
		return err
	}
	t = cloneTemplate(t)
	switch t.Kind {
	case TemplateGlobalFunc:
		if _, exists := r.globals[t.Name]; exists {
			return fmt.Errorf("call template %s conflicts with existing global template %s", t.ID, t.Name)
		}
		r.globals[t.Name] = t
	case TemplatePackageFunc:
		key := packageKey(t.PackagePath, t.Member)
		if _, exists := r.packages[key]; exists {
			return fmt.Errorf("call template %s conflicts with existing package template %s", t.ID, key)
		}
		for _, existing := range r.packages {
			if existing.PackagePath == t.PackagePath && existing.PackageMode != t.PackageMode {
				return fmt.Errorf("call template %s mixes package mode %s with existing %s for package %s", t.ID, t.PackageMode, existing.PackageMode, t.PackagePath)
			}
		}
		r.packages[key] = t
	default:
		return fmt.Errorf("unsupported template kind %q", t.Kind)
	}
	return nil
}

func (r *Registry) Global(name string) (FunctionTemplate, bool) {
	if r == nil {
		return FunctionTemplate{}, false
	}
	t, ok := r.globals[name]
	return t, ok
}

func (r *Registry) PackageMember(path, member string) (FunctionTemplate, bool) {
	if r == nil {
		return FunctionTemplate{}, false
	}
	t, ok := r.packages[packageKey(path, member)]
	return t, ok
}

func (r *Registry) Globals() map[string]FunctionTemplate {
	if r == nil || len(r.globals) == 0 {
		return nil
	}
	out := make(map[string]FunctionTemplate, len(r.globals))
	for name, t := range r.globals {
		out[name] = cloneTemplate(t)
	}
	return out
}

func (r *Registry) PackageTemplates() map[string]FunctionTemplate {
	if r == nil || len(r.packages) == 0 {
		return nil
	}
	out := make(map[string]FunctionTemplate, len(r.packages))
	for key, t := range r.packages {
		out[key] = cloneTemplate(t)
	}
	return out
}

func (r *Registry) FuncSchemas() map[ast.Ident]*runtime.RuntimeFuncSig {
	if r == nil || (len(r.globals) == 0 && len(r.packages) == 0) {
		return nil
	}
	out := make(map[ast.Ident]*runtime.RuntimeFuncSig, len(r.globals)+len(r.packages))
	for name, t := range r.globals {
		out[ast.Ident(name)] = runtime.CloneRuntimeFuncSig(t.SourceSig)
	}
	for _, t := range r.packages {
		out[ast.Ident(t.PackagePath+"."+t.Member)] = runtime.CloneRuntimeFuncSig(t.SourceSig)
	}
	return out
}

func (r *Registry) GlobalTemplate(name string) (FunctionTemplate, bool) {
	return r.Global(name)
}

func (r *Registry) ReservedNames() map[string]struct{} {
	if r == nil {
		return nil
	}
	out := make(map[string]struct{}, len(r.globals))
	for name := range r.globals {
		out[name] = struct{}{}
	}
	return out
}

func (r *Registry) CompletionSchemas() map[string]string {
	if r == nil {
		return nil
	}
	out := make(map[string]string)
	for name, t := range r.globals {
		out[name] = t.SourceSig.SignatureString()
	}
	return out
}

func (r *Registry) CompileOnlyPackage(path string) bool {
	if r == nil {
		return false
	}
	for _, t := range r.packages {
		if t.PackagePath == path && t.PackageMode == CompileOnlyPackage {
			return true
		}
	}
	return false
}

func validateTemplate(t FunctionTemplate) error {
	if strings.TrimSpace(t.ID) == "" {
		return errors.New("call template requires id")
	}
	if t.SourceSig == nil {
		return fmt.Errorf("call template %s requires source signature", t.ID)
	}
	if strings.TrimSpace(t.Body) == "" {
		return fmt.Errorf("call template %s requires body", t.ID)
	}
	switch t.BodyKind {
	case TemplateExpr, TemplateStmt:
	default:
		return fmt.Errorf("call template %s has unsupported body kind %q", t.ID, t.BodyKind)
	}
	switch t.Kind {
	case TemplateGlobalFunc:
		if t.Name == "" {
			return fmt.Errorf("global call template %s requires name", t.ID)
		}
		if !validTemplateIdent(t.Name) {
			return fmt.Errorf("global call template %s has invalid name %s", t.ID, t.Name)
		}
	case TemplatePackageFunc:
		if t.PackagePath == "" || t.Member == "" {
			return fmt.Errorf("package call template %s requires package path and member", t.ID)
		}
		if !validTemplateIdent(t.Member) {
			return fmt.Errorf("package call template %s has invalid member %s", t.ID, t.Member)
		}
		switch t.PackageMode {
		case RuntimePackage, CompileOnlyPackage:
		default:
			return fmt.Errorf("package call template %s has unsupported package mode %q", t.ID, t.PackageMode)
		}
	default:
		return fmt.Errorf("call template %s has unsupported kind %q", t.ID, t.Kind)
	}
	if t.BodyKind == TemplateStmt && !t.SourceSig.ReturnType.IsVoid() {
		return fmt.Errorf("statement call template %s must have Void return type", t.ID)
	}
	seenImports := make(map[string]struct{}, len(t.Imports))
	for _, imp := range t.Imports {
		path := strings.TrimSpace(imp.Path)
		if path == "" {
			return fmt.Errorf("call template %s has empty import path", t.ID)
		}
		if imp.AliasHint != "" && !validTemplateIdent(imp.AliasHint) {
			return fmt.Errorf("call template %s import %s has invalid alias hint %s", t.ID, path, imp.AliasHint)
		}
		if _, ok := seenImports[path]; ok {
			return fmt.Errorf("call template %s imports %s more than once", t.ID, path)
		}
		seenImports[path] = struct{}{}
	}
	tpl, err := template.New(t.ID).Funcs(template.FuncMap{
		"pkg":      func(string) string { return "" },
		"arg":      func(int) string { return "" },
		"callArg":  func(int) string { return "" },
		"args":     func() string { return "" },
		"argc":     func() int { return 0 },
		"ellipsis": func() bool { return false },
		"fresh":    func(string) string { return "" },
	}).Parse(t.Body)
	if err != nil {
		return fmt.Errorf("parse call template %s: %w", t.ID, err)
	}
	if err := rejectTemplateDataAccess(tpl.Tree.Root); err != nil {
		return fmt.Errorf("parse call template %s: %w", t.ID, err)
	}
	return nil
}

func rejectTemplateDataAccess(root parse.Node) error {
	var walk func(parse.Node) error
	walk = func(node parse.Node) error {
		if node == nil {
			return nil
		}
		switch n := node.(type) {
		case *parse.ListNode:
			for _, child := range n.Nodes {
				if err := walk(child); err != nil {
					return err
				}
			}
		case *parse.ActionNode:
			return walk(n.Pipe)
		case *parse.PipeNode:
			for _, cmd := range n.Cmds {
				if err := walk(cmd); err != nil {
					return err
				}
			}
		case *parse.CommandNode:
			for _, arg := range n.Args {
				if err := walk(arg); err != nil {
					return err
				}
			}
		case *parse.FieldNode:
			return fmt.Errorf("data field .%s is not supported; use template functions", strings.Join(n.Ident, "."))
		case *parse.DotNode:
			return errors.New("dot data is not supported; use template functions")
		case *parse.VariableNode:
			if len(n.Ident) > 1 {
				return fmt.Errorf("template variable field %s is not supported; use template functions", strings.Join(n.Ident, "."))
			}
		case *parse.ChainNode:
			if err := walk(n.Node); err != nil {
				return err
			}
			if len(n.Field) > 0 {
				return fmt.Errorf("template chain field .%s is not supported; use template functions", strings.Join(n.Field, "."))
			}
		case *parse.IfNode:
			if err := walk(n.Pipe); err != nil {
				return err
			}
			if err := walk(n.List); err != nil {
				return err
			}
			return walk(n.ElseList)
		case *parse.RangeNode:
			if err := walk(n.Pipe); err != nil {
				return err
			}
			if err := walk(n.List); err != nil {
				return err
			}
			return walk(n.ElseList)
		case *parse.WithNode:
			if err := walk(n.Pipe); err != nil {
				return err
			}
			if err := walk(n.List); err != nil {
				return err
			}
			return walk(n.ElseList)
		case *parse.TemplateNode:
			return walk(n.Pipe)
		}
		return nil
	}
	return walk(root)
}

func normalizeTemplate(t FunctionTemplate) FunctionTemplate {
	t.Name = strings.TrimSpace(t.Name)
	t.PackagePath = strings.TrimSpace(t.PackagePath)
	t.Member = strings.TrimSpace(t.Member)
	if t.Kind == TemplatePackageFunc && t.PackageMode == "" {
		t.PackageMode = RuntimePackage
	}
	for i := range t.Imports {
		t.Imports[i].Path = strings.TrimSpace(t.Imports[i].Path)
		t.Imports[i].AliasHint = strings.TrimSpace(t.Imports[i].AliasHint)
	}
	return t
}

func validTemplateIdent(name string) bool {
	return token.IsIdentifier(name) && !token.Lookup(name).IsKeyword() && name != "_" && !strings.HasPrefix(name, InternalNamePrefix)
}

func sameRuntimeFuncSig(a, b *runtime.RuntimeFuncSig) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	default:
		return a.Spec == b.Spec
	}
}

func cloneTemplate(t FunctionTemplate) FunctionTemplate {
	t.SourceSig = runtime.CloneRuntimeFuncSig(t.SourceSig)
	t.Imports = append([]TemplateImport(nil), t.Imports...)
	return t
}

func packageKey(path, member string) string {
	return path + "\x00" + member
}
