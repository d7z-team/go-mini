package language

import (
	"sort"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	check "github.com/d7z-team/mini-go/compiler/semantic"
)

func (e *Engine) DocumentSymbols(uri DocumentURI) []DocumentSymbol {
	document, ok := e.snapshot.documents[uri]
	if !ok {
		return nil
	}
	var out []DocumentSymbol
	for _, occurrence := range e.snapshot.occurrences[uri] {
		if occurrence.Role != ast.NameDefinition || occurrence.Name.Text == "_" {
			continue
		}
		selected, err := document.Index.Range(occurrence.Name.Span.Start.Offset, occurrence.Name.Span.End.Offset)
		if err != nil {
			continue
		}
		out = append(out, DocumentSymbol{Name: occurrence.Name.Text, Detail: occurrence.TypeText, Kind: e.symbolKind(occurrence), Range: selected, SelectionRange: selected})
	}
	return out
}

func (e *Engine) WorkspaceSymbols(query string) []SymbolInformation {
	query = strings.ToLower(strings.TrimSpace(query))
	kinds := make(map[string]int, len(e.snapshot.definitions))
	containers := make(map[string]string, len(e.snapshot.definitions))
	for _, occurrences := range e.snapshot.occurrences {
		for _, occurrence := range occurrences {
			if occurrence.Role == ast.NameDefinition && occurrence.Key != "" {
				kinds[occurrence.Key] = e.symbolKind(occurrence)
				containers[occurrence.Key] = occurrence.ModulePath
			}
		}
	}
	var out []SymbolInformation
	for key, location := range e.snapshot.definitions {
		name := key[strings.LastIndex(key, "|")+1:]
		if query == "" || strings.Contains(strings.ToLower(name), query) {
			kind := kinds[key]
			if kind == 0 {
				kind = 13
			}
			out = append(out, SymbolInformation{Name: name, Kind: kind, Location: location, ContainerName: containers[key]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (e *Engine) symbolKind(occurrence indexedOccurrence) int {
	if strings.HasPrefix(occurrence.Key, "method|") {
		return 6
	}
	if strings.HasPrefix(occurrence.Key, "field|") {
		return 8
	}
	pkg, ok := e.snapshot.packages[occurrence.ModulePath]
	if !ok {
		return 13
	}
	object, ok := pkg.Checked.Info.Objects[occurrence.Object]
	if !ok {
		return 13
	}
	switch object.Kind {
	case check.ObjectFunc:
		return 12
	case check.ObjectField:
		return 8
	case check.ObjectConst:
		return 14
	case check.ObjectTypeParam:
		return 26
	case check.ObjectType:
		view := pkg.Checked.Info.Relations.View(object.Type)
		if _, _, _, isInterface := view.Interface(); isInterface {
			return 11
		}
		if _, isStruct := view.StructFields(); isStruct {
			return 23
		}
		return 5
	default:
		return 13
	}
}
