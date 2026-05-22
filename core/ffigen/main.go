package ffigen

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// Options configures one ffigen generation run.
type Options struct {
	PackageName string
	Output      string
	Args        []string
}

type Generator struct {
	opts Options

	typeInfo     *types.Info
	fset         *token.FileSet
	knownImports map[string]string
	moduleCache  map[string]string
	packagePath  string
	packageDir   string
	modulePath   string
	moduleDir    string
}

func NewGenerator(opts Options) *Generator {
	return &Generator{opts: opts}
}

type targetMeta struct {
	moduleName      string
	methodsPrefix   string
	methodsMarked   bool
	interfaceMarked bool
	structTarget    bool
}

type ffigenTarget struct {
	spec *ast.TypeSpec
	meta targetMeta
}

type generationMode int

const (
	modeFiles generationMode = iota
	modeDirectory
)

type schemaDecl struct {
	varName     string
	displayName string
	specLiteral string
	ownership   string
}

type schemaRegistry struct {
	ordered []schemaDecl
	byVar   map[string]int
}

func newSchemaRegistry() *schemaRegistry {
	return &schemaRegistry{byVar: make(map[string]int)}
}

func (r *schemaRegistry) Ensure(displayName, ownership, specLiteral string) string {
	varName := structSchemaVarName(displayName)
	if idx, ok := r.byVar[varName]; ok {
		if r.ordered[idx].ownership != ownership {
			panic(fmt.Sprintf("ffigen: conflicting ownership for %s: %s vs %s", displayName, r.ordered[idx].ownership, ownership))
		}
		if existing := r.ordered[idx].specLiteral; existing == "" || (specLiteral != "" && len(specLiteral) > len(existing)) {
			r.ordered[idx] = schemaDecl{
				varName:     varName,
				displayName: displayName,
				specLiteral: specLiteral,
				ownership:   ownership,
			}
		}
		return varName
	}
	r.byVar[varName] = len(r.ordered)
	r.ordered = append(r.ordered, schemaDecl{
		varName:     varName,
		displayName: displayName,
		specLiteral: specLiteral,
		ownership:   ownership,
	})
	return varName
}

type displayTypeResolver struct {
	gen               *Generator
	moduleName        string
	importAliases     map[string]string
	collidingBaseName map[string]bool
}
