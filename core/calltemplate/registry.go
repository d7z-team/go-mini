package calltemplate

import (
	"errors"
	"fmt"
	"go/token"
	"strings"
	"text/template"
	"text/template/parse"

	"gopkg.d7z.net/go-mini/core/runtime"
)

// InternalNamePrefix is reserved for identifiers synthesized during call
// template expansion.
const InternalNamePrefix = "__gomini_tpl_"

// FunctionTemplate describes a compiler-only call template.
//
// A template with an empty PackagePath is invoked as a global function. A
// template with a PackagePath is invoked as a package member. By default the
// compiler validates the real package member lazily when the template is used.
type FunctionTemplate struct {
	// ID is a stable diagnostic name. It is derived from PackagePath and Name
	// when left empty.
	ID string
	// PackagePath is the VM-visible package path for a package-member template.
	// Empty means the template is global.
	PackagePath string
	// Name is the source-visible global function or package member name.
	Name string
	// TemplateOnly marks a package-member template as a direct-call source
	// facade. If the package exists, the member must not be exported as a
	// runtime package member. The member cannot be used as a function value.
	TemplateOnly bool
	// SourceSig is the signature exposed to the first semantic check before the
	// template is expanded.
	SourceSig *runtime.RuntimeFuncSig
	// RawArgs marks 0-based argument positions that should keep their AST form
	// during the first semantic check instead of being forced through normal
	// assignability to SourceSig parameter types.
	RawArgs []int
	// Body is a Go text/template fragment rendered to Mini source and parsed
	// back into AST during compiler expansion.
	Body string
}

type registeredTemplate struct {
	FunctionTemplate
	pkgRefs map[string]struct{}
}

// Registry stores compiler-only call templates.
//
// Registry methods return cloned data so callers cannot mutate registered
// templates after validation.
type Registry struct {
	globals  map[string]registeredTemplate
	packages map[string]registeredTemplate
}

// NewRegistry creates an empty call template registry.
func NewRegistry() *Registry {
	return &Registry{
		globals:  make(map[string]registeredTemplate),
		packages: make(map[string]registeredTemplate),
	}
}

// Clone returns a deep copy of the registry.
func (r *Registry) Clone() *Registry {
	if r == nil {
		return nil
	}
	out := NewRegistry()
	for k, v := range r.globals {
		out.globals[k] = cloneRegisteredTemplate(v)
	}
	for k, v := range r.packages {
		out.packages[k] = cloneRegisteredTemplate(v)
	}
	return out
}

// Register validates and adds a call template.
//
// Global templates reserve their source-visible name immediately. Package
// templates reserve their package-member template signature but defer real
// package existence and signature checks until the template is used.
func (r *Registry) Register(t FunctionTemplate) error {
	if r == nil {
		return errors.New("nil template registry")
	}
	t = normalizeTemplate(t)
	rt, err := validateTemplate(t)
	if err != nil {
		return err
	}
	if rt.PackagePath == "" {
		if _, exists := r.globals[rt.Name]; exists {
			return fmt.Errorf("call template %s conflicts with existing global template %s", rt.ID, rt.Name)
		}
		r.globals[rt.Name] = cloneRegisteredTemplate(rt)
		return nil
	}
	key := packageKey(rt.PackagePath, rt.Name)
	if _, exists := r.packages[key]; exists {
		return fmt.Errorf("call template %s conflicts with existing package template %s.%s", rt.ID, rt.PackagePath, rt.Name)
	}
	r.packages[key] = cloneRegisteredTemplate(rt)
	return nil
}

// Global returns a global call template by source-visible name.
func (r *Registry) Global(name string) (FunctionTemplate, bool) {
	rt, ok := r.global(name)
	if !ok {
		return FunctionTemplate{}, false
	}
	return cloneTemplate(rt.FunctionTemplate), true
}

// PackageMember returns a package-member call template by package path and
// member name.
func (r *Registry) PackageMember(path, name string) (FunctionTemplate, bool) {
	rt, ok := r.packageMember(path, name)
	if !ok {
		return FunctionTemplate{}, false
	}
	return cloneTemplate(rt.FunctionTemplate), true
}

func (r *Registry) global(name string) (registeredTemplate, bool) {
	if r == nil {
		return registeredTemplate{}, false
	}
	t, ok := r.globals[name]
	return cloneRegisteredTemplate(t), ok
}

func (r *Registry) packageMember(path, name string) (registeredTemplate, bool) {
	if r == nil {
		return registeredTemplate{}, false
	}
	t, ok := r.packages[packageKey(path, name)]
	return cloneRegisteredTemplate(t), ok
}

// Globals returns all global call templates keyed by source-visible name.
func (r *Registry) Globals() map[string]FunctionTemplate {
	if r == nil || len(r.globals) == 0 {
		return nil
	}
	out := make(map[string]FunctionTemplate, len(r.globals))
	for name, t := range r.globals {
		out[name] = cloneTemplate(t.FunctionTemplate)
	}
	return out
}

// PackageTemplates returns all package-member call templates keyed by
// "<package path>.<member name>".
func (r *Registry) PackageTemplates() map[string]FunctionTemplate {
	if r == nil || len(r.packages) == 0 {
		return nil
	}
	out := make(map[string]FunctionTemplate, len(r.packages))
	for key, t := range r.packages {
		out[key] = cloneTemplate(t.FunctionTemplate)
	}
	return out
}

// ReservedNames returns global names that cannot be declared by user code.
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

// CompletionSchemas returns global template signatures for editor metadata.
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

func validateTemplate(t FunctionTemplate) (registeredTemplate, error) {
	if t.SourceSig == nil {
		return registeredTemplate{}, fmt.Errorf("call template %s requires source signature", templateID(t))
	}
	for _, idx := range t.RawArgs {
		if idx < 0 || idx >= len(t.SourceSig.ParamTypes) {
			return registeredTemplate{}, fmt.Errorf("call template %s raw arg index %d is out of range", templateID(t), idx)
		}
		if t.SourceSig.Variadic && idx == len(t.SourceSig.ParamTypes)-1 {
			return registeredTemplate{}, fmt.Errorf("call template %s raw arg index %d targets variadic parameter", templateID(t), idx)
		}
	}
	if strings.TrimSpace(t.Body) == "" {
		return registeredTemplate{}, fmt.Errorf("call template %s requires body", templateID(t))
	}
	if t.PackagePath == "" {
		if t.TemplateOnly {
			return registeredTemplate{}, fmt.Errorf("global call template %s cannot be template-only", templateID(t))
		}
		if t.Name == "" {
			return registeredTemplate{}, fmt.Errorf("global call template %s requires name", templateID(t))
		}
		if !validTemplateIdent(t.Name) {
			return registeredTemplate{}, fmt.Errorf("global call template %s has invalid name %s", templateID(t), t.Name)
		}
	} else {
		if err := validatePackagePath(t.PackagePath); err != nil {
			return registeredTemplate{}, fmt.Errorf("package call template %s has invalid package path %s: %w", templateID(t), t.PackagePath, err)
		}
		if t.Name == "" {
			return registeredTemplate{}, fmt.Errorf("package call template %s requires name", templateID(t))
		}
		if !validTemplateIdent(t.Name) {
			return registeredTemplate{}, fmt.Errorf("package call template %s has invalid name %s", templateID(t), t.Name)
		}
	}
	if t.ID == "" {
		t.ID = templateID(t)
	}
	tpl, err := parseTemplateBody(t.ID, t.Body)
	if err != nil {
		return registeredTemplate{}, err
	}
	pkgRefs, err := inspectTemplateBody(tpl.Tree.Root)
	if err != nil {
		return registeredTemplate{}, fmt.Errorf("parse call template %s: %w", t.ID, err)
	}
	return registeredTemplate{FunctionTemplate: cloneTemplate(t), pkgRefs: pkgRefs}, nil
}

func parseTemplateBody(id, body string) (*template.Template, error) {
	tpl, err := template.New(id).Funcs(template.FuncMap{
		"pkg":      func(string) string { return "" },
		"arg":      func(int) string { return "" },
		"callArg":  func(int) string { return "" },
		"argType":  func(int) string { return "" },
		"args":     func() string { return "" },
		"argc":     func() int { return 0 },
		"ellipsis": func() bool { return false },
		"fresh":    func(string) string { return "" },
	}).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse call template %s: %w", id, err)
	}
	return tpl, nil
}

func inspectTemplateBody(root parse.Node) (map[string]struct{}, error) {
	refs := make(map[string]struct{})
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
			for i, arg := range n.Args {
				id, isIdent := arg.(*parse.IdentifierNode)
				if isIdent && id.Ident == "pkg" {
					if i != 0 || len(n.Args) != 2 {
						return errors.New(`pkg must be called directly as {{ pkg "full/package/path" }}`)
					}
					pathArg, ok := n.Args[1].(*parse.StringNode)
					if !ok {
						return errors.New("pkg requires a string literal package path")
					}
					pkgPath := strings.TrimSpace(pathArg.Text)
					if err := validatePackagePath(pkgPath); err != nil {
						return fmt.Errorf("pkg package path %q is invalid: %w", pkgPath, err)
					}
					refs[pkgPath] = struct{}{}
					return nil
				}
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
	return refs, walk(root)
}

func normalizeTemplate(t FunctionTemplate) FunctionTemplate {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.PackagePath = strings.TrimSpace(t.PackagePath)
	return t
}

func templateID(t FunctionTemplate) string {
	if t.ID != "" {
		return t.ID
	}
	if t.PackagePath != "" {
		if t.Name != "" {
			return t.PackagePath + "." + t.Name
		}
		return t.PackagePath
	}
	if t.Name != "" {
		return "global." + t.Name
	}
	return "<unnamed>"
}

func validatePackagePath(path string) error {
	switch {
	case path == "":
		return errors.New("empty path")
	case strings.HasPrefix(path, "/"):
		return errors.New("absolute path is not allowed")
	case strings.HasSuffix(path, "/"):
		return errors.New("trailing slash is not allowed")
	case strings.Contains(path, "//"):
		return errors.New("empty path segment is not allowed")
	case strings.Contains(path, ".."):
		return errors.New("relative path segments are not allowed")
	}
	for _, r := range path {
		if r <= ' ' {
			return errors.New("whitespace is not allowed")
		}
	}
	return nil
}

func validTemplateIdent(name string) bool {
	return token.IsIdentifier(name) && !token.Lookup(name).IsKeyword() && name != "_" && !strings.HasPrefix(name, InternalNamePrefix)
}

func cloneTemplate(t FunctionTemplate) FunctionTemplate {
	t.SourceSig = runtime.CloneRuntimeFuncSig(t.SourceSig)
	t.RawArgs = append([]int(nil), t.RawArgs...)
	return t
}

func cloneRegisteredTemplate(t registeredTemplate) registeredTemplate {
	refs := t.pkgRefs
	t.FunctionTemplate = cloneTemplate(t.FunctionTemplate)
	if refs != nil {
		t.pkgRefs = make(map[string]struct{}, len(t.pkgRefs))
		for path := range refs {
			t.pkgRefs[path] = struct{}{}
		}
	}
	return t
}

func packageKey(path, name string) string {
	return path + "\x00" + name
}
