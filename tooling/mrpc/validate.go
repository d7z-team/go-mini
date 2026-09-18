package mrpc

import (
	"fmt"
	"strings"
)

var scalarTypes = map[string]struct{}{
	"bool": {}, "string": {},
	"int8": {}, "int16": {}, "int32": {}, "int64": {},
	"uint8": {}, "uint16": {}, "uint32": {}, "uint64": {},
	"float32": {}, "float64": {}, "complex64": {}, "complex128": {},
}

// Validate checks one complete namespace. Imports are file-scoped while
// declarations and generator options are namespace-scoped.
func Validate(files []File) []Diagnostic {
	var diagnostics []Diagnostic
	add := func(code, message string, span Span) {
		diagnostics = append(diagnostics, Diagnostic{Code: DiagnosticCode(code), Severity: SeverityError, Message: message, Primary: span})
	}
	namespace := ""
	declarations := make(map[string]string)
	types := make(map[string]string)
	options := make(map[string]string)
	register := func(name, kind string, span Span) {
		if !validIdentifier(name) {
			add("mrpc.name", fmt.Sprintf("invalid %s name %q", kind, name), span)
			return
		}
		if previous, exists := declarations[name]; exists {
			add("mrpc.name.duplicate", fmt.Sprintf("%s %q conflicts with %s", kind, name, previous), span)
			return
		}
		declarations[name] = kind
	}
	for _, file := range files {
		if file.Syntax != Syntax {
			add("mrpc.syntax.version", "missing valid syntax declaration", Span{})
		}
		if !validNamespace(file.Namespace) {
			add("mrpc.namespace", "invalid contract namespace", file.NamespaceSpan)
		} else if namespace == "" {
			namespace = file.Namespace
		} else if file.Namespace != namespace {
			add("mrpc.namespace.mismatch", fmt.Sprintf("namespace %q does not match %q", file.Namespace, namespace), file.NamespaceSpan)
		}
		for _, option := range file.Options {
			if !validIdentifier(option.Name) || strings.TrimSpace(option.Value) == "" {
				add("mrpc.option", "invalid generator option", option.Span)
				continue
			}
			if previous, exists := options[option.Name]; exists && previous != option.Value {
				add("mrpc.option.conflict", fmt.Sprintf("option %q has conflicting values", option.Name), option.Span)
			}
			options[option.Name] = option.Value
		}
		for _, message := range file.Messages {
			register(message.Name, "message", message.NameSpan)
			types[message.Name] = "message"
		}
		for _, enum := range file.Enums {
			register(enum.Name, "enum", enum.NameSpan)
			types[enum.Name] = "enum"
		}
		for _, resource := range file.Resources {
			register(resource.Name, "resource", resource.NameSpan)
			types[resource.Name] = "resource"
		}
		for _, service := range file.Services {
			register(service.Name, "service", service.NameSpan)
		}
	}
	for _, file := range files {
		imports := make(map[string]string)
		for _, imported := range file.Imports {
			alias := imported.Alias
			if alias == "" {
				add("mrpc.import.alias", "contract import requires an explicit alias", imported.Span)
				continue
			}
			if !validIdentifier(alias) || strings.TrimSpace(imported.Path) == "" {
				add("mrpc.import", "invalid contract import", imported.Span)
				continue
			}
			if _, exists := imports[alias]; exists {
				add("mrpc.import.duplicate", fmt.Sprintf("duplicate import alias %q", alias), imported.Span)
			}
			imports[alias] = imported.Path
		}
		validateFields := func(owner string, fields []Field, allowDirectResource bool) {
			names, ids := make(map[string]struct{}), make(map[int]struct{})
			for _, field := range fields {
				if !validIdentifier(field.Name) {
					add("mrpc.field.name", fmt.Sprintf("%s has invalid field name %q", owner, field.Name), field.NameSpan)
				}
				if _, duplicate := names[field.Name]; duplicate {
					add("mrpc.field.name_duplicate", fmt.Sprintf("%s has duplicate field %q", owner, field.Name), field.NameSpan)
				}
				names[field.Name] = struct{}{}
				if field.ID <= 0 {
					add("mrpc.field.id", fmt.Sprintf("%s field %q must have a positive ID", owner, field.Name), field.IDSpan)
				} else if _, duplicate := ids[field.ID]; duplicate {
					add("mrpc.field.id_duplicate", fmt.Sprintf("%s has duplicate field ID %d", owner, field.ID), field.IDSpan)
				}
				ids[field.ID] = struct{}{}
				if !validateTypeReference(field.Type, types, imports) {
					add("mrpc.type.unknown", fmt.Sprintf("%s uses unknown type %s", owner, field.Type.String()), field.Type.Span)
				} else if invalidResourcePosition(field.Type, types, allowDirectResource) {
					add("mrpc.resource.nested", owner+" uses a resource outside a direct method field", field.Type.Span)
				}
			}
		}
		for _, enum := range file.Enums {
			names := make(map[string]struct{}, len(enum.Values))
			numbers := make(map[int64]struct{}, len(enum.Values))
			if len(enum.Values) == 0 {
				add("mrpc.enum.empty", "enum "+enum.Name+" has no values", enum.Span)
			}
			for _, value := range enum.Values {
				if value.Number < -1<<31 || value.Number > 1<<31-1 {
					add("mrpc.enum.range", "enum value must fit signed int32", value.Span)
				}
				if !validIdentifier(value.Name) {
					add("mrpc.enum.name", fmt.Sprintf("enum %s has invalid value %q", enum.Name, value.Name), value.NameSpan)
				}
				if _, duplicate := names[value.Name]; duplicate {
					add("mrpc.enum.name_duplicate", fmt.Sprintf("enum %s has duplicate value %q", enum.Name, value.Name), value.NameSpan)
				}
				if _, duplicate := numbers[value.Number]; duplicate {
					add("mrpc.enum.number_duplicate", fmt.Sprintf("enum %s has duplicate number %d", enum.Name, value.Number), value.Span)
				}
				names[value.Name], numbers[value.Number] = struct{}{}, struct{}{}
			}
		}
		for _, message := range file.Messages {
			validateFields("message "+message.Name, message.Fields, false)
		}
		validateMethods := func(owner string, methods []Method) {
			names := make(map[string]struct{})
			for _, method := range methods {
				if !validIdentifier(method.Name) {
					add("mrpc.method.name", fmt.Sprintf("%s has invalid method name %q", owner, method.Name), method.NameSpan)
				}
				if _, duplicate := names[method.Name]; duplicate {
					add("mrpc.method.duplicate", fmt.Sprintf("%s has duplicate method %q", owner, method.Name), method.NameSpan)
				}
				names[method.Name] = struct{}{}
				if method.Name == "Close" {
					add("mrpc.method.close_reserved", owner+".Close conflicts with generated resource lifecycle", method.NameSpan)
				}
				validateFields(owner+"."+method.Name+" request", method.Params, true)
				validateFields(owner+"."+method.Name+" response", method.Results, true)
			}
		}
		for _, resource := range file.Resources {
			validateMethods("resource "+resource.Name, resource.Methods)
		}
		for _, service := range file.Services {
			validateMethods("service "+service.Name, service.Methods)
		}
	}
	return NormalizeDiagnostics(diagnostics)
}

// ValidateDependencies verifies imported type references against already
// parsed catalogs keyed by the import strings used by the source files.
func ValidateDependencies(catalog Catalog, dependencies map[string]Catalog) []Diagnostic {
	var diagnostics []Diagnostic
	for _, file := range catalog.Files {
		imports := make(map[string]Catalog)
		for _, imported := range file.Imports {
			dependency, ok := dependencies[imported.Path]
			if !ok {
				diagnostics = append(diagnostics, Diagnostic{Code: "mrpc.import.missing", Severity: SeverityError, Message: fmt.Sprintf("missing imported contract %q", imported.Path), Primary: imported.PathSpan})
				continue
			}
			imports[imported.Alias] = dependency
		}
		var checkType func(string, Type, bool)
		checkType = func(owner string, typ Type, allowDirectResource bool) {
			switch typ.Kind {
			case TypeSlice, TypeOptional:
				checkType(owner, *typ.Elem, false)
				return
			case TypeMap:
				checkType(owner, *typ.Key, false)
				checkType(owner, *typ.Elem, false)
				return
			}
			if typ.Qualifier == "" {
				return
			}
			dependency, ok := imports[typ.Qualifier]
			if !ok || !catalogHasType(dependency, typ.Name) {
				diagnostics = append(diagnostics, Diagnostic{Code: "mrpc.type.unknown", Severity: SeverityError, Message: fmt.Sprintf("%s uses unknown type %s", owner, typ.String()), Primary: typ.Span})
			} else if catalogHasResource(dependency, typ.Name) && !allowDirectResource {
				diagnostics = append(diagnostics, Diagnostic{Code: "mrpc.resource.nested", Severity: SeverityError, Message: owner + " uses a resource outside a direct method field", Primary: typ.Span})
			}
		}
		for _, message := range file.Messages {
			for _, field := range message.Fields {
				checkType("message "+message.Name, field.Type, false)
			}
		}
		for _, resource := range file.Resources {
			for _, method := range resource.Methods {
				for _, field := range append(append([]Field(nil), method.Params...), method.Results...) {
					checkType("resource "+resource.Name+"."+method.Name, field.Type, true)
				}
			}
		}
		for _, service := range file.Services {
			for _, method := range service.Methods {
				for _, field := range append(append([]Field(nil), method.Params...), method.Results...) {
					checkType("service "+service.Name+"."+method.Name, field.Type, true)
				}
			}
		}
	}
	return NormalizeDiagnostics(diagnostics)
}

func catalogHasType(catalog Catalog, name string) bool {
	for _, file := range catalog.Files {
		for _, enum := range file.Enums {
			if enum.Name == name {
				return true
			}
		}
		for _, message := range file.Messages {
			if message.Name == name {
				return true
			}
		}
		for _, resource := range file.Resources {
			if resource.Name == name {
				return true
			}
		}
	}
	return false
}

func catalogHasResource(catalog Catalog, name string) bool {
	for _, file := range catalog.Files {
		for _, resource := range file.Resources {
			if resource.Name == name {
				return true
			}
		}
	}
	return false
}

func invalidResourcePosition(typ Type, local map[string]string, allowDirect bool) bool {
	if typ.Kind == TypeName {
		return typ.Qualifier == "" && local[typ.Name] == "resource" && !allowDirect
	}
	var nested func(Type) bool
	nested = func(value Type) bool {
		if value.Kind == TypeName {
			return value.Qualifier == "" && local[value.Name] == "resource"
		}
		if value.Key != nil && nested(*value.Key) {
			return true
		}
		return value.Elem != nil && nested(*value.Elem)
	}
	return nested(typ)
}

func validateTypeReference(typ Type, local, imports map[string]string) bool {
	switch typ.Kind {
	case TypeSlice, TypeOptional:
		return typ.Elem != nil && validateTypeReference(*typ.Elem, local, imports)
	case TypeMap:
		return typ.Key != nil && typ.Elem != nil && validMapKey(*typ.Key) && validateTypeReference(*typ.Elem, local, imports)
	case TypeName:
		if typ.Qualifier != "" {
			_, ok := imports[typ.Qualifier]
			return ok && validIdentifier(typ.Name)
		}
		if _, scalar := scalarTypes[typ.Name]; scalar {
			return true
		}
		_, ok := local[typ.Name]
		return ok
	default:
		return false
	}
}

func validMapKey(typ Type) bool {
	if typ.Kind != TypeName || typ.Qualifier != "" {
		return false
	}
	switch typ.Name {
	case "string", "bool", "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64":
		return true
	default:
		return false
	}
}

func validNamespace(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !validIdentifier(part) {
			return false
		}
	}
	return true
}

func validIdentifier(value string) bool {
	if value == "" || value == "_" {
		return false
	}
	for index, r := range value {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (index == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}
