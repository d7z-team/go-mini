package compiler

import (
	"context"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/cache"
	"github.com/d7z-team/mini-go/compiler/emit"
	"github.com/d7z-team/mini-go/compiler/lower"
	"github.com/d7z-team/mini-go/compiler/optimize"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/specialize"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/types"
	"github.com/d7z-team/mini-go/compiler/workspace"
	ir "github.com/d7z-team/mini-go/runtime/bytecode"
)

type Result struct {
	Target         target.Target
	Artifacts      map[string]ir.Artifact
	PackageSymbols map[string]ir.PackageSymbols
	// ArtifactHashes identify artifacts with their dependency hashes bound.
	ArtifactHashes map[string]string
	// PackageActionHashes identify unbound package artifacts stored by compile actions.
	PackageActionHashes map[string]string
	ExportData          map[string]cache.PackageData
	ExportHashes        map[string]string
	Order               []string
	GraphHash           string
	Diagnostics         []source.Diagnostic
	Stats               Stats
}

type Stats struct {
	PackagesScanned    int
	PackagesParsed     int
	PackagesAnalyzed   int
	PackagesLowered    int
	PackagesCompiled   int
	PackageCacheHits   int
	PackageCacheMisses int
	PrepareCacheHits   int
	PrepareCacheMisses int
	ImagesLinked       int
	TestsDiscovered    int
}

type Request struct {
	Context          context.Context
	Root             string
	Target           target.Target
	Sources          workspace.SourceSet
	EntryPoints      []EntryPoint
	Cache            cache.Cache
	CacheVerify      bool
	CacheHash        bool
	TraceCache       func(cache.Event)
	Limits           Limits
	HostCapabilities map[string][]string
	Optimization     OptimizationLevel
	Symbols          bool
	workspace        *workspace.Loader
}

func loadHeaderGraph(request Request, roots []string) (workspace.HeaderGraph, error) {
	limits := request.Limits.workspaceLimits()
	if request.workspace != nil {
		return request.workspace.LoadHeadersForRoots(roots)
	}
	return workspace.LoadHeadersForRootsWithLimits(roots, request.Sources, request.Target, limits)
}

func (r Result) OK() bool {
	return !source.HasErrors(r.Diagnostics)
}

func (r Result) Artifact(modulePath string) (ir.Artifact, bool) {
	if r.Artifacts == nil {
		return ir.Artifact{}, false
	}
	artifact, ok := r.Artifacts[strings.TrimSpace(modulePath)]
	return artifact, ok
}

type compiledPackage struct {
	Artifact    ir.Artifact
	Symbols     ir.PackageSymbols
	Checked     check.CheckedProgram
	ExportData  cache.PackageData
	Diagnostics []source.Diagnostic
}

func (r compiledPackage) OK() bool {
	return !source.HasErrors(r.Diagnostics)
}

func compileParsedPackageWithLimits(ctx context.Context, program ast.Program, options lower.Options, dependencies map[string]cache.PackageData, limits Limits, optimization OptimizationLevel) (compiledPackage, error) {
	// Runtime exports contain concrete declarations. Source analysis also needs
	// the signatures of generic templates, including file-scoped dot imports.
	options.Dependencies = append([]check.DependencyPackage(nil), options.Dependencies...)
	for i := range options.Dependencies {
		dependency := &options.Dependencies[i]
		dependency.Members = append([]check.DependencyExport(nil), dependency.Members...)
		path := dependency.ModulePath
		data := dependencies[path]
		for _, template := range data.GenericTemplates {
			switch template.Kind {
			case "function":
				params := make([]string, len(template.Decl.Func.TypeParams))
				for i, param := range template.Decl.Func.TypeParams {
					params[i] = param.Name
				}
				dependency.Members = append(dependency.Members, check.DependencyExport{
					ModulePath: path, Name: template.Name, Kind: check.ObjectFunc, Type: template.Type, TypeParams: params,
				})
			case "type":
				export := check.DependencyExport{ModulePath: path, Name: template.Name, Kind: check.ObjectType, Type: template.Type}
				for _, param := range template.Decl.Type.TypeParams {
					export.TypeParams = append(export.TypeParams, param.Name)
				}
				if node, ok := data.TypeTable.Named(types.TypeKey{ModulePath: path, DeclID: types.DeclID(template.Name)}); ok {
					export.Underlying = types.FormatWithTable(&data.TypeTable, node.Underlying)
				}
				dependency.Members = append(dependency.Members, export)
			}
		}
	}
	sourceChecked, err := analyzeProgram(ctx, program, options.Dependencies, limits)
	if err != nil {
		return compiledPackage{}, err
	}
	sourceProgram := sourceChecked.Program
	semanticDiagnostics := sourceChecked.Info.Diagnostics
	if len(semanticDiagnostics) != 0 {
		return compiledPackage{Checked: sourceChecked, Diagnostics: boundedDiagnostics(semanticDiagnostics, limits.MaxDiagnostics)}, nil
	}
	if err := ctx.Err(); err != nil {
		return compiledPackage{}, err
	}
	checked := sourceChecked
	if specialize.Required(sourceChecked, dependencies) {
		specialized, genericDiagnostics, err := specialize.ApplyWithLimits(sourceChecked, dependencies, specialize.Limits{
			MaxSpecializations: limits.MaxSpecializations, MaxDiagnostics: limits.MaxDiagnostics,
		})
		if err != nil {
			return compiledPackage{}, err
		}
		if len(genericDiagnostics) != 0 {
			return compiledPackage{Diagnostics: genericDiagnostics}, nil
		}
		if err := ctx.Err(); err != nil {
			return compiledPackage{}, err
		}
		checked, err = analyzeProgram(ctx, specialized, options.Dependencies, limits)
		if err != nil {
			return compiledPackage{}, err
		}
	}
	hirProgram, diagnostics := lower.Lower(checked, options)
	if len(diagnostics) != 0 {
		return compiledPackage{Diagnostics: boundedDiagnostics(diagnostics, limits.MaxDiagnostics)}, nil
	}
	if err := ctx.Err(); err != nil {
		return compiledPackage{}, err
	}
	hirProgram, err = optimize.Apply(hirProgram, optimize.Level(optimization))
	if err != nil {
		return compiledPackage{Diagnostics: []source.Diagnostic{diagnostic("compiler.optimize", "optimize module "+program.ModulePath+": "+err.Error())}}, nil
	}
	if err := ctx.Err(); err != nil {
		return compiledPackage{}, err
	}
	artifact, symbols, err := emit.LowerUnvalidatedWithSymbols(hirProgram)
	if err != nil {
		return compiledPackage{Diagnostics: []source.Diagnostic{diagnostic("compiler.emit", "lower module "+program.ModulePath+": "+err.Error())}}, nil
	}
	if err := ir.ValidateArtifact(&artifact); err != nil {
		return compiledPackage{Diagnostics: []source.Diagnostic{diagnostic("compiler.emit", "validate module "+program.ModulePath+": "+err.Error())}}, nil
	}
	if err := ctx.Err(); err != nil {
		return compiledPackage{}, err
	}
	packageData, err := buildPackageData(sourceProgram, artifact, symbols, sourceChecked.Info)
	if err != nil {
		return compiledPackage{}, err
	}
	return compiledPackage{Artifact: artifact, Symbols: symbols, Checked: sourceChecked, ExportData: packageData}, nil
}

type packageCacheLookup struct {
	Artifact      ir.Artifact
	Symbols       ir.PackageSymbols
	ExportData    cache.PackageData
	ArtifactHash  string
	ExportHash    string
	Hit           bool
	ReuseArtifact bool
	ActionKey     string
	Unlock        func()
}

func Compile(request Request) (Result, error) {
	request.Context = requestContext(request.Context)
	if err := request.Context.Err(); err != nil {
		return Result{}, err
	}
	request.Limits = normalizeCompilerLimits(request.Limits)
	if err := validateOptimizationLevel(request.Optimization); err != nil {
		return Result{}, err
	}
	normalizedTarget, err := target.Normalize(request.Target)
	if err != nil {
		return Result{}, err
	}
	request.Target = normalizedTarget
	if request.Cache == nil && !request.CacheHash {
		return compileWorkspace(request, nil)
	}
	return compileWorkspace(request, &workspaceBuildCache{request: request})
}

func requestContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func diagnostic(code, message string) source.Diagnostic {
	return source.Diagnostic{
		Code:     source.DiagnosticCode(code),
		Severity: "error",
		Message:  message,
	}
}
