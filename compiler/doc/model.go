// Package doc extracts documentation from compiler source facts.
package doc

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/analysis"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/target"
)

func normalizeReceiver(receiver string) string {
	receiver = strings.TrimSpace(strings.TrimPrefix(receiver, "*"))
	if bracket := strings.IndexByte(receiver, '['); bracket >= 0 {
		receiver = receiver[:bracket]
	}
	if dot := strings.LastIndexByte(receiver, '.'); dot >= 0 {
		receiver = receiver[dot+1:]
	}
	return receiver
}

type Catalog struct {
	Target      target.Target
	Packages    []Package
	Diagnostics []source.Diagnostic
}

type Package struct {
	ModulePath string
	Name       string
	Generate   bool
	Imports    map[string]string
	Doc        Comment
	Symbols    []Symbol
	Notes      []Note
}

type Symbol struct {
	Key              analysis.SymbolKey
	Kind             string
	Name             string
	Receiver         string
	Exported         bool
	DisplaySignature string
	CanonicalType    string
	Exact            string
	Doc              Comment
	Deprecated       string
	Span             source.Span
	TypeParameters   []TypeParameter
	Members          []Member
	Methods          []Symbol
}

type TypeParameter struct {
	Name       string
	Constraint string
}

type Member struct {
	Kind      string
	Name      string
	Signature string
	Exported  bool
	Embedded  bool
	RawTag    string
	Tag       string
	Doc       Comment
	Span      source.Span
}

type Comment struct {
	Key  string
	Text string
	Span source.Span
}

type Note struct {
	Marker string
	UID    string
	Body   string
	Span   source.Span
}

type BuildRequest struct {
	Workspace analysis.WorkspaceResult
	Packages  []string
}

// LookupComment returns source documentation for a package symbol or member.
func (catalog Catalog) LookupComment(modulePath, receiver, name string) (Comment, bool) {
	receiver = normalizeReceiver(receiver)
	for _, pkg := range catalog.Packages {
		if pkg.ModulePath != modulePath {
			continue
		}
		for _, symbol := range pkg.Symbols {
			if receiver == "" && symbol.Name == name {
				return symbol.Doc, true
			}
			if symbol.Name != receiver {
				continue
			}
			for _, method := range symbol.Methods {
				if method.Name == name {
					return method.Doc, true
				}
			}
			for _, member := range symbol.Members {
				if member.Name == name {
					return member.Doc, true
				}
			}
		}
		return Comment{}, false
	}
	return Comment{}, false
}
