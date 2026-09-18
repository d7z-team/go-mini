// Package gen contains language-independent preparation shared by MRPC generators.
package gen

import (
	"errors"
	"sort"
	"strconv"

	"github.com/d7z-team/mini-go/tooling/mrpc"
)

// LocalName chooses a generated local that cannot shadow a method field.
func LocalName(method mrpc.Method, base string) string {
	used := map[string]bool{}
	for _, field := range method.Params {
		used[field.Name] = true
	}
	for _, field := range method.Results {
		used[field.Name] = true
	}
	for used[base] {
		base += "_"
	}
	return base
}

// CanonicalCatalog rebuilds a catalog from its source files and verifies that
// callers did not alter its namespace or contract identity after parsing.
func CanonicalCatalog(catalog mrpc.Catalog) (mrpc.Catalog, error) {
	if len(catalog.Files) == 0 || catalog.Namespace == "" || catalog.ContractID == "" {
		return mrpc.Catalog{}, errors.New("empty catalog")
	}
	canonical, err := mrpc.NewCatalogWithDependencies(catalog.Files, catalog.Dependencies)
	if err != nil {
		return mrpc.Catalog{}, err
	}
	if canonical.Namespace != catalog.Namespace || canonical.ContractID != catalog.ContractID {
		return mrpc.Catalog{}, errors.New("catalog identity does not match its sources")
	}
	// Each source file owns its aliases; the generated target is one file.
	// Assign a deterministic, unique target alias per dependency and rewrite
	// structural type uses while leaving the validated source catalog untouched.
	aliases := map[string]string{}
	paths := map[string]string{}
	for _, file := range canonical.Files {
		for _, imported := range file.Imports {
			if previous, ok := paths[imported.Path]; !ok || imported.Alias < previous {
				paths[imported.Path] = imported.Alias
			}
		}
	}
	var ordered []string
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for i, path := range ordered {
		alias := paths[path]
		if _, used := aliases[alias]; used {
			alias += "_" + strconv.Itoa(i)
		}
		for {
			if _, used := aliases[alias]; !used {
				break
			}
			alias += "_"
		}
		aliases[alias], paths[path] = path, alias
	}
	canonical.Files = append([]mrpc.File(nil), canonical.Files...)
	for i := range canonical.Files {
		file := &canonical.Files[i]
		mapping := map[string]string{}
		file.Imports = append([]mrpc.Import(nil), file.Imports...)
		for j := range file.Imports {
			imported := &file.Imports[j]
			mapping[imported.Alias] = paths[imported.Path]
			imported.Alias = paths[imported.Path]
		}
		var rewriteType func(mrpc.Type) mrpc.Type
		rewriteType = func(typ mrpc.Type) mrpc.Type {
			if typ.Qualifier != "" {
				typ.Qualifier = mapping[typ.Qualifier]
			}
			if typ.Elem != nil {
				elem := rewriteType(*typ.Elem)
				typ.Elem = &elem
			}
			if typ.Key != nil {
				key := rewriteType(*typ.Key)
				typ.Key = &key
			}
			return typ
		}
		rewriteFields := func(fields []mrpc.Field) []mrpc.Field {
			out := append([]mrpc.Field(nil), fields...)
			for j := range out {
				out[j].Type = rewriteType(out[j].Type)
			}
			return out
		}
		rewriteMethods := func(methods []mrpc.Method) []mrpc.Method {
			out := append([]mrpc.Method(nil), methods...)
			for j := range out {
				out[j].Params = rewriteFields(out[j].Params)
				out[j].Results = rewriteFields(out[j].Results)
			}
			return out
		}
		file.Messages = append([]mrpc.Message(nil), file.Messages...)
		for j := range file.Messages {
			file.Messages[j].Fields = rewriteFields(file.Messages[j].Fields)
		}
		file.Resources = append([]mrpc.Resource(nil), file.Resources...)
		for j := range file.Resources {
			file.Resources[j].Methods = rewriteMethods(file.Resources[j].Methods)
		}
		file.Services = append([]mrpc.Service(nil), file.Services...)
		for j := range file.Services {
			file.Services[j].Methods = rewriteMethods(file.Services[j].Methods)
		}
	}
	return canonical, nil
}

// UsesFloatingPoint reports whether generated codecs need float or complex bit conversion.
func UsesFloatingPoint(catalog mrpc.Catalog) bool {
	var usesType func(mrpc.Type) bool
	usesType = func(typ mrpc.Type) bool {
		switch typ.Kind {
		case mrpc.TypeSlice, mrpc.TypeOptional:
			return typ.Elem != nil && usesType(*typ.Elem)
		case mrpc.TypeMap:
			return typ.Key != nil && usesType(*typ.Key) || typ.Elem != nil && usesType(*typ.Elem)
		default:
			return typ.Qualifier == "" && (typ.Name == "float32" || typ.Name == "float64" || typ.Name == "complex64" || typ.Name == "complex128")
		}
	}
	usesFields := func(fields []mrpc.Field) bool {
		for _, field := range fields {
			if usesType(field.Type) {
				return true
			}
		}
		return false
	}
	for _, file := range catalog.Files {
		for _, message := range file.Messages {
			if usesFields(message.Fields) {
				return true
			}
		}
		for _, service := range file.Services {
			for _, method := range service.Methods {
				if usesFields(method.Params) || usesFields(method.Results) {
					return true
				}
			}
		}
		for _, resource := range file.Resources {
			for _, method := range resource.Methods {
				if usesFields(method.Params) || usesFields(method.Results) {
					return true
				}
			}
		}
	}
	return false
}
