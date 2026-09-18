package mrpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Catalog contains one validated namespace and all resolved dependencies.
type Catalog struct {
	Namespace    string
	Files        []File
	Options      map[string]string
	Dependencies map[string]Catalog
	ContractID   string
}

// ResourceTypeID returns the stable wire identity of one resource declaration.
func (c Catalog) ResourceTypeID(name string) string {
	sum := sha256.Sum256([]byte(ProtocolDomain + "\x00resource\x00" + c.ContractID + "\x00" + name))
	return hex.EncodeToString(sum[:])
}

// NewCatalog validates files and constructs their canonical schema identity.
func NewCatalog(files []File) (Catalog, error) {
	return NewCatalogWithDependencies(files, nil)
}

// NewCatalogWithDependencies validates files and constructs canonical schema
// identity. Dependencies are keyed by the exact import path used in source.
func NewCatalogWithDependencies(files []File, dependencies map[string]Catalog) (Catalog, error) {
	if err := validateCatalogDependencies(dependencies, make(map[string]bool)); err != nil {
		return Catalog{}, err
	}
	diagnostics := Validate(files)
	if HasErrors(diagnostics) {
		return Catalog{}, fmt.Errorf("%s: %s", diagnostics[0].Code, diagnostics[0].Message)
	}
	out := buildCatalog(files, dependencies)
	diagnostics = ValidateDependencies(out, dependencies)
	if HasErrors(diagnostics) {
		return Catalog{}, fmt.Errorf("%s: %s", diagnostics[0].Code, diagnostics[0].Message)
	}
	return out, nil
}

func buildCatalog(files []File, dependencies map[string]Catalog) Catalog {
	out := Catalog{Files: append([]File(nil), files...), Options: make(map[string]string), Dependencies: make(map[string]Catalog, len(dependencies))}
	for path, dependency := range dependencies {
		out.Dependencies[path] = dependency
	}
	if len(files) != 0 {
		out.Namespace = files[0].Namespace
	}
	for _, file := range files {
		for _, option := range file.Options {
			out.Options[option.Name] = option.Value
		}
	}
	material := catalogMaterial{Version: Syntax, Namespace: out.Namespace}
	imports := make(map[string]string)
	for _, file := range files {
		for _, imported := range file.Imports {
			if dependency, ok := dependencies[imported.Path]; ok {
				imports[imported.Path] = dependency.ContractID
			}
		}
		for _, item := range file.Enums {
			material.Enums = append(material.Enums, canonicalEnum(item))
		}
		for _, item := range file.Messages {
			material.Messages = append(material.Messages, canonicalMessage(item, file.Imports, out.Namespace))
		}
		for _, item := range file.Resources {
			material.Resources = append(material.Resources, canonicalResource(item, file.Imports, out.Namespace))
		}
		for _, item := range file.Services {
			material.Services = append(material.Services, canonicalResource(Resource{Name: item.Name, Methods: item.Methods}, file.Imports, out.Namespace))
		}
	}
	for path, contractID := range imports {
		material.Imports = append(material.Imports, canonicalImport{Path: path, ContractID: contractID})
	}
	sort.Slice(material.Imports, func(i, j int) bool { return material.Imports[i].Path < material.Imports[j].Path })
	sort.Slice(material.Enums, func(i, j int) bool { return material.Enums[i].Name < material.Enums[j].Name })
	sort.Slice(material.Messages, func(i, j int) bool { return material.Messages[i].Name < material.Messages[j].Name })
	sort.Slice(material.Resources, func(i, j int) bool { return material.Resources[i].Name < material.Resources[j].Name })
	sort.Slice(material.Services, func(i, j int) bool { return material.Services[i].Name < material.Services[j].Name })
	encoded, _ := json.Marshal(material)
	sum := sha256.Sum256(append([]byte(ProtocolDomain+"\x00"), encoded...))
	out.ContractID = hex.EncodeToString(sum[:])
	return out
}

func validateCatalog(catalog Catalog, visiting map[string]bool) error {
	key := catalog.Namespace + "\x00" + catalog.ContractID
	if visiting[key] {
		return errors.New("MRPC catalog dependency cycle")
	}
	visiting[key] = true
	defer delete(visiting, key)
	if err := validateCatalogDependencies(catalog.Dependencies, visiting); err != nil {
		return err
	}
	diagnostics := Validate(catalog.Files)
	if HasErrors(diagnostics) {
		return fmt.Errorf("%s: %s", diagnostics[0].Code, diagnostics[0].Message)
	}
	expected := buildCatalog(catalog.Files, catalog.Dependencies)
	diagnostics = ValidateDependencies(expected, catalog.Dependencies)
	if HasErrors(diagnostics) {
		return fmt.Errorf("%s: %s", diagnostics[0].Code, diagnostics[0].Message)
	}
	if catalog.Namespace != expected.Namespace || catalog.ContractID != expected.ContractID {
		return errors.New("MRPC catalog identity does not match its sources")
	}
	return nil
}

func validateCatalogDependencies(dependencies map[string]Catalog, visiting map[string]bool) error {
	for path, dependency := range dependencies {
		if err := validateCatalog(dependency, visiting); err != nil {
			return fmt.Errorf("MRPC dependency %q: %w", path, err)
		}
	}
	return nil
}

type catalogMaterial struct {
	Version   string                  `json:"version"`
	Namespace string                  `json:"namespace"`
	Imports   []canonicalImport       `json:"imports,omitempty"`
	Enums     []canonicalEnumType     `json:"enums,omitempty"`
	Messages  []canonicalMessageType  `json:"messages,omitempty"`
	Resources []canonicalResourceType `json:"resources,omitempty"`
	Services  []canonicalServiceType  `json:"services,omitempty"`
}

type canonicalImport struct {
	Path       string `json:"path"`
	ContractID string `json:"contract_id"`
}

type canonicalEnumType struct {
	Name   string               `json:"name"`
	Values []canonicalEnumValue `json:"values"`
}

type canonicalEnumValue struct {
	Name   string `json:"name"`
	Number int64  `json:"number"`
}

type canonicalMessageType struct {
	Name   string           `json:"name"`
	Fields []canonicalField `json:"fields,omitempty"`
}

type canonicalResourceType struct {
	Name    string            `json:"name"`
	Methods []canonicalMethod `json:"methods,omitempty"`
}

type canonicalServiceType = canonicalResourceType

type canonicalMethod struct {
	Name    string           `json:"name"`
	Params  []canonicalField `json:"params,omitempty"`
	Results []canonicalField `json:"results,omitempty"`
}

type canonicalField struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type"`
	ID   int    `json:"id"`
}

func canonicalEnum(item Enum) canonicalEnumType {
	out := canonicalEnumType{Name: item.Name, Values: make([]canonicalEnumValue, len(item.Values))}
	for i, value := range item.Values {
		out.Values[i] = canonicalEnumValue{Name: value.Name, Number: value.Number}
	}
	sort.Slice(out.Values, func(i, j int) bool {
		if out.Values[i].Number != out.Values[j].Number {
			return out.Values[i].Number < out.Values[j].Number
		}
		return out.Values[i].Name < out.Values[j].Name
	})
	return out
}

func canonicalMessage(item Message, imports []Import, namespace string) canonicalMessageType {
	out := canonicalMessageType{Name: item.Name}
	for _, field := range item.Fields {
		out.Fields = append(out.Fields, canonicalField{Name: field.Name, Type: canonicalType(field.Type, imports, namespace), ID: field.ID})
	}
	return out
}

func canonicalResource(item Resource, imports []Import, namespace string) canonicalResourceType {
	out := canonicalResourceType{Name: item.Name}
	for _, method := range item.Methods {
		out.Methods = append(out.Methods, canonicalMethodValue(method, imports, namespace))
	}
	sort.Slice(out.Methods, func(i, j int) bool { return out.Methods[i].Name < out.Methods[j].Name })
	return out
}

func canonicalMethodValue(method Method, imports []Import, namespace string) canonicalMethod {
	out := canonicalMethod{Name: method.Name}
	for _, field := range method.Params {
		out.Params = append(out.Params, canonicalField{Name: field.Name, Type: canonicalType(field.Type, imports, namespace), ID: field.ID})
	}
	for _, field := range method.Results {
		out.Results = append(out.Results, canonicalField{Name: field.Name, Type: canonicalType(field.Type, imports, namespace), ID: field.ID})
	}
	return out
}

func canonicalType(typ Type, imports []Import, namespace string) string {
	aliases := make(map[string]string, len(imports))
	for _, item := range imports {
		aliases[item.Alias] = item.Path
	}
	var format func(Type) string
	format = func(value Type) string {
		switch value.Kind {
		case TypeSlice:
			return "[]" + format(*value.Elem)
		case TypeMap:
			return "map[" + format(*value.Key) + "]" + format(*value.Elem)
		case TypeOptional:
			return "optional[" + format(*value.Elem) + "]"
		default:
			if value.Qualifier != "" {
				return aliases[value.Qualifier] + "." + value.Name
			}
			if _, scalar := scalarTypes[value.Name]; !scalar {
				return namespace + "." + value.Name
			}
			return value.Name
		}
	}
	return format(typ)
}

// CanonicalType resolves a catalog type through its imported contract
// namespaces. Generated languages therefore share the same wire identity even
// when their package paths differ.
func (c Catalog) CanonicalType(typ Type) (string, error) {
	switch typ.Kind {
	case TypeSlice, TypeOptional:
		if typ.Elem == nil {
			return "", errors.New("missing element type")
		}
		elem, err := c.CanonicalType(*typ.Elem)
		if typ.Kind == TypeSlice {
			return "[]" + elem, err
		}
		return "optional[" + elem + "]", err
	case TypeMap:
		if typ.Key == nil || typ.Elem == nil {
			return "", errors.New("missing map type")
		}
		key, err := c.CanonicalType(*typ.Key)
		if err != nil {
			return "", err
		}
		elem, err := c.CanonicalType(*typ.Elem)
		return "map[" + key + "]" + elem, err
	}
	resolved, err := c.ResolveType(typ)
	if err != nil {
		return "", err
	}
	if resolved.Kind == "scalar" {
		return typ.Name, nil
	}
	return resolved.Owner.Namespace + "." + resolved.Name, nil
}

// ResolvedResource retains the owning catalog of a resource declaration.
type ResolvedResource struct {
	Resource
	Owner Catalog
}

// ServiceResources returns the transitive resource method closure, retaining
// each declaration's contract owner across imports.
func (c Catalog) ServiceResources(service Service) []ResolvedResource {
	var out []ResolvedResource
	seen := map[string]bool{}
	var visit func(Catalog, Type)
	visit = func(owner Catalog, typ Type) {
		if typ.Kind != TypeName {
			return
		}
		resolved, err := owner.ResolveType(typ)
		if err != nil || resolved.Resource == nil {
			return
		}
		key := resolved.Owner.ContractID + "/" + resolved.Name
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ResolvedResource{Resource: *resolved.Resource, Owner: resolved.Owner})
		for _, method := range resolved.Resource.Methods {
			for _, field := range method.Params {
				visit(resolved.Owner, field.Type)
			}
			for _, field := range method.Results {
				visit(resolved.Owner, field.Type)
			}
		}
	}
	for _, method := range service.Methods {
		for _, field := range method.Params {
			visit(c, field.Type)
		}
		for _, field := range method.Results {
			visit(c, field.Type)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Owner.Namespace+"/"+out[i].Name < out[j].Owner.Namespace+"/"+out[j].Name
	})
	return out
}
