package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/parser"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

const (
	GraphFormat  = "mini-go-workspace-graph"
	GraphVersion = 3
)

type Package struct {
	Source    SourcePackage
	Documents []parser.Document
	Program   ast.Program
	Imports   []string
	Hash      string
}

type Graph struct {
	Root        string
	Target      target.Target
	Packages    map[string]Package
	Order       []string
	Hash        string
	Diagnostics []source.Diagnostic
}

func Load(root string, sources SourceSet, buildTarget target.Target) (Graph, error) {
	return LoadWithLimits(root, sources, buildTarget, Limits{})
}

func LoadWithLimits(root string, sources SourceSet, buildTarget target.Target, limits Limits) (Graph, error) {
	headers, err := LoadHeadersWithLimits(root, sources, buildTarget, limits)
	if err != nil {
		return Graph{}, err
	}
	graph := Graph{Root: headers.Root, Target: headers.Target, Packages: map[string]Package{}, Order: append([]string(nil), headers.Order...), Hash: headers.Hash, Diagnostics: append([]source.Diagnostic(nil), headers.Diagnostics...)}
	if len(graph.Diagnostics) != 0 {
		return graph, nil
	}
	for _, modulePath := range graph.Order {
		parsed, diagnostics, parseErr := ParsePackageWithLimits(headers.Packages[modulePath].Source, limits)
		if parseErr != nil {
			return Graph{}, parseErr
		}
		graph.Diagnostics = append(graph.Diagnostics, diagnostics...)
		graph.Packages[modulePath] = parsed
	}
	graph.Diagnostics = source.NormalizeDiagnostics(graph.Diagnostics)
	return graph, nil
}

func ParsePackage(pkg SourcePackage) (Package, []source.Diagnostic, error) {
	return ParsePackageWithLimits(pkg, Limits{})
}

func ParsePackageWithLimits(pkg SourcePackage, limits Limits) (Package, []source.Diagnostic, error) {
	limits = normalizeLimits(limits)
	pkg, err := normalizePackage(pkg)
	if err != nil {
		return Package{}, nil, err
	}
	program, imports, documents, diagnostics := parsePackage(pkg, limits)
	for i := range pkg.Files {
		pkg.Files[i].Hash = program.Files[i].Hash
	}
	return Package{Source: pkg, Documents: documents, Program: program, Imports: imports, Hash: packageContentHash(pkg)}, source.NormalizeDiagnostics(diagnostics), nil
}

func parsePackage(pkg SourcePackage, limits Limits) (ast.Program, []string, []parser.Document, []source.Diagnostic) {
	program := ast.Program{ModulePath: pkg.ModulePath}
	imports := map[string]struct{}{}
	documents := make([]parser.Document, 0, len(pkg.Files))
	collector := source.NewDiagnosticCollector(limits.MaxDiagnostics)
	nodeCount := 1
	for _, file := range pkg.Files {
		if file.OriginPath != "" {
			file.Path = file.OriginPath
		}
		document := parser.ParseDocumentFileWithLimits(pkg.ModulePath, file, limits.parserLimits())
		documents = append(documents, document)
		if document.NodeCount > 0 {
			nodeCount += document.NodeCount - 1
		}
		collector.AddAll(document.Diagnostics...)
		parsed := document.Program
		if program.Package == "" {
			program.Package = parsed.Package
			program.PackageID = parsed.PackageID
		} else if parsed.Package != "" && parsed.Package != program.Package {
			diagnostic := workspaceDiagnostic("compiler.package.name.mismatch", fmt.Sprintf("file %q has package %q, want %q", file.Path, parsed.Package, program.Package))
			if len(parsed.Files) != 0 {
				diagnostic.Primary = parsed.Files[0].Span
			}
			collector.Add(diagnostic)
		}
		for _, parsedFile := range parsed.Files {
			parsedFile.ID = file.ID
			if file.Hash != "" {
				parsedFile.Hash = file.Hash
			}
			program.Files = append(program.Files, parsedFile)
			for _, decl := range parsedFile.Decls {
				if decl.Kind == ast.DeclImport {
					if decl.Import.IsEmbedMarker() {
						continue
					}
					dependency := strings.TrimSpace(decl.Import.Path)
					if dependency != "" {
						imports[dependency] = struct{}{}
					}
				}
			}
		}
	}
	collector.AddAll(resolveEmbeds(&program, pkg.Resources)...)
	for _, file := range program.Files {
		for _, decl := range file.Decls {
			for _, value := range decl.Var.Values {
				if value.Kind == ast.ExprEmbed {
					nodeCount++
				}
			}
		}
	}
	if nodeCount > limits.MaxASTNodes {
		collector.Add(workspaceDiagnostic("ast.limit.nodes", "AST node count exceeds compiler limit"))
	}
	if source.HasErrors(collector.Diagnostics()) {
		return program, nil, documents, collector.Diagnostics()
	}
	ordered := make([]string, 0, len(imports))
	for dependency := range imports {
		ordered = append(ordered, dependency)
	}
	sort.Strings(ordered)
	return program, ordered, documents, collector.Diagnostics()
}

func packageContentHash(pkg SourcePackage) string {
	hasher := sha256.New()
	hasher.Write([]byte(source.HashFiles(pkg.Files)))
	for _, resource := range pkg.Resources {
		hasher.Write([]byte{0})
		hasher.Write([]byte(resource.Path))
		hasher.Write([]byte{0})
		hasher.Write([]byte(resource.Hash))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func workspaceDiagnostic(code, message string) source.Diagnostic {
	return source.Diagnostic{Code: source.DiagnosticCode(code), Severity: source.SeverityError, Message: message}
}
