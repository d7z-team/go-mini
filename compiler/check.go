package compiler

import (
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/parser"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/types"
)

func checkWorkspace(request Request, roots []string) (CheckResult, error) {
	analyzed, err := analyzePackages(AnalysisRequest{Request: request, Roots: roots}, false)
	if err != nil {
		return CheckResult{}, err
	}
	documents := make(map[string][]parser.Document, len(analyzed.Packages))
	for path, pkg := range analyzed.Packages {
		documents[path] = pkg.Documents
	}
	return CheckResult{
		Target: analyzed.Target, Documents: documents, ExportHashes: analyzed.ExportHashes,
		Order: analyzed.Order, GraphHash: analyzed.GraphHash, Diagnostics: analyzed.Diagnostics, Stats: analyzed.Stats,
	}, nil
}

// DependencyExports returns the deterministic public semantic surface consumed
// when checking packages that import info's package.
func DependencyExports(info *check.ProgramInfo) []check.DependencyExport {
	if info == nil {
		return nil
	}
	scope := info.Scope(info.PackageScope)
	if scope == nil {
		return nil
	}
	names := make([]string, 0, len(scope.Objects))
	for name := range scope.Objects {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]check.DependencyExport, 0, len(names))
	for _, name := range names {
		object, ok := info.Object(scope.Objects[name])
		if !ok || (!object.Exported && object.Kind != check.ObjectType) {
			continue
		}
		export := check.DependencyExport{
			ModulePath: info.ModulePath, Name: name,
			Type: types.FormatWithTable(info.TypeTable, object.Type), Untyped: object.Untyped,
		}
		for _, param := range info.GenericDecls[object.ID] {
			export.TypeParams = append(export.TypeParams, info.Objects[param].Name)
		}
		switch object.Kind {
		case check.ObjectConst:
			export.Kind, export.ID = check.ObjectConst, "const."+name
			if value, ok := info.ConstObjects[object.ID]; ok {
				export.Value = value.JSON()
			}
		case check.ObjectVar:
			export.Kind, export.ID = check.ObjectVar, "global."+name
		case check.ObjectFunc:
			export.Kind, export.ID = check.ObjectFunc, firstNonEmpty(object.FunctionID, "fn."+name)
			if signature, ok := info.TypeTable.IsFunction(object.Type); ok {
				export.Variadic = signature.Variadic
			}
		case check.ObjectType:
			export.Kind, export.ID = check.ObjectType, "type."+name
			node, ok := info.TypeTable.Node(object.Type)
			if !ok {
				continue
			}
			if node.Alias {
				export.Type = types.FormatWithTable(info.TypeTable, node.AliasTarget)
			} else {
				export.Underlying = types.FormatWithTable(info.TypeTable, node.Underlying)
			}
			for _, field := range node.Fields {
				export.Fields = append(export.Fields, check.DependencyTypeField{
					Name: field.Name, Type: types.FormatWithTable(info.TypeTable, field.Type),
					Tag: field.Tag, Embedded: field.Embedded,
				})
			}
			for _, method := range node.Methods {
				export.Methods = append(export.Methods, check.DependencyTypeMethod{
					Name: method.Name, Receiver: types.FormatWithTable(info.TypeTable, method.Receiver),
					Signature: types.FormatSignature(info.TypeTable, method.Signature),
					Variadic:  method.Signature.Variadic, FunctionID: method.FunctionID,
					ModulePath: firstNonEmpty(method.ModulePath, info.ModulePath),
				})
			}
		default:
			continue
		}
		if strings.TrimSpace(export.Type) == "" {
			export.Type = "Any"
		}
		out = append(out, export)
	}
	return out
}
