package language

import (
	"sort"

	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (e *Engine) TypeDefinition(uri DocumentURI, position Position) []Location {
	occurrence, ok := e.occurrence(uri, position)
	if !ok || !occurrence.Type.Valid() {
		return nil
	}
	info := e.snapshot.packages[occurrence.ModulePath].Checked.Info
	node, exists := resolvedTypeNode(info.TypeTable, occurrence.Type)
	if !exists || node.Identity.ModulePath == "" || node.Identity.DeclID == "" {
		return nil
	}
	key := "export|" + node.Identity.ModulePath + "|" + string(node.Identity.DeclID)
	if location, found := e.snapshot.definitions[key]; found {
		return []Location{location}
	}
	return nil
}

func (e *Engine) Implementation(uri DocumentURI, position Position) []Location {
	occurrence, ok := e.occurrence(uri, position)
	if !ok || !occurrence.Type.Valid() {
		return nil
	}
	targetInfo := e.snapshot.packages[occurrence.ModulePath].Checked.Info
	targetMethods, _, typeSet, isInterface := targetInfo.Relations.View(occurrence.Type).Interface()
	if !isInterface || typeSet {
		return nil
	}
	target := make(map[string]string, len(targetMethods))
	for _, method := range targetMethods {
		target[methodIdentity(method)] = types.FormatSignature(targetInfo.TypeTable, method.Signature)
	}
	var out []Location
	for modulePath, pkg := range e.snapshot.packages {
		info := pkg.Checked.Info
		for _, object := range info.Objects {
			if object.Kind != check.ObjectType || object.Scope != info.PackageScope || object.Alias || !object.Type.Valid() {
				continue
			}
			if modulePath == occurrence.ModulePath && object.Type == occurrence.Type {
				continue
			}
			node, exists := resolvedTypeNode(info.TypeTable, object.Type)
			if !exists {
				continue
			}
			available := make(map[string]string, len(node.Methods))
			for _, method := range node.Methods {
				available[methodIdentity(method)] = types.FormatSignature(info.TypeTable, method.Signature)
			}
			matches := true
			for name, signature := range target {
				if available[name] != signature {
					matches = false
					break
				}
			}
			if matches {
				if location, found := e.snapshot.definitions["export|"+modulePath+"|"+object.Name]; found {
					out = append(out, location)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].URI != out[j].URI {
			return out[i].URI < out[j].URI
		}
		return out[i].Range.Start.Line < out[j].Range.Start.Line
	})
	return out
}

func methodIdentity(method types.Method) string {
	if method.Name != "" && method.Name[0] >= 'a' && method.Name[0] <= 'z' {
		return method.ModulePath + "|" + method.Name
	}
	return method.Name
}

func resolvedTypeNode(table *types.TypeTable, ref types.TypeRef) (types.TypeNode, bool) {
	if node, ok := table.Node(ref); ok {
		return node, true
	}
	if ref.Kind == types.Named {
		return table.Named(ref.Named)
	}
	return types.TypeNode{}, false
}

func (e *Engine) Highlights(uri DocumentURI, position Position) []DocumentHighlight {
	locations := e.References(uri, position, true)
	var out []DocumentHighlight
	for _, location := range locations {
		if location.URI == uri {
			out = append(out, DocumentHighlight{Range: location.Range, Kind: 1})
		}
	}
	return out
}
