package mrpc

import "fmt"

// ResolvedType identifies the declaration and catalog that own a named type.
// Source import aliases are resolved in the file containing the type use.
type ResolvedType struct {
	Kind     string
	Owner    Catalog
	File     File
	Name     string
	Resource *Resource
	Message  *Message
	Enum     *Enum
}

func (c Catalog) ResolveType(typ Type) (ResolvedType, error) {
	if typ.Kind != TypeName {
		return ResolvedType{}, fmt.Errorf("%s is not a named type", typ.String())
	}
	owner := c
	if typ.Qualifier != "" {
		path := ""
		for _, file := range c.Files {
			if typ.Span.Start.File != "" && file.Path != typ.Span.Start.File {
				continue
			}
			for _, imported := range file.Imports {
				if imported.Alias == typ.Qualifier {
					if path != "" && path != imported.Path {
						return ResolvedType{}, fmt.Errorf("ambiguous MRPC alias %q", typ.Qualifier)
					}
					path = imported.Path
				}
			}
		}
		var ok bool
		owner, ok = c.Dependencies[path]
		if !ok {
			return ResolvedType{}, fmt.Errorf("unknown MRPC alias %q", typ.Qualifier)
		}
	} else if _, scalar := scalarTypes[typ.Name]; scalar {
		return ResolvedType{Kind: "scalar", Name: typ.Name, Owner: c}, nil
	}
	for _, file := range owner.Files {
		for _, resource := range file.Resources {
			if resource.Name == typ.Name {
				return ResolvedType{Kind: "resource", Name: typ.Name, Owner: owner, File: file, Resource: &resource}, nil
			}
		}
		for _, message := range file.Messages {
			if message.Name == typ.Name {
				return ResolvedType{Kind: "message", Name: typ.Name, Owner: owner, File: file, Message: &message}, nil
			}
		}
		for _, enum := range file.Enums {
			if enum.Name == typ.Name {
				return ResolvedType{Kind: "enum", Name: typ.Name, Owner: owner, File: file, Enum: &enum}, nil
			}
		}
	}
	return ResolvedType{}, fmt.Errorf("unknown MRPC type %s", typ.String())
}
